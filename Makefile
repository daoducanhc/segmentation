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
		--openapi_out=fq_schema_naming=true,default_response=false:. \
		$(API_PROTO_FILES)

.PHONY: build
build:
	mkdir -p bin/ && go build -ldflags "-X main.Version=$(VERSION)" -o ./bin/ ./...

.PHONY: test
test:
	go test -v ./... -cover

.PHONY: run
run:
	go run ./cmd/server -conf ./configs/config.yaml

.PHONY: docker
docker:
	docker build -t segmentation:$(VERSION) .

.PHONY: docker-compose-up
docker-compose-up:
	docker-compose up -d

.PHONY: docker-compose-down
docker-compose-down:
	docker-compose down

.PHONY: migrate
migrate:
	docker exec -i segmentation-clickhouse-1 clickhouse-client --multiline < ./migrations/001_init_schema.sql

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: all
all: api build

help:
	@echo ''
	@echo 'Usage:'
	@echo ' make [target]'
	@echo ''
	@echo 'Targets:'
	@echo '  init                Install dependencies and tools'
	@echo '  api                 Generate API from proto files'
	@echo '  build               Build the binary'
	@echo '  test                Run tests'
	@echo '  run                 Run the server locally'
	@echo '  docker              Build Docker image'
	@echo '  docker-compose-up   Start all services with docker-compose'
	@echo '  docker-compose-down Stop all services'
	@echo '  migrate             Run database migrations'
	@echo '  lint                Run linter'
	@echo '  clean               Clean build artifacts'
