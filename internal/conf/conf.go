package conf

// Bootstrap holds all configuration
type Bootstrap struct {
	Server       *Server       `yaml:"server"`
	Data         *Data         `yaml:"data"`
	Kafka        *Kafka        `yaml:"kafka"`
	MySQL        *MySQL        `yaml:"mysql"`
	ThinkingData *ThinkingData `yaml:"thinkingdata"`
}

// Server configuration
type Server struct {
	HTTP *HTTPServer `yaml:"http"`
	GRPC *GRPCServer `yaml:"grpc"`
}

// HTTPServer configuration
type HTTPServer struct {
	Network string `yaml:"network"`
	Addr    string `yaml:"addr"`
	Timeout int64  `yaml:"timeout"` // seconds
}

// GRPCServer configuration
type GRPCServer struct {
	Network string `yaml:"network"`
	Addr    string `yaml:"addr"`
	Timeout int64  `yaml:"timeout"` // seconds
}

// Data configuration
type Data struct {
	Clickhouse *Clickhouse `yaml:"clickhouse"`
}

// Clickhouse configuration
type Clickhouse struct {
	Addr            string `yaml:"addr"`
	Database        string `yaml:"database"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	DialTimeout     int64  `yaml:"dial_timeout"` // seconds
	MaxOpenConns    int32  `yaml:"max_open_conns"`
	MaxIdleConns    int32  `yaml:"max_idle_conns"`
	ConnMaxLifetime int64  `yaml:"conn_max_lifetime"` // minutes
}

// Kafka configuration
type Kafka struct {
	Address  []string       `yaml:"address"`
	Producer *KafkaProducer `yaml:"producer"`
	Consumer *KafkaConsumer `yaml:"consumer"`
}

// KafkaProducer configuration
type KafkaProducer struct {
	Topics []string `yaml:"topics"`
}

// KafkaConsumer configuration
type KafkaConsumer struct {
	Offset        int64    `yaml:"offset"`
	Group         string   `yaml:"group"`
	Topics        []string `yaml:"topics"`
	UngroupTopics []string `yaml:"ungroup_topics"`
}

// MySQL configuration for sync
type MySQL struct {
	DSN                    string `yaml:"dsn"`
	MaxOpenConns           int32  `yaml:"max_open_conns"`
	MaxIdleConns           int32  `yaml:"max_idle_conns"`
	ConnMaxLifetimeMinutes int64  `yaml:"conn_max_lifetime_minutes"`
	SyncBatchSize          int32  `yaml:"sync_batch_size"`
	SyncIntervalMinutes    int32  `yaml:"sync_interval_minutes"`
}

// ThinkingData configuration for event sync
type ThinkingData struct {
	VN                  *TDEndpoint    `yaml:"vn"`
	Global              *TDEndpoint    `yaml:"global"`
	EventView           string         `yaml:"event_view"` // e.g., "v_event_8"
	SyncIntervalMinutes int32          `yaml:"sync_interval_minutes"`
	BatchSize           int32          `yaml:"batch_size"`
	LookbackDays        int32          `yaml:"lookback_days"`
	TimeoutSeconds      int32          `yaml:"timeout_seconds"`
	Events              []*TDEventSync `yaml:"events"` // Event types to sync
}

// TDEndpoint represents a ThinkingData API endpoint
type TDEndpoint struct {
	QueryURL   string `yaml:"query_url"`
	QueryToken string `yaml:"query_token"`
}

// TDEventSync defines how to sync a specific event type
type TDEventSync struct {
	EventName string   `yaml:"event_name"` // TD event name, e.g., "pay", "login"
	Fields    []string `yaml:"fields"`     // Additional fields to fetch besides default
	Enabled   bool     `yaml:"enabled"`
}
