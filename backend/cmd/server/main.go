package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"

	"segmentation/internal/conf"
	"segmentation/internal/consumer"
	"segmentation/internal/data"
	"segmentation/internal/engine"
	"segmentation/internal/scheduler"
	"segmentation/internal/server"
	"segmentation/internal/service"
	"segmentation/pkg/configx"
)

var flagconf string
var staticDir string

func init() {
	flag.StringVar(&flagconf, "conf", "configs/config.yaml", "config path, eg: -conf config.yaml")
	flag.StringVar(&staticDir, "s", "", "static files directory, eg: -s ./dist")
}

func main() {
	flag.Parse()

	logger := log.With(log.NewStdLogger(os.Stdout),
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service.name", "segmentation",
	)

	// Load config
	c := config.New(
		config.WithSource(
			file.NewSource(flagconf),
		),
	)
	defer c.Close()

	if err := c.Load(); err != nil {
		log.NewHelper(logger).Fatalf("failed to load config: %v", err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		log.NewHelper(logger).Fatalf("failed to scan config: %v", err)
	}

	// ClickHouse
	if addr := configx.GetEnvOrString("CLICKHOUSE_ADDR", ""); addr != "" {
		bc.Data.Clickhouse.Addr = addr
	}
	if db := configx.GetEnvOrString("CLICKHOUSE_DATABASE", ""); db != "" {
		bc.Data.Clickhouse.Database = db
	}
	if user := configx.GetEnvOrString("CLICKHOUSE_USERNAME", ""); user != "" {
		bc.Data.Clickhouse.Username = user
	}
	if pass := configx.GetEnvOrString("CLICKHOUSE_PASSWORD", ""); pass != "" {
		bc.Data.Clickhouse.Password = pass
	}

	// Kafka
	if bc.Kafka != nil {
		if address := configx.GetEnvOrString("KAFKA_ADDRESS", ""); address != "" {
			bc.Kafka.Address = configx.GetEnvOrStrings("KAFKA_ADDRESS", bc.Kafka.Address)
		}
		if topic := configx.GetEnvOrString("KAFKA_TOPIC", ""); topic != "" {
			bc.Kafka.Topic = topic
		}
		if groupID := configx.GetEnvOrString("KAFKA_GROUP_ID", ""); groupID != "" {
			bc.Kafka.GroupId = groupID
		}
	}

	// Load ThinkingData URLs from environment variables
	if bc.ThinkingData != nil {
		if bc.ThinkingData.VN == nil {
			bc.ThinkingData.VN = &conf.TDEndpoint{}
		}
		if bc.ThinkingData.Global == nil {
			bc.ThinkingData.Global = &conf.TDEndpoint{}
		}
		bc.ThinkingData.VN.QueryURL = configx.GetEnvOrString("THINKINGDATA_VN_QUERY_URL", "")
		bc.ThinkingData.VN.QueryToken = configx.GetEnvOrString("THINKINGDATA_VN_QUERY_TOKEN", "")
		bc.ThinkingData.Global.QueryURL = configx.GetEnvOrString("THINKINGDATA_GLOBAL_QUERY_URL", "")
		bc.ThinkingData.Global.QueryToken = configx.GetEnvOrString("THINKINGDATA_GLOBAL_QUERY_TOKEN", "")

		// Override sync_on_startup from environment variable if set
		if syncOnStartup := configx.GetEnvOrString("SYNC_ON_STARTUP", ""); syncOnStartup != "" {
			bc.ThinkingData.SyncOnStartup = syncOnStartup == "true"
		}

		log.NewHelper(logger).Infof("TD VN: url=%s, token_len=%d", bc.ThinkingData.VN.QueryURL, len(bc.ThinkingData.VN.QueryToken))
	}

	log.NewHelper(logger).Infof("ClickHouse: %s/%s", bc.Data.Clickhouse.Addr, bc.Data.Clickhouse.Database)
	log.NewHelper(logger).Infof("Kafka: %v", bc.Kafka.Address)

	// Initialize data layer
	dataInstance, cleanup, err := data.NewData(bc.Data, logger)
	if err != nil {
		log.NewHelper(logger).Fatalf("failed to create data: %v", err)
	}
	defer cleanup()

	// Run auto migration if enabled
	if bc.Data.Clickhouse.AutoMigrate {
		migrator := data.NewMigrator(dataInstance, logger)
		migrationsDir := bc.Data.Clickhouse.MigrationsDir
		if migrationsDir == "" {
			migrationsDir = "migrations" // default path
		}
		if err := migrator.AutoMigrate(context.Background(), migrationsDir); err != nil {
			log.NewHelper(logger).Fatalf("failed to run auto migration: %v", err)
		}
	}

	// Initialize repositories
	segmentRepo := data.NewSegmentRepo(dataInstance, logger)
	userRepo := data.NewUserRepo(dataInstance, logger)
	eventRepo := data.NewEventRepo(dataInstance, logger)
	aggregationRepo := data.NewAggregationRepo(dataInstance, logger)

	// Initialize engine
	sqlGenerator := engine.NewSQLGenerator()
	evaluator := engine.NewEvaluator(sqlGenerator, segmentRepo, logger)
	criteriaLib := engine.NewCriteriaLibrary()

	// Initialize service
	segmentService := service.NewSegmentService(segmentRepo, evaluator, sqlGenerator, criteriaLib, logger)

	// Initialize servers
	grpcServer := server.NewGRPCServer(bc.Server, segmentService, logger)
	httpServer := server.NewHTTPServer(bc.Server, segmentService, logger, staticDir)

	// Initialize Kafka consumer
	var kafkaConsumer *consumer.KafkaConsumer
	if bc.Kafka != nil && len(bc.Kafka.Address) > 0 {
		kafkaConsumer, err = consumer.NewKafkaConsumer(bc.Kafka, eventRepo, userRepo, logger)
		if err != nil {
			log.NewHelper(logger).Warnf("failed to create Kafka consumer: %v", err)
		}
	}

	// Initialize ThinkingData sync
	var tdSync *consumer.ThinkingDataSync
	if bc.ThinkingData != nil && len(bc.ThinkingData.Events) > 0 {
		tdSync = consumer.NewThinkingDataSync(bc.ThinkingData, eventRepo, aggregationRepo, logger)
	}

	// Initialize scheduler for periodic aggregation refresh
	var refreshScheduler *scheduler.Scheduler
	if bc.Scheduler != nil && bc.Scheduler.Enabled {
		schedulerConfig := &scheduler.Config{
			Enabled:         true,
			IncrementalDays: int(bc.Scheduler.IncrementalDays),
			FullRefreshHour: int(bc.Scheduler.FullRefreshHour),
		}
		if bc.Scheduler.IncrementalRefreshMinutes > 0 {
			schedulerConfig.IncrementalRefreshInterval = time.Duration(bc.Scheduler.IncrementalRefreshMinutes) * time.Minute
		} else {
			schedulerConfig.IncrementalRefreshInterval = 10 * time.Minute
		}
		if schedulerConfig.IncrementalDays <= 0 {
			schedulerConfig.IncrementalDays = 7
		}
		if schedulerConfig.FullRefreshHour < 0 || schedulerConfig.FullRefreshHour > 23 {
			schedulerConfig.FullRefreshHour = 3
		}
		refreshScheduler = scheduler.NewScheduler(schedulerConfig, aggregationRepo, logger)
	}

	// Create Kratos app
	app := kratos.New(
		kratos.Name("segmentation"),
		kratos.Version("1.0.0"),
		kratos.Logger(logger),
		kratos.Server(
			grpcServer,
			httpServer,
		),
	)

	// Start background services
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if kafkaConsumer != nil {
		if err := kafkaConsumer.Start(ctx); err != nil {
			log.NewHelper(logger).Errorf("failed to start Kafka consumer: %v", err)
		}
		defer kafkaConsumer.Stop()
	}

	if tdSync != nil {
		if err := tdSync.Start(ctx); err != nil {
			log.NewHelper(logger).Errorf("failed to start ThinkingData sync: %v", err)
		}
		defer tdSync.Stop()
	}

	// Start scheduler (handles periodic aggregation refresh)
	if refreshScheduler != nil {
		if err := refreshScheduler.Start(ctx); err != nil {
			log.NewHelper(logger).Errorf("failed to start scheduler: %v", err)
		}
		defer refreshScheduler.Stop()
	}

	// Handle shutdown signals
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
		app.Stop()
	}()

	if err := app.Run(); err != nil {
		log.NewHelper(logger).Fatalf("failed to run app: %v", err)
	}
}
