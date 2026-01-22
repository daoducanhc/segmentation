// Package data provides the data access layer for ClickHouse.
package data

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// AggregationRepo handles scheduled refresh jobs for pre-aggregated tables.
// Uses FINAL modifier for deduplication at read time (no OPTIMIZE TABLE needed).
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

// RefreshDailyActivity rebuilds user_daily_activity from deduplicated events
// Uses FINAL modifier to deduplicate events at read time (much faster than OPTIMIZE)
// Supports incremental refresh for recent days only
func (r *AggregationRepo) RefreshDailyActivity(ctx context.Context) error {
	return r.RefreshDailyActivityDays(ctx, 0) // 0 = full refresh
}

// RefreshDailyActivityDays refreshes daily activity for the last N days.
// Pass 0 for full refresh, or a positive number for incremental (e.g., 7 for last week).
func (r *AggregationRepo) RefreshDailyActivityDays(ctx context.Context, days int) error {
	scope := "full"
	dateFilter := "1=1"
	if days > 0 {
		scope = fmt.Sprintf("last %d days", days)
		dateFilter = fmt.Sprintf("e.event_date >= today() - %d", days)
	}
	r.log.Infof("Starting user_daily_activity refresh (%s)", scope)
	start := time.Now()

	// Use FINAL modifier to deduplicate events at read time
	// This is MUCH faster than OPTIMIZE TABLE FINAL
	query := fmt.Sprintf(`INSERT INTO segmentation.user_daily_activity 
			(user_id, app_id, activity_date, platform, country, language, os, login_count, event_count, revenue, purchase_count, webshop_purchase_count, google_purchase_count, apple_purchase_count, third_party_purchase_count, max_vip_level, event_counts, updated_at) 
		SELECT 
			e.user_id AS user_id, 
			e.app_id AS app_id, 
			toDate(e.event_time, 'Asia/Ho_Chi_Minh') AS activity_date, 
			any(e.platform) AS platform, 
			any(e.country) AS country, 
			any(e.language) AS language, 
			any(e.os) AS os, 
			countIf(e.event_name = 'app_page_view') AS login_count, 
			count(*) AS event_count, 
			sum(e.revenue) AS revenue, 
			countIf(e.revenue > 0) AS purchase_count, 
			countIf(e.revenue > 0 AND e.payment_channel = 'webshop') AS webshop_purchase_count, 
			countIf(e.revenue > 0 AND e.payment_channel = 'google') AS google_purchase_count, 
			countIf(e.revenue > 0 AND e.payment_channel = 'apple') AS apple_purchase_count, 
			countIf(e.revenue > 0 AND e.payment_channel = '3rd_party') AS third_party_purchase_count, 
			max(e.vip_level) AS max_vip_level, 
			sumMap(map(e.event_name, 1)) AS event_counts, 
			now64(3) AS updated_at 
		FROM segmentation.events AS e FINAL
		WHERE %s 
		GROUP BY e.user_id, e.app_id, toDate(e.event_time, 'Asia/Ho_Chi_Minh')`, dateFilter)

	if err := r.data.ExecuteExec(ctx, query); err != nil {
		return fmt.Errorf("failed to refresh user_daily_activity: %w", err)
	}

	// No OPTIMIZE needed - ReplacingMergeTree will merge in background
	// Query-time deduplication via FINAL handles accuracy

	r.log.Infof("Completed user_daily_activity refresh in %v", time.Since(start))
	return nil
}

