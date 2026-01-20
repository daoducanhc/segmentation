package data

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// AggregationRepo handles pre-aggregation refresh jobs
type AggregationRepo struct {
	data *Data
	log  *log.Helper
}

// NewAggregationRepo creates a new AggregationRepo
func NewAggregationRepo(data *Data, logger log.Logger) *AggregationRepo {
	return &AggregationRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// RefreshUserActivitySummary computes rolling activity windows for all users
// This should be run periodically (e.g., daily or hourly)
func (r *AggregationRepo) RefreshUserActivitySummary(ctx context.Context) error {
	r.log.Info("Starting user_activity_summary refresh")
	start := time.Now()

	// Insert new summary rows - ReplacingMergeTree will handle deduplication
	query := `
		INSERT INTO segmentation.user_activity_summary
		(user_id, days_active_7d, days_active_30d, days_active_90d,
		 last_activity_date, days_since_last_activity,
		 revenue_7d, revenue_30d, is_active_7d, is_active_30d, is_churned, computed_at)
		SELECT
			user_id,
			-- Days active in windows
			toUInt8(countIf(activity_date >= today() - 7)) AS days_active_7d,
			toUInt8(countIf(activity_date >= today() - 30)) AS days_active_30d,
			toUInt8(countIf(activity_date >= today() - 90)) AS days_active_90d,
			-- Last activity
			max(activity_date) AS last_activity_date,
			toUInt16(dateDiff('day', max(activity_date), today())) AS days_since_last_activity,
			-- Revenue windows
			sumIf(revenue, activity_date >= today() - 7) AS revenue_7d,
			sumIf(revenue, activity_date >= today() - 30) AS revenue_30d,
			-- Flags
			toUInt8(countIf(activity_date >= today() - 7) > 0) AS is_active_7d,
			toUInt8(countIf(activity_date >= today() - 30) > 0) AS is_active_30d,
			toUInt8(dateDiff('day', max(activity_date), today()) > 30) AS is_churned,
			now64(3) AS computed_at
		FROM segmentation.user_daily_activity
		GROUP BY user_id
	`

	if err := r.data.ExecuteExec(ctx, query); err != nil {
		return fmt.Errorf("failed to refresh user_activity_summary: %w", err)
	}

	r.log.Infof("Completed user_activity_summary refresh in %v", time.Since(start))
	return nil
}

// RefreshUsersTable syncs aggregated data to the users table
// Updates lifetime metrics from daily activity
func (r *AggregationRepo) RefreshUsersTable(ctx context.Context) error {
	r.log.Info("Starting users table refresh")
	start := time.Now()

	query := `
		INSERT INTO segmentation.users
		(user_id, platform, country, language, device_type, app_version,
		 first_seen_at, last_seen_at, is_paying_user, total_revenue, total_purchases,
		 lifetime_events, created_at, updated_at, _version)
		SELECT
			user_id,
			argMax(platform, activity_date) AS platform,
			argMax(country, activity_date) AS country,
			'' AS language,
			argMax(device_type, activity_date) AS device_type,
			'' AS app_version,
			min(activity_date) AS first_seen_at,
			max(activity_date) AS last_seen_at,
			toUInt8(sum(revenue) > 0) AS is_paying_user,
			sum(revenue) AS total_revenue,
			sum(purchase_count) AS total_purchases,
			sum(event_count) AS lifetime_events,
			now64(3) AS created_at,
			now64(3) AS updated_at,
			toUnixTimestamp64Milli(now64(3)) AS _version
		FROM segmentation.user_daily_activity
		GROUP BY user_id
	`

	if err := r.data.ExecuteExec(ctx, query); err != nil {
		return fmt.Errorf("failed to refresh users table: %w", err)
	}

	r.log.Infof("Completed users table refresh in %v", time.Since(start))
	return nil
}

// OptimizeTables runs OPTIMIZE on pre-aggregate tables to merge parts
func (r *AggregationRepo) OptimizeTables(ctx context.Context) error {
	tables := []string{
		"segmentation.user_daily_activity",
		"segmentation.user_activity_summary",
		"segmentation.users",
	}

	for _, table := range tables {
		r.log.Infof("Optimizing table %s", table)
		query := fmt.Sprintf("OPTIMIZE TABLE %s FINAL", table)
		if err := r.data.ExecuteExec(ctx, query); err != nil {
			r.log.Warnf("Failed to optimize %s: %v", table, err)
			// Continue with other tables
		}
	}

	return nil
}

// GetAggregationStats returns stats about the pre-aggregate tables
func (r *AggregationRepo) GetAggregationStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Events count
	var eventsCount uint64
	if err := r.data.QueryRow(ctx, "SELECT count() FROM segmentation.events").Scan(&eventsCount); err != nil {
		return nil, fmt.Errorf("failed to count events: %w", err)
	}
	stats["events_count"] = eventsCount

	// Daily activity rows
	var dailyRows uint64
	if err := r.data.QueryRow(ctx, "SELECT count() FROM segmentation.user_daily_activity").Scan(&dailyRows); err != nil {
		return nil, fmt.Errorf("failed to count daily activity: %w", err)
	}
	stats["daily_activity_rows"] = dailyRows

	// Unique users in daily activity
	var uniqueUsers uint64
	if err := r.data.QueryRow(ctx, "SELECT count(DISTINCT user_id) FROM segmentation.user_daily_activity").Scan(&uniqueUsers); err != nil {
		return nil, fmt.Errorf("failed to count unique users: %w", err)
	}
	stats["unique_users"] = uniqueUsers

	// User activity summary rows
	var summaryRows uint64
	if err := r.data.QueryRow(ctx, "SELECT count() FROM segmentation.user_activity_summary FINAL").Scan(&summaryRows); err != nil {
		return nil, fmt.Errorf("failed to count summary: %w", err)
	}
	stats["activity_summary_rows"] = summaryRows

	// Date range
	var minDate, maxDate string
	if err := r.data.QueryRow(ctx, `
		SELECT 
			toString(min(activity_date)),
			toString(max(activity_date))
		FROM segmentation.user_daily_activity
	`).Scan(&minDate, &maxDate); err != nil {
		r.log.Warnf("Failed to get date range: %v", err)
	} else {
		stats["activity_date_range"] = fmt.Sprintf("%s to %s", minDate, maxDate)
	}

	return stats, nil
}

// RunAllRefreshJobs runs all pre-aggregation refresh jobs
func (r *AggregationRepo) RunAllRefreshJobs(ctx context.Context) error {
	r.log.Info("Running all aggregation refresh jobs")

	// 1. Refresh user activity summary (rolling windows)
	if err := r.RefreshUserActivitySummary(ctx); err != nil {
		return fmt.Errorf("RefreshUserActivitySummary: %w", err)
	}

	// 2. Refresh users table
	if err := r.RefreshUsersTable(ctx); err != nil {
		return fmt.Errorf("RefreshUsersTable: %w", err)
	}

	// 3. Optimize tables (optional, can be slow for large tables)
	// Uncomment if needed: r.OptimizeTables(ctx)

	r.log.Info("All aggregation refresh jobs completed")
	return nil
}
