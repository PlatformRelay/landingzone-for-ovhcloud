//go:build !linux

package live

import "testing"

// watchEvents needs inotify (Linux, where lz-live runs): elsewhere it observes nothing and the
// order checks that need it are skipped.
const (
	evOpen = 1 << iota
	evCreate
	evMovedTo
	evMovedFrom
	evCloseWrite
)

const fsEventsObservable = false

type fsEvent struct {
	Dir  string
	Name string
	Mask uint32
}

func watchEvents(t *testing.T, dirs ...string) func() []fsEvent {
	t.Helper()
	return func() []fsEvent { return nil }
}
