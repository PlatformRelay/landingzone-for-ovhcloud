package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/probes/live/state"
)

// recoverySeam is the locked-recovery check's tofu seam (spec 003 T036, data-model.md *Probe
// checks*): the pinned OpenTofu the guard resolved (T093), run with `-chdir` on the probe root's
// disposable backend root (tests/live/probes/locked-recovery/backend). It is the one child of a
// run that receives a credential the root published (live.ProbeCheck): the state user's key pair,
// AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, and nothing else; any other key is refused before
// a child starts. The child's environment is env.go's allowlist (PATH, a scratch HOME) plus that
// pair and its own TF_DATA_DIR inside the scratch HOME: the run core's kind of HOME
// (live.ScratchHome: 0700 under the resolved TMPDIR, which lz-live refused inside the checkout
// before anything ran), created at the first call and removed by close when the run ends, so init's backend configuration carries
// over to the later commands of the run and never into the checkout. Every child runs in a
// process group of its own; its output (stdout and stderr, capped) comes back with every value of
// env redacted, the only secrets it holds.
//
// kill (writer A's apply, which runs without -auto-approve): stdin stays an open pipe, so tofu
// waits at its approval prompt (`Enter a value:`, internal/command/ui_input.go:113) holding the
// lock (internal/backend/local/backend_local.go:60 locks before backend_apply.go:217 asks); once
// the prompt is in the output, the seam sends SIGKILL to the group (its provider plugins with it)
// and reports Signaled only when the child then exited on that SIGKILL. A child that exits before
// its prompt, or that shows none within promptWait, is not reported killed (the latter is killed
// and returns an error): the check reads both as "owner alive".
type recoverySeam struct {
	tofu       string        // the pinned OpenTofu
	dir        string        // the backend root
	promptWait time.Duration // 0: recoveryPromptWait

	mu     sync.Mutex
	home   string
	remove func()
}

// recoveryPromptWait bounds how long writer A may take to reach its approval prompt (init is
// done; the plan of one terraform_data, after the lock, against the S3 backend).
const recoveryPromptWait = 5 * time.Minute

// recoveryOutputCap bounds what the seam keeps of a child's output.
const recoveryOutputCap = 1 << 20

// recoveryPrompt is OpenTofu's input prompt, the last thing an apply prints before it waits.
var recoveryPrompt = []byte("Enter a value:")

// recoveryEnvKeys are the only variables the check may hand the child.
var recoveryEnvKeys = []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}

func newRecoverySeam(tofu, root string) *recoverySeam {
	return &recoverySeam{tofu: tofu, dir: filepath.Join(root, "backend")}
}

// scratch returns the seam's HOME, creating it at the first call.
func (s *recoverySeam) scratch() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.home != "" {
		return s.home, nil
	}
	d, remove, err := live.ScratchHome()
	if err != nil {
		return "", err
	}
	s.home, s.remove = d, remove
	return d, nil
}

// close removes the seam's HOME (and with it the tofu data directory).
func (s *recoverySeam) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remove != nil {
		s.remove()
		s.home, s.remove = "", nil
	}
}

// seamOutput collects a child's output (capped) and closes seen once the prompt appears; the
// prompt is looked for in what arrives (with the tail of what came before, so a prompt split
// across writes counts), not in the capped buffer, so a long output cannot hide it (review r1).
type seamOutput struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	tail []byte
	seen chan struct{}
	once sync.Once
}

func (w *seamOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := recoveryOutputCap - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	scan := append(w.tail, p...)
	if w.seen != nil && bytes.Contains(scan, recoveryPrompt) {
		w.once.Do(func() { close(w.seen) })
	}
	w.tail = bytes.Clone(scan[max(0, len(scan)-len(recoveryPrompt)+1):])
	return len(p), nil
}

func (w *seamOutput) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Clone(w.buf.Bytes())
}

// run is RecoveryOptions.Tofu.
func (s *recoverySeam) run(ctx context.Context, env map[string]string, kill bool, args ...string) (state.TofuRun, error) {
	if got := slices.Sorted(maps.Keys(env)); !slices.Equal(got, recoveryEnvKeys) || env["AWS_ACCESS_KEY_ID"] == "" || env["AWS_SECRET_ACCESS_KEY"] == "" {
		return state.TofuRun{}, fmt.Errorf("tofu seam: the child gets the state key pair only, not the keys %s", strings.Join(got, ", "))
	}
	home, err := s.scratch()
	if err != nil {
		return state.TofuRun{}, fmt.Errorf("tofu seam: scratch HOME: %w", err)
	}
	cmd := live.Command(live.AuthorityBootstrap, env, home, s.tofu, append([]string{"-chdir=" + s.dir}, args...)...)
	cmd.Env = append(cmd.Env, "TF_DATA_DIR="+filepath.Join(home, "tofu-data"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A grandchild holding the output pipes open cannot keep Wait from returning.
	cmd.WaitDelay = 10 * time.Second
	out := &seamOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	var stdin io.WriteCloser
	if kill {
		out.seen = make(chan struct{})
		if stdin, err = cmd.StdinPipe(); err != nil {
			return state.TofuRun{}, fmt.Errorf("tofu seam: stdin: %w", err)
		}
	}
	if err := cmd.Start(); err != nil {
		return state.TofuRun{}, fmt.Errorf("tofu seam: start: %w", err)
	}
	group := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	var werr, stop error
	sent := false
	if kill {
		wait := s.promptWait
		if wait <= 0 {
			wait = recoveryPromptWait
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-out.seen:
			group()
			sent = true
			werr = <-exited
		case werr = <-exited:
		case <-timer.C:
			group()
			werr, stop = <-exited, fmt.Errorf("tofu seam: no approval prompt within %s", wait)
		case <-ctx.Done():
			group()
			werr, stop = <-exited, fmt.Errorf("tofu seam: %w", ctx.Err())
		}
		stdin.Close()
	} else {
		select {
		case werr = <-exited:
		case <-ctx.Done():
			group()
			werr, stop = <-exited, fmt.Errorf("tofu seam: %w", ctx.Err())
		}
	}
	redact := live.NewRedactor(slices.Collect(maps.Values(env))...)
	r := state.TofuRun{Exit: cmd.ProcessState.ExitCode(), Output: []byte(redact.Redact(string(out.bytes())))}
	var exit *exec.ExitError
	if werr != nil && !errors.As(werr, &exit) {
		return r, fmt.Errorf("tofu seam: wait: %w", werr)
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && sent {
		r.Signaled = ws.Signaled() && ws.Signal() == syscall.SIGKILL
	}
	return r, stop
}
