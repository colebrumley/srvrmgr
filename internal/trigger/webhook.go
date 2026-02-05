// internal/trigger/webhook.go
package trigger

import (
	"context"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/colebrumley/srvrmgr/internal/config"
)

// Webhook handles HTTP webhook triggers
type Webhook struct {
	ruleName       string
	listenPath     string
	allowedMethods map[string]bool
	requireSecret  bool
	secretHeader   string
	secret         string
}

// NewWebhook creates a new webhook trigger
func NewWebhook(ruleName string, cfg config.Trigger) (*Webhook, error) {
	methods := make(map[string]bool)
	for _, m := range cfg.AllowedMethods {
		methods[m] = true
	}

	var secret string
	if cfg.RequireSecret && cfg.SecretEnvVar != "" {
		secret = os.Getenv(cfg.SecretEnvVar)
	}

	return &Webhook{
		ruleName:       ruleName,
		listenPath:     cfg.ListenPath,
		allowedMethods: methods,
		requireSecret:  cfg.RequireSecret,
		secretHeader:   cfg.SecretHeader,
		secret:         secret,
	}, nil
}

func (w *Webhook) RuleName() string {
	return w.ruleName
}

func (w *Webhook) ListenPath() string {
	return w.listenPath
}

// Start for webhook just blocks until context is cancelled
// The actual HTTP handling is done by the shared server
func (w *Webhook) Start(ctx context.Context, events chan<- Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func (w *Webhook) Stop() error {
	return nil
}

// HandleRequest processes an incoming HTTP request
func (w *Webhook) HandleRequest(r *http.Request, events chan<- Event) bool {
	// Check method
	if len(w.allowedMethods) > 0 && !w.allowedMethods[r.Method] {
		return false
	}

	// Check secret if required
	if w.requireSecret && w.secret != "" {
		headerVal := r.Header.Get(w.secretHeader)
		if headerVal != w.secret {
			return false
		}
	}

	// Read body
	body, _ := io.ReadAll(r.Body)

	// Build headers map
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	events <- Event{
		RuleName:  w.ruleName,
		Type:      "webhook",
		Timestamp: time.Now(),
		Data: map[string]any{
			"http_body":    string(body),
			"http_headers": headers,
			"http_method":  r.Method,
			"http_path":    r.URL.Path,
		},
	}

	return true
}
