// Package service implements the business logic layer.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "segmentation/api/segment/v1"
	"segmentation/internal/data"
	"segmentation/internal/engine"
	"segmentation/pkg/fileparser"
)

// SegmentService implements the SegmentService gRPC/HTTP service.
type SegmentService struct {
	v1.UnimplementedSegmentServiceServer

	segmentRepo *data.SegmentRepo
	evaluator   *engine.Evaluator
	generator   *engine.SQLGenerator
	criteria    *engine.CriteriaLibrary
	builder     *engine.SegmentBuilder
	log         *log.Helper
}

// NewSegmentService creates a new SegmentService
func NewSegmentService(
	segmentRepo *data.SegmentRepo,
	evaluator *engine.Evaluator,
	generator *engine.SQLGenerator,
	criteria *engine.CriteriaLibrary,
	logger log.Logger,
) *SegmentService {
	return &SegmentService{
		segmentRepo: segmentRepo,
		evaluator:   evaluator,
		generator:   generator,
		criteria:    criteria,
		builder:     engine.NewSegmentBuilder(criteria),
		log:         log.NewHelper(logger),
	}
}

// CreateSegment creates a new segment
func (s *SegmentService) CreateSegment(ctx context.Context, req *v1.CreateSegmentRequest) (*v1.CreateSegmentResponse, error) {
	// Validate definition
	if err := s.generator.ValidateDefinition(req.Definition); err != nil {
		return nil, err
	}

	// Generate SQL
	sql, err := s.generator.GenerateSQL(req.Definition)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	segment := &data.Segment{
		Name:         req.Name,
		Description:  req.Description,
		Definition:   req.Definition,
		GeneratedSQL: sql,
		CreatedBy:    req.CreatedBy,
		CreatedAt:    now,
		UpdatedAt:    now,
		IsActive:     true,
	}

	if err := s.segmentRepo.Create(ctx, segment); err != nil {
		return nil, err
	}

	return &v1.CreateSegmentResponse{
		Segment: s.toProtoSegment(segment),
	}, nil
}

// GetSegment retrieves a segment by ID
func (s *SegmentService) GetSegment(ctx context.Context, req *v1.GetSegmentRequest) (*v1.GetSegmentResponse, error) {
	segment, err := s.segmentRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	return &v1.GetSegmentResponse{
		Segment: s.toProtoSegment(segment),
	}, nil
}

// UpdateSegment updates an existing segment
func (s *SegmentService) UpdateSegment(ctx context.Context, req *v1.UpdateSegmentRequest) (*v1.UpdateSegmentResponse, error) {
	segment, err := s.segmentRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		segment.Name = req.Name
	}
	if req.Description != "" {
		segment.Description = req.Description
	}
	if req.Definition != nil {
		if err := s.generator.ValidateDefinition(req.Definition); err != nil {
			return nil, err
		}
		segment.Definition = req.Definition
		sql, err := s.generator.GenerateSQL(req.Definition)
		if err != nil {
			return nil, err
		}
		segment.GeneratedSQL = sql
	}

	segment.UpdatedAt = time.Now()

	if err := s.segmentRepo.Update(ctx, segment); err != nil {
		return nil, err
	}

	return &v1.UpdateSegmentResponse{
		Segment: s.toProtoSegment(segment),
	}, nil
}

// DeleteSegment deletes a segment
func (s *SegmentService) DeleteSegment(ctx context.Context, req *v1.DeleteSegmentRequest) (*v1.DeleteSegmentResponse, error) {
	if err := s.segmentRepo.Delete(ctx, req.Id); err != nil {
		return nil, err
	}

	return &v1.DeleteSegmentResponse{
		Success: true,
	}, nil
}

// ListSegments lists segments with pagination
func (s *SegmentService) ListSegments(ctx context.Context, req *v1.ListSegmentsRequest) (*v1.ListSegmentsResponse, error) {
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}

	segments, total, err := s.segmentRepo.List(ctx, page, pageSize, req.NameFilter, req.TypeFilter)
	if err != nil {
		return nil, err
	}

	protoSegments := make([]*v1.Segment, len(segments))
	for i, seg := range segments {
		protoSegments[i] = s.toProtoSegment(seg)
	}

	return &v1.ListSegmentsResponse{
		Segments: protoSegments,
		Total:    total,
	}, nil
}

