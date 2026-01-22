// Package server provides HTTP and gRPC server implementations.
package server

import (
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"

	v1 "segmentation/api/segment/v1"
	"segmentation/internal/conf"
	"segmentation/internal/service"
)

// NewGRPCServer creates a new gRPC server.
func NewGRPCServer(c *conf.Server, segmentService *service.SegmentService, logger log.Logger) *grpc.Server {
	var opts = []grpc.ServerOption{
		grpc.Middleware(
			recovery.Recovery(),
			logging.Server(logger),
		),
	}

	if c.GRPC.Addr != "" {
		opts = append(opts, grpc.Address(c.GRPC.Addr))
	}
	if c.GRPC.Timeout > 0 {
		opts = append(opts, grpc.Timeout(time.Duration(c.GRPC.Timeout)*time.Second))
	}

	srv := grpc.NewServer(opts...)
	v1.RegisterSegmentServiceServer(srv, segmentService)

	return srv
}
