package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	v1 "segmentation/api/segment/v1"
	"segmentation/internal/data"
)

// Evaluator handles segment evaluation
type Evaluator struct {
	generator   *SQLGenerator
	segmentRepo *data.SegmentRepo
	log         *log.Helper
}

// NewEvaluator creates a new segment evaluator
func NewEvaluator(generator *SQLGenerator, segmentRepo *data.SegmentRepo, logger log.Logger) *Evaluator {
	return &Evaluator{
		generator:   generator,
		segmentRepo: segmentRepo,
		log:         log.NewHelper(logger),
	}
}

// EvaluationResult contains the result of a segment evaluation
type EvaluationResult struct {
	SegmentID    string
	UserIDs      []string
	TotalCount   int64
	ExecutionMs  int64
	GeneratedSQL string
	Error        error
}

// Evaluate evaluates a segment and returns matching user IDs
func (e *Evaluator) Evaluate(ctx context.Context, segmentID string) (*EvaluationResult, error) {
	startTime := time.Now()

	// Get segment definition
	segment, err := e.segmentRepo.GetByID(ctx, segmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get segment: %w", err)
	}

	// Generate SQL
	sql, err := e.generator.GenerateSQL(segment.Definition)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SQL: %w", err)
	}

	result := &EvaluationResult{
		SegmentID:    segmentID,
		GeneratedSQL: sql,
	}

	// Execute query
	userIDs, total, err := e.segmentRepo.ExecuteSegmentQuery(ctx, sql, 0, 0)
	if err != nil {
		result.Error = err
		result.ExecutionMs = time.Since(startTime).Milliseconds()
		e.recordEvaluation(ctx, segmentID, result, "error", err.Error())
		return result, err
	}

	result.UserIDs = userIDs
	result.TotalCount = total
	result.ExecutionMs = time.Since(startTime).Milliseconds()

	// Cache results
	if err := e.segmentRepo.CacheResults(ctx, segmentID, userIDs); err != nil {
		e.log.Warnf("failed to cache results: %v", err)
	}

	// Update segment metadata
	if err := e.segmentRepo.UpdateEvaluationMetadata(ctx, segmentID, total, time.Now()); err != nil {
		e.log.Warnf("failed to update evaluation metadata: %v", err)
	}

	// Record successful evaluation
	e.recordEvaluation(ctx, segmentID, result, "success", "")

	return result, nil
}

// EvaluateWithPagination evaluates a segment with pagination
func (e *Evaluator) EvaluateWithPagination(ctx context.Context, segmentID string, limit, offset int32) (*EvaluationResult, error) {
	startTime := time.Now()

	// Get segment definition
	segment, err := e.segmentRepo.GetByID(ctx, segmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get segment: %w", err)
	}

	// Generate SQL
	sql, err := e.generator.GenerateSQL(segment.Definition)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SQL: %w", err)
	}

	result := &EvaluationResult{
		SegmentID:    segmentID,
		GeneratedSQL: sql,
	}

	// Execute query with pagination
	userIDs, total, err := e.segmentRepo.ExecuteSegmentQuery(ctx, sql, limit, offset)
	if err != nil {
		result.Error = err
		result.ExecutionMs = time.Since(startTime).Milliseconds()
		return result, err
	}

	result.UserIDs = userIDs
	result.TotalCount = total
	result.ExecutionMs = time.Since(startTime).Milliseconds()

	return result, nil
}

// Preview generates a preview of a segment without caching
func (e *Evaluator) Preview(ctx context.Context, def *v1.SegmentDefinition, limit int32) (*EvaluationResult, error) {
	startTime := time.Now()

	// Validate definition
	if err := e.generator.ValidateDefinition(def); err != nil {
		return nil, fmt.Errorf("invalid definition: %w", err)
	}

	// Generate SQL
	sql, err := e.generator.GenerateSQL(def)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SQL: %w", err)
	}

	result := &EvaluationResult{
		GeneratedSQL: sql,
	}

	// Execute query with limit for preview
	if limit <= 0 {
		limit = 100
	}

	userIDs, total, err := e.segmentRepo.ExecuteSegmentQuery(ctx, sql, limit, 0)
	if err != nil {
		result.Error = err
		result.ExecutionMs = time.Since(startTime).Milliseconds()
		return result, err
	}

	result.UserIDs = userIDs
	result.TotalCount = total
	result.ExecutionMs = time.Since(startTime).Milliseconds()

	return result, nil
}

// Count returns just the count of users in a segment
func (e *Evaluator) Count(ctx context.Context, def *v1.SegmentDefinition) (int64, error) {
	sql, err := e.generator.GenerateCountSQL(def)
	if err != nil {
		return 0, fmt.Errorf("failed to generate count SQL: %w", err)
	}

	count, err := e.segmentRepo.ExecuteCountQuery(ctx, sql)
	if err != nil {
		return 0, fmt.Errorf("failed to execute count query: %w", err)
	}

	return count, nil
}

