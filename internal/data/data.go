package data

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/go-kratos/kratos/v2/log"

	"segmentation/internal/conf"
)

// Data manages database connections
type Data struct {
	clickhouse driver.Conn
	log        *log.Helper
}

// NewData creates a new Data instance
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	l := log.NewHelper(logger)

	// Connect to ClickHouse
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{c.Clickhouse.Addr},
		Auth: clickhouse.Auth{
			Database: c.Clickhouse.Database,
			Username: c.Clickhouse.Username,
			Password: c.Clickhouse.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:     time.Duration(c.Clickhouse.DialTimeout) * time.Second,
		MaxOpenConns:    int(c.Clickhouse.MaxOpenConns),
		MaxIdleConns:    int(c.Clickhouse.MaxIdleConns),
		ConnMaxLifetime: time.Duration(c.Clickhouse.ConnMaxLifetime) * time.Minute,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	// Verify connection
	if err := conn.Ping(context.Background()); err != nil {
		return nil, nil, fmt.Errorf("failed to ping clickhouse: %w", err)
	}

	l.Info("Connected to ClickHouse")

	cleanup := func() {
		l.Info("Closing ClickHouse connection")
		conn.Close()
	}

	return &Data{
		clickhouse: conn,
		log:        l,
	}, cleanup, nil
}

// ClickHouse returns the ClickHouse connection
func (d *Data) ClickHouse() driver.Conn {
	return d.clickhouse
}

// ExecuteQuery executes a query and returns rows
func (d *Data) ExecuteQuery(ctx context.Context, query string, args ...interface{}) (driver.Rows, error) {
	return d.clickhouse.Query(ctx, query, args...)
}

// ExecuteExec executes a query without returning rows
func (d *Data) ExecuteExec(ctx context.Context, query string, args ...interface{}) error {
	return d.clickhouse.Exec(ctx, query, args...)
}

// Batch creates a batch for bulk inserts
func (d *Data) Batch(ctx context.Context, query string) (driver.Batch, error) {
	return d.clickhouse.PrepareBatch(ctx, query)
}

// QueryRow executes a query and returns a single row
func (d *Data) QueryRow(ctx context.Context, query string, args ...interface{}) driver.Row {
	return d.clickhouse.QueryRow(ctx, query, args...)
}

// NullTime represents a nullable time
type NullTime struct {
	Time  time.Time
	Valid bool
}

// Scan implements the sql.Scanner interface
func (nt *NullTime) Scan(value interface{}) error {
	if value == nil {
		nt.Time, nt.Valid = time.Time{}, false
		return nil
	}
	nt.Valid = true
	switch v := value.(type) {
	case time.Time:
		nt.Time = v
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}
	return nil
}

// ToPointer converts NullTime to *time.Time
func (nt NullTime) ToPointer() *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}

// NullString represents a nullable string
type NullString struct {
	sql.NullString
}

// ToPointer converts NullString to *string
func (ns NullString) ToPointer() *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}
