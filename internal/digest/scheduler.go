package digest

import (
	"context"
	"log"
	"time"

	"github.com/greencode/greenforge/internal/config"
	"github.com/greencode/greenforge/internal/notify"
)

// Scheduler runs the morning digest on a schedule or on-demand.
type Scheduler struct {
	cfg       *config.Config
	collector *Collector
	notifier  *notify.Engine
}

// NewScheduler creates a digest scheduler.
func NewScheduler(cfg *config.Config, collector *Collector, notifier *notify.Engine) *Scheduler {
	return &Scheduler{
		cfg:       cfg,
		collector: collector,
		notifier:  notifier,
	}
}

// Start runs the scheduler in the background. Respects the configured mode:
// "automatic" - sends at configured time every day
// "on_demand" - does nothing (triggered via API/CLI)
// "both" - sends automatically AND allows on-demand
func (s *Scheduler) Start(ctx context.Context) {
	mode := s.cfg.Notify.MorningDigest.Mode
	if mode == "on_demand" {
		log.Println("Digest scheduler: on-demand mode, not starting cron")
		return
	}

	targetTime := s.cfg.Notify.MorningDigest.Time
	if targetTime == "" {
		targetTime = "07:30"
	}

	log.Printf("Digest scheduler started (mode=%s, time=%s)", mode, targetTime)

	for {
		// Calculate next run time
		next := nextOccurrence(targetTime)
		waitDuration := time.Until(next)

		log.Printf("Digest scheduler: next digest at %s (in %s)", next.Format("15:04"), waitDuration.Round(time.Minute))

		select {
		case <-ctx.Done():
			log.Println("Digest scheduler stopped")
			return
		case <-time.After(waitDuration):
			s.RunDigest(ctx)
		}
	}
}

// RunDigest collects and sends the digest immediately.
func (s *Scheduler) RunDigest(ctx context.Context) {
	log.Println("Collecting digest data...")

	data, err := s.collector.Collect(ctx)
	if err != nil {
		log.Printf("Digest collection error: %v", err)
		return
	}

	text := Format(data)
	log.Printf("Digest collected: %d projects", len(data.Projects))

	// Send via notification engine
	msg := notify.Message{
		Title:    "Morning Digest",
		Body:     text,
		Severity: "info",
		Event:    "digest",
	}

	if err := s.notifier.Send(ctx, msg); err != nil {
		log.Printf("Digest notification error: %v", err)
	}
}

// GetDigest collects and returns digest data without sending notifications.
func (s *Scheduler) GetDigest(ctx context.Context) (*DigestData, error) {
	return s.collector.Collect(ctx)
}

// nextOccurrence returns the next time.Time for a given HH:MM string.
// If the time has already passed today, returns tomorrow's occurrence.
func nextOccurrence(timeStr string) time.Time {
	now := time.Now()
	target, err := time.Parse("15:04", timeStr)
	if err != nil {
		// Default to 07:30
		target, _ = time.Parse("15:04", "07:30")
	}

	next := time.Date(now.Year(), now.Month(), now.Day(),
		target.Hour(), target.Minute(), 0, 0, now.Location())

	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}
