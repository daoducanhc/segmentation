package service

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "segmentation/api/segment/v1"
	"segmentation/internal/data"
	"segmentation/internal/engine"
)

// SegmentService implements the SegmentService gRPC service
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

// EvaluateSegment evaluates a segment and returns matching user IDs
func (s *SegmentService) EvaluateSegment(ctx context.Context, req *v1.EvaluateSegmentRequest) (*v1.EvaluateSegmentResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 1000
	}

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
