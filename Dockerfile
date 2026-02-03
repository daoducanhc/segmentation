# Build frontend
FROM node:18-alpine AS frontend-builder

WORKDIR /usr/src/app/
USER root

COPY frontend/ ./
RUN npm install --silent --no-cache
RUN npm run build

# Build backend
FROM golang:1.21-alpine AS backend-builder

ARG APP=segmentation
ARG VERSION=v1.0.0

RUN apk add --no-cache git gcc musl-dev

WORKDIR /go/src/${APP}
COPY backend/ .

RUN CGO_ENABLED=0 go build -ldflags "-w -s -X main.VERSION=${VERSION}" -o ./${APP} ./cmd/server

# Final image
FROM alpine:3.20

ARG APP=segmentation

RUN apk update && apk upgrade && \
    apk --update --no-cache add tzdata ca-certificates

WORKDIR /app

# Copy backend
COPY --from=backend-builder /go/src/${APP}/${APP} /usr/bin/
COPY --from=backend-builder /go/src/${APP}/configs /usr/bin/configs
COPY --from=backend-builder /go/src/${APP}/migrations /usr/bin/migrations

# Copy frontend
COPY --from=frontend-builder /usr/src/app/dist /usr/bin/dist

# Copy entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8000 9000

ENTRYPOINT ["/entrypoint.sh"]
