// internal/trigger/scheduled.go
package trigger

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/robfig/cron/v3"
)

// Scheduled fires events on a cron schedule
type Scheduled struct {
	ruleName string
	cron     *cron.Cron
	events   chan<- Event
	mu       sync.Mutex
}

// NewScheduled creates a new scheduled trigger
func NewScheduled(ruleName string, cfg config.Trigger) (*Scheduled, error) {
	// Use cron with seconds field support
	c := cron.New(cron.WithSeconds())

	s := &Scheduled{
		ruleName: ruleName,
		cron:     c,
	}

	// Parse the cron expression
	cronExpr := cfg.CronExpression
	if cronExpr == "" {
		// Convert simple syntax to cron
		var err error
		cronExpr, err = convertSimpleToCron(cfg.RunEvery, cfg.RunAt)
		if err != nil {
			return nil, fmt.Errorf("invalid schedule: %w", err)
		}
	}

	_, err := c.AddFunc(cronExpr, func() {
		s.mu.Lock()
		events := s.events
		s.mu.Unlock()
		if events != nil {
			now := time.Now()
			events <- Event{
				RuleName:  s.ruleName,
				Type:      "scheduled",
				Timestamp: now,
				Data: map[string]any{
					"timestamp": now.Format(time.RFC3339),
				},
			}
		}
	})
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Scheduled) RuleName() string {
	return s.ruleName
}

func (s *Scheduled) Start(ctx context.Context, events chan<- Event) error {
	s.mu.Lock()
	s.events = events
	s.mu.Unlock()
	s.cron.Start()

	<-ctx.Done()
	return ctx.Err()
}

func (s *Scheduled) Stop() error {
	ctx := s.cron.Stop()
	<-ctx.Done() // wait for running jobs to finish
	return nil
}

// convertSimpleToCron converts run_every or run_at to cron expression.
// Returns an error if the input is invalid.
func convertSimpleToCron(runEvery, runAt string) (string, error) {
	// Default: every hour
	if runEvery == "" && runAt == "" {
		return "0 0 * * * *", nil
	}

	// run_at: "HH:MM" -> run daily at that time
	if runAt != "" {
		if len(runAt) != 5 || runAt[2] != ':' {
			return "", fmt.Errorf("invalid run_at format %q, expected HH:MM", runAt)
		}
		hour, err := strconv.Atoi(runAt[0:2])
		if err != nil || hour < 0 || hour > 23 {
			return "", fmt.Errorf("invalid hour in run_at %q", runAt)
		}
		min, err := strconv.Atoi(runAt[3:5])
		if err != nil || min < 0 || min > 59 {
			return "", fmt.Errorf("invalid minute in run_at %q", runAt)
		}
		return fmt.Sprintf("0 %d %d * * *", min, hour), nil
	}

	// run_every: "1h", "30m", "6h", etc.
	if runEvery != "" {
		if len(runEvery) < 2 {
			return "", fmt.Errorf("invalid run_every format %q", runEvery)
		}
		unit := runEvery[len(runEvery)-1]
		val, err := strconv.Atoi(runEvery[:len(runEvery)-1])
		if err != nil || val <= 0 {
			return "", fmt.Errorf("invalid run_every value %q, must be a positive integer", runEvery)
		}

		switch unit {
		case 'h':
			return fmt.Sprintf("0 0 */%d * * *", val), nil
		case 'm':
			return fmt.Sprintf("0 */%d * * * *", val), nil
		default:
			return "", fmt.Errorf("invalid run_every unit %q, expected 'h' or 'm'", string(unit))
		}
	}

	return "0 0 * * * *", nil
}
