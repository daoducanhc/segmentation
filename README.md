# User Segmentation Engine

A high-performance, event-based user segmentation engine built with Go, Kratos framework, and ClickHouse.

## Features

- **Criteria Library**: Predefined criteria (A7, A30, PU, Platform, Country, Churned, etc.)
- **Flexible Segments**: Combine criteria with AND/OR/NOT logic to create segments
- **Parent-Child Segments**: Composite segments that combine multiple child segments
- **High Performance**: ClickHouse-powered analytics for ~2M users
- **Event Streaming**: Kafka integration for real-time event ingestion
- **Data Sync**: MySQL synchronization for user data
- **REST & gRPC APIs**: Dual-protocol support via Kratos

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Clients                                  │
│                    (REST / gRPC API)                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Kratos Server                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ HTTP Server │  │ gRPC Server │  │    Segment Service      │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Segment Engine                              │
│  ┌────────────────┐  ┌────────────────┐  ┌──────────────────┐   │
│  │ SQL Generator  │  │   Evaluator    │  │ Criteria Library │   │
│  └────────────────┘  └────────────────┘  └──────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Data Layer                                │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                     ClickHouse                            │   │
│  │  - users (ReplacingMergeTree)                            │   │
│  │  - events (MergeTree, partitioned by month)              │   │
│  │  - user_daily_activity (SummingMergeTree)                │   │
│  │  - segment_definitions (ReplacingMergeTree)              │   │
│  │  - segment_results (ReplacingMergeTree)                  │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              │
       ┌──────────────────────┼──────────────────────┐
       ▼                      ▼                      ▼
┌─────────────┐       ┌─────────────┐       ┌─────────────┐
│    Kafka    │       │    MySQL    │       │ ThinkingData│
│   Events    │       │  User Sync  │       │   (Future)  │
└─────────────┘       └─────────────┘       └─────────────┘
```

## Quick Start

### Prerequisites

- Go 1.21+
- Docker & Docker Compose
- protoc (Protocol Buffer compiler)

### Installation

1. Clone and setup:
```bash
cd segmentation
make init
```

2. Generate API code:
```bash
make api
```

3. Start infrastructure:
```bash
make docker-compose-up
```

4. Run migrations:
```bash
make migrate
```

5. Run the server:
```bash
make run
```

## API Usage

### Create a Segment

```bash
# Create A7 segment (active in last 7 days)
curl -X POST http://localhost:8000/v1/segments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "A7 Users",
    "description": "Users active in the last 7 days",
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

### Create Composite Segment

```bash
# A7 AND PU (active paying users)
curl -X POST http://localhost:8000/v1/segments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Active Paying Users",
    "description": "Paying users active in the last 7 days",
    "definition": {
      "type": "SEGMENT_TYPE_COMPOSITE",
      "child_segments": [
        {"segment_id": "a7-segment-id"},
        {"segment_id": "pu-segment-id"}
      ],
      "child_logic": "LOGICAL_OPERATOR_AND"
    }
  }'
```

### Complex Segment with NOT

```bash
# A30 AND NOT churned (recently active users)
curl -X POST http://localhost:8000/v1/segments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Recently Active Non-Churned",
    "definition": {
      "type": "SEGMENT_TYPE_COMPOSITE",
      "child_segments": [
        {"segment_id": "a30-segment-id", "negated": false},
        {"segment_id": "churned-segment-id", "negated": true}
      ],
      "child_logic": "LOGICAL_OPERATOR_AND"
    }
  }'
```

### Evaluate Segment

```bash
# Get user IDs for a segment
curl -X POST http://localhost:8000/v1/segments/{id}/evaluate \
  -H "Content-Type: application/json" \
  -d '{
    "limit": 1000,
    "offset": 0,
    "force_refresh": false
  }'
```

### Preview Segment

```bash
# Preview without saving
curl -X POST http://localhost:8000/v1/segments/preview \
  -H "Content-Type: application/json" \
  -d '{
    "definition": {
      "type": "SEGMENT_TYPE_DYNAMIC",
      "user_conditions": {
        "operator": "LOGICAL_OPERATOR_AND",
        "conditions": [{
          "field": "platform",
          "operator": "COMPARISON_OPERATOR_EQ",
          "value": {"string_value": "ios"}
        }]
      }
    },
    "limit": 100
  }'
```

