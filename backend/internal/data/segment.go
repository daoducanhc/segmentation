// Package data provides the data access layer for ClickHouse.
package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	v1 "segmentation/api/segment/v1"
)

// Segment represents a segment definition in the database.
type Segment struct {
	ID            string
	Name          string
	Description   string
	Definition    *v1.SegmentDefinition
	GeneratedSQL  string
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	IsActive      bool
	ExpiresAt     *time.Time
	CachedCount   int64
	LastEvaluated *time.Time
}

// SegmentRepo handles segment CRUD and query operations.
type SegmentRepo struct {
	data *Data
	log  *log.Helper
}

// NewSegmentRepo creates a new SegmentRepo
func NewSegmentRepo(data *Data, logger log.Logger) *SegmentRepo {
	return &SegmentRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// Create creates a new segment
func (r *SegmentRepo) Create(ctx context.Context, seg *Segment) error {
	if seg.ID == "" {
		seg.ID = uuid.New().String()
	}

	defJSON, err := protojson.Marshal(seg.Definition)
	if err != nil {
		return fmt.Errorf("failed to marshal definition: %w", err)
	}

	query := `
		INSERT INTO segmentation.segment_definitions 
		(id, name, description, segment_type, definition, created_by, created_at, updated_at, is_active, expires_at, cached_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	segType := "DYNAMIC"
	if seg.Definition != nil {
		segType = seg.Definition.Type.String()
	}

	var expiresAt time.Time
	if seg.ExpiresAt != nil {
		expiresAt = *seg.ExpiresAt
	}

	err = r.data.ExecuteExec(ctx, query,
		seg.ID,
		seg.Name,
		seg.Description,
		segType,
		string(defJSON),
		seg.CreatedBy,
		seg.CreatedAt,
		seg.UpdatedAt,
		boolToUint8(seg.IsActive),
		expiresAt,
		seg.CachedCount,
	)

	if err != nil {
		return fmt.Errorf("failed to insert segment: %w", err)
	}

	return nil
}

// Update updates an existing segment
func (r *SegmentRepo) Update(ctx context.Context, seg *Segment) error {
	defJSON, err := protojson.Marshal(seg.Definition)
	if err != nil {
		return fmt.Errorf("failed to marshal definition: %w", err)
	}

	segType := "DYNAMIC"
	if seg.Definition != nil {
		segType = seg.Definition.Type.String()
	}

	query := `
		INSERT INTO segmentation.segment_definitions 
		(id, name, description, segment_type, definition, generated_sql, created_by, created_at, updated_at, is_active, expires_at, cached_count, last_evaluated, _version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var lastEval time.Time
	if seg.LastEvaluated != nil {
		lastEval = *seg.LastEvaluated
	}

	var expiresAt time.Time
	if seg.ExpiresAt != nil {
		expiresAt = *seg.ExpiresAt
	}

	err = r.data.ExecuteExec(ctx, query,
		seg.ID,
		seg.Name,
		seg.Description,
		segType,
		string(defJSON),
		seg.GeneratedSQL,
		seg.CreatedBy,
		seg.CreatedAt,
		time.Now(),
		boolToUint8(seg.IsActive),
		expiresAt,
		seg.CachedCount,
		lastEval,
		time.Now().UnixMilli(),
	)

	if err != nil {
		return fmt.Errorf("failed to update segment: %w", err)
	}

	return nil
}

// GetByID retrieves a segment by ID
func (r *SegmentRepo) GetByID(ctx context.Context, id string) (*Segment, error) {
	query := `
		SELECT id, name, description, segment_type, definition, generated_sql, 
		       created_by, created_at, updated_at, is_active, expires_at, cached_count, last_evaluated
		FROM segmentation.segment_definitions FINAL
		WHERE id = ?
	`

	row := r.data.QueryRow(ctx, query, id)

	var seg Segment
	var defJSON string
	var segType string
	var isActive uint8
	var expiresAt NullTime
	var lastEval NullTime

	err := row.Scan(
		&seg.ID, &seg.Name, &seg.Description, &segType, &defJSON, &seg.GeneratedSQL,
		&seg.CreatedBy, &seg.CreatedAt, &seg.UpdatedAt, &isActive, &expiresAt, &seg.CachedCount, &lastEval,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan segment: %w", err)
	}

	seg.IsActive = isActive == 1
	seg.ExpiresAt = expiresAt.ToPointer()
	seg.LastEvaluated = lastEval.ToPointer()

	if defJSON != "" {
		seg.Definition = &v1.SegmentDefinition{}
		if err := protojson.Unmarshal([]byte(defJSON), seg.Definition); err != nil {
			return nil, fmt.Errorf("failed to unmarshal definition: %w", err)
		}
	}

	return &seg, nil
}

// List lists segments with pagination
func (r *SegmentRepo) List(ctx context.Context, page, pageSize int32, nameFilter string, typeFilter v1.SegmentType) ([]*Segment, int32, error) {
	// Count query - exclude expired segments
	countQuery := `SELECT count() FROM segmentation.segment_definitions FINAL WHERE is_active = 1 AND (expires_at IS NULL OR expires_at > now())`
	countArgs := []interface{}{}

	if nameFilter != "" {
		countQuery += ` AND name LIKE ?`
		countArgs = append(countArgs, "%"+nameFilter+"%")
	}
	if typeFilter != v1.SegmentType_SEGMENT_TYPE_UNSPECIFIED {
		countQuery += ` AND segment_type = ?`
		countArgs = append(countArgs, typeFilter.String())
	}

	var total uint64
	if err := r.data.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count segments: %w", err)
	}

	// List query - exclude expired segments
	query := `
		SELECT id, name, description, segment_type, definition, generated_sql,
		       created_by, created_at, updated_at, is_active, expires_at, cached_count, last_evaluated
		FROM segmentation.segment_definitions FINAL
		WHERE is_active = 1 AND (expires_at IS NULL OR expires_at > now())
	`
	args := []interface{}{}

	if nameFilter != "" {
		query += ` AND name LIKE ?`
		args = append(args, "%"+nameFilter+"%")
	}
	if typeFilter != v1.SegmentType_SEGMENT_TYPE_UNSPECIFIED {
		query += ` AND segment_type = ?`
		args = append(args, typeFilter.String())
	}

	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	args = append(args, pageSize, offset)

	rows, err := r.data.ExecuteQuery(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list segments: %w", err)
	}
	defer rows.Close()

	var segments []*Segment
	for rows.Next() {
		var seg Segment
		var defJSON string
		var segType string
		var isActive uint8
		var expiresAt NullTime
		var lastEval NullTime

		err := rows.Scan(
			&seg.ID, &seg.Name, &seg.Description, &segType, &defJSON, &seg.GeneratedSQL,
			&seg.CreatedBy, &seg.CreatedAt, &seg.UpdatedAt, &isActive, &expiresAt, &seg.CachedCount, &lastEval,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan segment: %w", err)
		}

		seg.IsActive = isActive == 1
		seg.ExpiresAt = expiresAt.ToPointer()
		seg.LastEvaluated = lastEval.ToPointer()

		if defJSON != "" {
			seg.Definition = &v1.SegmentDefinition{}
			if err := protojson.Unmarshal([]byte(defJSON), seg.Definition); err != nil {
				r.log.Warnf("failed to unmarshal definition for segment %s: %v", seg.ID, err)
			}
		}

		segments = append(segments, &seg)
	}

	return segments, int32(total), nil
}

// Delete soft-deletes a segment
func (r *SegmentRepo) Delete(ctx context.Context, id string) error {
	seg, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	seg.IsActive = false
	return r.Update(ctx, seg)
}

// ExecuteSegmentQuery executes a segment SQL query and returns user IDs
func (r *SegmentRepo) ExecuteSegmentQuery(ctx context.Context, sql string, limit, offset int32) ([]string, int64, error) {
	// First get total count
	countSQL := fmt.Sprintf("SELECT count() FROM (%s)", sql)
	var total uint64
	if err := r.data.QueryRow(ctx, countSQL).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count results: %w", err)
	}

	// Apply limit/offset
	finalSQL := sql
	if limit > 0 {
		finalSQL = fmt.Sprintf("%s LIMIT %d OFFSET %d", sql, limit, offset)
	}

	rows, err := r.data.ExecuteQuery(ctx, finalSQL)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute segment query: %w", err)
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

// ExecuteCountQuery executes a count query
func (r *SegmentRepo) ExecuteCountQuery(ctx context.Context, sql string) (int64, error) {
	var count uint64
	if err := r.data.QueryRow(ctx, sql).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to execute count query: %w", err)
	}
	return int64(count), nil
}