// EvaluateSegment evaluates a segment and return	s matching user IDs
func (s *SegmentService) EvaluateSegment(ctx context.Context, req *v1.EvaluateSegmentRequest) (*v1.EvaluateSegmentResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 1000
	}

	// Get segment to check type
	segment, err := s.segmentRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	// For STATIC segments, fetch from cached results (DB)
	if segment.Definition.Type == v1.SegmentType_SEGMENT_TYPE_STATIC {
		userIDs, total, err := s.segmentRepo.GetCachedResults(ctx, req.Id, limit, req.Offset)
		if err != nil {
			return nil, fmt.Errorf("failed to get cached results: %w", err)
		}

		return &v1.EvaluateSegmentResponse{
			UserIds:      userIDs,
			TotalCount:   total,
			EvaluatedAt:  timestamppb.Now(),
			GeneratedSql: "",
		}, nil
	}

	// For DYNAMIC and COMPOSITE segments, run evaluation query
	result, err := s.evaluator.EvaluateWithPagination(ctx, req.Id, limit, req.Offset)
	if err != nil {
		return nil, err
	}

	return &v1.EvaluateSegmentResponse{
		UserIds:      result.UserIDs,
		TotalCount:   result.TotalCount,
		EvaluatedAt:  timestamppb.Now(),
		GeneratedSql: result.GeneratedSQL,
	}, nil
}

// PreviewSegment previews a segment definition without saving
func (s *SegmentService) PreviewSegment(ctx context.Context, req *v1.PreviewSegmentRequest) (*v1.PreviewSegmentResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	result, err := s.evaluator.Preview(ctx, req.Definition, limit)
	if err != nil {
		return nil, err
	}

	return &v1.PreviewSegmentResponse{
		UserIds:      result.UserIDs,
		TotalCount:   result.TotalCount,
		GeneratedSql: result.GeneratedSQL,
	}, nil
}

// GetSegmentUserCount returns the count of users in a segment
func (s *SegmentService) GetSegmentUserCount(ctx context.Context, req *v1.GetSegmentUserCountRequest) (*v1.GetSegmentUserCountResponse, error) {
	segment, err := s.segmentRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}

	count, err := s.evaluator.Count(ctx, segment.Definition)
	if err != nil {
		return nil, err
	}

	return &v1.GetSegmentUserCountResponse{
		Count: count,
	}, nil
}

// toProtoSegment converts a data.Segment to v1.Segment
func (s *SegmentService) toProtoSegment(seg *data.Segment) *v1.Segment {
	protoSeg := &v1.Segment{
		Id:          seg.ID,
		Name:        seg.Name,
		Description: seg.Description,
		Definition:  seg.Definition,
		CreatedBy:   seg.CreatedBy,
		CreatedAt:   timestamppb.New(seg.CreatedAt),
		UpdatedAt:   timestamppb.New(seg.UpdatedAt),
		IsActive:    seg.IsActive,
		CachedCount: seg.CachedCount,
	}

	if seg.LastEvaluated != nil {
		protoSeg.LastEvaluated = timestamppb.New(*seg.LastEvaluated)
	}

	return protoSeg
}

// UploadStaticSegment creates a static segment from uploaded user IDs
func (s *SegmentService) UploadStaticSegment(ctx context.Context, req *v1.UploadStaticSegmentRequest) (*v1.UploadStaticSegmentResponse, error) {
	var userIDs []string
	var skipped int
	var errors []string

	// Parse user IDs from file or direct list
	if req.FileContent != "" && req.HeaderName != "" {
		result, err := fileparser.ParseFile(req.FileContent, req.FileName, req.HeaderName)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file: %w", err)
		}
		userIDs = result.UserIDs
		skipped = result.Skipped
		errors = result.Errors
	} else if len(req.UserIds) > 0 {
		result := fileparser.ParseUserIDList(req.UserIds)
		userIDs = result.UserIDs
		skipped = result.Skipped
	} else {
		return nil, fmt.Errorf("either file_content with header_name or user_ids must be provided")
	}

	if len(userIDs) == 0 {
		return nil, fmt.Errorf("no valid user IDs found in the input")
	}

	// Create static segment definition
	now := time.Now()
	segment := &data.Segment{
		Name:        req.Name,
		Description: req.Description,
		Definition: &v1.SegmentDefinition{
			Type: v1.SegmentType_SEGMENT_TYPE_STATIC,
		},
		GeneratedSQL:  "", // Static segments don't have generated SQL
		CreatedBy:     req.CreatedBy,
		CreatedAt:     now,
		UpdatedAt:     now,
		IsActive:      true,
		CachedCount:   int64(len(userIDs)),
		LastEvaluated: &now,
	}

	// Create the segment
	if err := s.segmentRepo.Create(ctx, segment); err != nil {
		return nil, fmt.Errorf("failed to create segment: %w", err)
	}

	// Cache the user IDs
	if err := s.segmentRepo.CacheResults(ctx, segment.ID, userIDs); err != nil {
		return nil, fmt.Errorf("failed to cache user IDs: %w", err)
	}

	return &v1.UploadStaticSegmentResponse{
		Segment:       s.toProtoSegment(segment),
		UsersImported: int32(len(userIDs)),
		UsersSkipped:  int32(skipped),
		Errors:        errors,
	}, nil
}

