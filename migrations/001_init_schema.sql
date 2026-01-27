-- ClickHouse Schema for User Segmentation Engine
-- Optimized for ~2M users, event-based analytics

-- =============================================================================
-- DATABASE
-- =============================================================================
CREATE DATABASE IF NOT EXISTS segmentation;

-- =============================================================================
-- USERS TABLE
-- =============================================================================
-- Core user attributes aggregated from events
CREATE TABLE IF NOT EXISTS segmentation.users
(
    user_id String,
    platform LowCardinality(String),           -- 'web_mobile', 'web_pc', 'app'
    country LowCardinality(String),
    language LowCardinality(String),
    os LowCardinality(String),                 -- Operating system (iOS, Android, etc.)
    
    -- User lifecycle dates
    first_seen_at DateTime64(3),
    last_seen_at DateTime64(3),
    registered_at DateTime64(3) NULL,
    
    -- User status
    is_registered UInt8 DEFAULT 0,
    is_paying_user UInt8 DEFAULT 0,             -- PU flag
    
    -- Monetary metrics
    total_revenue Float64 DEFAULT 0,
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
-- Event stream from ThinkingData
-- Optimized for Phase 1 criteria: Activity, Monetization, RFM, Profile
-- Uses ReplacingMergeTree for deduplication (TD fetch + Kafka can have duplicates)
CREATE TABLE IF NOT EXISTS segmentation.events
(
    -- Composite unique key: user + app + event + time + revenue
    user_id String,
    app_id LowCardinality(String),            -- Game/app identifier
    event_name LowCardinality(String),        -- e.g., 'app_page_view', 'pay', 'app_vip_level_up'
    event_time DateTime64(3),
    
    -- Profile/Demographic (for profile criteria)
    platform LowCardinality(String) DEFAULT '',      -- 'web_mobile', 'web_pc', 'app' (from plt_type)
    country LowCardinality(String) DEFAULT '',       -- Country code (from #country_code)
    language LowCardinality(String) DEFAULT '',      -- User language (from #system_language[:2])
    os LowCardinality(String) DEFAULT '',            -- Operating system (from #os)
    
    -- Monetization (for PU, RFM criteria)
    revenue Float64 DEFAULT 0,                       -- Payment amount (VND or base currency)
    currency LowCardinality(String) DEFAULT '',      -- Original currency
    payment_channel LowCardinality(String) DEFAULT '', -- 'webshop', 'google', '3rd_party', 'apple'
    vip_level UInt8 DEFAULT 0,                       -- Current VIP level (from app_vip_level_up)
    
    -- Flexible storage for other event-specific data
    properties String DEFAULT '{}',
    
    -- Processing metadata
    received_at DateTime64(3) DEFAULT now64(3),
    
    -- Partition key
    event_date Date DEFAULT toDate(event_time)
)
ENGINE = ReplacingMergeTree(received_at)
PARTITION BY toYYYYMM(event_date)
ORDER BY (app_id, user_id, event_name, event_time, revenue)
TTL event_date + INTERVAL 365 DAY
SETTINGS index_granularity = 8192;

-- Indexes for common query patterns
ALTER TABLE segmentation.events ADD INDEX idx_user_id user_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE segmentation.events ADD INDEX idx_event_name event_name TYPE set(100) GRANULARITY 4;
ALTER TABLE segmentation.events ADD INDEX idx_platform platform TYPE set(10) GRANULARITY 4;
ALTER TABLE segmentation.events ADD INDEX idx_country country TYPE set(300) GRANULARITY 4;
ALTER TABLE segmentation.events ADD INDEX idx_payment_channel payment_channel TYPE set(5) GRANULARITY 4;

-- =============================================================================
-- DAILY USER ACTIVITY TABLE
-- =============================================================================
-- Pre-aggregated daily activity for A1/A3/A7/A30 and RFM calculations
-- NOTE: Populated via scheduled job (not MV) to ensure deduplication from events table
CREATE TABLE IF NOT EXISTS segmentation.user_daily_activity
(
    user_id String,
    app_id LowCardinality(String),
    activity_date Date,
    
    -- Profile (latest per day)
    platform LowCardinality(String),
    country LowCardinality(String),
    language LowCardinality(String),
    os LowCardinality(String),
    
    -- Activity metrics
    login_count UInt32,                       -- For activity criteria
    event_count UInt32,
    
    -- Monetization metrics (for PU, RFM)
    revenue Float64,                          -- Total spend that day
    purchase_count UInt32,                    -- Number of purchases
    webshop_purchase_count UInt32,            -- Purchases via webshop
    google_purchase_count UInt32,             -- Purchases via Google Play
    apple_purchase_count UInt32,              -- Purchases via Apple App Store
    third_party_purchase_count UInt32,        -- Purchases via 3rd party (ZaloPay/Dana)
    max_vip_level UInt8,                      -- Highest VIP level reached
    
    -- Event breakdown
    event_counts Map(LowCardinality(String), UInt32),
    
    -- Update tracking
    updated_at DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(updated_at)
PARTITION BY toYYYYMM(activity_date)
ORDER BY (app_id, user_id, activity_date);

-- =============================================================================
-- REFRESH DAILY ACTIVITY PROCEDURE
-- =============================================================================
-- Run this after each TD sync or Kafka batch to rebuild daily aggregates
-- Uses FINAL to read deduplicated events
-- 
-- Example usage (run via scheduled job or after sync):
-- INSERT INTO segmentation.user_daily_activity
-- SELECT ... FROM segmentation.events FINAL WHERE event_date >= today() - 7
-- GROUP BY user_id, app_id, toDate(event_time);
--
-- Or use the helper view below with FINAL modifier

-- =============================================================================
-- MATERIALIZED VIEW: User Activity Summary (Rolling Windows)
-- =============================================================================
-- For quick A1/A3/A7/A30/A90 and churn lookups
-- Predefined lookback windows: 1, 3, 7, 30, 90 days
CREATE TABLE IF NOT EXISTS segmentation.user_activity_summary
(
    user_id String,
    
    -- Activity flags (predefined lookback windows: 1, 3, 7, 30, 90)
    is_active_1d UInt8,     -- A1: had activity in last 1 day
    is_active_3d UInt8,     -- A3: had activity in last 3 days
    is_active_7d UInt8,     -- A7: had activity in last 7 days
    is_active_30d UInt8,    -- A30: had activity in last 30 days
    is_active_90d UInt8,    -- A90: had activity in last 90 days
    
    -- Paying user flags (predefined lookback windows: 1, 3, 7, 30, 90)
    is_pu_1d UInt8,         -- PU1: made purchase in last 1 day
    is_pu_3d UInt8,         -- PU3: made purchase in last 3 days
    is_pu_7d UInt8,         -- PU7: made purchase in last 7 days
    is_pu_30d UInt8,        -- PU30: made purchase in last 30 days
    is_pu_90d UInt8,        -- PU90: made purchase in last 90 days
    
    -- Churn flags (predefined inactivity windows: 1, 3, 7, 30, 90)
    is_churned_1d UInt8,    -- No activity in 1+ days
    is_churned_3d UInt8,    -- No activity in 3+ days
    is_churned_7d UInt8,    -- No activity in 7+ days
    is_churned_30d UInt8,   -- No activity in 30+ days
    is_churned_90d UInt8,   -- No activity in 90+ days
    
    -- Update tracking
    computed_at DateTime64(3) DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree(computed_at)
ORDER BY user_id;

-- =============================================================================
-- SEGMENT DEFINITIONS TABLE
-- =============================================================================
-- Stores segment metadata and definitions
-- NOTE: Segments are combinations of criteria (A7, A30, PU, etc.) using AND/OR/NOT logic
CREATE TABLE IF NOT EXISTS segmentation.segment_definitions
(
    id UUID,
    name String,
    description String DEFAULT '',
    segment_type LowCardinality(String),  -- 'static', 'dynamic', 'composite'
    
    -- JSON definition (matches protobuf SegmentDefinition)
    -- Contains criteria combinations with AND/OR/NOT logic
    definition String,
    
    -- Generated SQL cache
    generated_sql String DEFAULT '',
    
    -- Metadata
    created_by String DEFAULT '',
    created_at DateTime64(3) DEFAULT now64(3),
    updated_at DateTime64(3) DEFAULT now64(3),
    is_active UInt8 DEFAULT 1,
    
    -- Expiration: segment becomes inactive after this date
    -- NULL means never expires
    expires_at DateTime64(3) NULL,
    
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
TTL toDateTime(evaluated_at) + INTERVAL 90 DAY;

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
--     revenue Float64,
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
-- HELPER VIEWS FOR PHASE 1 CRITERIA
-- =============================================================================

-- ============ ACTIVITY CRITERIA ============

-- View: Active users in last 1 day (A1)
CREATE VIEW IF NOT EXISTS segmentation.v_active_1d AS
SELECT DISTINCT user_id, app_id
FROM segmentation.user_daily_activity
WHERE activity_date >= today() - 1;

-- View: Active users in last 3 days (A3)
CREATE VIEW IF NOT EXISTS segmentation.v_active_3d AS
SELECT DISTINCT user_id, app_id
FROM segmentation.user_daily_activity
WHERE activity_date >= today() - 3;

-- View: Active users in last 7 days (A7)
CREATE VIEW IF NOT EXISTS segmentation.v_active_7d AS
SELECT DISTINCT user_id, app_id
FROM segmentation.user_daily_activity
WHERE activity_date >= today() - 7;

-- View: Active users in last 30 days (A30)
CREATE VIEW IF NOT EXISTS segmentation.v_active_30d AS
SELECT DISTINCT user_id, app_id
FROM segmentation.user_daily_activity
WHERE activity_date >= today() - 30;

-- View: Churned users (no activity in 30+ days)
CREATE VIEW IF NOT EXISTS segmentation.v_churned_users AS
SELECT u.user_id, u.app_id
FROM (
    SELECT user_id, app_id, max(activity_date) as last_active
    FROM segmentation.user_daily_activity
    GROUP BY user_id, app_id
) u
WHERE u.last_active < today() - 30;

-- ============ MONETIZATION CRITERIA ============

-- View: Paying users (at least 1 purchase ever)
CREATE VIEW IF NOT EXISTS segmentation.v_paying_users AS
SELECT user_id, app_id, sum(revenue) as total_revenue, sum(purchase_count) as total_purchases
FROM segmentation.user_daily_activity
WHERE purchase_count > 0
GROUP BY user_id, app_id;

-- View: Recent paying users (purchase in last N days - parameterized via query)
CREATE VIEW IF NOT EXISTS segmentation.v_recent_payers AS
SELECT user_id, app_id, sum(revenue) as revenue_period, sum(purchase_count) as purchases_period
FROM segmentation.user_daily_activity
WHERE purchase_count > 0
GROUP BY user_id, app_id;

-- View: Payment channel breakdown (4 channels: webshop, google, apple, 3rd_party)
CREATE VIEW IF NOT EXISTS segmentation.v_payment_channels AS
SELECT 
    user_id, 
    app_id,
    sum(webshop_purchase_count) as webshop_purchases,
    sum(google_purchase_count) as google_purchases,
    sum(apple_purchase_count) as apple_purchases,
    sum(third_party_purchase_count) as third_party_purchases,
    -- Primary channel is the one with most purchases
    arrayElement(
        ['webshop', 'google', 'apple', '3rd_party'],
        indexOf(
            [sum(webshop_purchase_count), sum(google_purchase_count), sum(apple_purchase_count), sum(third_party_purchase_count)],
            greatest(sum(webshop_purchase_count), sum(google_purchase_count), sum(apple_purchase_count), sum(third_party_purchase_count))
        )
    ) as primary_channel
FROM segmentation.user_daily_activity
GROUP BY user_id, app_id;

-- View: VIP users
CREATE VIEW IF NOT EXISTS segmentation.v_vip_users AS
SELECT user_id, app_id, max(max_vip_level) as vip_level
FROM segmentation.user_daily_activity
WHERE max_vip_level > 0
GROUP BY user_id, app_id;

-- ============ RFM CRITERIA ============

-- View: RFM base data for bucketing
CREATE VIEW IF NOT EXISTS segmentation.v_rfm_base AS
SELECT 
    user_id,
    app_id,
    -- Recency: days since last purchase
    dateDiff('day', max(activity_date), today()) as days_since_purchase,
    -- Frequency: total purchase count
    sum(purchase_count) as total_purchases,
    -- Monetary: total revenue
    sum(revenue) as total_revenue
FROM segmentation.user_daily_activity
WHERE purchase_count > 0
GROUP BY user_id, app_id;

-- ============ PROFILE CRITERIA ============

-- View: User latest profile (most recent activity's profile data)
CREATE VIEW IF NOT EXISTS segmentation.v_user_profiles AS
SELECT 
    user_id,
    app_id,
    -- All unique values seen (historical)
    groupArray(DISTINCT platform) as platforms,
    groupArray(DISTINCT country) as countries,
    groupArray(DISTINCT language) as languages,
    groupArray(DISTINCT os) as os_list,
    -- Time ranges
    min(activity_date) as first_active_date,
    max(activity_date) as last_active_date,
    dateDiff('day', min(activity_date), today()) as account_age_days
FROM segmentation.user_daily_activity
GROUP BY user_id, app_id;
