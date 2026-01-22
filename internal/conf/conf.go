// Package conf defines configuration structures for the application.
package conf

// Bootstrap holds all application configuration.
type Bootstrap struct {
	Server       *Server          `yaml:"server" json:"server"`
	Data         *Data            `yaml:"data" json:"data"`
	Kafka        *Kafka           `yaml:"kafka" json:"kafka"`
	ThinkingData *ThinkingData    `yaml:"thinkingdata" json:"thinkingdata"`
	RFM          *RFMConfig       `yaml:"rfm" json:"rfm"`
	Scheduler    *SchedulerConfig `yaml:"scheduler" json:"scheduler"`
}

// SchedulerConfig holds aggregation refresh scheduler configuration
type SchedulerConfig struct {
	Enabled                   bool  `yaml:"enabled" json:"enabled"`
	IncrementalRefreshMinutes int32 `yaml:"incremental_refresh_minutes" json:"incremental_refresh_minutes"` // How often to run incremental refresh (default: 10)
	IncrementalDays           int32 `yaml:"incremental_days" json:"incremental_days"`                       // Days to include in incremental (default: 7)
	FullRefreshHour           int32 `yaml:"full_refresh_hour" json:"full_refresh_hour"`                     // Hour (0-23) for daily full refresh (default: 3)
}

// RFMConfig holds RFM analysis thresholds
type RFMConfig struct {
	Currency  string        `yaml:"currency" json:"currency"`
	Recency   *RFMThreshold `yaml:"recency" json:"recency"`
	Frequency *RFMThreshold `yaml:"frequency" json:"frequency"`
	Monetary  *RFMThreshold `yaml:"monetary" json:"monetary"`
}

// RFMThreshold defines Low/Medium/High boundaries
type RFMThreshold struct {
	LowMax  float64 `yaml:"low_max" json:"low_max"`
	HighMin float64 `yaml:"high_min" json:"high_min"`
}

// Server configuration
type Server struct {
	HTTP *HTTPServer `yaml:"http" json:"http"`
	GRPC *GRPCServer `yaml:"grpc" json:"grpc"`
}

// HTTPServer configuration
type HTTPServer struct {
	Network string `yaml:"network" json:"network"`
	Addr    string `yaml:"addr" json:"addr"`
	Timeout int64  `yaml:"timeout" json:"timeout"`
}

// GRPCServer configuration
type GRPCServer struct {
	Network string `yaml:"network" json:"network"`
	Addr    string `yaml:"addr" json:"addr"`
	Timeout int64  `yaml:"timeout" json:"timeout"`
}

// Data configuration
type Data struct {
	Clickhouse *Clickhouse `yaml:"clickhouse" json:"clickhouse"`
}

// Clickhouse configuration
type Clickhouse struct {
	Addr            string `yaml:"addr" json:"addr"`
	Database        string `yaml:"database" json:"database"`
	Username        string `yaml:"username" json:"username"`
	Password        string `yaml:"password" json:"password"`
	DialTimeout     int64  `yaml:"dial_timeout" json:"dial_timeout"`
	MaxOpenConns    int32  `yaml:"max_open_conns" json:"max_open_conns"`
	MaxIdleConns    int32  `yaml:"max_idle_conns" json:"max_idle_conns"`
	ConnMaxLifetime int64  `yaml:"conn_max_lifetime" json:"conn_max_lifetime"`
}

// Kafka configuration
type Kafka struct {
	Address  []string       `yaml:"address" json:"address"`
	Producer *KafkaProducer `yaml:"producer" json:"producer"`
	Consumer *KafkaConsumer `yaml:"consumer" json:"consumer"`
}

// KafkaProducer configuration
type KafkaProducer struct {
	Topics []string `yaml:"topics" json:"topics"`
}

// KafkaConsumer configuration
type KafkaConsumer struct {
	Offset        int64    `yaml:"offset" json:"offset"`
	Group         string   `yaml:"group" json:"group"`
	Topics        []string `yaml:"topics" json:"topics"`
	UngroupTopics []string `yaml:"ungroup_topics" json:"ungroup_topics"`
}

// ThinkingData configuration for event sync
type ThinkingData struct {
	VN                *TDEndpoint    `yaml:"vn" json:"vn"`
	Global            *TDEndpoint    `yaml:"global" json:"global"`
	SyncIntervalHours int32          `yaml:"sync_interval_hours" json:"sync_interval_hours"`
	BatchSize         int32          `yaml:"batch_size" json:"batch_size"`
	LookbackDays      int32          `yaml:"lookback_days" json:"lookback_days"`
	TimeoutSeconds    int32          `yaml:"timeout_seconds" json:"timeout_seconds"`
	Events            []*TDEventSync `yaml:"events" json:"events"`
}

// TDEndpoint represents a ThinkingData API endpoint
type TDEndpoint struct {
	QueryURL   string `yaml:"query_url" json:"query_url"`
	QueryToken string `yaml:"query_token" json:"query_token"`
	EventView  string `yaml:"event_view" json:"event_view"`
}

// TDEventSync defines how to sync a specific event type
type TDEventSync struct {
	EventName string   `yaml:"event_name" json:"event_name"`
	Fields    []string `yaml:"fields" json:"fields"`
}
