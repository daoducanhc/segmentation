package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"github.com/spf13/cast"

	"segmentation/internal/conf"
	"segmentation/internal/data"
)

// TDResponseHeader represents ThinkingData query response header
type TDResponseHeader struct {
	ReturnCode    int32    `json:"return_code"`
	ReturnMessage string   `json:"return_message"`
	Headers       []string `json:"headers"`
}

// ThinkingDataSync handles syncing events from ThinkingData to ClickHouse
type ThinkingDataSync struct {
	config    *conf.ThinkingData
	eventRepo *data.EventRepo
	logger    *log.Helper
	client    *req.Client

	mu           sync.RWMutex
	lastSyncTime map[string]time.Time // per event type
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

// NewThinkingDataSync creates a new ThinkingData sync consumer
func NewThinkingDataSync(config *conf.ThinkingData, eventRepo *data.EventRepo, logger log.Logger) *ThinkingDataSync {
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &ThinkingDataSync{
		config:       config,
		eventRepo:    eventRepo,
		logger:       log.NewHelper(log.With(logger, "module", "consumer/thinkingdata")),
		client:       req.C().SetTimeout(timeout),
		lastSyncTime: make(map[string]time.Time),
		stopChan:     make(chan struct{}),
	}
}

// Start begins the periodic sync process
func (t *ThinkingDataSync) Start(ctx context.Context) error {
	t.logger.Info("starting ThinkingData sync")

	// Initial sync with lookback
	for _, eventConf := range t.config.Events {
		if eventConf.Enabled {
			t.lastSyncTime[eventConf.EventName] = time.Now().AddDate(0, 0, -int(t.config.LookbackDays))
		}
	}

	// Start periodic sync
	t.wg.Add(1)
	go t.syncLoop(ctx)

	return nil
}

// Stop gracefully stops the sync process
func (t *ThinkingDataSync) Stop() {
	t.logger.Info("stopping ThinkingData sync")
	close(t.stopChan)
	t.wg.Wait()
}

func (t *ThinkingDataSync) syncLoop(ctx context.Context) {
	defer t.wg.Done()

	interval := time.Duration(t.config.SyncIntervalMinutes) * time.Minute
	if interval == 0 {
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	t.runSync(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stopChan:
			return
		case <-ticker.C:
			t.runSync(ctx)
		}
	}
}

func (t *ThinkingDataSync) runSync(ctx context.Context) {
	for _, eventConf := range t.config.Events {
		if !eventConf.Enabled {
			continue
		}

		t.logger.Infof("syncing event: %s", eventConf.EventName)

		// Sync from VN endpoint
		if t.config.VN != nil && t.config.VN.QueryURL != "" {
			if err := t.syncEventFromEndpoint(ctx, t.config.VN, eventConf, "vn"); err != nil {
				t.logger.Errorf("failed to sync %s from VN: %v", eventConf.EventName, err)
			}
		}

		// Sync from Global endpoint
		if t.config.Global != nil && t.config.Global.QueryURL != "" {
			if err := t.syncEventFromEndpoint(ctx, t.config.Global, eventConf, "global"); err != nil {
				t.logger.Errorf("failed to sync %s from Global: %v", eventConf.EventName, err)
			}
		}

		// Update last sync time
		t.mu.Lock()
		t.lastSyncTime[eventConf.EventName] = time.Now()
		t.mu.Unlock()
	}
}

func (t *ThinkingDataSync) syncEventFromEndpoint(
	ctx context.Context,
	endpoint *conf.TDEndpoint,
	eventConf *conf.TDEventSync,
	region string,
) error {
	t.mu.RLock()
	lastSync := t.lastSyncTime[eventConf.EventName]
	t.mu.RUnlock()

	// Build query
	query := t.buildQuery(eventConf, lastSync)
	t.logger.Debugf("executing TD query for %s: %s", eventConf.EventName, query)

	// Execute query
	resp, err := t.executeQuery(ctx, endpoint, query)
	if err != nil {
		return fmt.Errorf("query execution failed: %w", err)
	}

	// Parse response
	header, rows, err := t.parseResponse(resp)
	if err != nil {
		return fmt.Errorf("response parsing failed: %w", err)
	}

	if header.ReturnCode != 0 {
		return fmt.Errorf("TD returned error: %s (code: %d)", header.ReturnMessage, header.ReturnCode)
	}

	if len(rows) == 0 {
		t.logger.Infof("no new events for %s from %s", eventConf.EventName, region)
		return nil
	}

	t.logger.Infof("fetched %d events for %s from %s", len(rows), eventConf.EventName, region)

	// Transform and insert events
	events := t.transformEvents(header.Headers, rows, eventConf.EventName, region)
	if len(events) == 0 {
		return nil
	}

	// Batch insert
	for i := 0; i < len(events); i += int(t.config.BatchSize) {
		end := i + int(t.config.BatchSize)
		if end > len(events) {
			end = len(events)
		}

		batch := events[i:end]
		if err := t.eventRepo.InsertBatch(ctx, batch); err != nil {
			t.logger.Errorf("failed to insert batch: %v", err)
			continue
		}

		t.logger.Debugf("inserted %d events", len(batch))
	}

	return nil
}

func (t *ThinkingDataSync) buildQuery(eventConf *conf.TDEventSync, since time.Time) string {
	// Base fields that are always fetched
	baseFields := []string{
		`"#account_id"`,
		`"#event_id"`,
		`"#event_time"`,
	}

	// Add configured fields
	for _, f := range eventConf.Fields {
		baseFields = append(baseFields, fmt.Sprintf(`"%s"`, f))
	}

	fields := strings.Join(baseFields, ", ")

	// Format date for TD query
	dateFilter := since.Format("2006-01-02")

	query := fmt.Sprintf(
		`SELECT %s FROM %s WHERE "#event_name"='%s' AND "$part_date" >= '%s'`,
		fields,
		t.config.EventView,
		eventConf.EventName,
		dateFilter,
	)

	return query
}

func (t *ThinkingDataSync) executeQuery(ctx context.Context, endpoint *conf.TDEndpoint, query string) (string, error) {
	resp, err := t.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]interface{}{
			"token": endpoint.QueryToken,
			"sql":   query,
		}).
		Post(endpoint.QueryURL)

	if err != nil {
		return "", err
	}

	if !resp.IsSuccessState() {
		return "", fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	return resp.String(), nil
}

