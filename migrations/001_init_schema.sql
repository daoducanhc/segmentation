-- ClickHouse Schema for User Segmentation Engine
-- Optimized for ~2M users, event-based analytics

-- =============================================================================
-- DATABASE
-- =============================================================================
CREATE DATABASE IF NOT EXISTS segmentation;

-- =============================================================================
-- USERS TABLE
-- =============================================================================
-- Core user attributes synced from MySQL
CREATE TABLE IF NOT EXISTS segmentation.users
(
    user_id String,
    platform LowCardinality(String),           -- 'ios', 'android', 'web'
    country LowCardinality(String),
    language LowCardinality(String),
    device_type LowCardinality(String),
    app_version String,
    
    -- User lifecycle dates
    first_seen_at DateTime64(3),
    last_seen_at DateTime64(3),
    registered_at DateTime64(3) NULL,
    
    -- User status
    is_registered UInt8 DEFAULT 0,
    is_paying_user UInt8 DEFAULT 0,             -- PU flag
    
    -- Monetary metrics
    total_revenue Decimal64(4) DEFAULT 0,
    total_purchases UInt32 DEFAULT 0,
    
    -- Engagement metrics (updated periodically)
    lifetime_sessions UInt32 DEFAULT 0,
    lifetime_events UInt32 DEFAULT 0,
    
    -- Custom attributes (flexible JSON storage)
    custom_attributes String DEFAULT '{}',
    
    -- Metadata
    created_at DateTime64(3) DEFAULT now64(3),
    updated_at DateTime64(3) DEFAULT now64(3),
    
    -- Sign for ReplacingMergeTree versioning
    _version UInt64 DEFAULT toUnixTimestamp64Milli(now64(3))
)
ENGINE = ReplacingMergeTree(_version)
PARTITION BY toYYYYMM(first_seen_at)
ORDER BY user_id
SETTINGS index_granularity = 8192;

-- Index for common queries
ALTER TABLE segmentation.users ADD INDEX idx_platform platform TYPE set(10) GRANULARITY 4;
ALTER TABLE segmentation.users ADD INDEX idx_country country TYPE set(200) GRANULARITY 4;
ALTER TABLE segmentation.users ADD INDEX idx_is_pu is_paying_user TYPE set(2) GRANULARITY 4;

-- =============================================================================
-- EVENTS TABLE
-- =============================================================================
-- Event stream from Kafka/ThinkingData
CREATE TABLE IF NOT EXISTS segmentation.events
(
    event_id UUID DEFAULT generateUUIDv4(),
    user_id String,
    event_name LowCardinality(String),
    event_time DateTime64(3),
    
    -- Event context
    session_id String DEFAULT '',
    platform LowCardinality(String) DEFAULT '',
    app_version String DEFAULT '',
    
    -- Event properties (flexible JSON)
    properties String DEFAULT '{}',
    
    -- Revenue tracking
    revenue Decimal64(4) DEFAULT 0,
    currency LowCardinality(String) DEFAULT '',
    
    -- Processing metadata
    received_at DateTime64(3) DEFAULT now64(3),
    
    -- Partition key
    event_date Date DEFAULT toDate(event_time)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_name, user_id, event_time)
TTL event_date + INTERVAL 365 DAY
SETTINGS index_granularity = 8192;

-- Indexes for common query patterns
ALTER TABLE segmentation.events ADD INDEX idx_user_id user_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE segmentation.events ADD INDEX idx_event_name event_name TYPE set(1000) GRANULARITY 4;

-- =============================================================================
-- MATERIALIZED VIEW: Daily User Activity
-- =============================================================================
-- Pre-aggregated daily activity for A7/A30 calculations
CREATE TABLE IF NOT EXISTS segmentation.user_daily_activity
(
    user_id String,
    activity_date Date,
    platform LowCardinality(String),
    
    -- Activity metrics
    session_count UInt32,
    event_count UInt32,
    
    -- Revenue metrics
    revenue Decimal64(4),
    purchase_count UInt32,
    
    -- Event breakdown (top events)
    event_counts Map(LowCardinality(String), UInt32)
)
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(activity_date)
ORDER BY (user_id, activity_date, platform);

-- Materialized view to populate daily activity
CREATE MATERIALIZED VIEW IF NOT EXISTS segmentation.mv_user_daily_activity
TO segmentation.user_daily_activity
AS SELECT
    user_id,
    toDate(event_time) AS activity_date,
    platform,
    uniqExact(session_id) AS session_count,
    count() AS event_count,
    sum(revenue) AS revenue,
    countIf(revenue > 0) AS purchase_count,
    sumMap(map(event_name, toUInt32(1))) AS event_counts
FROM segmentation.events
GROUP BY user_id, activity_date, platform;

