package consumer

import (
	"context"
	"database/sql"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	_ "github.com/go-sql-driver/mysql"

	"segmentation/internal/conf"
	"segmentation/internal/data"
)

// MySQLSync handles syncing user data from MySQL
type MySQLSync struct {
	db       *sql.DB
	userRepo *data.UserRepo
	log      *log.Helper
	interval time.Duration
	running  bool
	cancel   context.CancelFunc
}

// NewMySQLSync creates a new MySQL sync service
func NewMySQLSync(cfg *conf.MySQL, userRepo *data.UserRepo, logger log.Logger) (*MySQLSync, error) {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &MySQLSync{
		db:       db,
		userRepo: userRepo,
		log:      log.NewHelper(logger),
		interval: 5 * time.Minute,
	}, nil
}

// Start starts the sync process
func (s *MySQLSync) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true

	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// Initial sync
		s.sync(ctx)

		for {
			select {
			case <-ticker.C:
				s.sync(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	s.log.Info("MySQL sync started")
	return nil
}

// Stop stops the sync process
func (s *MySQLSync) Stop() error {
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
	return s.db.Close()
}

// sync performs the actual sync
func (s *MySQLSync) sync(ctx context.Context) {
	s.log.Info("Starting MySQL user sync...")

	// Query users updated since last sync
	// This is a sample query - adjust based on your actual schema
	query := `
		SELECT 
			user_id,
			platform,
			country,
			language,
			device_type,
			app_version,
			first_seen_at,
			last_seen_at,
			registered_at,
			is_registered,
			is_paying_user,
			total_revenue,
			total_purchases,
			updated_at
		FROM users
		WHERE updated_at > DATE_SUB(NOW(), INTERVAL 10 MINUTE)
		LIMIT 10000
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		s.log.Errorf("Failed to query users: %v", err)
		return
	}
	defer rows.Close()

	var users []*data.User
	for rows.Next() {
		var u data.User
		var isRegistered, isPayingUser int
		var registeredAt sql.NullTime

		err := rows.Scan(
			&u.UserID,
			&u.Platform,
			&u.Country,
			&u.Language,
			&u.DeviceType,
			&u.AppVersion,
			&u.FirstSeenAt,
			&u.LastSeenAt,
			&registeredAt,
			&isRegistered,
			&isPayingUser,
			&u.TotalRevenue,
			&u.TotalPurchases,
			&u.UpdatedAt,
		)
		if err != nil {
			s.log.Warnf("Failed to scan user row: %v", err)
			continue
		}

		u.IsRegistered = isRegistered == 1
		u.IsPayingUser = isPayingUser == 1
		if registeredAt.Valid {
			u.RegisteredAt = &registeredAt.Time
		}

		users = append(users, &u)
	}

	if len(users) == 0 {
		s.log.Debug("No users to sync")
		return
	}

	// Batch upsert to ClickHouse
	if err := s.userRepo.UpsertBatch(ctx, users); err != nil {
		s.log.Errorf("Failed to upsert users: %v", err)
		return
	}

	s.log.Infof("Synced %d users from MySQL", len(users))
}

// SyncFullTable performs a full table sync
func (s *MySQLSync) SyncFullTable(ctx context.Context) error {
	s.log.Info("Starting full MySQL user sync...")

	query := `
		SELECT 
			user_id,
			platform,
			country,
			language,
			device_type,
			app_version,
			first_seen_at,
			last_seen_at,
			registered_at,
			is_registered,
			is_paying_user,
			total_revenue,
			total_purchases,
			updated_at
		FROM users
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var batch []*data.User
	batchSize := 5000
	totalSynced := 0

	for rows.Next() {
		var u data.User
		var isRegistered, isPayingUser int
		var registeredAt sql.NullTime

		err := rows.Scan(
			&u.UserID,
			&u.Platform,
			&u.Country,
			&u.Language,
			&u.DeviceType,
			&u.AppVersion,
			&u.FirstSeenAt,
			&u.LastSeenAt,
			&registeredAt,
			&isRegistered,
			&isPayingUser,
			&u.TotalRevenue,
			&u.TotalPurchases,
			&u.UpdatedAt,
		)
		if err != nil {
			s.log.Warnf("Failed to scan user row: %v", err)
			continue
		}

		u.IsRegistered = isRegistered == 1
		u.IsPayingUser = isPayingUser == 1
		if registeredAt.Valid {
			u.RegisteredAt = &registeredAt.Time
		}

		batch = append(batch, &u)

		if len(batch) >= batchSize {
			if err := s.userRepo.UpsertBatch(ctx, batch); err != nil {
				return err
			}
			totalSynced += len(batch)
			batch = batch[:0]
			s.log.Infof("Synced %d users...", totalSynced)
		}
	}

	// Flush remaining
	if len(batch) > 0 {
		if err := s.userRepo.UpsertBatch(ctx, batch); err != nil {
			return err
		}
		totalSynced += len(batch)
	}

	s.log.Infof("Full sync completed: %d users", totalSynced)
	return nil
}