// EvaluateParentChild evaluates a child segment within a parent segment
func (e *Evaluator) EvaluateParentChild(ctx context.Context, parentID, childID string, intersect bool) (*EvaluationResult, error) {
	startTime := time.Now()

	// Get parent segment
	parent, err := e.segmentRepo.GetByID(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent segment: %w", err)
	}

	// Get child segment
	child, err := e.segmentRepo.GetByID(ctx, childID)
	if err != nil {
		return nil, fmt.Errorf("failed to get child segment: %w", err)
	}

	// Generate parent SQL
	parentSQL, err := e.generator.GenerateSQL(parent.Definition)
	if err != nil {
		return nil, fmt.Errorf("failed to generate parent SQL: %w", err)
	}

	// Generate child SQL
	childSQL, err := e.generator.GenerateSQL(child.Definition)
	if err != nil {
		return nil, fmt.Errorf("failed to generate child SQL: %w", err)
	}

	// Combine queries
	var sql string
	if intersect {
		sql = fmt.Sprintf("SELECT user_id FROM (%s) INTERSECT SELECT user_id FROM (%s)", parentSQL, childSQL)
	} else {
		sql = fmt.Sprintf("SELECT user_id FROM (%s) EXCEPT SELECT user_id FROM (%s)", parentSQL, childSQL)
	}

	result := &EvaluationResult{
		GeneratedSQL: sql,
	}

	// Execute query
	userIDs, total, err := e.segmentRepo.ExecuteSegmentQuery(ctx, sql, 0, 0)
	if err != nil {
		result.Error = err
		result.ExecutionMs = time.Since(startTime).Milliseconds()
		return result, err
	}

	result.UserIDs = userIDs
	result.TotalCount = total
	result.ExecutionMs = time.Since(startTime).Milliseconds()

	return result, nil
}

// EvaluateMultiple evaluates multiple segments and combines results
func (e *Evaluator) EvaluateMultiple(ctx context.Context, segmentIDs []string, operator v1.LogicalOperator) (*EvaluationResult, error) {
	startTime := time.Now()

	if len(segmentIDs) == 0 {
		return nil, fmt.Errorf("no segment IDs provided")
	}

	var queries []string
	for _, id := range segmentIDs {
		segment, err := e.segmentRepo.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get segment %s: %w", id, err)
		}

		sql, err := e.generator.GenerateSQL(segment.Definition)
		if err != nil {
			return nil, fmt.Errorf("failed to generate SQL for segment %s: %w", id, err)
		}

		queries = append(queries, sql)
	}

	// Combine queries
	op := "INTERSECT"
	if operator == v1.LogicalOperator_LOGICAL_OPERATOR_OR {
		op = "UNION DISTINCT"
	}

	sql := fmt.Sprintf("SELECT user_id FROM (%s)", queries[0])
	for i := 1; i < len(queries); i++ {
		sql = fmt.Sprintf("%s %s SELECT user_id FROM (%s)", sql, op, queries[i])
	}

	result := &EvaluationResult{
		GeneratedSQL: sql,
	}

	// Execute combined query
	userIDs, total, err := e.segmentRepo.ExecuteSegmentQuery(ctx, sql, 0, 0)
	if err != nil {
		result.Error = err
		result.ExecutionMs = time.Since(startTime).Milliseconds()
		return result, err
	}

	result.UserIDs = userIDs
	result.TotalCount = total
	result.ExecutionMs = time.Since(startTime).Milliseconds()

	return result, nil
}

// GetCachedResults returns cached results for a segment
func (e *Evaluator) GetCachedResults(ctx context.Context, segmentID string, limit, offset int32) ([]string, int64, error) {
	return e.segmentRepo.GetCachedResults(ctx, segmentID, limit, offset)
}

// CheckUserInSegment checks if a user is in a segment
func (e *Evaluator) CheckUserInSegment(ctx context.Context, userID, segmentID string) (bool, error) {
	segment, err := e.segmentRepo.GetByID(ctx, segmentID)
	if err != nil {
		return false, fmt.Errorf("failed to get segment: %w", err)
	}

	sql, err := e.generator.GenerateSQL(segment.Definition)
	if err != nil {
		return false, fmt.Errorf("failed to generate SQL: %w", err)
	}

	// Add user filter
	checkSQL := fmt.Sprintf("SELECT count() FROM (%s) WHERE user_id = '%s'", sql, escapeSQLString(userID))

	count, err := e.segmentRepo.ExecuteCountQuery(ctx, checkSQL)
	if err != nil {
		return false, fmt.Errorf("failed to check user in segment: %w", err)
	}

	return count > 0, nil
}

// recordEvaluation records an evaluation in history
func (e *Evaluator) recordEvaluation(ctx context.Context, segmentID string, result *EvaluationResult, status, errorMsg string) {
	err := e.segmentRepo.RecordEvaluation(ctx, segmentID, result.TotalCount, int32(result.ExecutionMs), result.GeneratedSQL, status, errorMsg)
	if err != nil {
		e.log.Warnf("failed to record evaluation: %v", err)
	}
}