-- =============================================================================
-- MATERIALIZED VIEW: User Activity Summary (Rolling Windows)
-- =============================================================================
-- For quick A7/A30/churn lookups
CREATE TABLE IF NOT EXISTS segmentation.user_activity_summary
(
    user_id String,
    
    -- Last N days activity
    days_active_7d UInt8,
    days_active_30d UInt8,
    days_active_90d UInt8,
    
    -- Last activity
    last_activity_date Date,
    days_since_last_activity UInt16,
    
    -- Revenue windows
    revenue_7d Decimal64(4),
    revenue_30d Decimal64(4),
    
    -- Flags
    is_active_7d UInt8,     -- A7: active in last 7 days
    is_active_30d UInt8,    -- A30: active in last 30 days
    is_churned UInt8,       -- No activity in 30+ days
    
    -- Update tracking
    computed_at DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(computed_at)
ORDER BY user_id;

-- =============================================================================
-- SEGMENT DEFINITIONS TABLE
-- =============================================================================
-- Stores segment metadata and definitions
CREATE TABLE IF NOT EXISTS segmentation.segment_definitions
(
    id UUID,
    name String,
    description String DEFAULT '',
    segment_type LowCardinality(String),  -- 'static', 'dynamic', 'composite'
    
    -- JSON definition (matches protobuf SegmentDefinition)
    definition String,
    
    -- Generated SQL cache
    generated_sql String DEFAULT '',
    
    -- Metadata
    created_by String DEFAULT '',
    created_at DateTime64(3) DEFAULT now64(3),
    updated_at DateTime64(3) DEFAULT now64(3),
    is_active UInt8 DEFAULT 1,
    
    -- Cached results
    cached_count Int64 DEFAULT -1,
    last_evaluated DateTime64(3) NULL,
    
    _version UInt64 DEFAULT toUnixTimestamp64Milli(now64(3))
)
ENGINE = ReplacingMergeTree(_version)
ORDER BY id;

-- =============================================================================
-- SEGMENT RESULTS CACHE TABLE
-- =============================================================================
-- Caches evaluated segment user lists
CREATE TABLE IF NOT EXISTS segmentation.segment_results
(
    segment_id UUID,
    user_id String,
    added_at DateTime64(3) DEFAULT now64(3),
    evaluation_id UUID                       -- Links to specific evaluation run
)
ENGINE = ReplacingMergeTree(added_at)
PARTITION BY segment_id
ORDER BY (segment_id, user_id);

-- =============================================================================
-- SEGMENT EVALUATION HISTORY
-- =============================================================================
CREATE TABLE IF NOT EXISTS segmentation.segment_evaluations
(
    id UUID DEFAULT generateUUIDv4(),
    segment_id UUID,
    
    -- Results
    user_count Int64,
    execution_time_ms UInt32,
    generated_sql String,
    
    -- Status
    status LowCardinality(String),           -- 'success', 'error', 'timeout'
    error_message String DEFAULT '',
    
    evaluated_at DateTime64(3) DEFAULT now64(3),
    evaluated_by String DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(evaluated_at)
ORDER BY (segment_id, evaluated_at)
TTL evaluated_at + INTERVAL 90 DAY;

-- =============================================================================
-- KAFKA INTEGRATION (Optional - for event streaming)
-- =============================================================================
-- Kafka engine table for event ingestion
-- CREATE TABLE IF NOT EXISTS segmentation.events_kafka
-- (
--     event_id UUID,
--     user_id String,
--     event_name String,
--     event_time DateTime64(3),
--     session_id String,
--     platform String,
--     app_version String,
--     properties String,
--     revenue Decimal64(4),
--     currency String
-- )
-- ENGINE = Kafka
-- SETTINGS
--     kafka_broker_list = 'kafka:9092',
--     kafka_topic_list = 'user_events',
--     kafka_group_name = 'segmentation_consumer',
--     kafka_format = 'JSONEachRow',
--     kafka_max_block_size = 65536;

-- CREATE MATERIALIZED VIEW IF NOT EXISTS segmentation.events_kafka_mv
-- TO segmentation.events
-- AS SELECT
--     event_id,
--     user_id,
--     event_name,
--     event_time,
--     session_id,
--     platform,
--     app_version,
--     properties,
--     revenue,
--     currency,
--     now64(3) AS received_at,
--     toDate(event_time) AS event_date
-- FROM segmentation.events_kafka;

-- =============================================================================
-- HELPER VIEWS
-- =============================================================================

-- View: Active users in last 7 days (A7)
CREATE VIEW IF NOT EXISTS segmentation.v_active_7d AS
SELECT DISTINCT user_id
FROM segmentation.user_daily_activity
WHERE activity_date >= today() - 7;

-- View: Active users in last 30 days (A30)
CREATE VIEW IF NOT EXISTS segmentation.v_active_30d AS
SELECT DISTINCT user_id
FROM segmentation.user_daily_activity
WHERE activity_date >= today() - 30;

-- View: Paying users (PU)
CREATE VIEW IF NOT EXISTS segmentation.v_paying_users AS
SELECT user_id
FROM segmentation.users FINAL
WHERE is_paying_user = 1;

-- View: Churned users (no activity in 30+ days, but were active before)
CREATE VIEW IF NOT EXISTS segmentation.v_churned_users AS
SELECT u.user_id
FROM segmentation.users FINAL AS u
LEFT JOIN (
    SELECT DISTINCT user_id
    FROM segmentation.user_daily_activity
    WHERE activity_date >= today() - 30
) AS active ON u.user_id = active.user_id
WHERE active.user_id IS NULL
AND u.last_seen_at < now() - INTERVAL 30 DAY;