// AddUsersToStaticSegment adds users to an existing static segment
func (s *SegmentService) AddUsersToStaticSegment(ctx context.Context, req *v1.AddUsersToStaticSegmentRequest) (*v1.AddUsersToStaticSegmentResponse, error) {
	// Verify segment exists and is static
	segment, err := s.segmentRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("segment not found: %w", err)
	}

	if segment.Definition == nil || segment.Definition.Type != v1.SegmentType_SEGMENT_TYPE_STATIC {
		return nil, fmt.Errorf("segment %s is not a static segment", req.Id)
	}

	var userIDs []string
	var skipped int
	var errors []string

	// Parse user IDs from file or direct list
	if req.FileContent != "" && req.HeaderName != "" {
		result, err := fileparser.ParseFile(req.FileContent, req.FileName, req.HeaderName)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file: %w", err)
		}
		userIDs = result.UserIDs
		skipped = result.Skipped
		errors = result.Errors
	} else if len(req.UserIds) > 0 {
		result := fileparser.ParseUserIDList(req.UserIds)
		userIDs = result.UserIDs
		skipped = result.Skipped
	} else {
		return nil, fmt.Errorf("either file_content with header_name or user_ids must be provided")
	}

	// Add users to segment
	added, dbSkipped, err := s.segmentRepo.AddUsersToStaticSegment(ctx, req.Id, userIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to add users: %w", err)
	}

	return &v1.AddUsersToStaticSegmentResponse{
		UsersAdded:   int32(added),
		UsersSkipped: int32(skipped + dbSkipped),
		Errors:       errors,
	}, nil
}

// RemoveUsersFromStaticSegment removes users from a static segment
func (s *SegmentService) RemoveUsersFromStaticSegment(ctx context.Context, req *v1.RemoveUsersFromStaticSegmentRequest) (*v1.RemoveUsersFromStaticSegmentResponse, error) {
	// Verify segment exists and is static
	segment, err := s.segmentRepo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("segment not found: %w", err)
	}

	if segment.Definition == nil || segment.Definition.Type != v1.SegmentType_SEGMENT_TYPE_STATIC {
		return nil, fmt.Errorf("segment %s is not a static segment", req.Id)
	}

	removed, err := s.segmentRepo.RemoveUsersFromStaticSegment(ctx, req.Id, req.UserIds)
	if err != nil {
		return nil, fmt.Errorf("failed to remove users: %w", err)
	}

	return &v1.RemoveUsersFromStaticSegmentResponse{
		UsersRemoved: int32(removed),
	}, nil
}

// GetDistinctValues returns distinct values for a profile field
func (s *SegmentService) GetDistinctValues(ctx context.Context, req *v1.GetDistinctValuesRequest) (*v1.GetDistinctValuesResponse, error) {
	// Validate field name
	validFields := map[string]bool{
		"platform": true,
		"country":  true,
		"os":       true,
		"language": true,
		"app_id":   true,
	}

	if !validFields[req.Field] {
		return nil, fmt.Errorf("invalid field: %s (allowed: platform, country, os, language, app_id)", req.Field)
	}

	values, err := s.segmentRepo.GetDistinctValues(ctx, req.Field)
	if err != nil {
		return nil, fmt.Errorf("failed to get distinct values: %w", err)
	}

	return &v1.GetDistinctValuesResponse{
		Values: values,
	}, nil
}