## Segment Definition Examples

### User Attribute Conditions

```json
{
  "type": "SEGMENT_TYPE_DYNAMIC",
  "user_conditions": {
    "operator": "LOGICAL_OPERATOR_AND",
    "conditions": [
      {
        "field": "platform",
        "operator": "COMPARISON_OPERATOR_IN",
        "value": {"string_list": {"values": ["ios", "android"]}}
      },
      {
        "field": "total_revenue",
        "operator": "COMPARISON_OPERATOR_GTE",
        "value": {"double_value": 50.0}
      }
    ]
  }
}
```

### Event-Based Conditions

```json
{
  "type": "SEGMENT_TYPE_DYNAMIC",
  "event_conditions": [
    {
      "event_name": "purchase",
      "lookback_days": 30,
      "count_operator": "COMPARISON_OPERATOR_GTE",
      "count_value": 3
    },
    {
      "event_name": "app_open",
      "lookback_days": 7,
      "count_operator": "COMPARISON_OPERATOR_GTE",
      "count_value": 5
    }
  ],
  "event_logic": "LOGICAL_OPERATOR_AND"
}
```

### Combined Conditions

```json
{
  "type": "SEGMENT_TYPE_DYNAMIC",
  "user_conditions": {
    "operator": "LOGICAL_OPERATOR_AND",
    "conditions": [
      {
        "field": "is_paying_user",
        "operator": "COMPARISON_OPERATOR_EQ",
        "value": {"int_value": 1}
      }
    ]
  },
  "event_conditions": [
    {
      "lookback_days": 7,
      "count_operator": "COMPARISON_OPERATOR_GTE",
      "count_value": 1
    }
  ],
  "overall_logic": "LOGICAL_OPERATOR_AND"
}
```

## Criteria vs Segments

**Criteria** are building blocks (characteristics) that define user attributes:

| Criterion | Description |
|-----------|-------------|
| A7 | Active in last 7 days |
| A30 | Active in last 30 days |
| PU | Paying Users |
| NPU | Non-Paying Users |
| CHURNED | No activity in N+ days |
| HIGH_VALUE | Revenue > threshold |
| NEW_USERS | Registered in last N days |
| PLATFORM | Users on specific platform (ios, android, web) |
| COUNTRY | Users from specific country |

**Segments** are combinations of criteria using AND/OR/NOT logic:

```go
// Example: Active paying iOS users
builder.BuildSegmentAND(
    criteria.A7(),           // Active in 7 days
    criteria.PayingUsers(),  // PU
    criteria.Platform("ios"), // iOS platform
)

// Example: Mobile users (iOS OR Android)
builder.BuildSegmentOR(
    criteria.Platform("ios"),
    criteria.Platform("android"),
)

// Example: Non-active users (NOT A30)
builder.BuildSegmentNOT(criteria.A30())
```

## Configuration

Edit `configs/config.yaml`:

```yaml
server:
  http:
    addr: 0.0.0.0:8000
  grpc:
    addr: 0.0.0.0:9000

data:
  clickhouse:
    addr: clickhouse:9000
    database: segmentation
    max_open_conns: 10

kafka:
  brokers:
    - kafka:9092
  topic: user_events
  batch_size: 1000

mysql:
  dsn: "user:password@tcp(mysql:3306)/app"
  sync_interval_minutes: 5
```

## Scale Considerations

- **~2M users**: ClickHouse handles this scale easily
- **A7 ≈ 100k, A30 ≈ 400k**: Pre-computed materialized views
- **Low concurrency (3-5 operators)**: No special optimization needed
- **Event ingestion**: Kafka batching with configurable batch size

## Performance Tips

1. Use `force_refresh: false` to leverage cached results
2. Composite segments cache child results
3. Materialized views pre-aggregate daily activity
4. Segment evaluation results are cached for 5 minutes

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Build binary
make build

# Build Docker image
make docker
```

## License

MIT
