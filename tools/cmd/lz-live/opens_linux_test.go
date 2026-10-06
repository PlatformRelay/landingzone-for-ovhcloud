//go:build linux

package main

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

// watchOpens watches paths for being opened (inotify IN_OPEN; T077 review r1: a credential file
// read and then discarded leaves no other trace). The returned func lists the paths opened since,
// each once; call it before the test itself opens any of them.
func watchOpens(t *testing.T, paths ...string) func() []string {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	byWD := map[int32]string{}
	for _, p := range paths {
		wd, err := syscall.InotifyAddWatch(fd, p, syscall.IN_OPEN)
		if err != nil {
			t.Fatalf("watch %s: %v", p, err)
		}
		byWD[int32(wd)] = p
	}
	return func() []string {
		seen := map[string]bool{}
		var out []string
		buf := make([]byte, 64*(syscall.SizeofInotifyEvent+syscall.NAME_MAX+1))
		for {
			n, err := syscall.Read(fd, buf)
			switch {
			case errors.Is(err, syscall.EINTR):
				continue
			case errors.Is(err, syscall.EAGAIN):
				return out // drained
			case err != nil:
				t.Fatalf("reading the open watch: %v", err) // never "no open" on a broken watch
			case n <= 0:
				return out
			}
			for off := 0; off+syscall.SizeofInotifyEvent <= n; {
				ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[off]))
				if p := byWD[ev.Wd]; p != "" && ev.Mask&syscall.IN_OPEN != 0 && !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
				off += syscall.SizeofInotifyEvent + int(ev.Len)
			}
		}
	}
}

const opensObservable = true
