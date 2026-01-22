// Package data provides the data access layer for ClickHouse.
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// Event represents a user event in the database.
type Event struct {
	UserID    string
	AppID     string
	EventName string
	EventTime time.Time

	// Profile/Demographic
	Platform string
	Country  string
	Language string
	OS       string

	// Monetization
	Revenue        float64
	Currency       string
	PaymentChannel string
	VIPLevel       uint8

	// Flexible properties (JSON)
	Properties map[string]interface{}

	// Metadata
	ReceivedAt time.Time
}

// EventRepo handles event data operations.
type EventRepo struct {
	data *Data
	log  *log.Helper
}

// NewEventRepo creates a new EventRepo
func NewEventRepo(data *Data, logger log.Logger) *EventRepo {
	return &EventRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// Insert inserts a single event
func (r *EventRepo) Insert(ctx context.Context, event *Event) error {
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now()
	}

	propsJSON := "{}"
	if event.Properties != nil {
		b, err := json.Marshal(event.Properties)
		if err != nil {
			return fmt.Errorf("failed to marshal properties: %w", err)
		}
		propsJSON = string(b)
	}

	query := `
		INSERT INTO segmentation.events 
		(user_id, app_id, event_name, event_time, 
		 platform, country, language, os,
		 revenue, currency, payment_channel, vip_level,
		 properties, received_at, event_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	return r.data.ExecuteExec(ctx, query,
		event.UserID,
		event.AppID,
		event.EventName,
		event.EventTime,
		event.Platform,
		event.Country,
		event.Language,
		event.OS,
		event.Revenue,
		event.Currency,
		event.PaymentChannel,
		event.VIPLevel,
		propsJSON,
		event.ReceivedAt,
		event.EventTime,
	)
}

// InsertBatch inserts multiple events
func (r *EventRepo) InsertBatch(ctx context.Context, events []*Event) error {
	if len(events) == 0 {
		return nil
	}

	query := `
		INSERT INTO segmentation.events 
		(user_id, app_id, event_name, event_time, 
		 platform, country, language, os,
		 revenue, currency, payment_channel, vip_level,
		 properties, received_at, event_date)
	`

	batch, err := r.data.Batch(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}

	for _, event := range events {
		if event.ReceivedAt.IsZero() {
			event.ReceivedAt = time.Now()
		}

		propsJSON := "{}"
		if event.Properties != nil {
			b, _ := json.Marshal(event.Properties)
			propsJSON = string(b)
		}

		err := batch.Append(
			event.UserID,
			event.AppID,
			event.EventName,
			event.EventTime,
			event.Platform,
			event.Country,
			event.Language,
			event.OS,
			event.Revenue,
			event.Currency,
			event.PaymentChannel,
			event.VIPLevel,
			propsJSON,
			event.ReceivedAt,
			event.EventTime,
		)
		if err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}

	return batch.Send()
}

// GetEventCount returns the total number of events
func (r *EventRepo) GetEventCount(ctx context.Context) (int64, error) {
	query := `SELECT count() FROM segmentation.events`
	var count uint64
	if err := r.data.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}
	return int64(count), nil
}

// GetEventCountByDay returns event counts per day
func (r *EventRepo) GetEventCountByDay(ctx context.Context, days int) (map[string]int64, error) {
	query := `
		SELECT event_date, count() as cnt
		FROM segmentation.events
		WHERE event_date >= today() - ?
		GROUP BY event_date
		ORDER BY event_date DESC
	`

	rows, err := r.data.ExecuteQuery(ctx, query, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get event counts: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var date time.Time
		var count uint64
		if err := rows.Scan(&date, &count); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		result[date.Format("2006-01-02")] = int64(count)
	}

	return result, nil
}

// GetUserEventCount returns event count for a specific user
func (r *EventRepo) GetUserEventCount(ctx context.Context, userID string, eventName string, days int) (int64, error) {
	query := `
		SELECT count()
		FROM segmentation.events
		WHERE user_id = ?
		AND event_date >= today() - ?
	`
	args := []interface{}{userID, days}

	if eventName != "" {
		query += ` AND event_name = ?`
		args = append(args, eventName)
	}

	var count uint64
	if err := r.data.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count user events: %w", err)
	}
	return int64(count), nil
}

// GetDistinctEventNames returns all distinct event names
func (r *EventRepo) GetDistinctEventNames(ctx context.Context) ([]string, error) {
	query := `SELECT DISTINCT event_name FROM segmentation.events ORDER BY event_name`

	rows, err := r.data.ExecuteQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get event names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan event name: %w", err)
		}
		names = append(names, name)
	}

	return names, nil
}

// VIPLevelRange represents min/max VIP level for an app
type VIPLevelRange struct {
	AppID    string
	MinLevel uint8
	MaxLevel uint8
}

// GetVIPLevelRangeByAppID returns the min and max VIP levels for each app_id
// This is used for UI dropdowns since each game may have different VIP level ranges
func (r *EventRepo) GetVIPLevelRangeByAppID(ctx context.Context) ([]VIPLevelRange, error) {
	query := `
		SELECT 
			app_id,
			min(vip_level) AS min_level,
			max(vip_level) AS max_level
		FROM segmentation.events
		WHERE event_name = 'app_vip_level_up' 
		AND vip_level > 0
		AND app_id != ''
		GROUP BY app_id
		ORDER BY app_id
	`

	rows, err := r.data.ExecuteQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get VIP level ranges: %w", err)
	}
	defer rows.Close()

	var ranges []VIPLevelRange
	for rows.Next() {
		var r VIPLevelRange
		if err := rows.Scan(&r.AppID, &r.MinLevel, &r.MaxLevel); err != nil {
			return nil, fmt.Errorf("failed to scan VIP level range: %w", err)
		}
		ranges = append(ranges, r)
	}

	return ranges, nil
}

// GetVIPLevelRangeForApp returns the min and max VIP levels for a specific app_id
func (r *EventRepo) GetVIPLevelRangeForApp(ctx context.Context, appID string) (*VIPLevelRange, error) {
	query := `
		SELECT 
			app_id,
			min(vip_level) AS min_level,
			max(vip_level) AS max_level
		FROM segmentation.events
		WHERE event_name = 'app_vip_level_up' 
		AND vip_level > 0
		AND app_id = ?
		GROUP BY app_id
	`

	row := r.data.QueryRow(ctx, query, appID)

	var result VIPLevelRange
	if err := row.Scan(&result.AppID, &result.MinLevel, &result.MaxLevel); err != nil {
		return nil, fmt.Errorf("failed to get VIP level range for app %s: %w", appID, err)
	}

	return &result, nil
}

// GetUsersByEvent returns user IDs who triggered a specific event
func (r *EventRepo) GetUsersByEvent(ctx context.Context, eventName string, days int, minCount int64, limit, offset int32) ([]string, int64, error) {
	countQuery := `
		SELECT count() FROM (
			SELECT user_id
			FROM segmentation.events
			WHERE event_name = ?
			AND event_date >= today() - ?
			GROUP BY user_id
			HAVING count() >= ?
		)
	`
	var total uint64
	if err := r.data.QueryRow(ctx, countQuery, eventName, days, minCount).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	query := `
		SELECT user_id
		FROM segmentation.events
		WHERE event_name = ?
		AND event_date >= today() - ?
		GROUP BY user_id
		HAVING count() >= ?
		LIMIT ? OFFSET ?
	`

	rows, err := r.data.ExecuteQuery(ctx, query, eventName, days, minCount, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get users by event: %w", err)
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, 0, fmt.Errorf("failed to scan user_id: %w", err)
		}
		userIDs = append(userIDs, userID)
	}

	return userIDs, int64(total), nil
}
