FROM golang:1.21-alpine AS builder

ARG APP=segmentation
ARG VERSION=v1.0.0

RUN apk add --no-cache git make gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-w -s -X main.VERSION=${VERSION}" -o /segmentation ./cmd/server

FROM alpine:3.19

ARG APP=segmentation

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /segmentation .
COPY configs/ ./configs/
COPY migrations/ ./migrations/

EXPOSE 8000 9000

ENTRYPOINT ["./segmentation"]
CMD ["-conf", "./configs/config.yaml"]
