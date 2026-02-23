//go:build !darwin

package trigger

import (
	"context"
	"fmt"
	"runtime"

	"github.com/colebrumley/srvrmgr/internal/config"
)

// Filesystem is a stub on non-darwin platforms.
type Filesystem struct {
	ruleName string
}

var _ Trigger = (*Filesystem)(nil)

func NewFilesystem(ruleName string, cfg config.Trigger, runAsUser string) (*Filesystem, error) {
	return nil, fmt.Errorf("filesystem triggers require macOS (FSEvents); running on %s", runtime.GOOS)
}

func (f *Filesystem) RuleName() string { return f.ruleName }
func (f *Filesystem) Start(ctx context.Context, events chan<- Event) error {
	return fmt.Errorf("filesystem triggers require macOS (FSEvents); running on %s", runtime.GOOS)
}
func (f *Filesystem) Stop() error { return nil }
