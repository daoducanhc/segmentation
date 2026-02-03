GOHOSTOS:=$(shell go env GOHOSTOS)
GOPATH:=$(shell go env GOPATH)
VERSION=$(shell git describe --tags --always)

ifeq ($(GOHOSTOS), windows)
	Git_Bash=$(subst \,/,$(subst cmd\,bindockerfile\git-bash.exe,$(dir $(shell where git))))
	INTERNAL_PROTO_FILES=$(shell $(Git_Bash) -c "find internal -name *.proto")
	API_PROTO_FILES=$(shell $(Git_Bash) -c "find api -name *.proto")
else
	INTERNAL_PROTO_FILES=$(shell find internal -name *.proto)
	API_PROTO_FILES=$(shell find api -name *.proto)
endif

.PHONY: init
init:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/go-kratos/kratos/cmd/kratos/v2@latest
	go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest
	go install github.com/google/gnostic/cmd/protoc-gen-openapi@latest
	go install github.com/google/wire/cmd/wire@latest

.PHONY: api
api:
	protoc --proto_path=./api \
		--proto_path=./third_party \
		--go_out=paths=source_relative:./api \
		--go-http_out=paths=source_relative:./api \
		--go-grpc_out=paths=source_relative:./api \
		$(API_PROTO_FILES)

.PHONY: swagger
swagger:
	protoc --proto_path=./api \
		--proto_path=./third_party \
		--openapi_out=fq_schema_naming=true,default_response=false:. \
		$(API_PROTO_FILES)
	@# Add server URL to openapi.yaml
	@sed -i '/^info:/a\servers:\n    - url: http://localhost:8000\n      description: Local development server' openapi.yaml 2>/dev/null || \
		sed -i '' '/^info:/a\'$$'\n''servers:'$$'\n''    - url: http://localhost:8000'$$'\n''      description: Local development server' openapi.yaml
	@echo "Generated openapi.yaml - Swagger UI available at http://localhost:8000/q/swagger-ui"

.PHONY: build
build:
	mkdir -p bin/ && go build -ldflags "-X main.Version=$(VERSION)" -o ./bin/ ./...

.PHONY: run
run:
	go run ./cmd/server -conf ./configs/config.yaml

.PHONY: docker
docker:
	docker build -t segmentation:$(VERSION) .

.PHONY: migrate
migrate:
	docker exec -i mgp-clickhouse-local clickhouse-client --query "DROP DATABASE IF EXISTS segmentation"
	docker exec -i mgp-clickhouse-local clickhouse-client --multiquery < ./migrations/001_init_schema.sql

.PHONY: migrate-soft
migrate-soft:
	docker exec -i mgp-clickhouse-local clickhouse-client --multiquery < ./migrations/001_init_schema.sql

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: all
all: api build swagger

help:
	@echo ''
	@echo 'Usage:'
	@echo ' make [target]'
	@echo ''
	@echo 'Targets:'
	@echo '  init                Install dependencies and tools'
	@echo '  api                 Generate Go code from proto files'
	@echo '  swagger             Generate openapi.yaml from proto files'
	@echo '  build               Build the binary'
	@echo '  run                 Run the server locally'
	@echo '  migrate             Reset and run database migrations (drops DB first)'
	@echo '  migrate-soft        Run migrations without dropping (may fail if exists)'
	@echo '  clean               Clean build artifacts'
	@echo ''
	@echo 'Swagger UI: http://localhost:8000/q/swagger-ui (when server is running)'