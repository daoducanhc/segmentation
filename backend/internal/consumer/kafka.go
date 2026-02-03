// Package consumer provides data ingestion from external sources.
package consumer

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-kratos/kratos/v2/log"

	"segmentation/internal/conf"
	"segmentation/internal/data"
)

// KafkaConsumer handles real-time event consumption from Kafka.
type KafkaConsumer struct {
	client     sarama.ConsumerGroup
	eventRepo  *data.EventRepo
	userRepo   *data.UserRepo
	log        *log.Helper
	topics     []string
	ungrouped  []string
	running    bool
	cancelFunc context.CancelFunc
}

// ThinkingDataEvent represents an event from ThinkingData
type ThinkingDataEvent struct {
	DistinctID string                 `json:"#distinct_id"`
	EventName  string                 `json:"#event_name"`
	Time       string                 `json:"#time"`
	Type       string                 `json:"#type"`
	IP         string                 `json:"#ip"`
	Properties map[string]interface{} `json:"properties"`
}

// NewKafkaConsumer creates a new Kafka consumer
func NewKafkaConsumer(cfg *conf.Kafka, eventRepo *data.EventRepo, userRepo *data.UserRepo, logger log.Logger) (*KafkaConsumer, error) {
	config := sarama.NewConfig()
	config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	config.Version = sarama.V2_8_0_0

	group := "segmentation-consumer"
	if cfg.GroupId != "" {
		group = cfg.GroupId
	}

	client, err := sarama.NewConsumerGroup(cfg.Address, group, config)
	if err != nil {
		return nil, err
	}

	topics := []string{cfg.Topic}
	if cfg.Topic == "" {
		topics = []string{"user_events"} // default
	}

	return &KafkaConsumer{
		client:    client,
		eventRepo: eventRepo,
		userRepo:  userRepo,
		log:       log.NewHelper(logger),
		topics:    topics,
		ungrouped: []string{},
	}, nil
}

// Start starts consuming messages
func (c *KafkaConsumer) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel
	c.running = true

	handler := &consumerHandler{
		eventRepo: c.eventRepo,
		userRepo:  c.userRepo,
		log:       c.log,
	}

	// Start consuming grouped topics
	go func() {
		for c.running {
			if err := c.client.Consume(ctx, c.topics, handler); err != nil {
				c.log.Errorf("Error consuming from Kafka: %v", err)
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()

	c.log.Info("Kafka consumer started")
	return nil
}

// Stop stops the consumer
func (c *KafkaConsumer) Stop() error {
	c.running = false
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	return c.client.Close()
}

// consumerHandler implements sarama.ConsumerGroupHandler
type consumerHandler struct {
	eventRepo *data.EventRepo
	userRepo  *data.UserRepo
	log       *log.Helper
}

func (h *consumerHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *consumerHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *consumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	batch := make([]*data.Event, 0, 1000)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				// Flush remaining batch
				if len(batch) > 0 {
					h.flushBatch(session.Context(), batch)
				}
				return nil
			}

			event, err := h.parseMessage(msg.Value)
			if err != nil {
				h.log.Warnf("Failed to parse message: %v", err)
				session.MarkMessage(msg, "")
				continue
			}

			batch = append(batch, event)
			session.MarkMessage(msg, "")

			if len(batch) >= 1000 {
				h.flushBatch(session.Context(), batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				h.flushBatch(session.Context(), batch)
				batch = batch[:0]
			}

		case <-session.Context().Done():
			if len(batch) > 0 {
				h.flushBatch(context.Background(), batch)
			}
			return nil
		}
	}
}

func (h *consumerHandler) parseMessage(msg []byte) (*data.Event, error) {
	var tdEvent ThinkingDataEvent
	if err := json.Unmarshal(msg, &tdEvent); err != nil {
		return nil, err
	}

	eventTime, _ := time.Parse("2006-01-02 15:04:05.000", tdEvent.Time)
	if eventTime.IsZero() {
		eventTime = time.Now()
	}

	event := &data.Event{
		UserID:     tdEvent.DistinctID,
		EventName:  tdEvent.EventName,
		EventTime:  eventTime,
		Properties: tdEvent.Properties,
		ReceivedAt: time.Now(),
	}

	// Extract common properties
	if props := tdEvent.Properties; props != nil {
		// Platform from plt_type (web_mobile, web_pc, app)
		if pltType, ok := props["plt_type"].(string); ok {
			event.Platform = pltType
		}
		// Country from #country_code
		if country, ok := props["#country_code"].(string); ok {
			event.Country = country
		}
		// OS from #os
		if os, ok := props["#os"].(string); ok {
			event.OS = os
		}
		// Language from #system_language (first 2 chars)
		if lang, ok := props["#system_language"].(string); ok {
			if len(lang) > 2 {
				lang = lang[:2]
			}
			event.Language = lang
		}
		if appID, ok := props["appid"].(string); ok {
			event.AppID = appID
		}
		if revenue, ok := props["amount"].(float64); ok {
			event.Revenue = revenue
		}
		if currency, ok := props["currency"].(string); ok {
			event.Currency = currency
		}
		// Detect payment channel based on storename (only for "pay" event)
		if event.EventName == "pay" {
			storename, _ := props["storename"].(string)
			switch {
			case strings.HasPrefix(storename, "shop"):
				event.PaymentChannel = "webshop"
			case storename == "Google Play Store Gateway":
				event.PaymentChannel = "google"
			case storename == "3rdParty IAP Gateway":
				event.PaymentChannel = "3rd_party"
			case storename == "Apple Store Gateway":
				event.PaymentChannel = "apple"
			default:
				event.PaymentChannel = "webshop"
			}
		}
	}

	return event, nil
}

func (h *consumerHandler) flushBatch(ctx context.Context, batch []*data.Event) {
	if len(batch) == 0 {
		return
	}

	if err := h.eventRepo.InsertBatch(ctx, batch); err != nil {
		h.log.Errorf("Failed to insert batch: %v", err)
	} else {
		h.log.Debugf("Flushed %d events", len(batch))
	}

	// Update user event counts
	userCounts := make(map[string]uint32)
	for _, e := range batch {
		userCounts[e.UserID]++
	}

	for userID, count := range userCounts {
		if err := h.userRepo.IncrementEventCount(ctx, userID, count); err != nil {
			h.log.Warnf("Failed to update user event count: %v", err)
		}
	}
}