// RefreshUserActivitySummary computes predefined activity/PU/churn flags
// This should be run periodically (e.g., every 5-15 minutes for near real-time)
// Uses FINAL to read deduplicated data from user_daily_activity
func (r *AggregationRepo) RefreshUserActivitySummary(ctx context.Context) error {
	r.log.Info("Starting user_activity_summary refresh")
	start := time.Now()

	// Insert new summary rows - ReplacingMergeTree will handle deduplication
	// Use FINAL to read deduplicated daily activity data
	query := `
		INSERT INTO segmentation.user_activity_summary
		(user_id, 
		 is_active_7d, is_active_30d, is_active_90d,
		 is_pu_7d, is_pu_30d, is_pu_90d,
		 is_churned_7d, is_churned_30d, is_churned_90d,
		 computed_at)
		SELECT
			user_id,
			-- Activity flags (had any activity in window)
			toUInt8(countIf(activity_date >= today() - 7) > 0) AS is_active_7d,
			toUInt8(countIf(activity_date >= today() - 30) > 0) AS is_active_30d,
			toUInt8(countIf(activity_date >= today() - 90) > 0) AS is_active_90d,
			-- Paying user flags (made purchase in window)
			toUInt8(sumIf(purchase_count, activity_date >= today() - 7) > 0) AS is_pu_7d,
			toUInt8(sumIf(purchase_count, activity_date >= today() - 30) > 0) AS is_pu_30d,
			toUInt8(sumIf(purchase_count, activity_date >= today() - 90) > 0) AS is_pu_90d,
			-- Churn flags (no activity in N+ days)
			toUInt8(dateDiff('day', max(activity_date), today()) >= 7) AS is_churned_7d,
			toUInt8(dateDiff('day', max(activity_date), today()) >= 30) AS is_churned_30d,
			toUInt8(dateDiff('day', max(activity_date), today()) >= 90) AS is_churned_90d,
			now64(3) AS computed_at
		FROM segmentation.user_daily_activity FINAL
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
// Uses FINAL to read deduplicated data (no OPTIMIZE needed)
func (r *AggregationRepo) RefreshUsersTable(ctx context.Context) error {
	r.log.Info("Starting users table refresh")
	start := time.Now()

	// Use FINAL to read deduplicated daily activity
	query := `
		INSERT INTO segmentation.users
		(user_id, platform, country, language, os,
		 first_seen_at, last_seen_at, is_paying_user, total_revenue, total_purchases,
		 lifetime_events, created_at, updated_at, _version)
		SELECT
			user_id,
			arrayStringConcat(arrayFilter(x -> x != '', groupArray(DISTINCT platform)), ',') AS platform,
			arrayStringConcat(arrayFilter(x -> x != '', groupArray(DISTINCT country)), ',') AS country,
			arrayStringConcat(arrayFilter(x -> x != '', groupArray(DISTINCT language)), ',') AS language,
			arrayStringConcat(arrayFilter(x -> x != '', groupArray(DISTINCT os)), ',') AS os,
			min(activity_date) AS first_seen_at,
			max(activity_date) AS last_seen_at,
			toUInt8(sum(revenue) > 0) AS is_paying_user,
			sum(revenue) AS total_revenue,
			sum(purchase_count) AS total_purchases,
			sum(event_count) AS lifetime_events,
			now64(3) AS created_at,
			now64(3) AS updated_at,
			toUnixTimestamp64Milli(now64(3)) AS _version
		FROM segmentation.user_daily_activity FINAL
		GROUP BY user_id
	`

	if err := r.data.ExecuteExec(ctx, query); err != nil {
		return fmt.Errorf("failed to refresh users table: %w", err)
	}

	// No OPTIMIZE needed - ReplacingMergeTree merges in background
	// Query-time deduplication via FINAL handles accuracy

	r.log.Infof("Completed users table refresh in %v", time.Since(start))
	return nil
}

// OptimizeTables runs OPTIMIZE on pre-aggregate tables to merge parts
// NOTE: This is expensive and should only be run during maintenance windows
// For routine operations, use FINAL modifier at query time instead
func (r *AggregationRepo) OptimizeTables(ctx context.Context) error {
	r.log.Warn("Running OPTIMIZE FINAL - this is expensive, use only for maintenance")

	tables := []string{
		"segmentation.events",
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

// OptimizeTablesLight runs OPTIMIZE without FINAL (triggers background merge)
// This is much faster and can be run more frequently
func (r *AggregationRepo) OptimizeTablesLight(ctx context.Context) error {
	tables := []string{
		"segmentation.events",
		"segmentation.user_daily_activity",
		"segmentation.user_activity_summary",
		"segmentation.users",
	}

	for _, table := range tables {
		r.log.Infof("Triggering background merge for %s", table)
		query := fmt.Sprintf("OPTIMIZE TABLE %s", table) // No FINAL = fast
		if err := r.data.ExecuteExec(ctx, query); err != nil {
			r.log.Warnf("Failed to optimize %s: %v", table, err)
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

// RunAllRefreshJobs runs all pre-aggregation refresh jobs (full refresh)
// For routine updates, prefer RunIncrementalRefresh which is much faster
func (r *AggregationRepo) RunAllRefreshJobs(ctx context.Context) error {
	r.log.Info("Running all aggregation refresh jobs (full refresh)")
	start := time.Now()

	// No OPTIMIZE needed - we use FINAL modifier at query time

	// 1. Refresh daily activity from deduplicated events (full refresh)
	if err := r.RefreshDailyActivity(ctx); err != nil {
		return fmt.Errorf("RefreshDailyActivity: %w", err)
	}

	// 2. Refresh user activity summary (rolling windows)
	if err := r.RefreshUserActivitySummary(ctx); err != nil {
		return fmt.Errorf("RefreshUserActivitySummary: %w", err)
	}

	// 3. Refresh users table
	if err := r.RefreshUsersTable(ctx); err != nil {
		return fmt.Errorf("RefreshUsersTable: %w", err)
	}

	r.log.Infof("All aggregation refresh jobs completed in %v", time.Since(start))
	return nil
}

// RunIncrementalRefresh runs fast incremental refresh for recent data
// This is suitable for frequent execution (every 5-15 minutes)
func (r *AggregationRepo) RunIncrementalRefresh(ctx context.Context, days int) error {
	if days <= 0 {
		days = 7 // Default to last 7 days
	}
	r.log.Infof("Running incremental refresh for last %d days", days)
	start := time.Now()

	// 1. Refresh daily activity for recent days only
	if err := r.RefreshDailyActivityDays(ctx, days); err != nil {
		return fmt.Errorf("RefreshDailyActivityDays: %w", err)
	}

	// 2. Refresh user activity summary (always full, but reads from already-refreshed daily)
	if err := r.RefreshUserActivitySummary(ctx); err != nil {
		return fmt.Errorf("RefreshUserActivitySummary: %w", err)
	}

	// 3. Refresh users table
	if err := r.RefreshUsersTable(ctx); err != nil {
		return fmt.Errorf("RefreshUsersTable: %w", err)
	}

	r.log.Infof("Incremental refresh completed in %v", time.Since(start))
	return nil
}
