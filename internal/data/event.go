package data

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
)

// Event represents an event in the database
type Event struct {
	EventID    string
	UserID     string
	EventName  string
	EventTime  time.Time
	SessionID  string
	Platform   string
	AppVersion string
	Properties map[string]interface{}
	Revenue    float64
	Currency   string
	ReceivedAt time.Time
}

// EventRepo handles event data operations
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
	if event.EventID == "" {
		event.EventID = uuid.New().String()
	}
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
		(event_id, user_id, event_name, event_time, session_id, platform, 
		 app_version, properties, revenue, currency, received_at, event_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	return r.data.ExecuteExec(ctx, query,
		event.EventID,
		event.UserID,
		event.EventName,
		event.EventTime,
		event.SessionID,
		event.Platform,
		event.AppVersion,
		propsJSON,
		event.Revenue,
		event.Currency,
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
		(event_id, user_id, event_name, event_time, session_id, platform, 
		 app_version, properties, revenue, currency, received_at, event_date)
	`

	batch, err := r.data.Batch(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}

	for _, event := range events {
		if event.EventID == "" {
			event.EventID = uuid.New().String()
		}
		if event.ReceivedAt.IsZero() {
			event.ReceivedAt = time.Now()
		}

		propsJSON := "{}"
		if event.Properties != nil {
			b, _ := json.Marshal(event.Properties)
			propsJSON = string(b)
		}

		err := batch.Append(
			event.EventID,
			event.UserID,
			event.EventName,
			event.EventTime,
			event.SessionID,
			event.Platform,
			event.AppVersion,
			propsJSON,
			event.Revenue,
			event.Currency,
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
