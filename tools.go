//go:build tools
// +build tools

// This file declares tool dependencies that aren't directly imported in the code.
// It prevents `go mod tidy` from removing them.
// See: https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module

package tools

import (
	_ "github.com/knights-analytics/hugot"
)
