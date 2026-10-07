package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPTY opens a pseudo-terminal pair: the master (the operator's keyboard and screen) and the
// slave (what lz-live reads as its terminal). It skips when the host offers no /dev/ptmx.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	// Non-blocking, so os.NewFile puts it in the poller and read deadlines work.
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal on this host: %v", err)
	}
	m := os.NewFile(uintptr(fd), "/dev/ptmx")
	t.Cleanup(func() { m.Close() })
	var unlock int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		t.Fatalf("unlockpt: %v", e)
	}
	var n uint32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); e != 0 {
		t.Fatalf("ptsname: %v", e)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDONLY|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return m, s
}

func echoOn(t *testing.T, f *os.File) bool {
	t.Helper()
	st, err := getTermios(f.Fd())
	if err != nil {
		t.Fatal(err)
	}
	return st.Lflag&syscall.ECHO != 0
}

// waitEcho waits until the terminal's echo flag is want.
func waitEcho(t *testing.T, f *os.File, want bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if echoOn(t, f) == want {
			return
		}
	}
	t.Fatalf("terminal echo never became %v", want)
}

// screen drains what the terminal showed the operator (its echo) for a short while.
func screen(m *os.File) string {
	var out bytes.Buffer
	buf := make([]byte, 256)
	m.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		n, err := m.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			return out.String()
		}
	}
}

// TestBootstrapTerminalEcho (G2, FR-010): the root keys are read with the terminal's echo off and
// the echo is restored afterwards, also when the run is interrupted at the prompt; a project
// reference is read with echo on. Prompts go to the prompt writer, never the answers.
func TestBootstrapTerminalEcho(t *testing.T) {
	m, s := openPTY(t)
	if !echoOn(t, s) {
		t.Fatal("a fresh pseudo-terminal echoes")
	}
	prompts := &promptWriter{t: t, tty: s}
	term := newTTY(context.Background(), s, prompts)

	type answer struct {
		v   string
		err error
	}
	got := make(chan answer, 1)
	go func() {
		v, err := term.ReadSecret("application secret (AS): ")
		got <- answer{v, err}
	}()
	waitEcho(t, s, false)
	if _, err := m.Write([]byte("fakeRootSecret42\n")); err != nil {
		t.Fatal(err)
	}
	a := <-got
	if a.err != nil || a.v != "fakeRootSecret42" {
		t.Errorf("ReadSecret = %q, %v; want the typed line", a.v, a.err)
	}
	if shown := screen(m); bytes.Contains([]byte(shown), []byte("fakeRootSecret42")) {
		t.Errorf("the secret was echoed: %q", shown)
	}
	if !echoOn(t, s) {
		t.Error("echo not restored after the secret prompt")
	}
	if p := prompts.String(); !bytes.Contains([]byte(p), []byte("application secret (AS): ")) || bytes.Contains([]byte(p), []byte("fakeRootSecret42")) {
		t.Errorf("prompt writer got %q", p)
	}
	if prompts.shownWithEcho("application secret (AS): ") {
		t.Error("the secret prompt was shown while the terminal still echoed: an answer typed at once is echoed")
	}

	go func() {
		v, err := term.ReadLine("LZ_PROJECT_ID_STATE: ")
		got <- answer{v, err}
	}()
	time.Sleep(50 * time.Millisecond)
	if !echoOn(t, s) {
		t.Error("echo off for a project reference")
	}
	if _, err := m.Write([]byte("f0000000000000000000000000000002\n")); err != nil {
		t.Fatal(err)
	}
	if a := <-got; a.err != nil || a.v != "f0000000000000000000000000000002" {
		t.Errorf("ReadLine = %q, %v", a.v, a.err)
	}

	// An interrupt at the secret prompt returns an error and leaves the echo on.
	ctx, cancel := context.WithCancel(context.Background())
	term = newTTY(ctx, s, prompts)
	go func() {
		v, err := term.ReadSecret("consumer key (CK): ")
		got <- answer{v, err}
	}()
	waitEcho(t, s, false)
	cancel()
	select {
	case a := <-got:
		if !errors.Is(a.err, context.Canceled) {
			t.Errorf("interrupted ReadSecret = %q, %v; want context.Canceled", a.v, a.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an interrupted prompt did not return")
	}
	if !echoOn(t, s) {
		t.Error("echo left off after an interrupted secret prompt")
	}
}

// promptWriter records each prompt with the terminal's echo state at the moment it is shown.
type promptWriter struct {
	t    *testing.T
	tty  *os.File
	mu   sync.Mutex
	buf  bytes.Buffer
	echo map[string]bool
}

func (w *promptWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.echo == nil {
		w.echo = map[string]bool{}
	}
	w.echo[string(p)] = w.echo[string(p)] || echoOn(w.t, w.tty)
	return w.buf.Write(p)
}

func (w *promptWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *promptWriter) shownWithEcho(prompt string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.echo[prompt]
}