// CacheResults caches segment evaluation results
func (r *SegmentRepo) CacheResults(ctx context.Context, segmentID string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Clear existing results
	clearQuery := `ALTER TABLE segmentation.segment_results DELETE WHERE segment_id = ?`
	if err := r.data.ExecuteExec(ctx, clearQuery, segmentID); err != nil {
		r.log.Warnf("failed to clear existing results: %v", err)
	}

	// Insert new results in batches
	evaluationID := uuid.New().String()
	batchSize := 10000

	for i := 0; i < len(userIDs); i += batchSize {
		end := i + batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		batch := userIDs[i:end]

		insertQuery := `INSERT INTO segmentation.segment_results (segment_id, user_id, added_at, evaluation_id)`

		b, err := r.data.Batch(ctx, insertQuery)
		if err != nil {
			return fmt.Errorf("failed to prepare batch: %w", err)
		}

		for _, uid := range batch {
			if err := b.Append(segmentID, uid, time.Now(), evaluationID); err != nil {
				return fmt.Errorf("failed to append to batch: %w", err)
			}
		}

		if err := b.Send(); err != nil {
			return fmt.Errorf("failed to send batch: %w", err)
		}
	}

	return nil
}

// GetCachedResults retrieves cached segment results
func (r *SegmentRepo) GetCachedResults(ctx context.Context, segmentID string, limit, offset int32) ([]string, int64, error) {
	countQuery := `SELECT count() FROM segmentation.segment_results WHERE segment_id = ?`
	var total uint64
	if err := r.data.QueryRow(ctx, countQuery, segmentID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count cached results: %w", err)
	}

	query := `SELECT user_id FROM segmentation.segment_results WHERE segment_id = ?`
	args := []interface{}{segmentID}

	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}

	rows, err := r.data.ExecuteQuery(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get cached results: %w", err)
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

// UpdateEvaluationMetadata updates segment evaluation metadata
func (r *SegmentRepo) UpdateEvaluationMetadata(ctx context.Context, segmentID string, count int64, evaluatedAt time.Time) error {
	seg, err := r.GetByID(ctx, segmentID)
	if err != nil {
		return err
	}

	seg.CachedCount = count
	seg.LastEvaluated = &evaluatedAt

	return r.Update(ctx, seg)
}

// RecordEvaluation records a segment evaluation in history
func (r *SegmentRepo) RecordEvaluation(ctx context.Context, segmentID string, userCount int64, executionMs int32, generatedSQL, status, errorMsg string) error {
	query := `
		INSERT INTO segmentation.segment_evaluations 
		(segment_id, user_count, execution_time_ms, generated_sql, status, error_message, evaluated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	return r.data.ExecuteExec(ctx, query, segmentID, userCount, executionMs, generatedSQL, status, errorMsg, time.Now())
}

// AddUsersToStaticSegment adds users to a static segment
func (r *SegmentRepo) AddUsersToStaticSegment(ctx context.Context, segmentID string, userIDs []string) (added, skipped int, err error) {
	if len(userIDs) == 0 {
		return 0, 0, nil
	}

	// Get existing users to avoid duplicates
	existingQuery := `SELECT user_id FROM segmentation.segment_results WHERE segment_id = ?`
	rows, err := r.data.ExecuteQuery(ctx, existingQuery, segmentID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get existing users: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return 0, 0, fmt.Errorf("failed to scan user_id: %w", err)
		}
		existing[uid] = true
	}

	// Filter out duplicates
	var newUsers []string
	for _, uid := range userIDs {
		if !existing[uid] {
			newUsers = append(newUsers, uid)
			existing[uid] = true // Mark as seen for this batch
		} else {
			skipped++
		}
	}

	if len(newUsers) == 0 {
		return 0, skipped, nil
	}

	// Insert new users
	evaluationID := uuid.New().String()
	insertQuery := `INSERT INTO segmentation.segment_results (segment_id, user_id, added_at, evaluation_id)`

	b, err := r.data.Batch(ctx, insertQuery)
	if err != nil {
		return 0, skipped, fmt.Errorf("failed to prepare batch: %w", err)
	}

	for _, uid := range newUsers {
		if err := b.Append(segmentID, uid, time.Now(), evaluationID); err != nil {
			return 0, skipped, fmt.Errorf("failed to append to batch: %w", err)
		}
	}

	if err := b.Send(); err != nil {
		return 0, skipped, fmt.Errorf("failed to send batch: %w", err)
	}

	// Update cached count
	countQuery := `SELECT count() FROM segmentation.segment_results WHERE segment_id = ?`
	var count uint64
	if err := r.data.QueryRow(ctx, countQuery, segmentID).Scan(&count); err == nil {
		r.UpdateEvaluationMetadata(ctx, segmentID, int64(count), time.Now())
	}

	return len(newUsers), skipped, nil
}

// RemoveUsersFromStaticSegment removes users from a static segment
func (r *SegmentRepo) RemoveUsersFromStaticSegment(ctx context.Context, segmentID string, userIDs []string) (removed int, err error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	// Delete users one by one (ClickHouse ALTER TABLE DELETE doesn't support IN with parameters well)
	for _, uid := range userIDs {
		deleteQuery := `ALTER TABLE segmentation.segment_results DELETE WHERE segment_id = ? AND user_id = ?`
		if err := r.data.ExecuteExec(ctx, deleteQuery, segmentID, uid); err != nil {
			r.log.Warnf("failed to delete user %s from segment %s: %v", uid, segmentID, err)
			continue
		}
		removed++
	}

	// Update cached count
	countQuery := `SELECT count() FROM segmentation.segment_results WHERE segment_id = ?`
	var count uint64
	if err := r.data.QueryRow(ctx, countQuery, segmentID).Scan(&count); err == nil {
		r.UpdateEvaluationMetadata(ctx, segmentID, int64(count), time.Now())
	}

	return removed, nil
}

// ClearStaticSegmentUsers removes all users from a static segment
func (r *SegmentRepo) ClearStaticSegmentUsers(ctx context.Context, segmentID string) error {
	clearQuery := `ALTER TABLE segmentation.segment_results DELETE WHERE segment_id = ?`
	return r.data.ExecuteExec(ctx, clearQuery, segmentID)
}

// GetDistinctValues returns distinct values for a profile field from users table or user_daily_activity
func (r *SegmentRepo) GetDistinctValues(ctx context.Context, field string) ([]string, error) {
	// Map field to appropriate table and column
	// Note: Some fields may contain comma-separated values, so we need to split them
	var query string
	switch field {
	case "platform", "country", "language", "os":
		// From users table - split comma-separated values and get distinct items
		query = fmt.Sprintf(`
			SELECT DISTINCT arrayJoin(splitByChar(',', %s)) as value 
			FROM segmentation.users FINAL 
			WHERE %s != '' AND value != ''
			ORDER BY value 
			LIMIT 1000
		`, field, field)
	case "app_id":
		// From user_daily_activity table
		query = `SELECT DISTINCT app_id FROM segmentation.user_daily_activity FINAL WHERE app_id != '' ORDER BY app_id LIMIT 1000`
	default:
		return nil, fmt.Errorf("unsupported field: %s", field)
	}

	rows, err := r.data.ExecuteQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query distinct values: %w", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			r.log.Warnf("failed to scan value: %v", err)
			continue
		}
		// Trim whitespace from split values
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}

	return values, nil
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
