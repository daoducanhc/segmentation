# User Segmentation Engine

A high-performance, event-based user segmentation engine built with Go, Kratos framework, and ClickHouse.

## Features

- **Criteria Library**: Predefined criteria (A7, A30, PU, RFM, Platform, Country, Churned, etc.)
- **Flexible Segments**: Combine criteria with AND/OR/NOT logic
- **Real-time Ready**: Kafka integration for real-time event ingestion
- **High Performance**: ClickHouse-powered analytics for ~2M users
- **REST & gRPC APIs**: Dual-protocol support via Kratos

## Architecture

```
┌─────────────────┐     ┌─────────────────┐
│  ThinkingData   │     │     Kafka       │
│   (TD Sync)     │     │   (Real-time)   │
└────────┬────────┘     └────────┬────────┘
         │                       │
         └───────────┬───────────┘
                     ▼
         ┌───────────────────────┐
         │    events table       │  ReplacingMergeTree
         │  (deduplicated raw)   │  ORDER BY (app_id, user_id, event_name, event_time, revenue)
         └───────────┬───────────┘
                     │
                     ▼  Scheduler (every 10 min)
         ┌───────────────────────┐
         │  user_daily_activity  │  Aggregated by user + date
         └───────────┬───────────┘
                     │
         ┌───────────┴───────────┐
         ▼                       ▼
┌─────────────────┐    ┌─────────────────┐
│ activity_summary│    │     users       │
│  (A7/A30/PU)    │    │   (profiles)    │
└─────────────────┘    └─────────────────┘
         │                       │
         └───────────┬───────────┘
                     ▼
         ┌───────────────────────┐
         │   Segment Evaluation  │  SQL Generation + Execution
         └───────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.21+
- Docker & Docker Compose
- protoc (Protocol Buffer compiler)

### Setup

```bash
# 1. Clone and setup
git clone <repository>
cd segmentation
make init

# 2. Generate API code
make api

# 3. Start infrastructure (ClickHouse, Kafka)
make docker-compose-up

# 4. Initialize database schema
make migrate

# 5. Run the server
make run
```

**Endpoints:**
- REST API: http://localhost:8000
- gRPC: localhost:9000

## Configuration

### Main Config (`configs/config.yaml`)

```yaml
server:
  http:
    addr: 0.0.0.0:8000
  grpc:
    addr: 0.0.0.0:9000

data:
  clickhouse:
    addr: localhost:9100
    database: segmentation

# Aggregation refresh scheduler
scheduler:
  enabled: true
  incremental_refresh_minutes: 10  # How often to refresh
  incremental_days: 7              # Days to include
  full_refresh_hour: 3             # Daily full refresh at 3 AM

# RFM thresholds
rfm:
  currency: "VND"
  recency:
    low_max: 30
    high_min: 7
  frequency:
    low_max: 2
    high_min: 10
  monetary:
    low_max: 100000
    high_min: 2000000
```

### Environment Variables

Create `.env` file (not committed to git):

```bash
# ThinkingData VN
THINKINGDATA_VN_QUERY_URL=http://td-api.example.com:8992
THINKINGDATA_VN_QUERY_TOKEN=your_token_here

# ThinkingData Global
THINKINGDATA_GLOBAL_QUERY_URL=http://td-global.example.com:8992
THINKINGDATA_GLOBAL_QUERY_TOKEN=your_token_here
```

## Data Processing

### Deduplication Strategy

Events are deduplicated using ClickHouse's ReplacingMergeTree:

```sql
-- Composite key for deduplication
ORDER BY (app_id, user_id, event_name, event_time, revenue)
```

**Query-time deduplication** via `FINAL` modifier ensures accuracy without expensive `OPTIMIZE TABLE FINAL` operations.

### Scheduler Jobs

| Job | Frequency | Description |
|-----|-----------|-------------|
| Incremental Refresh | Every 10 min | Refresh last 7 days of data |
| Full Refresh | Daily 3 AM | Complete data refresh |

No `OPTIMIZE TABLE FINAL` needed - ClickHouse merges automatically in background.

### Timezone

All date calculations use **Vietnam timezone (Asia/Ho_Chi_Minh, GMT+7)**:

```sql
toDate(event_time, 'Asia/Ho_Chi_Minh') AS activity_date
```

## API Examples

### Create Segment

```bash
# A7 segment (active in last 7 days)
curl -X POST http://localhost:8000/v1/segments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "A7 Users",
    "definition": {
      "type": "SEGMENT_TYPE_DYNAMIC",
      "event_conditions": [{
        "lookback_days": 7,
        "count_operator": "COMPARISON_OPERATOR_GTE",
        "count_value": 1
      }]
    }
  }'
```

### Evaluate Segment

```bash
curl -X POST http://localhost:8000/v1/segments/{id}/evaluate \
  -d '{"limit": 1000}'
```

### Preview Segment

```bash
curl -X POST http://localhost:8000/v1/segments/preview \
  -d '{"definition": {...}, "limit": 100}'
```

## Segment Criteria Reference

| Criterion | Description | Performance |
|-----------|-------------|-------------|
| **A7, A30, A90** | Active in last N days | Sub-second |
| **PU7, PU30, PU90** | Paying user in last N days | Sub-second |
| **CHURNED_N** | No activity in N+ days | Sub-second |
| **Custom Activity** | Activity in custom window | 1-2 seconds |
| **PLATFORM** | User platform (app, web_mobile, web_pc) | Fast |
| **COUNTRY** | User country code | Fast |
| **RFM** | Recency/Frequency/Monetary buckets | Fast |
| **Event Properties** | Custom event filters | 5-10 seconds |

## Profile Array Fields

Profile fields (platform, country, language, os) support **multiple values** stored as comma-separated strings:

```sql
-- Query: platform = 'web_mobile'
has(splitByChar(',', platform), 'web_mobile')

-- Query: country IN ['VN', 'US']
hasAny(splitByChar(',', country), ['VN', 'US'])
```

## Project Structure

```
segmentation/
├── api/segment/v1/       # Protobuf definitions & generated code
├── cmd/server/           # Application entrypoint
├── configs/              # Configuration files
├── internal/
│   ├── conf/             # Config structs
│   ├── consumer/         # Kafka & ThinkingData consumers
│   ├── data/             # Repository layer (ClickHouse)
│   ├── engine/           # SQL generator & evaluator
│   ├── scheduler/        # Aggregation refresh scheduler
│   ├── server/           # HTTP & gRPC servers
│   └── service/          # Business logic
├── migrations/           # ClickHouse schema
├── pkg/                  # Shared utilities
└── third_party/          # Proto dependencies
```

## Development

```bash
# Build
make build

# Run tests
make test

# Generate API from proto
make api

# Run locally
make run

# Docker build
make docker
```

## Production Deployment

### Resource Requirements

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| Memory | 4GB | 8GB |
| CPU | 2 cores | 4 cores |
| Disk | 50GB | 100GB+ |

ClickHouse requirements:
- Memory: 8-16GB
- CPU: 4-8 cores
- Disk: SSD recommended

### Performance Expectations

| Scale | Query Performance |
|-------|-------------------|
| 2M users | All queries < 5 seconds |
| A7 segment (~100k users) | Sub-second |
| Complex composite segments | 3-5 seconds |

## License

MIT
