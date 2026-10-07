package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// The operator's terminal for `bootstrap --fresh-account` (FR-010, guard G2): the root keys are
// read from /dev/tty with the terminal's echo off, never from an argument, the environment, a file
// or a pipe; prompts go to stderr. A read is abandoned when the run's context is cancelled
// (SIGINT/SIGTERM), so the run can still revoke the keys; the echo is restored on every return.

// openTTY opens the controlling terminal read-only (lz-live writes no file outside files.go).
func openTTY(ctx context.Context) (live.Terminal, error) {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return nil, err
	}
	if _, err := getTermios(f.Fd()); err != nil {
		f.Close()
		return nil, fmt.Errorf("/dev/tty is not a terminal: %w", err)
	}
	return newTTY(ctx, f, os.Stderr), nil
}

type ttyLine struct {
	s   string
	err error
}

type tty struct {
	ctx    context.Context
	in     *os.File
	prompt io.Writer
	lines  chan ttyLine // one reader goroutine feeds every prompt
}

func newTTY(ctx context.Context, in *os.File, prompt io.Writer) *tty {
	t := &tty{ctx: ctx, in: in, prompt: prompt, lines: make(chan ttyLine)}
	go func() {
		r := bufio.NewReader(in)
		for {
			s, err := r.ReadString('\n')
			t.lines <- ttyLine{strings.TrimRight(s, "\r\n"), err}
			if err != nil {
				return
			}
		}
	}()
	return t
}

func getTermios(fd uintptr) (syscall.Termios, error) {
	var st syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&st))); e != 0 {
		return st, e
	}
	return st, nil
}

func setTermios(fd uintptr, st syscall.Termios) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&st))); e != 0 {
		return e
	}
	return nil
}

// read waits for the next line or the run's cancellation.
func (t *tty) read() (string, error) {
	select {
	case l := <-t.lines:
		if l.err != nil && l.s == "" {
			return "", l.err
		}
		return l.s, nil
	case <-t.ctx.Done():
		return "", t.ctx.Err()
	}
}

// ReadSecret reads one line with the echo off; it refuses to read at all when the echo cannot be
// turned off.
func (t *tty) ReadSecret(prompt string) (string, error) {
	saved, err := getTermios(t.in.Fd())
	if err != nil {
		return "", fmt.Errorf("terminal: %w", err)
	}
	quiet := saved
	quiet.Lflag &^= syscall.ECHO
	if err := setTermios(t.in.Fd(), quiet); err != nil {
		return "", fmt.Errorf("terminal: echo not turned off: %w", err)
	}
	defer func() {
		setTermios(t.in.Fd(), saved)
		fmt.Fprintln(t.prompt) // the operator's Enter was not echoed either
	}()
	// Shown only once the echo is off, so an answer typed at once is not echoed.
	fmt.Fprint(t.prompt, prompt)
	return t.read()
}

// ReadLine reads one line with the echo as the terminal has it (on).
func (t *tty) ReadLine(prompt string) (string, error) {
	fmt.Fprint(t.prompt, prompt)
	return t.read()
}
