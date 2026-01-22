// Package scheduler provides periodic aggregation refresh jobs.
// It handles incremental (every N minutes) and full (daily) data refreshes.
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"segmentation/internal/data"
)

// Config holds scheduler configuration.
type Config struct {
	// IncrementalRefreshInterval is how often to run incremental refresh (default: 10 minutes)
	IncrementalRefreshInterval time.Duration
	// IncrementalDays is how many days to include in incremental refresh (default: 7)
	IncrementalDays int
	// FullRefreshHour is the hour (0-23) to run full refresh (default: 3 AM)
	FullRefreshHour int
	// Enabled controls whether scheduler is active
	Enabled bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		IncrementalRefreshInterval: 10 * time.Minute,
		IncrementalDays:            7,
		FullRefreshHour:            3, // 3 AM
		Enabled:                    true,
	}
}

// Scheduler handles periodic aggregation refresh jobs
type Scheduler struct {
	config          *Config
	aggregationRepo *data.AggregationRepo
	logger          *log.Helper

	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewScheduler creates a new scheduler
func NewScheduler(config *Config, aggregationRepo *data.AggregationRepo, logger log.Logger) *Scheduler {
	if config == nil {
		config = DefaultConfig()
	}
	return &Scheduler{
		config:          config,
		aggregationRepo: aggregationRepo,
		logger:          log.NewHelper(log.With(logger, "module", "scheduler")),
		stopChan:        make(chan struct{}),
	}
}

// Start begins the scheduler
func (s *Scheduler) Start(ctx context.Context) error {
	if !s.config.Enabled {
		s.logger.Info("scheduler is disabled")
		return nil
	}

	s.logger.Infof("starting scheduler: incremental every %v (%d days), full refresh at %d:00",
		s.config.IncrementalRefreshInterval,
		s.config.IncrementalDays,
		s.config.FullRefreshHour,
	)

	// Start incremental refresh loop
	s.wg.Add(1)
	go s.incrementalRefreshLoop(ctx)

	// Start daily full refresh loop
	s.wg.Add(1)
	go s.fullRefreshLoop(ctx)

	return nil
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	s.logger.Info("stopping scheduler")
	close(s.stopChan)
	s.wg.Wait()
}

// incrementalRefreshLoop runs incremental refresh at regular intervals
func (s *Scheduler) incrementalRefreshLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.IncrementalRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.runIncrementalRefresh(ctx)
		}
	}
}

// fullRefreshLoop runs full refresh once daily at configured hour
func (s *Scheduler) fullRefreshLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		// Calculate time until next full refresh
		now := time.Now()
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), s.config.FullRefreshHour, 0, 0, 0, now.Location())
		if now.After(nextRun) {
			nextRun = nextRun.Add(24 * time.Hour)
		}
		waitDuration := nextRun.Sub(now)

		s.logger.Infof("next full refresh scheduled at %v (in %v)", nextRun.Format("2006-01-02 15:04:05"), waitDuration)

		timer := time.NewTimer(waitDuration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.stopChan:
			timer.Stop()
			return
		case <-timer.C:
			s.runFullRefresh(ctx)
		}
	}
}

// runIncrementalRefresh runs incremental refresh for recent data
func (s *Scheduler) runIncrementalRefresh(ctx context.Context) {
	s.logger.Infof("running incremental refresh (last %d days)", s.config.IncrementalDays)
	start := time.Now()

	if err := s.aggregationRepo.RunIncrementalRefresh(ctx, s.config.IncrementalDays); err != nil {
		s.logger.Errorf("incremental refresh failed: %v", err)
		return
	}

	s.logger.Infof("incremental refresh completed in %v", time.Since(start))
}

// runFullRefresh runs full refresh of all data
func (s *Scheduler) runFullRefresh(ctx context.Context) {
	s.logger.Info("running full refresh (daily maintenance)")
	start := time.Now()

	if err := s.aggregationRepo.RunAllRefreshJobs(ctx); err != nil {
		s.logger.Errorf("full refresh failed: %v", err)
		return
	}

	s.logger.Infof("full refresh completed in %v", time.Since(start))
}

// TriggerIncrementalRefresh manually triggers an incremental refresh
func (s *Scheduler) TriggerIncrementalRefresh(ctx context.Context) error {
	s.logger.Info("manual incremental refresh triggered")
	return s.aggregationRepo.RunIncrementalRefresh(ctx, s.config.IncrementalDays)
}

// TriggerFullRefresh manually triggers a full refresh
func (s *Scheduler) TriggerFullRefresh(ctx context.Context) error {
	s.logger.Info("manual full refresh triggered")
	return s.aggregationRepo.RunAllRefreshJobs(ctx)
}
