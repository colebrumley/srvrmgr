// internal/trigger/scheduled.go
package trigger

import (
	"context"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
	"github.com/robfig/cron/v3"
)

// Scheduled fires events on a cron schedule
type Scheduled struct {
	ruleName string
	cron     *cron.Cron
	events   chan<- Event
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
		cronExpr = convertSimpleToCron(cfg.RunEvery, cfg.RunAt)
	}

	_, err := c.AddFunc(cronExpr, func() {
		if s.events != nil {
			s.events <- Event{
				RuleName:  s.ruleName,
				Type:      "scheduled",
				Timestamp: time.Now(),
				Data:      map[string]any{},
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
	s.events = events
	s.cron.Start()

	<-ctx.Done()
	return ctx.Err()
}

func (s *Scheduled) Stop() error {
	s.cron.Stop()
	return nil
}

// convertSimpleToCron converts run_every or run_at to cron expression
func convertSimpleToCron(runEvery, runAt string) string {
	// Default: every hour
	if runEvery == "" && runAt == "" {
		return "0 0 * * * *"
	}

	// run_at: "HH:MM" -> run daily at that time
	if runAt != "" {
		// Parse "HH:MM" format
		if len(runAt) == 5 && runAt[2] == ':' {
			hour := runAt[0:2]
			min := runAt[3:5]
			return "0 " + min + " " + hour + " * * *"
		}
	}

	// run_every: "1h", "30m", "6h", etc.
	if runEvery != "" {
		if len(runEvery) >= 2 {
			unit := runEvery[len(runEvery)-1]
			val := runEvery[:len(runEvery)-1]

			switch unit {
			case 'h':
				return "0 0 */" + val + " * * *"
			case 'm':
				return "0 */" + val + " * * * *"
			}
		}
	}

	return "0 0 * * * *"
}
