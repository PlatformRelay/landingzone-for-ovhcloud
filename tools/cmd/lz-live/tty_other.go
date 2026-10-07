//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// openTTY: the no-echo prompt is implemented for Linux only, the live lane's host (AGENTS.md);
// elsewhere --fresh-account refuses before any key is read.
func openTTY(context.Context) (live.Terminal, error) {
	return nil, errors.New("the no-echo terminal prompt is implemented on Linux only")
}
