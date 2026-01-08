package data

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// User represents a user in the database
type User struct {
	UserID           string
	Platform         string
	Country          string
	Language         string
	DeviceType       string
	AppVersion       string
	FirstSeenAt      time.Time
	LastSeenAt       time.Time
	RegisteredAt     *time.Time
	IsRegistered     bool
	IsPayingUser     bool
	TotalRevenue     float64
	TotalPurchases   uint32
	LifetimeSessions uint32
	LifetimeEvents   uint32
	CustomAttributes map[string]interface{}
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// UserRepo handles user data operations
type UserRepo struct {
	data *Data
	log  *log.Helper
}

// NewUserRepo creates a new UserRepo
func NewUserRepo(data *Data, logger log.Logger) *UserRepo {
	return &UserRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// Upsert inserts or updates a user
func (r *UserRepo) Upsert(ctx context.Context, user *User) error {
	customAttrsJSON := "{}"
	if user.CustomAttributes != nil {
		b, err := json.Marshal(user.CustomAttributes)
		if err != nil {
			return fmt.Errorf("failed to marshal custom attributes: %w", err)
		}
		customAttrsJSON = string(b)
	}

	query := `
		INSERT INTO segmentation.users 
		(user_id, platform, country, language, device_type, app_version,
		 first_seen_at, last_seen_at, registered_at, is_registered, is_paying_user,
		 total_revenue, total_purchases, lifetime_sessions, lifetime_events,
		 custom_attributes, created_at, updated_at, _version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var registeredAt time.Time
	if user.RegisteredAt != nil {
		registeredAt = *user.RegisteredAt
	}

	return r.data.ExecuteExec(ctx, query,
		user.UserID,
		user.Platform,
		user.Country,
		user.Language,
		user.DeviceType,
		user.AppVersion,
		user.FirstSeenAt,
		user.LastSeenAt,
		registeredAt,
		boolToUint8(user.IsRegistered),
		boolToUint8(user.IsPayingUser),
		user.TotalRevenue,
		user.TotalPurchases,
		user.LifetimeSessions,
		user.LifetimeEvents,
		customAttrsJSON,
		user.CreatedAt,
		user.UpdatedAt,
		time.Now().UnixMilli(),
	)
}

// UpsertBatch inserts or updates multiple users
func (r *UserRepo) UpsertBatch(ctx context.Context, users []*User) error {
	if len(users) == 0 {
		return nil
	}

	query := `
		INSERT INTO segmentation.users 
		(user_id, platform, country, language, device_type, app_version,
		 first_seen_at, last_seen_at, registered_at, is_registered, is_paying_user,
		 total_revenue, total_purchases, lifetime_sessions, lifetime_events,
		 custom_attributes, created_at, updated_at, _version)
	`

	batch, err := r.data.Batch(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}

	for _, user := range users {
		customAttrsJSON := "{}"
		if user.CustomAttributes != nil {
			b, _ := json.Marshal(user.CustomAttributes)
			customAttrsJSON = string(b)
		}

		var registeredAt time.Time
		if user.RegisteredAt != nil {
			registeredAt = *user.RegisteredAt
		}

		err := batch.Append(
			user.UserID,
			user.Platform,
			user.Country,
			user.Language,
			user.DeviceType,
			user.AppVersion,
			user.FirstSeenAt,
			user.LastSeenAt,
			registeredAt,
			boolToUint8(user.IsRegistered),
			boolToUint8(user.IsPayingUser),
			user.TotalRevenue,
			user.TotalPurchases,
			user.LifetimeSessions,
			user.LifetimeEvents,
			customAttrsJSON,
			user.CreatedAt,
			user.UpdatedAt,
			time.Now().UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("failed to append to batch: %w", err)
		}
	}

	return batch.Send()
}

// GetByID retrieves a user by ID
func (r *UserRepo) GetByID(ctx context.Context, userID string) (*User, error) {
	query := `
		SELECT user_id, platform, country, language, device_type, app_version,
		       first_seen_at, last_seen_at, registered_at, is_registered, is_paying_user,
		       total_revenue, total_purchases, lifetime_sessions, lifetime_events,
		       custom_attributes, created_at, updated_at
		FROM segmentation.users FINAL
		WHERE user_id = ?
	`

	row := r.data.QueryRow(ctx, query, userID)

	var user User
	var isRegistered, isPayingUser uint8
	var registeredAt NullTime
	var customAttrsJSON string

	err := row.Scan(
		&user.UserID, &user.Platform, &user.Country, &user.Language, &user.DeviceType, &user.AppVersion,
		&user.FirstSeenAt, &user.LastSeenAt, &registeredAt, &isRegistered, &isPayingUser,
		&user.TotalRevenue, &user.TotalPurchases, &user.LifetimeSessions, &user.LifetimeEvents,
		&customAttrsJSON, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}

	user.IsRegistered = isRegistered == 1
	user.IsPayingUser = isPayingUser == 1
	user.RegisteredAt = registeredAt.ToPointer()

	if customAttrsJSON != "" && customAttrsJSON != "{}" {
		if err := json.Unmarshal([]byte(customAttrsJSON), &user.CustomAttributes); err != nil {
			r.log.Warnf("failed to unmarshal custom attributes for user %s: %v", userID, err)
		}
	}

	return &user, nil
}

// GetUserCount returns the total number of users
func (r *UserRepo) GetUserCount(ctx context.Context) (int64, error) {
	query := `SELECT count() FROM segmentation.users FINAL`
	var count uint64
	if err := r.data.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return int64(count), nil
}

// GetActiveUsers returns users active in the last N days
func (r *UserRepo) GetActiveUsers(ctx context.Context, days int, limit, offset int32) ([]string, int64, error) {
	countQuery := `
		SELECT count(DISTINCT user_id)
		FROM segmentation.user_daily_activity
		WHERE activity_date >= today() - ?
	`
	var total uint64
	if err := r.data.QueryRow(ctx, countQuery, days).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count active users: %w", err)
	}

	query := `
		SELECT DISTINCT user_id
		FROM segmentation.user_daily_activity
		WHERE activity_date >= today() - ?
		LIMIT ? OFFSET ?
	`

	rows, err := r.data.ExecuteQuery(ctx, query, days, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get active users: %w", err)
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

// UpdateLastSeen updates the last_seen_at for a user
func (r *UserRepo) UpdateLastSeen(ctx context.Context, userID string, lastSeenAt time.Time) error {
	user, err := r.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	user.LastSeenAt = lastSeenAt
	user.UpdatedAt = time.Now()
	return r.Upsert(ctx, user)
}

// IncrementEventCount increments the lifetime_events count
func (r *UserRepo) IncrementEventCount(ctx context.Context, userID string, count uint32) error {
	user, err := r.GetByID(ctx, userID)
	if err != nil {
		// User doesn't exist, create minimal record
		user = &User{
			UserID:         userID,
			FirstSeenAt:    time.Now(),
			LastSeenAt:     time.Now(),
			LifetimeEvents: count,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
	} else {
		user.LifetimeEvents += count
		user.LastSeenAt = time.Now()
		user.UpdatedAt = time.Now()
	}
	return r.Upsert(ctx, user)
}