func (t *ThinkingDataSync) parseResponse(resp string) (*TDResponseHeader, [][]interface{}, error) {
	// TD response format: first line is header JSON, remaining lines are data
	lines := strings.SplitN(resp, "\n", 2)
	if len(lines) == 0 {
		return nil, nil, fmt.Errorf("empty response")
	}

	// Parse header
	var header TDResponseHeader
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		return nil, nil, fmt.Errorf("failed to parse header: %w", err)
	}

	if len(lines) < 2 || strings.TrimSpace(lines[1]) == "" {
		return &header, nil, nil
	}

	// Parse data rows
	var rows [][]interface{}
	dataLines := strings.Split(strings.TrimSpace(lines[1]), "\n")
	for _, line := range dataLines {
		if line == "" {
			continue
		}

		var row []interface{}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.logger.Warnf("failed to parse row: %s, error: %v", line, err)
			continue
		}
		rows = append(rows, row)
	}

	return &header, rows, nil
}

func (t *ThinkingDataSync) transformEvents(headers []string, rows [][]interface{}, eventName, region string) []*data.Event {
	// Build header index map
	headerIdx := make(map[string]int)
	for i, h := range headers {
		headerIdx[h] = i
	}

	var events []*data.Event
	for _, row := range rows {
		event, err := t.rowToEvent(headerIdx, row, eventName, region)
		if err != nil {
			t.logger.Warnf("failed to transform row: %v", err)
			continue
		}
		events = append(events, event)
	}

	return events
}

func (t *ThinkingDataSync) rowToEvent(headerIdx map[string]int, row []interface{}, eventName, region string) (*data.Event, error) {
	getValue := func(key string) interface{} {
		if idx, ok := headerIdx[key]; ok && idx < len(row) {
			return row[idx]
		}
		return nil
	}

	// Required fields
	accountID := cast.ToString(getValue("#account_id"))
	if accountID == "" {
		return nil, fmt.Errorf("missing account_id")
	}

	eventID := cast.ToString(getValue("#event_id"))
	if eventID == "" {
		eventID = uuid.New().String()
	}

	// Parse event time
	eventTimeRaw := getValue("#event_time")
	var eventTime time.Time
	switch v := eventTimeRaw.(type) {
	case string:
		// Try parsing various formats
		formats := []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05.000",
		}
		for _, format := range formats {
			if parsed, err := time.Parse(format, v); err == nil {
				eventTime = parsed
				break
			}
		}
		if eventTime.IsZero() {
			eventTime = time.Now()
		}
	case float64:
		// Unix timestamp in milliseconds or seconds
		if v > 1e12 {
			eventTime = time.UnixMilli(int64(v))
		} else {
			eventTime = time.Unix(int64(v), 0)
		}
	default:
		eventTime = time.Now()
	}

	// Build properties map from all other fields
	properties := make(map[string]interface{})
	for header, idx := range headerIdx {
		// Skip base fields that are mapped directly
		if header == "#account_id" || header == "#event_id" || header == "#event_time" {
			continue
		}
		if idx < len(row) && row[idx] != nil {
			properties[header] = row[idx]
		}
	}

	// Add region to properties
	properties["region"] = region

	// Extract revenue for payment events
	var revenue float64
	if eventName == "pay" {
		revenue = cast.ToFloat64(getValue("amount"))
	}

	// Detect payment channel (web vs app)
	var paymentChannel string
	if storename := cast.ToString(getValue("storename")); storename != "" {
		if storename == "shop.vnggames.com/vn" {
			paymentChannel = "web"
		} else {
			paymentChannel = "app"
		}
		properties["payment_channel"] = paymentChannel
	}

	// Extract platform if available
	platform := cast.ToString(getValue("#os"))
	if platform == "" {
		platform = cast.ToString(getValue("platform"))
	}

	// Extract app_id
	appID := cast.ToString(getValue("appid"))
	if appID == "" {
		appID = cast.ToString(getValue("app_id"))
	}

	event := &data.Event{
		EventID:    eventID,
		UserID:     accountID,
		EventName:  eventName, // Keep original TD event name
		EventTime:  eventTime,
		Properties: properties,
		Revenue:    revenue,
		Platform:   platform,
		AppID:      appID,
		CreatedAt:  time.Now(),
	}

	return event, nil
}
