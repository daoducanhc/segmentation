# User Segmentation Engine

High-performance user segmentation with Go, Kratos, and ClickHouse.

## Features

- Predefined criteria (A7, A30, PU, RFM, Platform, Country, Churned)
- AND/OR/NOT logic
- Kafka integration
- REST & gRPC APIs

## Quick Start

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

Note: Enums accept both string names and numbers. Examples use string names for readability.

### 1. Simple Segment: Active in Last 7 Days (A7)

```bash
curl -X POST http://localhost:8000/v1/segments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "A7 Users",
    "description": "Users active in last 7 days",
    "definition": {
      "type": "SEGMENT_TYPE_DYNAMIC",
      "userConditions": {
        "operator": "LOGICAL_OPERATOR_AND",
        "conditions": [
          {
            "field": "is_active_7d",
            "operator": "COMPARISON_OPERATOR_EQ",
            "value": {"stringValue": "1"}
          }
        ]
      }
    }
  }'
```

### 2. Nested Groups: (A7 AND PU) OR A30

Single segment with nested `groups`:

```bash
curl -X POST http://localhost:8000/v1/segments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "(A7 AND PU) OR Revenue>500K",
    "definition": {
      "type": 2,
      "userConditions": {
        "operator": 2,
        "groups": [
          {"operator": 1, "conditions": [
            {"field": "is_active_7d", "operator": 1, "value": {"boolValue": true}},
            {
              "field": "total_revenue",
              "operator": 5,
              "value": {"doubleValue": 500000}
            }
          ]},
          {"operator": 1, "conditions": [
            {
              "field": "total_revenue",
              "operator": 4,
              "value": {"doubleValue": 500000}
            }
          ]}
        ]
      }
    }
  }'
```

### 3. Event-Based Segment: Recent High-Value Buyers

**Business Logic**: Users who made 3+ purchases in last 30 days with total revenue > 500K VND

```bash
curl -X POST http://localhost:8000/v1/segments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Recent High-Value Buyers",
    "description": "3+ purchases in 30d, revenue > 500K",
    "definition": {
      "type": 2,
      "eventConditions": [
        {
          "eventName": "pay",
          "lookbackDays": 30,
          "countOperator": 4,
          "countValue": 3
        }
      ],
      "userConditions": {
        "operator": 1,
        "conditions": [
          {
            "field": "total_revenue",
            "operator": 4,
            "value": {"doubleValue": 500000}
          }
        ]
      },
      "overallLogic": 1
    }
  }'
```

### 4. Preview Segment (Test Before Saving)

```bash
curl -X POST http://localhost:8000/v1/segments/preview \
  -H "Content-Type: application/json" \
  -d '{
    "definition": {
      "type": 2,
      "userConditions": {
        "operator": 1,
        "conditions": [
          {
            "field": "country",
            "operator": 7,
            "value": {"stringList": {"values": ["ID", "US"]}}
          },
          {
            "field": "platform",
            "operator": 1,
            "value": {"stringValue": "web_mobile"}
          }
        ]
      }
    },
    "limit": 100
  }'
```

**Response:**
```json
{
  "user_ids": ["user_123", "user_456", ...],
  "total_count": 12450,
  "generated_sql": "SELECT user_id FROM..."
}
```

### 5. Evaluate Existing Segment

```bash
# Replace {id} with actual segment ID from create response
curl -X POST http://localhost:8000/v1/segments/3358b290-d851-4083-9ced-54996cacb1b4/evaluate \
  -H "Content-Type: application/json" \
  -d '{
    "limit": 1000,
    "offset": 0
  }'
```

**Response:**
```json
{
  "user_ids": ["user_1", "user_2", ...],
  "total_count": 45230,
  "evaluated_at": "2026-01-26T14:25:30Z",
  "generated_sql": "SELECT DISTINCT user_id FROM..."
}
```

## Segment Criteria Reference

### Predefined Lookback Windows: 1, 3, 7, 30, 90 days

| Field | Description | Table |
|-------|-------------|-------|
| **is_active_1d** | Active in last 1 day | user_activity_summary |
| **is_active_3d** | Active in last 3 days | user_activity_summary |
| **is_active_7d** | Active in last 7 days | user_activity_summary |
| **is_active_30d** | Active in last 30 days | user_activity_summary |
| **is_active_90d** | Active in last 90 days | user_activity_summary |
| **is_pu_1d** | Made purchase in last 1 day | user_activity_summary |
| **is_pu_3d** | Made purchase in last 3 days | user_activity_summary |
| **is_pu_7d** | Made purchase in last 7 days | user_activity_summary |
| **is_pu_30d** | Made purchase in last 30 days | user_activity_summary |
| **is_pu_90d** | Made purchase in last 90 days | user_activity_summary |
| **is_churned_1d** | No activity in 1+ days | user_activity_summary |
| **is_churned_3d** | No activity in 3+ days | user_activity_summary |
| **is_churned_7d** | No activity in 7+ days | user_activity_summary |
| **is_churned_30d** | No activity in 30+ days | user_activity_summary |
| **is_churned_90d** | No activity in 90+ days | user_activity_summary |

### Other Criteria

| Criterion | Description | Performance |
|-----------|-------------|-------------|
| **total_revenue** | User's total revenue | Fast (users table) |
| **platform** | User platform (app, web_mobile, web_pc) | Fast |
| **country** | User country code | Fast |
| **Custom Activity** | Activity in custom window (eventConditions) | 1-2 seconds |
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

# Generate API from proto
make api

# Run locally
make run

# All
make all
```

## License

MIT
