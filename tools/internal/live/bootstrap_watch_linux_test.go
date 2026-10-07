//go:build linux

package live

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

// Filesystem events the bootstrap tests order (inotify): a file opened, created, moved in or
// out, or closed after writing.
const (
	evOpen       = syscall.IN_OPEN
	evCreate     = syscall.IN_CREATE
	evMovedTo    = syscall.IN_MOVED_TO
	evMovedFrom  = syscall.IN_MOVED_FROM
	evCloseWrite = syscall.IN_CLOSE_WRITE
)

const fsEventsObservable = true

// fsEvent is one event on an entry of a watched directory (Name empty: the directory itself).
type fsEvent struct {
	Dir  string
	Name string
	Mask uint32
}

// watchEvents watches dirs (one inotify queue, so the events of all of them are in the order the
// kernel saw them). The returned func drains the queue; call it once, after the run.
func watchEvents(t *testing.T, dirs ...string) func() []fsEvent {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	byWD := map[int32]string{}
	for _, d := range dirs {
		wd, err := syscall.InotifyAddWatch(fd, d, evOpen|evCreate|evMovedTo|evMovedFrom|evCloseWrite)
		if err != nil {
			t.Fatalf("watch %s: %v", d, err)
		}
		byWD[int32(wd)] = d
	}
	return func() []fsEvent {
		var out []fsEvent
		buf := make([]byte, 256*(syscall.SizeofInotifyEvent+syscall.NAME_MAX+1))
		for {
			n, err := syscall.Read(fd, buf)
			switch {
			case errors.Is(err, syscall.EINTR):
				continue
			case errors.Is(err, syscall.EAGAIN):
				return out
			case err != nil:
				t.Fatalf("reading the watch: %v", err) // never "no event" on a broken watch
			case n <= 0:
				return out
			}
			for off := 0; off+syscall.SizeofInotifyEvent <= n; {
				ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[off]))
				if ev.Mask&syscall.IN_Q_OVERFLOW != 0 {
					t.Fatal("inotify queue overflowed: event order unknown")
				}
				name := ""
				if ev.Len > 0 {
					raw := buf[off+syscall.SizeofInotifyEvent : off+syscall.SizeofInotifyEvent+int(ev.Len)]
					for i, c := range raw {
						if c == 0 {
							raw = raw[:i]
							break
						}
					}
					name = string(raw)
				}
				out = append(out, fsEvent{Dir: byWD[ev.Wd], Name: name, Mask: ev.Mask})
				off += syscall.SizeofInotifyEvent + int(ev.Len)
			}
		}
	}
}
