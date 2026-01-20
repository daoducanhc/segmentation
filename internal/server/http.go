package server

import (
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/go-kratos/swagger-api/openapiv2"

	v1 "segmentation/api/segment/v1"
	"segmentation/internal/conf"
	"segmentation/internal/service"
)

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(c *conf.Server, segmentSvc *service.SegmentService, logger log.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			logging.Server(logger),
		),
	}

	if c.HTTP.Network != "" {
		opts = append(opts, http.Network(c.HTTP.Network))
	}
	if c.HTTP.Addr != "" {
		opts = append(opts, http.Address(c.HTTP.Addr))
	}
	if c.HTTP.Timeout > 0 {
		opts = append(opts, http.Timeout(time.Duration(c.HTTP.Timeout)*time.Second))
	}

	srv := http.NewServer(opts...)

	// Register Swagger UI at /q/swagger-ui
	openAPIHandler := openapiv2.NewHandler()
	srv.HandlePrefix("/q/", openAPIHandler)

	v1.RegisterSegmentServiceHTTPServer(srv, segmentSvc)
	return srv
}
