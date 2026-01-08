package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"

	"segmentation/internal/conf"
	"segmentation/internal/consumer"
	"segmentation/internal/data"
	"segmentation/internal/engine"
	"segmentation/internal/server"
	"segmentation/internal/service"
)

var flagconf string

func init() {
	flag.StringVar(&flagconf, "conf", "configs/config.yaml", "config path, eg: -conf config.yaml")
}

func main() {
	flag.Parse()

	// Logger
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

	// Initialize data layer
	dataInstance, cleanup, err := data.NewData(bc.Data, logger)
	if err != nil {
		log.NewHelper(logger).Fatalf("failed to create data: %v", err)
	}
	defer cleanup()

	// Initialize repositories
	segmentRepo := data.NewSegmentRepo(dataInstance, logger)
	userRepo := data.NewUserRepo(dataInstance, logger)
	eventRepo := data.NewEventRepo(dataInstance, logger)

	// Initialize engine
	sqlGenerator := engine.NewSQLGenerator()
	evaluator := engine.NewEvaluator(sqlGenerator, segmentRepo, logger)
	criteriaLib := engine.NewCriteriaLibrary()

	// Initialize service
	segmentService := service.NewSegmentService(segmentRepo, evaluator, sqlGenerator, criteriaLib, logger)

	// Initialize servers
	grpcServer := server.NewGRPCServer(bc.Server, segmentService, logger)
	httpServer := server.NewHTTPServer(bc.Server, segmentService, logger)

	// Initialize Kafka consumer
	var kafkaConsumer *consumer.KafkaConsumer
	if bc.Kafka != nil && len(bc.Kafka.Address) > 0 {
		kafkaConsumer, err = consumer.NewKafkaConsumer(bc.Kafka, eventRepo, userRepo, logger)
		if err != nil {
			log.NewHelper(logger).Warnf("failed to create Kafka consumer: %v", err)
		}
	}

	// Initialize MySQL sync
	var mysqlSync *consumer.MySQLSync
	if bc.MySQL != nil && bc.MySQL.DSN != "" {
		mysqlSync, err = consumer.NewMySQLSync(bc.MySQL, userRepo, logger)
		if err != nil {
			log.NewHelper(logger).Warnf("failed to create MySQL sync: %v", err)
		}
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

	if mysqlSync != nil {
		if err := mysqlSync.Start(ctx); err != nil {
			log.NewHelper(logger).Errorf("failed to start MySQL sync: %v", err)
		}
		defer mysqlSync.Stop()
	}

	// Handle shutdown signals
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
		app.Stop()
	}()

	// Run app
	if err := app.Run(); err != nil {
		log.NewHelper(logger).Fatalf("failed to run app: %v", err)
	}
}
