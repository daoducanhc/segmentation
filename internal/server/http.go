// Package server provides HTTP and gRPC server implementations.
package server

import (
	"encoding/base64"
	"io"
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/go-kratos/swagger-api/openapiv2"

	v1 "segmentation/api/segment/v1"
	"segmentation/internal/conf"
	"segmentation/internal/service"
)

// NewHTTPServer creates a new HTTP server with REST endpoints.
func NewHTTPServer(c *conf.Server, segmentSvc *service.SegmentService, logger log.Logger) *kratoshttp.Server {
	var opts = []kratoshttp.ServerOption{
		kratoshttp.Middleware(
			recovery.Recovery(),
			logging.Server(logger),
		),
	}

	if c.HTTP.Network != "" {
		opts = append(opts, kratoshttp.Network(c.HTTP.Network))
	}
	if c.HTTP.Addr != "" {
		opts = append(opts, kratoshttp.Address(c.HTTP.Addr))
	}
	if c.HTTP.Timeout > 0 {
		opts = append(opts, kratoshttp.Timeout(time.Duration(c.HTTP.Timeout)*time.Second))
	}

	srv := kratoshttp.NewServer(opts...)

	// Register Swagger UI at /q/swagger-ui
	openAPIHandler := openapiv2.NewHandler()
	srv.HandlePrefix("/q/", openAPIHandler)

	// Register custom file upload handler (multipart form)
	srv.HandleFunc("/v1/segments/upload-file", makeUploadHandler(segmentSvc))
	srv.HandleFunc("/v1/segments/{id}/users-file", makeAddUsersHandler(segmentSvc))

	v1.RegisterSegmentServiceHTTPServer(srv, segmentSvc)
	return srv
}

// makeUploadHandler creates a handler for multipart file upload to create static segment
func makeUploadHandler(svc *service.SegmentService) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse multipart form (max 32MB)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Get form values
		name := r.FormValue("name")
		description := r.FormValue("description")
		headerName := r.FormValue("header_name")
		createdBy := r.FormValue("created_by")

		if name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if headerName == "" {
			http.Error(w, "header_name is required", http.StatusBadRequest)
			return
		}

		// Get uploaded file
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file is required: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Read file content
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Failed to read file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Convert to base64 for the service
		fileContent := base64.StdEncoding.EncodeToString(content)

		// Call service
		resp, err := svc.UploadStaticSegment(r.Context(), &v1.UploadStaticSegmentRequest{
			Name:        name,
			Description: description,
			FileContent: fileContent,
			FileName:    header.Filename,
			HeaderName:  headerName,
			CreatedBy:   createdBy,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Return JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Simple JSON response
		w.Write([]byte(`{"segment_id":"` + resp.Segment.Id + `","users_imported":` + itoa(int(resp.UsersImported)) + `,"users_skipped":` + itoa(int(resp.UsersSkipped)) + `}`))
	}
}

// makeAddUsersHandler creates a handler for multipart file upload to add users to static segment
func makeAddUsersHandler(svc *service.SegmentService) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract segment ID from path
		// Path is /v1/segments/{id}/users-file
		path := r.URL.Path
		// Simple extraction - find segment ID between /segments/ and /users-file
		start := len("/v1/segments/")
		end := len(path) - len("/users-file")
		if end <= start {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		segmentID := path[start:end]

		// Parse multipart form (max 32MB)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Get form values
		headerName := r.FormValue("header_name")
		if headerName == "" {
			http.Error(w, "header_name is required", http.StatusBadRequest)
			return
		}

		// Get uploaded file
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file is required: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Read file content
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Failed to read file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Convert to base64 for the service
		fileContent := base64.StdEncoding.EncodeToString(content)

		// Call service
		resp, err := svc.AddUsersToStaticSegment(r.Context(), &v1.AddUsersToStaticSegmentRequest{
			Id:          segmentID,
			FileContent: fileContent,
			FileName:    header.Filename,
			HeaderName:  headerName,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Return JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"users_added":` + itoa(int(resp.UsersAdded)) + `,"users_skipped":` + itoa(int(resp.UsersSkipped)) + `}`))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
