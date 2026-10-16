package live

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Run core (FR-011, research R12; guards G2 stream part, G8, G9 probe state). The test binary
// doubles as a fake tofu and a fake ovhcloud (invoked through a symlink of that name, as the fake
// git of fake_test.go), and as a fresh lz-live process ("lz-fake-run <config>") for the signal
// and cleanup controls. Coordinator decision (2026-10-06): every child gets a per-run scratch
// HOME, 0700, removed when the run ends, never the caller's.

// ---------------------------------------------------------------- fake tofu

// tofuStack is what the fake tofu does for one stack (the base name of its -chdir directory).
type tofuStack struct {
	ApplyStream string `json:"apply_stream"` // file whose lines `apply -json` prints
	ApplyExit   int    `json:"apply_exit"`
	HangAfter   int    `json:"hang_after"` // >0: after that many stream lines, hang until stopped
	// IgnoreInterrupt: while hanging, log SIGINT/SIGTERM but keep running (only a kill stops it).
	IgnoreInterrupt bool         `json:"ignore_interrupt"`
	Changes         []planChange `json:"changes"` // resource_changes of `show -json`
	PlanExit        int          `json:"plan_exit"`
	DestroyExit     int          `json:"destroy_exit"`
	InitExit        int          `json:"init_exit"` // T038: init fails with this exit
	StateList       []string     `json:"state_list"`
	// Outputs: file holding what `tofu output -json` prints (T075; captured format, see
	// testdata/tofu/output-companion.json); `output -json <name>` prints that output's value.
	Outputs string `json:"outputs"`
	// HoldLock (T075, P1–P3): apply holds the stack's state lock from its start until another
	// writer of that stack was refused (or 10 s passed); a writer that finds the lock held is
	// logged Locked and fails with the lock error, as tofu does with -lock-timeout=0s.
	HoldLock bool `json:"hold_lock"`
	// ScanRoots/ScanFor (T075): every call lists the regular files under these roots and the
	// child's HOME whose content holds any of ScanFor, as "<path>|<file mode>|<directory mode>".
	ScanRoots []string `json:"scan_roots"`
	ScanFor   []string `json:"scan_for"`
	// DestroyPlanErrors/DestroyErrors (T095): the n-th `plan -destroy` / destroy apply of the stack
	// prints the n-th entry on stderr and fails (exit 1); later calls behave as configured otherwise.
	DestroyPlanErrors []string `json:"destroy_plan_errors"`
	DestroyErrors     []string `json:"destroy_errors"`
	// MissingStateEmpty (T038): destroy reads a missing state file as an empty state.
	MissingStateEmpty bool `json:"missing_state_empty"`
	// DestroyHang (T018): the destroy apply hangs as a HangAfter apply does, until stopped.
	DestroyHang bool `json:"destroy_hang"`
	// ApplyDelay and DestroyDelay (T018): the apply, or the destroy apply, takes that long, then
	// logs "delay-done" and goes on as configured; a SIGINT or SIGTERM meanwhile is logged as a
	// "signal" call and stops it (exit 130).
	ApplyDelay   time.Duration `json:"apply_delay"`
	DestroyDelay time.Duration `json:"destroy_delay"`
}

// fakeDelay waits d for the fake's apply or destroy of stack (tofuStack.ApplyDelay, DestroyDelay):
// 0 when it waited it out ("delay-done" logged), 130 when a signal stopped it ("signal" logged).
func fakeDelay(bin, stack string, d time.Duration, sigs chan os.Signal) int {
	select {
	case sig := <-sigs:
		logCall(bin, "tofu.log", tofuCall{Cmd: "signal", Stack: stack, Args: []string{sig.String()}})
		return 130
	case <-time.After(d):
		logCall(bin, "tofu.log", tofuCall{Cmd: "delay-done", Stack: stack})
		return 0
	}
}

// planChange is one resource change the fake plan holds.
type planChange struct {
	Address string   `json:"address"`
	Actions []string `json:"actions"`
}

type tofuScenario struct {
	Stacks map[string]tofuStack `json:"stacks"`
}

// tofuCall is one logged invocation of a fake child (tofu, ovhcloud) or of the Protect hook.
type tofuCall struct {
	Cmd      string   `json:"cmd"` // init plan show-json show apply destroy state-list protect ovhcloud signal hang-timeout other
	Stack    string   `json:"stack"`
	Args     []string `json:"args"`
	Plan     string   `json:"plan"`  // plan file written (plan) or applied (apply, destroy)
	Nonce    string   `json:"nonce"` // nonce of that plan file
	Home     string   `json:"home"`
	HomeMode int      `json:"home_mode"` // -1: HOME missing
	OvhConf  bool     `json:"ovh_conf"`  // $HOME/.ovh.conf visible
	AWSCreds bool     `json:"aws_creds"` // $HOME/.aws/credentials visible
	Env      []string `json:"env"`
	PassOK   *bool    `json:"pass_ok,omitempty"` // destroy: the state's passphrase matched
	Pgrp     int      `json:"pgrp"`              // the child's process group
	DataDir  string   `json:"data_dir"`          // the data directory tofu used (TF_DATA_DIR, else <root>/.terraform), symlinks resolved
	Locked   bool     `json:"locked"`            // the stack's state lock was held by another writer (HoldLock)
	Holders  []string `json:"holders"`           // files holding any of ScanFor at the time of the call (ScanRoots)
	At       int64    `json:"at"`                // when the call was logged (UnixNano)
}

// nthCall counts the calls of one kind of a stack (a counter file next to the fake) and returns
// this call's index, from 0.
func nthCall(bin, stack, kind string) int {
	f := filepath.Join(bin, "count-"+stack+"-"+kind)
	raw, _ := os.ReadFile(f)
	n, _ := strconv.Atoi(string(raw))
	_ = os.WriteFile(f, []byte(strconv.Itoa(n+1)), 0o600)
	return n
}

// Backend model of the fake tofu (T073), as host tofu 1.10.3 behaves offline on a provider-free
// root with `backend "local" { path = var.state_path }` (t073-reinit.sh, 2026-10-07): init records
// the backend in <data dir>/terraform.tfstate, where the data dir is TF_DATA_DIR (relative to the
// -chdir directory) or <root>/.terraform; a second init with another path in the same data dir
// fails "Backend configuration changed" (unless -reconfigure); plan, destroy and state commands in
// a data dir no init prepared for this backend fail "Backend initialization required".
type fakeBackend struct {
	Backend struct {
		Type   string `json:"type"`
		Config struct {
			Path string `json:"path"`
		} `json:"config"`
	} `json:"backend"`
}

func fakeDataDir(root string) string {
	d := os.Getenv("TF_DATA_DIR")
	if d == "" {
		d = ".terraform"
	}
	if !filepath.IsAbs(d) {
		d = filepath.Join(root, d)
	}
	return d
}

// resolvedPath is p with symlinks resolved as far as p exists (a data directory before its first
// init does not).
func resolvedPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if parent := filepath.Dir(p); parent != p {
		return filepath.Join(resolvedPath(parent), filepath.Base(p))
	}
	return p
}

// fakeInit records the backend of statePath in dataDir, or fails as tofu does on a changed one.
func fakeInit(dataDir, statePath string, reconfigure bool) int {
	file := filepath.Join(dataDir, "terraform.tfstate")
	var b fakeBackend
	if raw, err := os.ReadFile(file); err == nil && json.Unmarshal(raw, &b) == nil && b.Backend.Config.Path != statePath && !reconfigure {
		fmt.Fprintln(os.Stderr, "Error: Backend configuration changed")
		return 1
	}
	b.Backend.Type, b.Backend.Config.Path = "local", statePath
	raw, _ := json.Marshal(b)
	if os.MkdirAll(dataDir, 0o755) != nil || os.WriteFile(file, raw, 0o644) != nil {
		fmt.Fprintln(os.Stderr, "Error: cannot write the data directory")
		return 1
	}
	return 0
}

// fakeInitialised reports whether init prepared dataDir for the backend of statePath.
func fakeInitialised(dataDir, statePath string) bool {
	var b fakeBackend
	raw, err := os.ReadFile(filepath.Join(dataDir, "terraform.tfstate"))
	if err != nil || json.Unmarshal(raw, &b) != nil || b.Backend.Config.Path != statePath {
		fmt.Fprintln(os.Stderr, `Error: Backend initialization required, please run "tofu init"`)
		return false
	}
	return true
}

func logCall(dir, name string, c tofuCall) {
	if c.Cmd != "protect" {
		c.Home = os.Getenv("HOME")
		c.HomeMode = -1
		if fi, err := os.Stat(c.Home); err == nil && c.Home != "" {
			c.HomeMode = int(fi.Mode().Perm())
		}
		_, err := os.Stat(filepath.Join(c.Home, ".ovh.conf"))
		c.OvhConf = c.Home != "" && err == nil
		_, err = os.Stat(filepath.Join(c.Home, ".aws", "credentials"))
		c.AWSCreds = c.Home != "" && err == nil
		c.Env = os.Environ()
		c.Pgrp = syscall.Getpgrp()
	}
	c.At = time.Now().UnixNano()
	line, _ := json.Marshal(c)
	if f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, string(line))
		f.Close()
	}
}

type planFile struct {
	Stack   string `json:"stack"`
	Nonce   string `json:"nonce"`
	Destroy bool   `json:"destroy"`
}

// fakeState models an encrypted probe state: only the passphrase that wrote it reads it.
type fakeState struct {
	PassSHA   string `json:"pass_sha"`
	Resources int    `json:"resources"`
}

func passSHA(p string) string {
	s := sha256.Sum256([]byte(p))
	return hex.EncodeToString(s[:])
}

func fakeTofu(args []string) int {
	bin := filepath.Dir(os.Args[0])
	if len(args) == 1 && args[0] == "plugin-hang" {
		// A provider plugin of the hanging apply: it ignores nothing and stops only when killed.
		time.Sleep(60 * time.Second)
		return 9
	}
	cwd, _ := os.Getwd()
	dir := cwd
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if v, ok := strings.CutPrefix(args[0], "-chdir="); ok {
			dir = v
			if !filepath.IsAbs(v) {
				dir = filepath.Join(cwd, v)
			}
		}
		args = args[1:]
	}
	dataDir := fakeDataDir(dir)
	call := tofuCall{Stack: filepath.Base(dir), Args: os.Args[1:], DataDir: resolvedPath(dataDir)}
	var sc tofuScenario
	if raw, err := os.ReadFile(filepath.Join(bin, "tofu.json")); err == nil {
		_ = json.Unmarshal(raw, &sc)
	}
	st := sc.Stacks[call.Stack]
	if len(args) == 0 {
		call.Cmd = "other"
		logCall(bin, "tofu.log", call)
		return 0
	}
	sub, rest := args[0], args[1:]
	has := func(flag string) bool { return slices.Contains(rest, flag) }
	positional := ""
	for i := 0; i < len(rest); i++ {
		if !strings.HasPrefix(rest[i], "-") {
			positional = rest[i]
		}
	}
	resolve := func(p string) string {
		if p != "" && !filepath.IsAbs(p) {
			return filepath.Join(dir, p)
		}
		return p
	}
	readPlan := func(p string) (planFile, bool) {
		var pf planFile
		raw, err := os.ReadFile(p)
		return pf, err == nil && json.Unmarshal(raw, &pf) == nil
	}
	secret := os.Getenv("OVH_CLIENT_SECRET")
	statePath := os.Getenv("TF_VAR_state_path")
	label := sub
	if pf, ok := readPlan(resolve(positional)); sub == "apply" && ok && pf.Destroy {
		label = "destroy" // the apply of a saved `plan -destroy` file is the destroy (T047)
	}
	// Every subcommand prints the credential and the probe passphrase on stderr: the runner must
	// redact every child stream, not only apply's.
	fmt.Fprintf(os.Stderr, "fake-tofu-%s-stderr token=%s passphrase=%s s3=%s p25=%s\n", label, secret, os.Getenv("TF_VAR_state_passphrase"),
		os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("TF_VAR_p25_client_secret"))
	// ... and every scanned secret (ScanFor) it holds in its environment, under whatever name
	// (T080): the terminal checks then cover published keys in any case.
	for _, kv := range os.Environ() {
		if k, v, _ := strings.Cut(kv, "="); v != "" && slices.Contains(st.ScanFor, v) {
			fmt.Fprintf(os.Stderr, "fake-tofu-%s-env %s=%s\n", sub, k, v)
		}
	}
	if len(st.ScanRoots) > 0 {
		call.Holders = holders(append(slices.Clone(st.ScanRoots), os.Getenv("HOME")), st.ScanFor)
	}
	lock := filepath.Join(bin, "lock-"+call.Stack)
	// Only operations that take the state lock wait for it (plan, apply, destroy, refresh, import,
	// state rm/mv/push), and none with -lock=false; init, output, show and state list do not.
	takesLock := slices.Contains([]string{"plan", "apply", "destroy", "refresh", "import"}, sub) ||
		(sub == "state" && len(rest) > 0 && slices.Contains([]string{"rm", "mv", "push"}, rest[0]))
	// A writer waits for the lock as long as its -lock-timeout (tofu's default: 0s, refused at once).
	waits := false
	for _, a := range rest {
		if v, ok := strings.CutPrefix(a, "-lock-timeout="); ok && v != "0s" && v != "0" {
			waits = true
		}
	}
	if takesLock && !has("-lock=false") && !waits && exists(lock) && (sub == "apply" || fakeInitialised(call.DataDir, statePath)) {
		// Another writer holds the state lock (HoldLock): refused at once, the holder released. A
		// writer whose data directory no init prepared fails on that first (below), not on the lock.
		call.Cmd, call.Locked = sub, true
		logCall(bin, "tofu.log", call)
		_ = os.WriteFile(filepath.Join(bin, "refused-"+call.Stack), nil, 0o600)
		fmt.Fprintln(os.Stderr, "Error: Error acquiring the state lock (fake-lock-marker)")
		return 1
	}
	switch {
	case sub == "output":
		call.Cmd = "output"
		logCall(bin, "tofu.log", call)
		if !fakeInitialised(call.DataDir, statePath) {
			return 1
		}
		raw, err := os.ReadFile(st.Outputs)
		if err != nil || !has("-json") {
			fmt.Fprintln(os.Stderr, "Error: fake output: no outputs or no -json")
			return 1
		}
		if positional == "" {
			fmt.Print(string(raw))
			return 0
		}
		var all map[string]struct {
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(raw, &all) != nil {
			return 1
		}
		o, ok := all[positional]
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: Output %q not found\n", positional)
			return 1
		}
		var b bytes.Buffer
		_ = json.Compact(&b, o.Value)
		fmt.Println(b.String())
		return 0
	case sub == "init":
		call.Cmd = "init"
		logCall(bin, "tofu.log", call)
		if st.InitExit != 0 {
			fmt.Fprintln(os.Stderr, "Error: fake init failure")
			return st.InitExit
		}
		return fakeInit(call.DataDir, statePath, has("-reconfigure"))
	case sub == "plan":
		call.Cmd = "plan"
		if !fakeInitialised(call.DataDir, statePath) {
			logCall(bin, "tofu.log", call)
			return 1
		}
		out := ""
		for i, a := range rest {
			if v, ok := strings.CutPrefix(a, "-out="); ok {
				out = v
			} else if a == "-out" && i+1 < len(rest) {
				out = rest[i+1]
			}
		}
		call.Plan = resolve(out)
		if has("-destroy") && len(st.DestroyPlanErrors) > 0 {
			if n := nthCall(bin, call.Stack, "destroy-plan"); n < len(st.DestroyPlanErrors) {
				logCall(bin, "tofu.log", call)
				fmt.Fprintln(os.Stderr, st.DestroyPlanErrors[n])
				return 1
			}
		}
		if st.PlanExit != 0 {
			logCall(bin, "tofu.log", call)
			fmt.Fprintln(os.Stderr, "Error: fake plan failure")
			return st.PlanExit
		}
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		call.Nonce = hex.EncodeToString(b)
		if out != "" {
			raw, _ := json.Marshal(planFile{Stack: call.Stack, Nonce: call.Nonce, Destroy: has("-destroy")})
			_ = os.WriteFile(call.Plan, raw, 0o600)
		}
		logCall(bin, "tofu.log", call)
		fmt.Println("Plan: 2 to add, 0 to change, 0 to destroy.")
		return 0
	case sub == "show":
		call.Plan = resolve(positional)
		pf, ok := readPlan(call.Plan)
		call.Nonce = pf.Nonce
		if has("-json") {
			call.Cmd = "show-json"
			logCall(bin, "tofu.log", call)
			if !ok {
				fmt.Fprintln(os.Stderr, "Error: no plan file")
				return 1
			}
			changes := []any{}
			for _, c := range st.Changes {
				changes = append(changes, map[string]any{"address": c.Address, "change": map[string]any{"actions": c.Actions}})
			}
			raw, _ := json.Marshal(map[string]any{"format_version": "1.2", "terraform_version": "1.13.0", "stack": pf.Stack, "nonce": pf.Nonce,
				"destroy": pf.Destroy, "planned_values": map[string]any{}, "resource_changes": changes})
			fmt.Println(string(raw))
			return 0
		}
		call.Cmd = "show"
		logCall(bin, "tofu.log", call)
		fmt.Printf("fake-rendered-plan-marker %s\n  + client_secret = %q\n", call.Stack, secret)
		return 0
	case sub == "apply" || sub == "destroy":
		call.Plan = resolve(positional)
		pf, planOK := readPlan(call.Plan)
		call.Nonce = pf.Nonce
		if sub == "destroy" || has("-destroy") || (planOK && pf.Destroy) {
			call.Cmd = "destroy"
			if sub == "destroy" && !fakeInitialised(call.DataDir, statePath) {
				logCall(bin, "tofu.log", call)
				return 1
			}
			if statePath != "" {
				var s fakeState
				raw, err := os.ReadFile(statePath)
				// MissingStateEmpty: a missing local state reads as empty, nothing to decrypt
				// (T038: the `--cleanup` of a run that wrote no state; believed, UNVERIFIED for
				// OpenTofu 1.13 with state encryption). Otherwise a missing state fails as before.
				ok := st.MissingStateEmpty && errors.Is(err, os.ErrNotExist) ||
					err == nil && json.Unmarshal(raw, &s) == nil && s.PassSHA == passSHA(os.Getenv("TF_VAR_state_passphrase"))
				call.PassOK = &ok
				if !ok {
					logCall(bin, "tofu.log", call)
					fmt.Fprintln(os.Stderr, "Error: cannot decrypt state")
					return 1
				}
			}
			var sigs chan os.Signal
			if st.DestroyHang || st.DestroyDelay > 0 {
				sigs = make(chan os.Signal, 8) // trapped before the call is logged, as for HangAfter
				signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			}
			logCall(bin, "tofu.log", call)
			if st.DestroyHang {
				return hang(bin, call.Stack, sigs, false)
			}
			if st.DestroyDelay > 0 {
				if code := fakeDelay(bin, call.Stack, st.DestroyDelay, sigs); code != 0 {
					return code
				}
			}
			if len(st.DestroyErrors) > 0 {
				if n := nthCall(bin, call.Stack, "destroy"); n < len(st.DestroyErrors) {
					fmt.Fprintln(os.Stderr, st.DestroyErrors[n])
					return 1
				}
			}
			if st.DestroyExit != 0 {
				fmt.Fprintf(os.Stderr, "Error: fake destroy failure token=%s\n", secret)
				return st.DestroyExit
			}
			if statePath != "" {
				raw, _ := json.Marshal(fakeState{PassSHA: passSHA(os.Getenv("TF_VAR_state_passphrase"))})
				_ = os.WriteFile(statePath, raw, 0o600)
			}
			return 0
		}
		call.Cmd = "apply"
		var sigs chan os.Signal
		if st.HangAfter > 0 || st.ApplyDelay > 0 {
			// Trapped before the call is logged, so a signal sent once the call (or the stream) is
			// seen is logged.
			sigs = make(chan os.Signal, 8)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		}
		logCall(bin, "tofu.log", call)
		if !planOK {
			fmt.Fprintln(os.Stderr, "Error: apply without a saved plan file")
			return 1
		}
		fmt.Fprintf(os.Stderr, "fake-tofu-stderr-marker token=%s passphrase=%s\n", secret, os.Getenv("TF_VAR_state_passphrase"))
		if statePath != "" {
			raw, _ := json.Marshal(fakeState{PassSHA: passSHA(os.Getenv("TF_VAR_state_passphrase")), Resources: 1})
			_ = os.WriteFile(statePath, raw, 0o600)
		}
		if st.HoldLock {
			if err := os.WriteFile(lock, nil, 0o600); err == nil {
				defer os.Remove(lock)
			}
		}
		if st.ApplyStream != "" {
			raw, _ := os.ReadFile(st.ApplyStream)
			for i, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
				fmt.Println(l)
				if st.HangAfter > 0 && i+1 == st.HangAfter {
					return hang(bin, call.Stack, sigs, st.IgnoreInterrupt)
				}
				if st.HoldLock && i == 1 {
					// Lock held, one resource being created: wait for the second writer.
					if !waitFor(10*time.Second, func() bool { return exists(filepath.Join(bin, "refused-"+call.Stack)) }) {
						logCall(bin, "tofu.log", tofuCall{Cmd: "lock-timeout", Stack: call.Stack})
					}
				}
			}
		}
		if st.ApplyDelay > 0 {
			if code := fakeDelay(bin, call.Stack, st.ApplyDelay, sigs); code != 0 {
				return code
			}
		}
		if st.ApplyExit != 0 {
			fmt.Fprintf(os.Stderr, "Error: fake apply failure token=%s\n", secret)
		}
		return st.ApplyExit
	case sub == "state" && len(rest) > 0 && rest[0] == "list":
		call.Cmd = "state-list"
		logCall(bin, "tofu.log", call)
		if !fakeInitialised(call.DataDir, statePath) {
			return 1
		}
		for _, l := range st.StateList {
			fmt.Println(l)
		}
		return 0
	}
	call.Cmd = "other"
	logCall(bin, "tofu.log", call)
	return 0
}

// hang models a tofu apply that is still running: it logs every SIGINT/SIGTERM it receives as a
// "signal" call (args: the signal) and stops on the first, as tofu stops gracefully on an interrupt;
// with ignore it keeps running, so only a kill stops it (no further log line). After 60 s it logs
// "hang-timeout": nobody stopped it.
func hang(bin, stack string, sigs chan os.Signal, ignore bool) int {
	if ignore {
		// Like tofu's provider plugins: a child in tofu's process group, not on its streams.
		plugin := exec.Command(os.Args[0], "plugin-hang")
		if plugin.Start() == nil {
			_ = os.WriteFile(filepath.Join(bin, "plugin-"+stack+".pid"), []byte(strconv.Itoa(plugin.Process.Pid)), 0o600)
		}
	}
	// Written once the apply hangs: a test's deadline is reached there (T114).
	_ = os.WriteFile(filepath.Join(bin, "hanging-"+stack), nil, 0o600)
	timeout := time.After(60 * time.Second)
	for {
		select {
		case sig := <-sigs:
			logCall(bin, "tofu.log", tofuCall{Cmd: "signal", Stack: stack, Args: []string{sig.String()}})
			if !ignore {
				return 130
			}
		case <-timeout:
			logCall(bin, "tofu.log", tofuCall{Cmd: "hang-timeout", Stack: stack})
			return 9
		}
	}
}

// signals returns the signals the hanging apply of stack received, in order.
func signals(calls []tofuCall, stack string) []string {
	var out []string
	for _, c := range calls {
		if c.Cmd == "signal" && c.Stack == stack {
			out = append(out, c.Args...)
		}
	}
	return out
}

// fakeOvhcloud logs its environment and answers every listing with an empty array.
func fakeOvhcloud() int {
	logCall(filepath.Dir(os.Args[0]), "ovhcloud.log", tofuCall{Cmd: "ovhcloud", Args: os.Args[1:]})
	fmt.Println("[]")
	return 0
}

// ---------------------------------------------------------------- world

type runWorld struct {
	bin, tofu, ovhcloud string
	stacks              string // parent of the stack directories
	runDir              string
	term                *syncBuffer
	scenario            tofuScenario
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newRunWorld(t *testing.T) *runWorld {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	w := &runWorld{bin: t.TempDir(), stacks: t.TempDir(), term: &syncBuffer{}, scenario: tofuScenario{Stacks: map[string]tofuStack{}}}
	w.runDir = filepath.Join(tempPrivate(t), "live", "20261006T120000Z-a1b2")
	w.tofu = filepath.Join(w.bin, "tofu")
	w.ovhcloud = filepath.Join(w.bin, "ovhcloud")
	for _, l := range []string{w.tofu, w.ovhcloud} {
		if err := os.Symlink(self, l); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// stack adds a stack directory and its fake behaviour; by default its apply streams two created
// resources of a matrix kind.
func (w *runWorld) stack(t *testing.T, id string, ephemeral bool, st tofuStack) Stack {
	t.Helper()
	dir := filepath.Join(w.stacks, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if st.ApplyStream == "" {
		st.ApplyStream = w.stream(t, id, resourceLine{"ovh_cloud_project_network_private", id + "_1", "pn-" + id + "-1"}, resourceLine{"ovh_cloud_project_network_private", id + "_2", "pn-" + id + "-2"})
	}
	w.scenario.Stacks[id] = st
	w.save(t)
	return Stack{ID: id, Dir: dir, Ephemeral: ephemeral}
}

func (w *runWorld) save(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(w.scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.bin, "tofu.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

type resourceLine struct{ typ, name, id string }

// stream writes a `tofu apply -json` stream in the captured format: the captured version line,
// then for each resource the captured apply_start and apply_complete lines with the address,
// type, name, id and message substituted.
func (w *runWorld) stream(t *testing.T, id string, rs ...resourceLine) string {
	t.Helper()
	lines := streamLines(t, "apply-ok.jsonl")
	var version, start, complete string
	for _, l := range lines {
		switch parseEvent(t, l).Type {
		case "version":
			version = l
		case "apply_start":
			if start == "" {
				start = l
			}
		case "apply_complete":
			if complete == "" {
				complete = l
			}
		}
	}
	out := []string{version}
	for _, r := range rs {
		for _, tmpl := range []string{start, complete} {
			var m map[string]any
			if err := json.Unmarshal([]byte(tmpl), &m); err != nil {
				t.Fatal(err)
			}
			hook := m["hook"].(map[string]any)
			res := hook["resource"].(map[string]any)
			addr := r.typ + "." + r.name
			res["addr"], res["resource"], res["resource_type"], res["resource_name"] = addr, addr, r.typ, r.name
			res["implied_provider"] = "ovh"
			if _, ok := hook["id_value"]; ok {
				hook["id_value"] = r.id
				m["@message"] = addr + ": Creation complete after 0s [id=" + r.id + "]"
			} else {
				m["@message"] = addr + ": Creating..."
			}
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, string(raw))
		}
	}
	path := filepath.Join(w.bin, "stream-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// cleanCheck is the leftover check of the clean synthetic world (seed applied, if any).
func cleanCheck(t testing.TB, seed string) (LeftoverCheck, *fakeLister) {
	t.Helper()
	w, seeds := loadSynthetic(t)
	s := syntheticSeed{}
	if seed != "" {
		var ok bool
		if s, ok = seeds.Seeds[seed]; !ok {
			t.Fatalf("no seed %s", seed)
		}
	}
	c, l, _ := worldCheck(t, w, s)
	return c, l
}

var runCreds = map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": "EU.tenantdeployer1", "OVH_CLIENT_SECRET": seedSecret}

func (w *runWorld) runner(t *testing.T, stacks ...Stack) Runner {
	t.Helper()
	c, _ := cleanCheck(t, "")
	return Runner{
		ID:               "20261006T120000Z-a1b2",
		Dir:              w.runDir,
		Tofu:             w.tofu,
		Authority:        AuthorityTenant,
		Creds:            runCreds,
		Stacks:           stacks,
		Deadline:         testWaitCap,
		Grace:            testGrace,
		companionBackoff: testBackoff,
		Protect: func(s Stack, plan []byte) error {
			var p planFile
			_ = json.Unmarshal(plan, &p)
			logCall(w.bin, "tofu.log", tofuCall{Cmd: "protect", Stack: s.ID, Nonce: p.Nonce})
			return nil
		},
		Leftovers: c,
		Terminal:  w.term,
		Signals:   make(chan os.Signal), // in-process: never fires
	}
}

func (w *runWorld) calls(t *testing.T, name string) []tofuCall {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.bin, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []tofuCall
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var c tofuCall
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatalf("log line: %v", err)
		}
		out = append(out, c)
	}
	return out
}

// sequence returns the stacks of the calls with cmd, in order.
func sequence(calls []tofuCall, cmd string) []string {
	var out []string
	for _, c := range calls {
		if c.Cmd == cmd {
			out = append(out, c.Stack)
		}
	}
	return out
}

func (w *runWorld) summary(t *testing.T) map[string]any {
	t.Helper()
	var s map[string]any
	readJSON(t, filepath.Join(w.runDir, "summary.json"), &s)
	return s
}

func execute(t *testing.T, r Runner) (error, time.Duration) {
	t.Helper()
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- r.Execute(context.Background()) }()
	select {
	case err := <-done:
		return err, time.Since(start)
	case <-time.After(45 * time.Second):
		t.Fatal("Execute did not return within 45 s")
		return nil, 0
	}
}

// hanging returns a channel that closes once stack's apply hangs (tofuStack.HangAfter): the run's
// deadline is reached there (Runner.deadlineAt, T114).
func (w *runWorld) hanging(t *testing.T, stack string) <-chan struct{} {
	t.Helper()
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		waitFor(testWaitCap, func() bool { return exists(filepath.Join(w.bin, "hanging-"+stack)) })
	}()
	return ch
}

// ---------------------------------------------------------------- controls

func TestRunnerDefaultDeadline(t *testing.T) {
	t.Parallel()
	if DefaultDeadline != 45*time.Minute {
		t.Errorf("DefaultDeadline = %v, want 45m (FR-011)", DefaultDeadline)
	}
	// A run without --deadline (0) runs under the default, which summary.json records.
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}))
	r.Deadline = 0
	if err, _ := execute(t, r); err != nil {
		t.Fatalf("Execute with Deadline 0: %v", err)
	}
	d, err := time.ParseDuration(fmt.Sprint(w.summary(t)["deadline"]))
	if err != nil || d != DefaultDeadline {
		t.Errorf("summary.json deadline = %v (%v), want %v", w.summary(t)["deadline"], err, DefaultDeadline)
	}
}

// testWaitCap bounds every wait a test runner injects, well under the offline entry's 240 s child
// limit (T114): a test that hangs then fails within it instead of holding the whole package.
const testWaitCap = 30 * time.Second

// testGrace and testBackoff are the grace period and companion backoff of every test runner (T114):
// a fake tofu stops at once on an interrupt, and a retry needs no real wait.
var (
	testGrace   = 5 * time.Second
	testBackoff = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}
)

// TestTestRunnersInjectWaits: T114 — every runner the tests build (the run world's, which every
// probe harness starts from; the fresh lz-live process's; the chain harness's) injects its
// deadline, grace period and companion backoff (the chain harness also the retained state read's
// backoff, T110), so no test runs under a production default (45 min, 2 min, 20 s and 60 s; 10 s
// and 30 s); and those production defaults are unchanged.
func TestTestRunnersInjectWaits(t *testing.T) {
	t.Parallel()
	if DefaultDeadline != 45*time.Minute || DefaultGrace != 2*time.Minute ||
		!slices.Equal(defaultCompanionBackoff, []time.Duration{20 * time.Second, 60 * time.Second}) {
		t.Errorf("production defaults changed: deadline %v, grace %v, companion backoff %v; want 45m, 2m, [20s 1m]",
			DefaultDeadline, DefaultGrace, defaultCompanionBackoff)
	}
	short := func(who, what string, d time.Duration) {
		if d <= 0 || d > testWaitCap {
			t.Errorf("%s: %s %v, want an injected wait in (0, %v] (0 is the production default)", who, what, d, testWaitCap)
		}
	}
	runner := func(who string, r Runner) {
		short(who, "Deadline", r.Deadline)
		short(who, "Grace", r.Grace)
		if len(r.companionBackoff) == 0 {
			t.Errorf("%s: companionBackoff %v, want injected waits (nil is the production default)", who, r.companionBackoff)
		}
		for _, b := range r.companionBackoff {
			short(who, "companionBackoff", b)
		}
	}
	w := newRunWorld(t)
	runner("runWorld.runner", w.runner(t))
	runner("fakeRunner (zero config)", fakeRunner(fakeRunConfig{}))
	short("chain harness (zero chainExec)", "Deadline", chainExec{}.chainDeadline())
	// T110: the retained state read's backoff, injected by the chain harness, default 10 s and 30 s.
	if !slices.Equal(defaultStateReadBackoff, []time.Duration{10 * time.Second, 30 * time.Second}) {
		t.Errorf("production state-read backoff %v, want [10s 30s]", defaultStateReadBackoff)
	}
	if b := (chainExec{}).stateReadBackoff(); len(b) == 0 {
		t.Errorf("chain harness (zero chainExec): stateReadBackoff %v, want injected waits (nil is the production default)", b)
	} else {
		for _, d := range b {
			short("chain harness (zero chainExec)", "stateReadBackoff", d)
		}
	}
}

// TestRunnerAppliesInOrderDestroysEphemeralInReverse: a run that succeeds still destroys its
// ephemeral stacks on exit, last applied first; a retained stack is never destroyed.
func TestRunnerAppliesInOrderDestroysEphemeralInReverse(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	a, r, b := w.stack(t, "a", true, tofuStack{}), w.stack(t, "r", false, tofuStack{}), w.stack(t, "b", true, tofuStack{})
	err, _ := execute(t, w.runner(t, a, r, b))
	if err != nil {
		t.Fatalf("Execute: %v\n%s", err, w.term.String())
	}
	calls := w.calls(t, "tofu.log")
	if got := sequence(calls, "apply"); !slices.Equal(got, []string{"a", "r", "b"}) {
		t.Errorf("applies %v, want [a r b]", got)
	}
	if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("destroys %v, want [b a] (reverse, ephemeral only)", got)
	}
	// T055 review: init never writes the dependency lock file into the reviewed checkout (a
	// changed .terraform.lock.hcl makes the tree dirty and the guard refuses the next run and the
	// cleanup); the saved plan files (they hold root variable values) do not outlive the run.
	for _, c := range calls {
		if c.Cmd == "init" && !slices.Contains(c.Args, "-lockfile=readonly") {
			t.Errorf("tofu init %v without -lockfile=readonly", c.Args)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(w.runDir, "*.tfplan")); len(left) != 0 {
		t.Errorf("saved plan files left in the run record: %v", left)
	}
	inv := readInventory(t, w.runDir)
	if len(inv) != 6 {
		t.Errorf("inventory holds %d entries, want 6 (two per stack)", len(inv))
	}
	if s := w.summary(t); s["outcome"] != "pass" {
		t.Errorf("summary.json outcome = %v, want pass", s["outcome"])
	}
	term := w.term.String()
	for _, want := range []string{"LZ-LIVE summary 20261006T120000Z-a1b2 pass", "record approximate cost for run 20261006T120000Z-a1b2"} {
		if !strings.Contains(term, want) {
			t.Errorf("terminal lacks %q:\n%s", want, term)
		}
	}
}

// TestRunnerDestroyOnExit: G8 — a failed apply destroys every ephemeral stack whose apply started,
// in reverse order, and nothing after it is planned; a failed destroy does not stop the others;
// either way the run fails.
func TestRunnerDestroyOnExit(t *testing.T) {
	t.Parallel()
	t.Run("apply-failure", func(t *testing.T) {
		w := newRunWorld(t)
		s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "r", false, tofuStack{}), w.stack(t, "b", true, tofuStack{}),
			w.stack(t, "c", true, tofuStack{ApplyExit: 1}), w.stack(t, "d", true, tofuStack{})}
		err, _ := execute(t, w.runner(t, s...))
		if ExitCode(err) == 0 {
			t.Error("a failed apply exited 0")
		}
		calls := w.calls(t, "tofu.log")
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"c", "b", "a"}) {
			t.Errorf("destroys %v, want [c b a]", got)
		}
		if slices.Contains(sequence(calls, "plan"), "d") || slices.Contains(sequence(calls, "apply"), "d") {
			t.Error("the run went on to stack d after c failed")
		}
		if s := w.summary(t); s["outcome"] != "fail" {
			t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
		}
		if !strings.Contains(w.term.String(), "LZ-LIVE summary 20261006T120000Z-a1b2 fail") {
			t.Errorf("terminal lacks the fail summary:\n%s", w.term.String())
		}
	})
	t.Run("destroy-failure-continues", func(t *testing.T) {
		w := newRunWorld(t)
		s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{DestroyExit: 1}), w.stack(t, "c", true, tofuStack{})}
		err, _ := execute(t, w.runner(t, s...))
		if ExitCode(err) == 0 {
			t.Error("a failed destroy exited 0")
		}
		if got := sequence(w.calls(t, "tofu.log"), "destroy"); !slices.Equal(got, []string{"c", "b", "a"}) {
			t.Errorf("destroys %v, want [c b a] (a still destroyed after b failed)", got)
		}
		if term := w.term.String(); strings.Contains(term, seedSecret) || !strings.Contains(term, "fake-tofu-destroy-stderr") {
			t.Errorf("the destroy children's stderr is missing from the terminal or carries the secret:\n%s", term)
		}
		if s := w.summary(t); s["outcome"] != "fail" {
			t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
		}
	})
	t.Run("plan-failure", func(t *testing.T) {
		w := newRunWorld(t)
		s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{PlanExit: 1})}
		err, _ := execute(t, w.runner(t, s...))
		if ExitCode(err) == 0 {
			t.Error("a failed plan exited 0")
		}
		calls := w.calls(t, "tofu.log")
		if slices.Contains(sequence(calls, "apply"), "b") {
			t.Error("b applied after its plan failed")
		}
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"a"}) {
			t.Errorf("destroys %v, want [a]", got)
		}
	})
}

// TestRunnerDestroyJudgedPlan (T047, coordinator decision 2: one destroy discipline): the run core
// destroys an ephemeral stack as the chain does: `plan -destroy` to a file, `show -json` of that
// file judged by the guard, and exactly that file applied; never `tofu destroy -auto-approve`. A
// destroy plan the guard refuses is not applied, the destroy goes on with the next stack, the
// leftover check still runs, and the run exits 3.
func TestRunnerDestroyJudgedPlan(t *testing.T) {
	t.Parallel()
	judged := func(t *testing.T, calls []tofuCall, id string) {
		t.Helper()
		var plan, protect, applied string
		for _, c := range calls {
			if c.Stack != id {
				continue
			}
			switch {
			case c.Cmd == "plan" && slices.Contains(c.Args, "-destroy"):
				plan = c.Nonce
			case c.Cmd == "protect" && c.Nonce == plan && plan != "":
				protect = c.Nonce
			case c.Cmd == "destroy":
				applied = c.Nonce
				if protect == "" || applied != protect {
					t.Errorf("%s destroyed (plan %q) before its destroy plan %q was judged (%q)", id, applied, plan, protect)
				}
			}
		}
		if applied == "" {
			t.Errorf("%s: no saved destroy plan applied", id)
		}
	}
	t.Run("judged-saved-plan", func(t *testing.T) {
		w := newRunWorld(t)
		s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "r", false, tofuStack{}), w.stack(t, "b", true, tofuStack{})}
		if err, _ := execute(t, w.runner(t, s...)); err != nil {
			t.Fatalf("Execute: %v\n%s", err, w.term.String())
		}
		calls := w.calls(t, "tofu.log")
		for _, c := range calls {
			if slices.Contains(c.Args, "destroy") || slices.Contains(c.Args, "-auto-approve") {
				t.Errorf("tofu %v: a destroy that no guard judged", c.Args)
			}
		}
		judged(t, calls, "b")
		judged(t, calls, "a")
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"b", "a"}) {
			t.Errorf("destroys %v, want [b a]", got)
		}
		if slices.ContainsFunc(calls, func(c tofuCall) bool { return c.Stack == "r" && slices.Contains(c.Args, "-destroy") }) {
			t.Error("a destroy plan of the retained stack")
		}
	})
	t.Run("refused-destroy-plan", func(t *testing.T) {
		w := newRunWorld(t)
		s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{})}
		r := w.runner(t, s...)
		hook := r.Protect
		r.Protect = func(st Stack, plan []byte) error {
			var p planFile
			_ = json.Unmarshal(plan, &p)
			if err := hook(st, plan); err != nil || !p.Destroy || st.ID != "b" {
				return err
			}
			return refuse(CondRetained, "fake refusal of the destroy plan of b")
		}
		err, _ := execute(t, r)
		if ExitCode(err) != RefusalExit {
			t.Errorf("exit %d (%v), want %d", ExitCode(err), err, RefusalExit)
		}
		if got := sequence(w.calls(t, "tofu.log"), "destroy"); !slices.Equal(got, []string{"a"}) {
			t.Errorf("destroys %v, want [a] (b's refused plan not applied, a still destroyed)", got)
		}
		if s := w.summary(t); s["outcome"] != "fail" {
			t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
		}
		if _, err := os.Stat(filepath.Join(w.runDir, "leftovers.json")); err != nil {
			t.Errorf("the leftover check did not run after a refused destroy: %v", err)
		}
	})
}

// TestRunnerDeadline: G8 — the deadline stops a hanging apply, and the destroy still runs (with
// its own time, not the expired one), in reverse order; what was created before the hang is in
// the inventory.
func TestRunnerDeadline(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{HangAfter: 3})}
	r := w.runner(t, s...)
	r.deadlineAt = w.hanging(t, "b")
	err, took := execute(t, r)
	if ExitCode(err) == 0 {
		t.Error("a run past its deadline exited 0")
	}
	if took > 30*time.Second {
		t.Errorf("Execute took %v: the deadline did not stop the hanging apply", took)
	}
	if got := sequence(w.calls(t, "tofu.log"), "destroy"); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("destroys %v, want [b a]", got)
	}
	found := false
	for _, e := range readInventory(t, w.runDir) {
		found = found || e.Stack == "b" && e.ID == "pn-b-1"
	}
	if !found {
		t.Error("the resource b created before the hang is not in the inventory")
	}
	if s := w.summary(t); s["outcome"] != "fail" {
		t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
	}
	// Coordinator decision (T055): the deadline interrupts the running tofu (it may write its
	// state), once, rather than killing it.
	if got := signals(w.calls(t, "tofu.log"), "b"); !slices.Equal(got, []string{"interrupt"}) {
		t.Errorf("the hanging apply of b received %v at the deadline, want [interrupt]", got)
	}
}

// TestRunnerSignals: G8 — SIGINT and SIGTERM to a fresh lz-live process (no injected channel)
// stop the hanging apply and fire the destroy in reverse order; the process exits non-zero.
func TestRunnerSignals(t *testing.T) {
	t.Parallel()
	for name, sig := range map[string]syscall.Signal{"SIGINT": syscall.SIGINT, "SIGTERM": syscall.SIGTERM, "SIGHUP": syscall.SIGHUP} {
		t.Run(name, func(t *testing.T) {
			w := newRunWorld(t)
			w.stack(t, "a", true, tofuStack{})
			w.stack(t, "b", true, tofuStack{HangAfter: 3})
			cmd := w.subprocess(t, fakeRunConfig{Mode: "execute", Stacks: []fakeRunStack{{"a", true}, {"b", true}}, DeadlineMS: 60000})
			out := &syncBuffer{}
			cmd.Stdout, cmd.Stderr = out, out
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			pgid := cmd.Process.Pid
			t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
			if !waitFor(20*time.Second, func() bool { return slices.Contains(sequence(w.calls(t, "tofu.log"), "apply"), "b") }) {
				_ = cmd.Process.Kill()
				t.Fatalf("apply of b never started:\n%s", out)
			}
			// While b hangs, the resource it created is already in the inventory (not parsed later).
			if !waitFor(10*time.Second, func() bool {
				entries, _ := ReadInventory(w.runDir)
				for _, e := range entries {
					if e.Stack == "b" && e.ID == "pn-b-1" {
						return true
					}
				}
				return false
			}) {
				t.Error("the resource b created before the hang is not in inventory.jsonl while b still runs")
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err == nil {
					t.Errorf("lz-live exited 0 after %s", name)
				}
			case <-time.After(30 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("lz-live did not stop within 30 s of %s", name)
			}
			if got := sequence(w.calls(t, "tofu.log"), "destroy"); !slices.Equal(got, []string{"b", "a"}) {
				t.Errorf("destroys after %s: %v, want [b a]", name, got)
			}
			// Coordinator decision (T055): the signal is forwarded to the running tofu, which
			// stops by itself; it is not killed.
			// A closed terminal (SIGHUP) stops tofu like an interrupt: tofu has no graceful
			// SIGHUP (believed).
			want := []string{sig.String()}
			if sig == syscall.SIGHUP {
				want = []string{syscall.SIGINT.String()}
			}
			if got := signals(w.calls(t, "tofu.log"), "b"); !slices.Equal(got, want) {
				t.Errorf("the hanging apply of b received %v after %s, want %v (forwarded once)", got, name, want)
			}
		})
	}
}

// ignoredGrace is TestRunnerInterruptGrace's grace period (T114: 2 s before; the assertions are
// relative to it, and the Ctrl-C row also checks the ignoring tofu is alive a third into it).
const ignoredGrace = time.Second

// TestRunnerInterruptGrace: coordinator decision (T055) — a tofu that does not stop after the
// forwarded interrupt is killed once the grace period has passed, and the destroy still runs. A
// Ctrl-C at the terminal signals lz-live's whole process group: tofu runs in a group of its own,
// so it receives the interrupt once (from lz-live), not twice (a second interrupt makes tofu exit
// at once, without writing its state; believed, OpenTofu docs not checked).
func TestRunnerInterruptGrace(t *testing.T) {
	t.Parallel()
	t.Run("deadline", func(t *testing.T) {
		w := newRunWorld(t)
		s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{HangAfter: 3, IgnoreInterrupt: true})}
		r := w.runner(t, s...)
		r.Grace, r.deadlineAt = ignoredGrace, w.hanging(t, "b")
		err, took := execute(t, r)
		if ExitCode(err) == 0 {
			t.Error("a run past its deadline exited 0")
		}
		if took > 25*time.Second {
			t.Errorf("Execute took %v: the tofu ignoring the interrupt was not killed after the grace period", took)
		}
		calls := w.calls(t, "tofu.log")
		if got := signals(calls, "b"); !slices.Equal(got, []string{"interrupt"}) {
			t.Errorf("the hanging apply of b received %v, want [interrupt] before the kill", got)
		}
		// T055 review: the kill reaches tofu's whole process group, so its plugins do not outlive it.
		raw, err := os.ReadFile(filepath.Join(w.bin, "plugin-b.pid"))
		pid, _ := strconv.Atoi(string(raw))
		if err != nil || pid <= 0 {
			t.Fatalf("the hanging apply started no plugin: %v", err)
		}
		if !waitFor(5*time.Second, func() bool { return syscall.Kill(pid, 0) != nil }) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Error("the plugin of the killed tofu is still running: the kill did not reach tofu's process group")
		}
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"b", "a"}) {
			t.Errorf("destroys %v, want [b a]", got)
		}
	})
	t.Run("terminal-ctrl-c", func(t *testing.T) {
		w := newRunWorld(t)
		w.stack(t, "a", true, tofuStack{})
		w.stack(t, "b", true, tofuStack{HangAfter: 3, IgnoreInterrupt: true})
		cmd := w.subprocess(t, fakeRunConfig{Mode: "execute", Stacks: []fakeRunStack{{"a", true}, {"b", true}}, DeadlineMS: 60000, GraceMS: int(ignoredGrace / time.Millisecond)})
		out := &syncBuffer{}
		cmd.Stdout, cmd.Stderr = out, out
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		pgid := cmd.Process.Pid
		t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
		if !waitFor(20*time.Second, func() bool {
			entries, _ := ReadInventory(w.runDir)
			return slices.ContainsFunc(entries, func(e InventoryEntry) bool { return e.Stack == "b" })
		}) {
			_ = cmd.Process.Kill()
			t.Fatalf("apply of b never streamed:\n%s", out)
		}
		// The hanging tofu's plugin: alive until the kill reaches tofu's group (T114 review r1:
		// the lower bound on lz-live's exit alone also counts the destroys' time).
		if !waitFor(20*time.Second, func() bool { return exists(filepath.Join(w.bin, "hanging-b")) }) {
			t.Fatalf("apply of b never hung:\n%s", out)
		}
		raw, err := os.ReadFile(filepath.Join(w.bin, "plugin-b.pid"))
		pid, _ := strconv.Atoi(string(raw))
		if err != nil || pid <= 0 {
			t.Fatalf("the hanging apply started no plugin: %v", err)
		}
		start := time.Now()
		if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil { // the terminal's Ctrl-C
			t.Fatal(err)
		}
		time.Sleep(ignoredGrace / 3)
		// Judged only while the grace period still runs: a sleep overrun on a loaded host is no
		// finding (review r2).
		if syscall.Kill(pid, 0) != nil && time.Since(start) < ignoredGrace {
			t.Errorf("the plugin of the tofu ignoring Ctrl-C is gone %v after it: killed before the grace period", ignoredGrace/3)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				t.Error("lz-live exited 0 after Ctrl-C")
			}
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatalf("lz-live did not stop within 30 s of Ctrl-C:\n%s", out)
		}
		if took := time.Since(start); took < ignoredGrace {
			t.Errorf("lz-live stopped %v after Ctrl-C: the tofu ignoring it was killed before the grace period", took)
		}
		calls := w.calls(t, "tofu.log")
		if got := signals(calls, "b"); !slices.Equal(got, []string{"interrupt"}) {
			t.Errorf("the hanging apply of b received %v after one Ctrl-C, want exactly [interrupt]", got)
		}
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"b", "a"}) {
			t.Errorf("destroys after Ctrl-C: %v, want [b a]", got)
		}
		// lz-live was started as the leader of its own group (pgid): a child in that group gets
		// the terminal's Ctrl-C directly as well as the forwarded one.
		for _, c := range calls {
			if c.Cmd != "protect" && c.Cmd != "signal" && c.Pgrp == pgid {
				t.Errorf("tofu %s runs in lz-live's process group %d: a Ctrl-C reaches it twice", c.Cmd, pgid)
			}
		}
		if len(sequence(calls, "hang-timeout")) != 0 {
			t.Error("the hanging apply was never stopped")
		}
	})
}

// TestRunnerDefaultProtect: coordinator decision (T055) — without an injected hook the run core
// judges every saved plan with live.Protect (protect.go, T064) against the run's retained set: a
// plan deleting a retained address is refused (exit 3) and never applied; the same plan with an
// empty retained set is applied.
func TestRunnerDefaultProtect(t *testing.T) {
	t.Parallel()
	del := tofuStack{Changes: []planChange{{"ovh_cloud_project_storage.state", []string{"delete"}}}}
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, del))
	r.Protect = nil
	r.Retained = []Retained{{Instance: "account-bootstrap", Addresses: []string{"ovh_cloud_project_storage.state"}}}
	err, _ := execute(t, r)
	if ExitCode(err) != RefusalExit || err == nil || !strings.Contains(err.Error(), "ovh_cloud_project_storage.state") {
		t.Errorf("Execute: %v (exit %d), want a retained refusal naming the address, exit %d", err, ExitCode(err), RefusalExit)
	}
	if got := sequence(w.calls(t, "tofu.log"), "apply"); len(got) != 0 {
		t.Errorf("applies %v after the default guard refused the plan", got)
	}

	w = newRunWorld(t)
	r = w.runner(t, w.stack(t, "a", true, del))
	r.Protect = nil
	if err, _ := execute(t, r); err != nil {
		t.Fatalf("Execute with no retained resource: %v", err)
	}
	if got := sequence(w.calls(t, "tofu.log"), "apply"); !slices.Equal(got, []string{"a"}) {
		t.Errorf("applies %v, want [a]", got)
	}
}

// TestRunnerPlanOnly: `lz-live probe --plan-only` — every stack is planned and its saved plan
// judged by the guard, nothing is applied or destroyed, the leftover check still lists (read
// only) and the run record says pass.
func TestRunnerPlanOnly(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{}))
	r.PlanOnly = true
	if err, _ := execute(t, r); err != nil {
		t.Fatalf("Execute: %v\n%s", err, w.term.String())
	}
	calls := w.calls(t, "tofu.log")
	if got := sequence(calls, "protect"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("guard saw %v, want [a b]", got)
	}
	if got := append(sequence(calls, "apply"), sequence(calls, "destroy")...); len(got) != 0 {
		t.Errorf("a plan-only run applied or destroyed %v", got)
	}
	if len(r.Leftovers.Lister.(*fakeLister).Asked()) == 0 {
		t.Error("a plan-only run skipped the leftover listings")
	}
	if s := w.summary(t); s["outcome"] != "pass" {
		t.Errorf("summary.json outcome = %v, want pass", s["outcome"])
	}
	if _, err := os.Stat(filepath.Join(w.runDir, "plan-a.txt")); err != nil {
		t.Errorf("no rendered plan for a: %v", err)
	}
}

// TestRunnerProtectBeforeApply: every plan goes through the retained-resource guard (given
// `tofu show -json` of the saved plan) before exactly that plan file is applied.
func TestRunnerProtectBeforeApply(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "r", false, tofuStack{})}
	if err, _ := execute(t, w.runner(t, s...)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	calls := w.calls(t, "tofu.log")
	for _, id := range []string{"a", "r"} {
		plan, protect, apply := -1, -1, -1
		for i, c := range calls {
			if c.Stack != id {
				continue
			}
			switch {
			case c.Cmd == "plan" && plan < 0:
				plan = i
			case c.Cmd == "protect" && protect < 0:
				protect = i
			case c.Cmd == "apply" && apply < 0:
				apply = i
			}
		}
		if plan < 0 || protect < 0 || apply < 0 || !(plan < protect && protect < apply) {
			t.Errorf("%s: plan at %d, protect at %d, apply at %d; want plan < protect < apply", id, plan, protect, apply)
			continue
		}
		if calls[plan].Plan == "" || calls[apply].Plan != calls[plan].Plan {
			t.Errorf("%s: applied %q, want the saved plan file %q", id, calls[apply].Plan, calls[plan].Plan)
		}
		if calls[protect].Nonce == "" || calls[protect].Nonce != calls[plan].Nonce || calls[apply].Nonce != calls[plan].Nonce {
			t.Errorf("%s: guard saw plan %q, applied plan %q, want both %q", id, calls[protect].Nonce, calls[apply].Nonce, calls[plan].Nonce)
		}
	}
}

// TestRunnerProtectRefusalStops: a guard refusal stops the run before that plan is applied;
// ephemeral stacks already applied are destroyed; the exit is a refusal (3).
func TestRunnerProtectRefusalStops(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{}), w.stack(t, "c", true, tofuStack{})}
	r := w.runner(t, s...)
	r.Protect = func(s Stack, plan []byte) error {
		if s.ID == "b" {
			return &Refusal{Condition: "retained", Detail: "plan deletes a retained resource"}
		}
		return nil
	}
	err, _ := execute(t, r)
	if ExitCode(err) != RefusalExit {
		t.Errorf("exit %d (%v), want %d", ExitCode(err), err, RefusalExit)
	}
	calls := w.calls(t, "tofu.log")
	if got := sequence(calls, "apply"); !slices.Equal(got, []string{"a"}) {
		t.Errorf("applies %v, want [a]: a refused plan is never applied", got)
	}
	if slices.Contains(sequence(calls, "plan"), "c") {
		t.Error("c planned after b was refused")
	}
	if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"a"}) {
		t.Errorf("destroys %v, want [a]", got)
	}
	if s := w.summary(t); s["outcome"] != "fail" {
		t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
	}
	if !strings.Contains(w.term.String(), "LZ-LIVE summary 20261006T120000Z-a1b2 fail") {
		t.Errorf("terminal lacks the fail summary:\n%s", w.term.String())
	}
	// T055 review r2: the refused plan file goes too.
	if left, _ := filepath.Glob(filepath.Join(w.runDir, "*.tfplan")); len(left) != 0 {
		t.Errorf("saved plan files left after the refusal: %v", left)
	}
}

// deadTerminal fails every write after the first n bytes, like a terminal that was hung up.
type deadTerminal struct {
	mu sync.Mutex
	n  int
}

func (d *deadTerminal) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.n <= 0 {
		return 0, syscall.EIO
	}
	d.n -= len(p)
	return len(p), nil
}

// TestRunnerDeadTerminal: T055 review r2 — once the terminal fails (SIGHUP: hung up, EIO), the
// children's output is dropped, not their pipes closed: a child writing to a closed pipe dies of
// SIGPIPE, which would kill the destroy-on-exit.
func TestRunnerDeadTerminal(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{}))
	r.Terminal = &deadTerminal{n: 64}
	if err, _ := execute(t, r); err != nil {
		t.Errorf("Execute with a dead terminal: %v", err)
	}
	calls := w.calls(t, "tofu.log")
	if got := sequence(calls, "apply"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("applies %v, want [a b]", got)
	}
	if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("destroys %v with a dead terminal, want [b a]", got)
	}
}

// TestRunnerErrorRedacted: G2 — the error Execute returns (lz-live prints it on stderr) holds no
// secret, also when a failure's detail carries one, and keeps its exit code.
func TestRunnerErrorRedacted(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}))
	r.Protect = func(Stack, []byte) error {
		return &Refusal{Condition: CondRetained, Detail: "plan echoes " + seedSecret}
	}
	// A listed leftover whose name carries the secret: it lands in leftovers.json.
	l := r.Leftovers.Lister.(*fakeLister)
	l.responses["/cloud/project/p1/network/private"] = []json.RawMessage{json.RawMessage(`[{"id":"pn-leak","name":"` + r.Leftovers.Prefix + `leak-` + seedSecret + `","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`)}
	l.responses["/cloud/project/p1/network/private/pn-leak/subnet"] = []json.RawMessage{json.RawMessage(`[]`)}
	err, _ := execute(t, r)
	if err == nil || strings.Contains(err.Error(), seedSecret) || !strings.Contains(err.Error(), "plan echoes") {
		t.Errorf("Execute error %q: want the detail without the secret", err)
	}
	// T055 review: summary.json carries the error text, leftovers.json the leftover's name.
	for file, marker := range map[string]string{"summary.json": "plan echoes", "leftovers.json": "pn-leak"} {
		raw, rerr := os.ReadFile(filepath.Join(w.runDir, file))
		if rerr != nil || bytes.Contains(raw, []byte(seedSecret)) || !bytes.Contains(raw, []byte(marker)) {
			t.Errorf("%s (%v) holds the secret or lacks %q:\n%s", file, rerr, marker, raw)
		}
	}
	if ExitCode(err) != RefusalExit {
		t.Errorf("exit %d, want %d: redaction must keep the refusal", ExitCode(err), RefusalExit)
	}
}

// TestRunnerStateListFallback: with P24 refuted, `tofu state list` after each stack fills the
// inventory.
// A failed apply is the case the fallback exists for: the captured failing apply leaves the
// errored resource in the state but not in the stream.
func TestRunnerStateListFallback(t *testing.T) {
	t.Parallel()
	for _, failing := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "apply-failure"}[failing], func(t *testing.T) {
			w := newRunWorld(t)
			noEvents := w.stream(t, "a-none")
			st := tofuStack{ApplyStream: noEvents, StateList: []string{"ovh_cloud_project_network_private.a", "module.m.ovh_cloud_project_network_private_subnet.s"}}
			if failing {
				st.ApplyExit = 1
			}
			r := w.runner(t, w.stack(t, "a", true, st))
			r.StateListFallback = true
			if err, _ := execute(t, r); failing == (err == nil) {
				t.Fatalf("Execute: %v", err)
			}
			var got []string
			for _, e := range readInventory(t, w.runDir) {
				got = append(got, e.Stack+" "+e.Address+" "+e.Type)
			}
			want := []string{"a ovh_cloud_project_network_private.a ovh_cloud_project_network_private", "a module.m.ovh_cloud_project_network_private_subnet.s ovh_cloud_project_network_private_subnet"}
			if !slices.Equal(got, want) {
				t.Errorf("inventory %v, want %v", got, want)
			}
		})
	}
}

// TestRunnerRedactsEveryOutput: G2 — the seeded secret a child prints on stdout and stderr, in a
// created id, in a rendered plan and in a listing reaches neither the terminal nor any file of the
// run record (inventory.jsonl, listings/, summary.json, plan-<id>.txt), nor any child's argv;
// the non-secret part of each output does arrive.
func TestRunnerRedactsEveryOutput(t *testing.T) {
	t.Parallel()
	for _, failing := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "apply-failure"}[failing], func(t *testing.T) {
			w := newRunWorld(t)
			st := tofuStack{ApplyStream: w.stream(t, "a", resourceLine{"ovh_cloud_project_network_private", "leak", seedSecret})}
			if failing {
				st.ApplyExit = 1
			}
			r := w.runner(t, w.stack(t, "a", true, st))
			l := r.Leftovers.Lister.(*fakeLister)
			l.responses["/cloud/project/p1/network/private"] = []json.RawMessage{json.RawMessage(`[{"id":"pn-111_0","name":"net-marker-` + seedSecret + `","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`)}
			l.responses["/cloud/project/p1/network/private/pn-111_0/subnet"] = []json.RawMessage{json.RawMessage(`[]`)}
			err, _ := execute(t, r)
			if failing == (err == nil) {
				t.Fatalf("Execute: %v", err)
			}
			term := w.term.String()
			if strings.Contains(term, seedSecret) {
				t.Errorf("terminal holds the seeded secret:\n%s", term)
			}
			for _, marker := range []string{"fake-tofu-stderr-marker", "fake-tofu-plan-stderr", "fake-tofu-destroy-stderr", "Creation complete"} {
				if !strings.Contains(term, marker) {
					t.Errorf("terminal lacks %q (a child stream never reached it):\n%s", marker, term)
				}
			}
			for rel, marker := range map[string]string{
				"inventory.jsonl": "ovh_cloud_project_network_private.leak",
				"summary.json":    "outcome",
				"plan-a.txt":      "fake-rendered-plan-marker",
				filepath.Join("listings", "ovh_cloud_project_network_private.json"): "net-marker-",
			} {
				raw, err := os.ReadFile(filepath.Join(w.runDir, rel))
				if err != nil {
					t.Errorf("run record lacks %s: %v", rel, err)
					continue
				}
				if !strings.Contains(string(raw), marker) {
					t.Errorf("%s lacks %q", rel, marker)
				}
			}
			_ = filepath.WalkDir(w.runDir, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					if raw, _ := os.ReadFile(p); bytes.Contains(raw, []byte(seedSecret)) {
						t.Errorf("%s holds the seeded secret", p)
					}
				}
				return nil
			})
			for _, c := range w.calls(t, "tofu.log") {
				if strings.Contains(strings.Join(c.Args, " "), seedSecret) {
					t.Errorf("tofu %s argv holds the seeded secret", c.Cmd)
				}
			}
		})
	}
}

// TestRunnerChildHome: coordinator decision — every child (tofu; since T084 the leftover check
// starts none, P18) runs with a
// per-run scratch HOME: not the caller's, 0700, the same for the whole run, removed when the run
// ends; a ~/.ovh.conf or ~/.aws/credentials planted in the caller's HOME is not visible; the child
// environment stays T053's allowlist (no ambient variable, the authority's credentials present).
func TestRunnerChildHome(t *testing.T) {
	callerHome := t.TempDir()
	writeFile(t, filepath.Join(callerHome, ".ovh.conf"), "[default]\nendpoint=ovh-eu\n[ovh-eu]\nclient_id=ambient-admin\nclient_secret=ambient-admin-secret\n")
	if err := os.Mkdir(filepath.Join(callerHome, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(callerHome, ".aws", "credentials"), "[default]\naws_access_key_id=ambient-aws\naws_secret_access_key=ambient-aws-secret\n")
	t.Setenv("HOME", callerHome)
	for k, v := range ambient {
		t.Setenv(k, v)
	}
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}))
	_, _ = execute(t, r)

	calls := w.calls(t, "tofu.log")
	homes := map[string]bool{}
	sawTofu := false
	for _, c := range calls {
		if c.Cmd == "protect" {
			continue
		}
		sawTofu = true
		homes[c.Home] = true
		if c.Home == "" || c.Home == callerHome || strings.HasPrefix(c.Home, callerHome+string(filepath.Separator)) {
			t.Errorf("%s ran with HOME %q (caller's HOME %q)", c.Cmd, c.Home, callerHome)
		}
		if c.HomeMode != 0o700 {
			t.Errorf("%s: HOME mode %#o, want 0700", c.Cmd, c.HomeMode)
		}
		if strings.Contains(strings.Join(c.Args, " "), seedSecret) {
			t.Errorf("%s argv holds the seeded secret", c.Cmd)
		}
		if c.OvhConf || c.AWSCreds {
			t.Errorf("%s sees the caller's ~/.ovh.conf (%v) or ~/.aws/credentials (%v)", c.Cmd, c.OvhConf, c.AWSCreds)
		}
		env := map[string]string{}
		for _, kv := range c.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
			if strings.Contains(v, "ambient-") {
				t.Errorf("%s: caller value reached the child: %s", c.Cmd, k)
			}
			if _, own := runCreds[k]; !own && k != "PATH" && k != "HOME" && k != "PWD" && !strings.HasPrefix(k, "TF_") {
				t.Errorf("%s carries %s, which is neither PATH, HOME, TF_* nor a credential", c.Cmd, k)
			}
		}
		for k, v := range runCreds {
			if env[k] != v {
				t.Errorf("%s: %s = %q, want the authority's value", c.Cmd, k, env[k])
			}
		}
	}
	if !sawTofu {
		t.Error("the run started no tofu child")
	}
	if len(homes) != 1 {
		t.Errorf("children ran with %d different HOMEs, want one per run: %v", len(homes), homes)
	}
	for h := range homes {
		if _, err := os.Stat(h); h != "" && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("scratch HOME %s still exists after the run (%v)", h, err)
		}
	}
	// Per run: a second run gets its own HOME.
	w2 := newRunWorld(t)
	_, _ = execute(t, w2.runner(t, w2.stack(t, "a", true, tofuStack{})))
	for _, c := range w2.calls(t, "tofu.log") {
		if c.Cmd != "protect" && homes[c.Home] {
			t.Errorf("a second run reused the scratch HOME %s", c.Home)
		}
	}
}

// ---------------------------------------------------------------- probe state

const (
	probeAccount = "ab12345-ovh"
	probeProject = "p1"
)

func (w *runWorld) probe(t *testing.T, root string, st tofuStack) Probe {
	t.Helper()
	s := w.stack(t, "p", true, st)
	r := w.runner(t, s)
	// The probes' project, as lz-live passes it (T074); a run records it for its cleanup (T077).
	r.Vars = map[string]string{"TF_VAR_project_id": probeProject}
	return Probe{Run: r, ConfigRoot: root, Account: probeAccount}
}

func probeFiles(t *testing.T, root string) (dir, state, pass string) {
	t.Helper()
	dir = filepath.Join(root, "accounts", probeAccount, "state", "probes", "20261006T120000Z-a1b2")
	return dir, filepath.Join(dir, "p.tfstate"), filepath.Join(dir, "passphrase.env")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// TestRunnerDataDirPerStack: T074 review r1 — a run of several stacks gives each stack its own
// data directory (backend configuration and module manifest are per root): with one directory for
// the run, the second stack's init replaces the first's, and the first stack's destroy-on-exit
// then runs against the other root's data. Every call of one stack (init, plan, show, apply, state
// list, destroy) uses that stack's directory; none is in a stack root or the run record.
func TestRunnerDataDirPerStack(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{})}
	r := w.runner(t, s...)
	r.StateListFallback = true
	if err, _ := execute(t, r); err != nil {
		t.Fatalf("run of two stacks: %v\n%s", err, w.term)
	}
	dirs := map[string]string{}
	cmds := map[string][]string{}
	for _, c := range w.calls(t, "tofu.log") {
		if c.Cmd == "protect" || c.Cmd == "signal" {
			continue
		}
		cmds[c.Stack] = append(cmds[c.Stack], c.Cmd)
		switch {
		case within(c.DataDir, resolvedPath(w.stacks)) || within(c.DataDir, resolvedPath(w.runDir)):
			t.Errorf("stack %s: tofu %s with its data directory %s in a stack root or the run record", c.Stack, c.Cmd, c.DataDir)
		case dirs[c.Stack] == "":
			dirs[c.Stack] = c.DataDir
		case dirs[c.Stack] != c.DataDir:
			t.Errorf("stack %s: tofu %s with data directory %s, earlier calls %s: one per stack", c.Stack, c.Cmd, c.DataDir, dirs[c.Stack])
		}
	}
	for _, id := range []string{"a", "b"} {
		for _, want := range []string{"init", "apply", "state-list", "destroy"} {
			if !slices.Contains(cmds[id], want) {
				t.Errorf("stack %s: no tofu %s (calls %v)", id, want, cmds[id])
			}
		}
	}
	if dirs["a"] == "" || dirs["a"] == dirs["b"] {
		t.Errorf("stacks a and b share the data directory %q: the second init replaces the first's backend and modules", dirs["a"])
	}
}

// TestProbeState: G9 probe part. A probe keeps its state and a private per-run passphrase under
// accounts/<account>/state/probes/<run-id>/ until destroy and the leftover check pass; a fresh
// process resumes the cleanup from them; both files are deleted only when both pass.
func TestProbeState(t *testing.T) {
	t.Run("dir", func(t *testing.T) {
		root := tempPrivate(t)
		got, err := ProbeDir(root, probeAccount, "20261006T120000Z-a1b2")
		want, _, _ := probeFiles(t, root)
		if err != nil || got != want {
			t.Errorf("ProbeDir = %q, %v; want %q", got, err, want)
		}
		for _, bad := range []string{"", "..", "../x", "a/b"} {
			if _, err := ProbeDir(root, probeAccount, bad); err == nil {
				t.Errorf("ProbeDir accepted run id %q", bad)
			}
		}
		for _, bad := range []string{"", "..", "../x", "a/b", "a/../../x"} {
			if _, err := ProbeDir(root, bad, "20261006T120000Z-a1b2"); err == nil {
				t.Errorf("ProbeDir accepted account %q", bad)
			}
		}
	})

	t.Run("failed-destroy-keeps-then-fresh-process-cleans", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{DestroyExit: 1})
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a probe whose destroy failed exited 0")
		}
		dir, state, pass := probeFiles(t, root)
		if !exists(state) || !exists(pass) {
			t.Fatalf("after a failed destroy: state kept %v, passphrase kept %v; want both", exists(state), exists(pass))
		}
		if m := mode(t, dir).Perm(); m != 0o700 {
			t.Errorf("probe directory mode %#o, want 0700", m)
		}
		if m := mode(t, pass); !m.IsRegular() || m.Perm() != 0o600 {
			t.Errorf("passphrase.env mode %v, want a 0600 regular file", m)
		}
		values, err := ReadCredentialFile(pass)
		if err != nil {
			t.Fatal(err)
		}
		phrase := values["TF_VAR_state_passphrase"]
		if len(phrase) < 16 {
			t.Fatalf("passphrase of %d characters, want at least 16", len(phrase))
		}
		var applied bool
		for _, c := range w.calls(t, "tofu.log") {
			if c.Cmd != "apply" {
				continue
			}
			applied = true
			env := map[string]string{}
			for _, kv := range c.Env {
				k, v, _ := strings.Cut(kv, "=")
				env[k] = v
			}
			if env["TF_VAR_state_passphrase"] != phrase || env["TF_VAR_state_path"] != state {
				t.Errorf("apply ran with state %q and another passphrase than passphrase.env (want state %q)", env["TF_VAR_state_path"], state)
			}
		}
		if !applied {
			t.Fatal("the probe never applied")
		}
		if strings.Contains(w.term.String(), phrase) {
			t.Error("the passphrase reached the terminal")
		}
		_ = filepath.WalkDir(w.runDir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if raw, _ := os.ReadFile(p); bytes.Contains(raw, []byte(phrase)) {
					t.Errorf("%s holds the passphrase", p)
				}
			}
			return nil
		})

		// A fresh process: the destroy now works; nothing is carried over in memory.
		w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
		w.save(t)
		passing := func() int {
			n := 0
			for _, c := range w.calls(t, "tofu.log") {
				if c.Cmd == "destroy" && c.PassOK != nil && *c.PassOK {
					n++
				}
			}
			return n
		}
		before := passing()
		// A destroy is a saved `plan -destroy` applied (T047): only forward plans and applies count.
		forward := func(calls []tofuCall) int {
			n := 0
			for _, c := range calls {
				if c.Cmd == "plan" && !slices.Contains(c.Args, "-destroy") || c.Cmd == "apply" {
					n++
				}
			}
			return n
		}
		applies := forward(w.calls(t, "tofu.log"))
		lister := filepath.Join(w.bin, "lister.log")
		out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}, ListerLog: lister}).CombinedOutput()
		if err != nil {
			t.Fatalf("cleanup process: %v\n%s", err, out)
		}
		if passing() == before {
			t.Error("the cleanup process never destroyed the probe with the retained passphrase")
		}
		if n := forward(w.calls(t, "tofu.log")); n != applies {
			t.Errorf("the cleanup process planned or applied %d times: it must only destroy", n-applies)
		}
		for _, c := range w.calls(t, "tofu.log") {
			if strings.Contains(strings.Join(c.Args, " "), phrase) {
				t.Errorf("the passphrase is in the argv of tofu %s", c.Cmd)
			}
		}
		if raw, _ := os.ReadFile(lister); !strings.Contains(string(raw), "/iam/policy#1") {
			t.Error("the cleanup process never ran the leftover check")
		}
		if exists(state) || exists(pass) {
			t.Errorf("after a passing cleanup: state kept %v, passphrase kept %v; want both deleted", exists(state), exists(pass))
		}
		if strings.Contains(string(out), phrase) {
			t.Error("the cleanup process printed the passphrase")
		}
	})

	t.Run("cleanup-keeps-on-leftover-or-failed-destroy", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{DestroyExit: 1})
		_ = p.Start(context.Background())
		_, state, pass := probeFiles(t, root)
		// The destroy fails again.
		if out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}}).CombinedOutput(); err == nil {
			t.Errorf("cleanup with a failing destroy exited 0:\n%s", out)
		}
		if !exists(state) || !exists(pass) {
			t.Fatalf("after a failed cleanup destroy: state kept %v, passphrase kept %v; want both", exists(state), exists(pass))
		}
		// The destroy works, but a leftover is listed.
		w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
		w.save(t)
		if out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}, Seed: "network"}).CombinedOutput(); err == nil {
			t.Errorf("cleanup with a leftover exited 0:\n%s", out)
		}
		if !exists(state) || !exists(pass) {
			t.Fatalf("after a cleanup whose leftover check failed: state kept %v, passphrase kept %v; want both", exists(state), exists(pass))
		}
		// Clean now: both go.
		if out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}}).CombinedOutput(); err != nil {
			t.Fatalf("clean cleanup: %v\n%s", err, out)
		}
		if exists(state) || exists(pass) {
			t.Error("a passing cleanup kept the probe's files")
		}
	})

	t.Run("cleanup-without-passphrase-destroys-nothing", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{DestroyExit: 1})
		_ = p.Start(context.Background())
		_, _, pass := probeFiles(t, root)
		if err := os.Remove(pass); err != nil {
			t.Fatal(err)
		}
		before := len(sequence(w.calls(t, "tofu.log"), "destroy"))
		w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
		w.save(t)
		if err := p.Cleanup(context.Background()); err == nil {
			t.Error("cleanup without the passphrase succeeded")
		}
		if after := len(sequence(w.calls(t, "tofu.log"), "destroy")); after != before {
			t.Errorf("cleanup without the passphrase ran %d destroys", after-before)
		}
	})

	t.Run("passing-run-deletes", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{})
		if err := p.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v\n%s", err, w.term.String())
		}
		_, state, pass := probeFiles(t, root)
		if exists(state) || exists(pass) {
			t.Errorf("after a passing probe: state kept %v, passphrase kept %v; want both deleted", exists(state), exists(pass))
		}
		if len(p.Run.Leftovers.Lister.(*fakeLister).Asked()) == 0 {
			t.Error("the probe never ran the leftover check")
		}
		if got := sequence(w.calls(t, "tofu.log"), "destroy"); !slices.Equal(got, []string{"p"}) {
			t.Errorf("destroys %v, want [p]", got)
		}
	})

	t.Run("leftover-keeps", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{})
		c, _ := cleanCheck(t, "network")
		p.Run.Leftovers = c
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Error("a probe with a leftover exited 0")
		}
		_, state, pass := probeFiles(t, root)
		if !exists(state) || !exists(pass) {
			t.Errorf("after a failed leftover check: state kept %v, passphrase kept %v; want both", exists(state), exists(pass))
		}
	})

	t.Run("passphrase-per-run", func(t *testing.T) {
		var phrases []string
		for i := 0; i < 2; i++ {
			root := tempPrivate(t)
			w := newRunWorld(t)
			p := w.probe(t, root, tofuStack{DestroyExit: 1})
			_ = p.Start(context.Background())
			_, _, pass := probeFiles(t, root)
			v, err := ReadCredentialFile(pass)
			if err != nil {
				t.Fatal(err)
			}
			phrases = append(phrases, v["TF_VAR_state_passphrase"])
		}
		if phrases[0] == "" || phrases[0] == phrases[1] {
			t.Errorf("two runs share the passphrase %q", phrases[0])
		}
	})
}

// TestProbeNoStateNoFiles: T055 review r2 — a probe that failed before writing any state (here
// its plan failed) has nothing to destroy: its passphrase and probe record are removed whatever the
// outcome, since no cleanup could use them.
func TestProbeNoStateNoFiles(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{PlanExit: 1})
	if err := p.Start(context.Background()); ExitCode(err) == 0 {
		t.Fatal("a probe whose plan failed exited 0")
	}
	dir, _, _ := probeFiles(t, root)
	if exists(dir) {
		left, _ := os.ReadDir(dir)
		t.Errorf("a probe that wrote no state kept %v", left)
	}
}

// TestProbeCleanupShortPassphrase: a retained passphrase shorter than 16 characters is not the
// one Start wrote; cleanup refuses it and destroys nothing.
func TestProbeCleanupShortPassphrase(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{DestroyExit: 1})
	_ = p.Start(context.Background())
	_, _, pass := probeFiles(t, root)
	if err := os.WriteFile(pass, []byte("TF_VAR_state_passphrase=short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(sequence(w.calls(t, "tofu.log"), "destroy"))
	w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
	w.save(t)
	if err := p.Cleanup(context.Background()); err == nil {
		t.Error("cleanup with a 5-character passphrase succeeded")
	}
	if after := len(sequence(w.calls(t, "tofu.log"), "destroy")); after != before {
		t.Errorf("cleanup with a short passphrase ran %d destroys", after-before)
	}
}

// ---------------------------------------------------------------- probe re-runs

// treeSnapshot maps every path under dir (relative; directories end in "/") to its content.
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		raw, err := os.ReadFile(p)
		out[rel] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// TestProbeRerun (T073; found in T008, decision request 4): the backend path of a probe root
// changes with every run id, and tofu keeps the backend it initialised in its data directory. A
// root initialised in place (<root>/.terraform) therefore fails `init` with "Backend configuration
// changed" on its second run under a new run id, and on a `--cleanup` of an older run after a
// later one (host tofu 1.10.3, t073-reinit.sh). Each run initialises in a TF_DATA_DIR of its own,
// outside the probe root and the run record (both in the reviewed checkout), so the second run
// and the cleanup initialise cleanly and the root is left exactly as it was: nothing added,
// changed or deleted, a `.terraform/` left by an older runner included. Every probe child also
// receives the run id as TF_VAR_run_id.
func TestProbeRerun(t *testing.T) {
	t.Parallel()
	const id2 = "20261006T130000Z-c3d4"
	for _, stale := range []bool{false, true} {
		name := "clean-root"
		if stale {
			name = "stale-terraform-dir-kept"
		}
		t.Run(name, func(t *testing.T) {
			root := tempPrivate(t)
			w := newRunWorld(t)
			p1 := w.probe(t, root, tofuStack{DestroyExit: 1})
			dir := p1.Run.Stacks[0].Dir
			if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte("# probe\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			unchanged := func(phase string, before map[string]string) {
				t.Helper()
				after := treeSnapshot(t, dir)
				for p, v := range before {
					if got, ok := after[p]; !ok {
						t.Errorf("%s: the probe root lost %s", phase, p)
					} else if got != v {
						t.Errorf("%s: the probe root's %s changed", phase, p)
					}
				}
				for p := range after {
					if _, ok := before[p]; !ok {
						t.Errorf("%s: a run wrote %s into the probe root", phase, p)
					}
				}
			}
			before := treeSnapshot(t, dir)

			// Run 1 applies, its destroy fails: state and passphrase stay for a cleanup.
			if err := p1.Start(context.Background()); ExitCode(err) == 0 {
				t.Fatal("run 1: a probe whose destroy failed exited 0")
			}
			n1 := len(w.calls(t, "tofu.log"))
			unchanged("run 1", before)
			if stale {
				// What a later in-place init of this root left behind (another run id's backend;
				// gitignored, not ours to delete): the cleanup of run 1 must not trip over it either.
				if err := os.MkdirAll(filepath.Join(dir, ".terraform"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ".terraform", "terraform.tfstate"), []byte(`{"backend":{"type":"local","config":{"path":"/old/state/probes/20261006T123000Z-ffff/p.tfstate"}}}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before = treeSnapshot(t, dir)
			// Run 2 of the same root under a new run id, destroy working again.
			w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
			w.save(t)
			p2 := p1
			p2.Run.ID, p2.Run.Dir = id2, filepath.Join(filepath.Dir(w.runDir), id2)
			if err := p2.Start(context.Background()); err != nil {
				t.Errorf("run 2 of the same root under a new run id: %v", err)
			}
			n2 := len(w.calls(t, "tofu.log"))
			// A fresh process cleans up run 1 after run 2.
			if out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}}).CombinedOutput(); err != nil {
				t.Errorf("cleanup of run 1 after run 2: %v\n%s", err, out)
			} else if strings.Contains(string(out), "Backend") {
				t.Errorf("cleanup of run 1 after run 2 initialised against another backend:\n%s", out)
			}
			for _, msg := range []string{"Backend configuration changed", "Backend initialization required"} {
				if strings.Contains(w.term.String(), msg) {
					t.Errorf("a run's tofu said %q:\n%s", msg, w.term.String())
				}
			}

			calls := w.calls(t, "tofu.log")
			runs := []struct {
				name, id, record string
				calls            []tofuCall
			}{
				{"run 1", p1.Run.ID, p1.Run.Dir, calls[:n1]},
				{"run 2", id2, p2.Run.Dir, calls[n1:n2]},
				{"cleanup of run 1", p1.Run.ID, w.runDir, calls[n2:]},
			}
			dataDirs := make([]string, len(runs))
			for i, r := range runs {
				var inits, destroys int
				for _, c := range r.calls {
					if c.Cmd == "protect" || c.Cmd == "signal" {
						continue
					}
					env := map[string]string{}
					for _, kv := range c.Env {
						k, v, _ := strings.Cut(kv, "=")
						env[k] = v
					}
					// The fake logs where the data directory really is (relative to -chdir, symlinks
					// resolved), so neither a relative path nor a symlink hides a directory in the root.
					dd := c.DataDir
					switch {
					case env["TF_DATA_DIR"] == "":
						t.Errorf("%s: tofu %s without TF_DATA_DIR: want a data directory of its own", r.name, c.Cmd)
					case within(dd, resolvedPath(dir)) || within(dd, resolvedPath(r.record)) || within(dd, resolvedPath(filepath.Dir(w.runDir))):
						t.Errorf("%s: tofu %s with its data directory %s inside the probe root or the run record (the checkout)", r.name, c.Cmd, dd)
					case dataDirs[i] == "":
						dataDirs[i] = dd
					case dataDirs[i] != dd:
						t.Errorf("%s: tofu %s with TF_DATA_DIR %s, earlier calls %s: one data directory per run", r.name, c.Cmd, dd, dataDirs[i])
					}
					if got := env["TF_VAR_run_id"]; got != r.id {
						t.Errorf("%s: tofu %s with TF_VAR_run_id %q, want %q", r.name, c.Cmd, got, r.id)
					}
					switch c.Cmd {
					case "init":
						inits++
					case "destroy":
						destroys++
					}
				}
				if inits != 1 || destroys != 1 {
					t.Errorf("%s: %d inits and %d destroys, want one each", r.name, inits, destroys)
				}
			}
			if dataDirs[1] != "" && (dataDirs[1] == dataDirs[0] || dataDirs[1] == dataDirs[2]) {
				t.Errorf("run 2 shares its data directory %s with run 1 or its cleanup", dataDirs[1])
			}
			unchanged("run 2 and the cleanup", before)
		})
	}
}

// ---------------------------------------------------------------- scratch location and cleanup binding

func mkdirPrivate(t *testing.T, d string) string {
	t.Helper()
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

// sameTree fails for every path of dir added, changed or lost since before (a treeSnapshot).
func sameTree(t *testing.T, what, dir string, before map[string]string) {
	t.Helper()
	after := treeSnapshot(t, dir)
	for p, v := range before {
		if got, ok := after[p]; !ok {
			t.Errorf("%s lost %s", what, p)
		} else if got != v {
			t.Errorf("%s: %s changed", what, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("the refused run wrote %s into %s", p, what)
		}
	}
}

// exitOf is a finished process's exit code (-1: it did not run).
func exitOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// TestRunnerScratchOutsideRoots (T077; T074 review r1 gap): the scratch HOME, and with it every
// stack's data directory, is made under $TMPDIR (os.MkdirTemp). A TMPDIR inside the reviewed
// checkout would put the children's HOME, provider plugins and backend data there. The runner
// does not know the checkout; it knows the parts of it a run touches, the stack roots and the run
// record (.local/live/<run-id>/, among the other runs' records). A TMPDIR that resolves inside one
// of them (symlinks resolved) is refused (exit 3, naming TMPDIR) before any file (no run record,
// nothing in TMPDIR) and before any child or leftover listing, for a run, a probe start and a
// fresh-process cleanup alike. A TMPDIR beside them, a prefix sibling included, is admitted and
// used. (lz-live refuses the whole checkout: TestProbeScratchOutsideCheckout.)
func TestRunnerScratchOutsideRoots(t *testing.T) {
	for name, place := range map[string]func(t *testing.T, w *runWorld, stack string) string{
		"in-stack-root": func(t *testing.T, _ *runWorld, stack string) string {
			return mkdirPrivate(t, filepath.Join(stack, "tmp"))
		},
		"stack-root-itself": func(_ *testing.T, _ *runWorld, stack string) string { return stack },
		"symlink-into-stack-root": func(t *testing.T, _ *runWorld, stack string) string {
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(mkdirPrivate(t, filepath.Join(stack, "tmp")), alias); err != nil {
				t.Fatal(err)
			}
			return alias
		},
		// Review r1 (review leg 1): ".." after a symlink climbs from the link's target, as the kernel
		// resolves it; a check that cleans the path first looks beside the alias instead.
		"dotdot-after-symlink-into-stack-root": func(t *testing.T, _ *runWorld, stack string) string {
			mkdirPrivate(t, filepath.Join(stack, "tmp"))
			outside := t.TempDir()
			mkdirPrivate(t, filepath.Join(outside, "tmp")) // where the cleaned path points: it exists
			alias := filepath.Join(outside, "alias")
			if err := os.Symlink(mkdirPrivate(t, filepath.Join(stack, "sub")), alias); err != nil {
				t.Fatal(err)
			}
			return alias + string(filepath.Separator) + ".." + string(filepath.Separator) + "tmp"
		},
		// Review r1 (review leg 1): a relative TMPDIR is resolved against the working directory.
		"relative-in-stack-root": func(t *testing.T, _ *runWorld, stack string) string {
			mkdirPrivate(t, filepath.Join(stack, "tmp"))
			t.Chdir(stack)
			return "tmp"
		},
		"in-run-record": func(t *testing.T, w *runWorld, _ string) string {
			return mkdirPrivate(t, filepath.Join(w.runDir, "tmp"))
		},
		"in-run-records-dir": func(t *testing.T, w *runWorld, _ string) string {
			return mkdirPrivate(t, filepath.Join(filepath.Dir(w.runDir), "tmp"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newRunWorld(t)
			s := w.stack(t, "p", true, tofuStack{})
			r := w.runner(t, s)
			lister := r.Leftovers.Lister.(*fakeLister)
			records := filepath.Dir(filepath.Dir(w.runDir))
			tmp := place(t, w, s.Dir)
			stacksBefore, recordsBefore := treeSnapshot(t, w.stacks), treeSnapshot(t, records)
			t.Setenv("TMPDIR", tmp)
			err, _ := execute(t, r)
			if ExitCode(err) != 3 || !strings.Contains(fmt.Sprint(err), "TMPDIR") {
				t.Errorf("TMPDIR %s: error %v (exit %d); want a refusal (exit 3) naming TMPDIR", tmp, err, ExitCode(err))
			}
			if n := len(w.calls(t, "tofu.log")); n != 0 {
				t.Errorf("%d tofu calls before the refusal", n)
			}
			if asked := lister.Asked(); len(asked) != 0 {
				t.Errorf("the leftover check listed %v before the refusal", asked)
			}
			sameTree(t, "the stack roots", w.stacks, stacksBefore)
			sameTree(t, "the run records", records, recordsBefore)
		})
	}

	for name, place := range map[string]func(t *testing.T, w *runWorld) string{
		"stack-root-prefix-sibling":  func(t *testing.T, w *runWorld) string { return mkdirPrivate(t, filepath.Join(w.stacks, "p-tmp")) },
		"records-dir-prefix-sibling": func(t *testing.T, w *runWorld) string { return mkdirPrivate(t, filepath.Dir(w.runDir)+"-tmp") },
	} {
		t.Run(name, func(t *testing.T) {
			w := newRunWorld(t)
			r := w.runner(t, w.stack(t, "p", true, tofuStack{}))
			tmp := place(t, w)
			t.Setenv("TMPDIR", tmp)
			if err, _ := execute(t, r); err != nil {
				t.Fatalf("TMPDIR %s beside the stack roots and run records: %v\n%s", tmp, err, w.term)
			}
			calls := w.calls(t, "tofu.log")
			if len(calls) == 0 {
				t.Fatal("no tofu call")
			}
			for _, c := range calls {
				if c.Cmd != "protect" && !within(resolvedPath(c.Home), resolvedPath(tmp)) {
					t.Errorf("tofu %s ran with HOME %s, not under TMPDIR %s", c.Cmd, c.Home, tmp)
				}
			}
		})
	}

	// Review r2 (review leg 1): a stack root given through a symlink and ".." is the directory the kernel
	// resolves (the alias target's parent), not the alias's lexical parent.
	t.Run("stack-dir-dotdot-after-symlink", func(t *testing.T) {
		w := newRunWorld(t)
		s := w.stack(t, "p", true, tofuStack{})
		r := w.runner(t, s)
		tmp := mkdirPrivate(t, filepath.Join(s.Dir, "tmp"))
		outside := t.TempDir()
		alias := filepath.Join(outside, "alias")
		if err := os.Symlink(mkdirPrivate(t, filepath.Join(s.Dir, "sub")), alias); err != nil {
			t.Fatal(err)
		}
		r.Stacks[0].Dir = alias + string(filepath.Separator) + ".."
		t.Setenv("TMPDIR", tmp)
		err, _ := execute(t, r)
		if ExitCode(err) != 3 || !strings.Contains(fmt.Sprint(err), "TMPDIR") {
			t.Errorf("stack root %s, TMPDIR %s: error %v (exit %d); want a refusal (exit 3) naming TMPDIR", r.Stacks[0].Dir, tmp, err, ExitCode(err))
		}
		if n := len(w.calls(t, "tofu.log")); n != 0 {
			t.Errorf("%d tofu calls before the refusal", n)
		}
	})

	// Review r1 (review leg 1): a relative TMPDIR outside the roots is admitted, and the children get it as
	// one absolute directory: a relative HOME or TF_DATA_DIR would land under the stack root tofu
	// runs in.
	t.Run("relative-beside", func(t *testing.T) {
		w := newRunWorld(t)
		r := w.runner(t, w.stack(t, "p", true, tofuStack{}))
		wd := t.TempDir()
		tmp := mkdirPrivate(t, filepath.Join(wd, "scratch"))
		t.Chdir(wd)
		t.Setenv("TMPDIR", "scratch")
		if err, _ := execute(t, r); err != nil {
			t.Fatalf("relative TMPDIR beside the roots: %v\n%s", err, w.term)
		}
		calls := w.calls(t, "tofu.log")
		if len(calls) == 0 {
			t.Fatal("no tofu call")
		}
		for _, c := range calls {
			if c.Cmd == "protect" {
				continue
			}
			if !filepath.IsAbs(c.Home) || !within(resolvedPath(c.Home), resolvedPath(tmp)) {
				t.Errorf("tofu %s ran with HOME %q, want an absolute directory under TMPDIR %s", c.Cmd, c.Home, tmp)
			}
			if !within(c.DataDir, resolvedPath(tmp)) {
				t.Errorf("tofu %s used the data directory %s, not under TMPDIR %s", c.Cmd, c.DataDir, tmp)
			}
		}
	})

	// Review r2: with the obstruction below, a check placed right after a probe-file write whose
	// error it ignores still refuses; without it, that write leaves its file behind.
	t.Run("probe-start-leaves-no-probe-file", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{})
		tmp := mkdirPrivate(t, filepath.Join(p.Run.Stacks[0].Dir, "tmp"))
		before := treeSnapshot(t, root)
		t.Setenv("TMPDIR", tmp)
		if err := p.Start(context.Background()); ExitCode(err) != 3 || !strings.Contains(fmt.Sprint(err), "TMPDIR") {
			t.Errorf("probe start, TMPDIR %s: error %v; want a refusal (exit 3) naming TMPDIR", tmp, err)
		}
		if n := len(w.calls(t, "tofu.log")); n != 0 {
			t.Errorf("%d tofu calls before the refusal", n)
		}
		sameTree(t, "the probe state", root, before)
	})

	t.Run("probe-start-in-stack-root", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{})
		tmp := mkdirPrivate(t, filepath.Join(p.Run.Stacks[0].Dir, "tmp"))
		// Review r1: Probe.run removes the files of a run that wrote no state, so their absence
		// afterwards cannot show they were never written. A regular file where the probes'
		// directory goes makes any write of probe.env or passphrase.env fail before the refusal
		// could be reached, so only a refusal ahead of the files exits 3 naming TMPDIR.
		dir, _, _ := probeFiles(t, root)
		mkdirPrivate(t, filepath.Dir(filepath.Dir(dir)))
		writeFile(t, filepath.Dir(dir), "not a directory\n")
		records := filepath.Dir(filepath.Dir(w.runDir))
		stacksBefore, recordsBefore, cfgBefore := treeSnapshot(t, w.stacks), treeSnapshot(t, records), treeSnapshot(t, root)
		t.Setenv("TMPDIR", tmp)
		err := p.Start(context.Background())
		if ExitCode(err) != 3 || !strings.Contains(fmt.Sprint(err), "TMPDIR") {
			t.Errorf("probe start, TMPDIR %s: error %v; want a refusal (exit 3) naming TMPDIR before any probe file", tmp, err)
		}
		if n := len(w.calls(t, "tofu.log")); n != 0 {
			t.Errorf("%d tofu calls before the refusal", n)
		}
		sameTree(t, "the stack roots", w.stacks, stacksBefore)
		sameTree(t, "the run records", records, recordsBefore)
		sameTree(t, "the probe state", root, cfgBefore)
	})

	t.Run("cleanup-in-stack-root", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{DestroyExit: 1})
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a probe whose destroy failed exited 0")
		}
		w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
		w.save(t)
		n := len(w.calls(t, "tofu.log"))
		tmp := mkdirPrivate(t, filepath.Join(p.Run.Stacks[0].Dir, "tmp"))
		before := treeSnapshot(t, w.stacks)
		cmd := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}})
		cmd.Env = append(os.Environ(), "TMPDIR="+tmp)
		out, err := cmd.CombinedOutput()
		if code := exitOf(err); code != 3 || !strings.Contains(string(out), "TMPDIR") {
			t.Errorf("cleanup, TMPDIR %s: exit %d; want 3 naming TMPDIR\n%s", tmp, code, out)
		}
		if got := len(w.calls(t, "tofu.log")) - n; got != 0 {
			t.Errorf("%d tofu calls before the refusal", got)
		}
		if _, state, pass := probeFiles(t, root); !exists(state) || !exists(pass) {
			t.Errorf("the refused cleanup lost the probe's files: state %v, passphrase %v", exists(state), exists(pass))
		}
		sameTree(t, "the stack roots", w.stacks, before)
	})

	// T077 gap "the runner-level cleanup row does not watch the passphrase read", narrowed: a
	// passphrase file another user could read is refused when read (file-mode, not naming TMPDIR),
	// so the TMPDIR refusal must win over a passphrase read error. A read whose result is discarded
	// before the check still passes here (review r1/r2, review leg 1); lz-live's order is watched in
	// cmd/lz-live (TestProbeScratchOutsideCheckout/cleanup).
	t.Run("cleanup-refused-before-passphrase", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{DestroyExit: 1})
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a probe whose destroy failed exited 0")
		}
		_, _, pass := probeFiles(t, root)
		if err := os.Chmod(pass, 0o644); err != nil {
			t.Fatal(err)
		}
		tmp := mkdirPrivate(t, filepath.Join(p.Run.Stacks[0].Dir, "tmp"))
		cmd := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}})
		cmd.Env = append(os.Environ(), "TMPDIR="+tmp)
		out, err := cmd.CombinedOutput()
		if code := exitOf(err); code != 3 || !strings.Contains(string(out), "TMPDIR") {
			t.Errorf("cleanup, TMPDIR %s, passphrase readable by others: exit %d; want 3 naming TMPDIR (the refusal wins over the passphrase error)\n%s", tmp, code, out)
		}
	})
}

// TestProbeCleanupRecordedProject (T077; T074 review r1/r2 gap): a probe run records the project
// it ran against (TF_VAR_project_id, LZ_PROJECT_ID_STATE in lz-live) in its probe.env, and a
// cleanup destroys against that recorded project, whatever the cleanup's caller now passes (an
// account.env edited between the failed destroy and the cleanup). A run record without a project
// id is refused for cleanup: no child runs, the probe's files stay.
func TestProbeCleanupRecordedProject(t *testing.T) {
	t.Parallel()
	failedRun := func(t *testing.T) (*runWorld, string) {
		t.Helper()
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{DestroyExit: 1})
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a probe whose destroy failed exited 0")
		}
		w.scenario.Stacks["p"] = tofuStack{ApplyStream: w.scenario.Stacks["p"].ApplyStream}
		w.save(t)
		return w, root
	}
	recorded := func(t *testing.T, root string) map[string]string {
		t.Helper()
		dir, _, _ := probeFiles(t, root)
		v, err := ReadCredentialFile(filepath.Join(dir, "probe.env"))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	t.Run("recorded-and-used", func(t *testing.T) {
		w, root := failedRun(t)
		var found bool
		for _, v := range recorded(t, root) {
			found = found || v == probeProject
		}
		if !found {
			t.Errorf("probe.env %v records no project id %s", recorded(t, root), probeProject)
		}
		n := len(w.calls(t, "tofu.log"))
		out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}},
			Vars: map[string]string{"TF_VAR_project_id": "p9"}}).CombinedOutput()
		if err != nil {
			t.Errorf("cleanup: %v\n%s", err, out)
		}
		var destroyed bool
		for _, c := range w.calls(t, "tofu.log")[n:] {
			if c.Cmd == "protect" || c.Cmd == "signal" {
				continue
			}
			destroyed = destroyed || c.Cmd == "destroy"
			env := map[string]string{}
			for _, kv := range c.Env {
				k, v, _ := strings.Cut(kv, "=")
				env[k] = v
			}
			if env["TF_VAR_project_id"] != probeProject {
				t.Errorf("cleanup: tofu %s with TF_VAR_project_id %q, want the recorded %q (the caller now passes p9)", c.Cmd, env["TF_VAR_project_id"], probeProject)
			}
		}
		if !destroyed {
			t.Error("the cleanup never destroyed")
		}
	})

	// Review r1 (strong review leg): a probe start that could record no project id is refused before any probe
	// file or child, not after the apply, when its cleanup would be refused.
	t.Run("start-without-project-refused", func(t *testing.T) {
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.probe(t, root, tofuStack{})
		p.Run.Vars = nil
		before := treeSnapshot(t, root)
		err := p.Start(context.Background())
		if ExitCode(err) != RefusalExit || !strings.Contains(fmt.Sprint(err), "project id") {
			t.Errorf("probe start without a project id: %v (exit %d); want %d naming the project id", err, ExitCode(err), RefusalExit)
		}
		if n := len(w.calls(t, "tofu.log")); n != 0 {
			t.Errorf("%d tofu calls for a probe start without a project id", n)
		}
		sameTree(t, "the probe state", root, before)
	})

	t.Run("record-without-project-refused", func(t *testing.T) {
		w, root := failedRun(t)
		dir, state, pass := probeFiles(t, root)
		var lines []string
		for k, v := range recorded(t, root) {
			if v != probeProject {
				lines = append(lines, k+"="+v+"\n")
			}
		}
		slices.Sort(lines)
		if err := os.WriteFile(filepath.Join(dir, "probe.env"), []byte(strings.Join(lines, "")), 0o600); err != nil {
			t.Fatal(err)
		}
		n := len(w.calls(t, "tofu.log"))
		// The caller still passes the right project: only the record counts.
		out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}},
			Vars: map[string]string{"TF_VAR_project_id": probeProject}}).CombinedOutput()
		// "project id": the temporary directories carry the test name, so "project" alone would
		// match any path in an unrelated error (review r1). A refusal (exit 3), as a recorded root
		// outside the probes (T078, coordinator decision).
		if exitOf(err) != RefusalExit || !strings.Contains(string(out), "project id") {
			t.Errorf("cleanup of a record without a project id: exit %d; want %d naming the project id\n%s", exitOf(err), RefusalExit, out)
		}
		if got := len(w.calls(t, "tofu.log")) - n; got != 0 {
			t.Errorf("%d tofu calls for a record without a project id", got)
		}
		if !exists(state) || !exists(pass) {
			t.Errorf("the refused cleanup lost the probe's files: state %v, passphrase %v", exists(state), exists(pass))
		}
	})
}

// ---------------------------------------------------------------- fresh process

type fakeRunStack struct {
	ID        string
	Ephemeral bool
}

// fakeRunConfig is what "lz-fake-run" needs to rebuild a run in a fresh process.
type fakeRunConfig struct {
	Mode       string // execute | probe-cleanup
	Bin        string
	Stacks     []fakeRunStack
	StacksDir  string
	RunDir     string
	DeadlineMS int
	GraceMS    int
	ConfigRoot string
	Seed       string
	ListerLog  string
	Vars       map[string]string // Runner.Vars of the fresh process (lz-live's current TF_VAR_project_id)
}

func (w *runWorld) subprocess(t *testing.T, c fakeRunConfig) *exec.Cmd {
	t.Helper()
	c.Bin, c.StacksDir, c.RunDir = w.bin, w.stacks, w.runDir
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(cfg, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exec.Command(self, fakeRunArg, cfg)
}

const fakeRunArg = "lz-fake-run"

// fakeRun is a fresh lz-live process: it rebuilds the run from its config and nothing else (no
// injected signal channel, no passphrase), and exits with ExitCode.
func fakeRun(cfgPath string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "fake run: fixtures:", r)
			code = 2
		}
	}()
	var c fakeRunConfig
	raw, err := os.ReadFile(cfgPath)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		fmt.Fprintln(os.Stderr, "fake run: bad config")
		return 2
	}
	r := fakeRunner(c)
	switch c.Mode {
	case "execute":
		err = r.Execute(context.Background())
	case "probe-cleanup":
		err = Probe{Run: r, ConfigRoot: c.ConfigRoot, Account: probeAccount}.Cleanup(context.Background())
	default:
		err = fmt.Errorf("unknown mode %q", c.Mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake run:", err)
	}
	return ExitCode(err)
}

// fakeRunner is the Runner a fresh lz-live process (fakeRun) rebuilds from its config.
func fakeRunner(c fakeRunConfig) Runner {
	check, lister := cleanCheck(&fakeTB{}, c.Seed)
	lister.log = c.ListerLog
	r := Runner{
		ID: "20261006T120000Z-a1b2", Dir: c.RunDir, Tofu: filepath.Join(c.Bin, "tofu"), Authority: AuthorityTenant,
		Creds: runCreds, Deadline: time.Duration(c.DeadlineMS) * time.Millisecond, Grace: time.Duration(c.GraceMS) * time.Millisecond,
		Vars: c.Vars, Leftovers: check, Terminal: os.Stdout,
		Protect: func(Stack, []byte) error { return nil },
	}
	if r.Deadline == 0 {
		r.Deadline = testWaitCap
	}
	if r.Grace == 0 {
		r.Grace = testGrace
	}
	r.companionBackoff = testBackoff
	for _, s := range c.Stacks {
		r.Stacks = append(r.Stacks, Stack{ID: s.ID, Dir: filepath.Join(c.StacksDir, s.ID), Ephemeral: s.Ephemeral})
	}
	return r
}

// fakeTB lets the fixture helpers run outside a test: a fatal error panics (fakeRun recovers).
type fakeTB struct{ testing.TB }

func (fakeTB) Helper()                        {}
func (fakeTB) Fatal(a ...any)                 { panic(fmt.Sprint(a...)) }
func (fakeTB) Fatalf(format string, a ...any) { panic(fmt.Sprintf(format, a...)) }

// ---------------------------------------------------------------- T095

// Companion destroy (T095; T010 run 20261010T173007Z-637a lost its tenant identity to a transient
// OAuth2 500 on the companion's destroy plan, and the root's destroy then removed the identity,
// leaving the companion's resources out of every cleanup's reach). The companion's destroy — its
// `plan -destroy`, the judged plan and its apply, as one unit (a saved plan is stale after a
// failed apply) — is retried on a transient failure: a 5xx, an OAuth2 token fetch failure or a
// connection reset, never when the output names a 4xx; a bounded number of attempts, with backoff,
// never waiting past the destroy deadline. When it still fails, the root is not destroyed at all
// (coordinator choice, evidence/T095.md decision 1): the identities, the companion's state bucket
// and its S3 credential stay, the run ends `fail` naming the kept root and `--cleanup <run-id>`,
// and the leftover check reports the kept root's resources like any other (decision 2). A cleanup
// re-reads the identity from the root's state, binds it, destroys the companion and then the root,
// and keeps the root again while the companion cannot be destroyed.

const (
	errOAuth500  = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/network/private": oauth2: cannot fetch token: 500 Internal Server Error` + "\nResponse: {\"message\":\"Internal server error\"}"
	errAPI503    = `Error: calling Delete /cloud/project/p1/network/private/pn-1: OVHcloud API error (status code 503): Server::ServiceUnavailable: "Service unavailable" (X-OVH-Query-Id: EU.ext-1)`
	errReset     = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/region": read tcp 10.0.0.2:51234->192.0.2.10:443: read: connection reset by peer`
	errAPI403    = `Error: calling Delete /cloud/project/p1/network/private/pn-1: OVHcloud API error (status code 403): Client::Forbidden: "This call has not been granted" (X-OVH-Query-Id: EU.ext-2)`
	errOAuth401  = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/region": oauth2: cannot fetch token: 401 Unauthorized` + "\nResponse: {\"error\":\"invalid_client\"}"
	errOAuthCode = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/region": oauth2: "invalid_client" "Client authentication failed"`
	// A transport failure on the token endpoint, and a token answer whose status has no reason phrase.
	errOAuthDial = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/region": Post "https://www.ovh.com/auth/oauth2/token": dial tcp: lookup www.ovh.com: i/o timeout`
	errOAuthBare = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/region": oauth2: cannot fetch token: 520` + "\nResponse: "
	// Review r1: a bare 4xx token status is a 4xx; a number in a response body is no status.
	errOAuth401Bare = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/region": oauth2: cannot fetch token: 401` + "\nResponse: denied"
	errLocked       = `Error: Delete "https://eu.api.ovh.com/1.0/cloud/project/p1/storage/b1": 423 Locked`
	errOAuth500Body = `Error: Get "https://eu.api.ovh.com/1.0/cloud/project/p1/region": oauth2: cannot fetch token: 500 Internal Server Error` + "\nResponse: failed deleting resource 400 Test Network"
)

// destroyCalls counts stack's destroy plans and destroy applies in calls, and returns the times of
// its destroy plans.
func destroyCalls(calls []tofuCall, stack string) (plans, applies int, at []time.Time) {
	for _, c := range calls {
		switch {
		case c.Stack == stack && c.Cmd == "plan" && slices.Contains(c.Args, "-destroy"):
			plans++
			at = append(at, time.Unix(0, c.At))
		case c.Stack == stack && c.Cmd == "destroy":
			applies++
		}
	}
	return plans, applies, at
}

// companionRetryProbe is identityProbe whose companion fails its destroy plans and applies as
// given, with a Bind that counts its calls and short backoff.
func (w *runWorld) companionRetryProbe(t *testing.T, root string, planErrs, destroyErrs []string, binds *int) Probe {
	t.Helper()
	p := w.identityProbe(t, root, tofuStack{DestroyPlanErrors: planErrs, DestroyErrors: destroyErrs})
	p.Bind = func(context.Context, Credential) error { *binds++; return nil }
	p.Run.companionBackoff = []time.Duration{30 * time.Millisecond, 60 * time.Millisecond}
	return p
}

// checkKeptRoot pins a run (or cleanup) whose companion could not be destroyed: it failed, the root
// was neither planned for destroy nor destroyed, the probe's state and passphrase stay, the summary
// names the kept root and the cleanup command, and no file keeps a probe secret.
func checkKeptRoot(t *testing.T, w *runWorld, root string, err error, calls []tofuCall) {
	t.Helper()
	if ExitCode(err) == 0 {
		t.Error("a run whose companion could not be destroyed exited 0")
	}
	if plans, applies, _ := destroyCalls(calls, "p"); plans+applies > 0 {
		t.Errorf("the root was planned for destroy %d times and destroyed %d times: it holds the identity the companion's resources need", plans, applies)
	}
	_, state, pass := probeFiles(t, root)
	if !exists(state) || !exists(pass) {
		t.Errorf("root kept: state kept %v, passphrase kept %v; want both, for --cleanup", exists(state), exists(pass))
	}
	sum := w.summary(t)
	msg, _ := sum["error"].(string)
	if sum["outcome"] != "fail" || !strings.Contains(msg, "--cleanup 20261006T120000Z-a1b2") || !strings.Contains(msg, "kept") {
		t.Errorf("summary %v: want outcome fail and an error naming the kept root and --cleanup 20261006T120000Z-a1b2", sum)
	}
	for _, dir := range []string{root, w.runDir, w.stacks} {
		if left := holders([]string{dir}, identitySecrets); len(left) > 0 {
			t.Errorf("%v hold a probe secret", left)
		}
	}
}

// cleanupAfter clears the companion's scripted failures and runs --cleanup: it must bind the
// identity again, destroy the companion and then the root, and remove the probe's files.
func cleanupAfter(t *testing.T, w *runWorld, p Probe, root string, binds *int) {
	t.Helper()
	st := w.scenario.Stacks["companion"]
	st.DestroyPlanErrors, st.DestroyErrors, st.DestroyExit = nil, nil, 0
	w.scenario.Stacks["companion"] = st
	w.save(t)
	before := len(w.calls(t, "tofu.log"))
	*binds = 0
	if err := p.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v\n%s", err, w.term)
	}
	calls := w.calls(t, "tofu.log")[before:]
	if *binds == 0 {
		t.Error("cleanup did not bind the identity again")
	}
	if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"companion", "p"}) {
		t.Errorf("cleanup destroys %v, want [companion p]", got)
	}
	_, state, pass := probeFiles(t, root)
	if exists(state) || exists(pass) {
		t.Errorf("after a passing cleanup: state kept %v, passphrase kept %v", exists(state), exists(pass))
	}
}

func TestProbeCompanionDestroyRetry(t *testing.T) {
	// Retried, then destroyed: the run passes, the companion is destroyed before the root.
	for _, c := range []struct {
		name                   string
		planErrs, destroyErrs  []string
		wantPlans, wantApplies int
	}{
		{"plan-oauth2-500", []string{errOAuth500}, nil, 2, 1},
		{"apply-5xx", nil, []string{errAPI503}, 2, 2},
		{"connection-reset", []string{errReset, errReset}, nil, 3, 1},
		{"oauth2-token-transport", []string{errOAuthDial}, nil, 2, 1},
		{"oauth2-token-bare-status", []string{errOAuthBare}, nil, 2, 1},
		{"5xx-with-number-in-body", []string{errOAuth500Body}, nil, 2, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds int
			p := w.companionRetryProbe(t, root, c.planErrs, c.destroyErrs, &binds)
			if err := p.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v (a transient failure of the companion's destroy is retried)\n%s", err, w.term)
			}
			calls := w.calls(t, "tofu.log")
			plans, applies, _ := destroyCalls(calls, "companion")
			if plans != c.wantPlans || applies != c.wantApplies {
				t.Errorf("companion: %d destroy plans and %d destroy applies, want %d and %d", plans, applies, c.wantPlans, c.wantApplies)
			}
			last := -1
			for i, call := range calls {
				if call.Stack == "companion" && (call.Cmd == "destroy" || call.Cmd == "plan" && slices.Contains(call.Args, "-destroy")) {
					last = i
				}
			}
			for i, call := range calls {
				if call.Stack == "p" && call.Cmd == "plan" && slices.Contains(call.Args, "-destroy") && i < last {
					t.Error("the root was planned for destroy before the companion's last destroy attempt")
				}
			}
			if _, rootApplies, _ := destroyCalls(calls, "p"); rootApplies != 1 {
				t.Errorf("the root was destroyed %d times, want once", rootApplies)
			}
		})
	}

	// Never retried, or retried until the bound: the root is kept, then --cleanup completes.
	for _, c := range []struct {
		name                  string
		planErrs, destroyErrs []string
		wantPlans             int
	}{
		{"4xx-api-403", []string{errAPI403, errAPI403, errAPI403}, nil, 1},
		{"4xx-oauth2-401", []string{errOAuth401, errOAuth401, errOAuth401}, nil, 1},
		{"4xx-oauth2-bare-status", []string{errOAuth401Bare, errOAuth401Bare, errOAuth401Bare}, nil, 1},
		{"4xx-oauth2-error-code", []string{errOAuthCode, errOAuthCode, errOAuthCode}, nil, 1},
		{"4xx-uncommon-with-5xx", []string{errAPI503 + "\n" + errLocked, errAPI503, errAPI503}, nil, 1},
		{"4xx-with-5xx", []string{errAPI503 + "\n" + errAPI403, errAPI503, errAPI503}, nil, 1},
		{"4xx-apply", nil, []string{errAPI403, errAPI403, errAPI403}, 1},
		{"persistent-5xx", []string{errOAuth500, errAPI503, errOAuth500, errOAuth500}, nil, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds int
			p := w.companionRetryProbe(t, root, c.planErrs, c.destroyErrs, &binds)
			err := p.Start(context.Background())
			calls := w.calls(t, "tofu.log")
			plans, _, at := destroyCalls(calls, "companion")
			if plans != c.wantPlans {
				t.Errorf("companion: %d destroy plans, want %d", plans, c.wantPlans)
			}
			// Backoff: each retry waits at least the configured time after the attempt before.
			for i := 1; i < len(at) && i <= len(p.Run.companionBackoff); i++ {
				if gap := at[i].Sub(at[i-1]); gap < p.Run.companionBackoff[i-1] {
					t.Errorf("retry %d came %v after the attempt before, want at least %v", i, gap, p.Run.companionBackoff[i-1])
				}
			}
			checkKeptRoot(t, w, root, err, calls)
			cleanupAfter(t, w, p, root, &binds)
		})
	}

	// A wait that would end after the destroy deadline is not started.
	t.Run("backoff-beyond-deadline", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		var binds int
		p := w.companionRetryProbe(t, root, []string{errOAuth500, errOAuth500}, nil, &binds)
		p.Run.Deadline = 20 * time.Second
		// Longer than the destroy-on-exit's budget, whichever it is: the run's 20 s deadline today,
		// at least DefaultDeadline under FR-002 (spec 006, T018).
		p.Run.companionBackoff = []time.Duration{DefaultDeadline + time.Minute}
		start := time.Now()
		err := p.Start(context.Background())
		if took := time.Since(start); took > 15*time.Second {
			t.Errorf("the run took %v: it waited for a retry the deadline did not leave time for", took)
		}
		calls := w.calls(t, "tofu.log")
		if plans, _, _ := destroyCalls(calls, "companion"); plans != 1 {
			t.Errorf("companion: %d destroy plans, want 1", plans)
		}
		checkKeptRoot(t, w, root, err, calls)
	})

	// --cleanup keeps the root again while the companion cannot be destroyed; a later one completes.
	t.Run("cleanup-fails-then-passes", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		var binds int
		p := w.companionRetryProbe(t, root, nil, []string{errAPI403, errAPI403}, &binds)
		err := p.Start(context.Background())
		checkKeptRoot(t, w, root, err, w.calls(t, "tofu.log"))
		before := len(w.calls(t, "tofu.log"))
		err = p.Cleanup(context.Background())
		calls := w.calls(t, "tofu.log")[before:]
		if _, applies, _ := destroyCalls(calls, "companion"); applies != 1 {
			t.Errorf("cleanup: the companion's destroy was applied %d times, want once", applies)
		}
		checkKeptRoot(t, w, root, err, calls)
		cleanupAfter(t, w, p, root, &binds)
	})

	// A cleanup retries a transient failure of the companion's destroy as the run does.
	t.Run("cleanup-retries", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		var binds int
		p := w.companionRetryProbe(t, root, nil, []string{errAPI403, errAPI503}, &binds)
		err := p.Start(context.Background())
		checkKeptRoot(t, w, root, err, w.calls(t, "tofu.log"))
		before := len(w.calls(t, "tofu.log"))
		if err := p.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup: %v (its companion destroy failed once with a 503)\n%s", err, w.term)
		}
		calls := w.calls(t, "tofu.log")[before:]
		if _, applies, _ := destroyCalls(calls, "companion"); applies != 2 {
			t.Errorf("cleanup: the companion's destroy was applied %d times, want twice", applies)
		}
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"companion", "companion", "p"}) {
			t.Errorf("cleanup destroys %v, want [companion companion p]", got)
		}
	})

	// A cleanup whose binding of the identity fails (an API error, not a refusal) keeps the root
	// too: the companion was never reached.
	t.Run("cleanup-bind-fails", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		var binds int
		p := w.companionRetryProbe(t, root, []string{errAPI403}, nil, &binds)
		err := p.Start(context.Background())
		checkKeptRoot(t, w, root, err, w.calls(t, "tofu.log"))
		failing := p
		failing.Bind = func(context.Context, Credential) error {
			return errors.New("GET /auth/details: 503 Service Unavailable")
		}
		before := len(w.calls(t, "tofu.log"))
		err = failing.Cleanup(context.Background())
		checkKeptRoot(t, w, root, err, w.calls(t, "tofu.log")[before:])
		cleanupAfter(t, w, p, root, &binds)
	})

	// Review r1: a binding refusal at cleanup (account.env edited since the run) is no reason to
	// destroy the root of a companion that ran: the root is kept.
	t.Run("cleanup-bind-refused", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		var binds int
		p := w.companionRetryProbe(t, root, []string{errAPI403}, nil, &binds)
		err := p.Start(context.Background())
		checkKeptRoot(t, w, root, err, w.calls(t, "tofu.log"))
		failing := p
		failing.Bind = func(context.Context, Credential) error {
			return refuse(CondAccount, "credential account %q, bound %q", "other", "this")
		}
		before := len(w.calls(t, "tofu.log"))
		err = failing.Cleanup(context.Background())
		checkKeptRoot(t, w, root, err, w.calls(t, "tofu.log")[before:])
		cleanupAfter(t, w, p, root, &binds)
	})

	// Review r2: an output of the wrong shape depends on the published values only — the run refused
	// it before the companion ran — so a cleanup refuses it again and still destroys the root.
	t.Run("cleanup-wrong-shape-destroys-root", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{})
		bad := filepath.Join(w.bin, "outputs-bad.json")
		if err := os.WriteFile(bad, []byte(`{"companion_env":{"sensitive":true,"type":"number","value":1}}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		st := w.scenario.Stacks["p"]
		st.Outputs, st.DestroyExit = bad, 1
		w.scenario.Stacks["p"] = st
		w.save(t)
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a run whose root destroy failed exited 0")
		}
		st.DestroyExit = 0
		w.scenario.Stacks["p"] = st
		w.save(t)
		before := len(w.calls(t, "tofu.log"))
		_ = p.Cleanup(context.Background())
		if got := sequence(w.calls(t, "tofu.log")[before:], "destroy"); !slices.Equal(got, []string{"p"}) {
			t.Errorf("cleanup destroys %v, want [p]: the companion never ran", got)
		}
	})

	// The kept root's resources are leftovers of the run: the leftover check is not told to spare
	// them. The listed probe client (seed oauth2-client) is reported.
	t.Run("kept-root-is-leftover", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		var binds int
		p := w.companionRetryProbe(t, root, []string{errAPI403}, nil, &binds)
		p.Run.Leftovers, _ = cleanCheck(t, "oauth2-client")
		err := p.Start(context.Background())
		checkKeptRoot(t, w, root, err, w.calls(t, "tofu.log"))
		var rep Report
		readJSON(t, filepath.Join(w.runDir, "leftovers.json"), &rep)
		if rep.Outcome != "fail" || !slices.ContainsFunc(rep.Leftovers, func(l Leftover) bool { return l.Type == "ovh_me_api_oauth2_client" }) {
			t.Errorf("leftovers.json %+v: want the kept probe client reported as a leftover", rep)
		}
	})
}

// TestDiagnosticLinesBounded (T095 review r1): a stream line longer than errLimit is dropped, not
// buffered, and the next diagnostic line is still kept, decoded, across split writes.
func TestDiagnosticLinesBounded(t *testing.T) {
	t.Parallel()
	d := &diagnosticLines{}
	long := bytes.Repeat([]byte("x"), errLimit/2)
	for i := 0; i < 3; i++ {
		if n, _ := d.Write(long); n != len(long) {
			t.Fatalf("Write returned %d, want %d", n, len(long))
		}
	}
	if len(d.part) > errLimit {
		t.Errorf("a %d-byte line without newline is buffered (%d bytes): want it dropped past %d", 3*len(long), len(d.part), errLimit)
	}
	// A line of exactly errLimit bytes is kept (review r2).
	exact := `{"type":"diagnostic","diagnostic":{"summary":"exact","detail":"`
	exact += strings.Repeat("y", errLimit-len(exact)-3) + `"}}`
	_, _ = d.Write([]byte("\n" + exact + "\n"))
	if !strings.Contains(d.kept.String(), "exact") {
		t.Errorf("a diagnostic line of exactly %d bytes was dropped", errLimit)
	}
	d.kept.Reset()
	line := `{"@level":"error","type":"diagnostic","diagnostic":{"severity":"error","summary":"refused","detail":"status code 400: body"}}` + "\n"
	_, _ = d.Write([]byte("\n" + line[:20]))
	_, _ = d.Write([]byte(line[20:]))
	if got := d.kept.String(); !strings.Contains(got, "diagnostic error: refused") || !strings.Contains(got, "status code 400: body") || strings.Contains(got, "xxx") {
		t.Errorf("kept %q: want the decoded diagnostic only", got)
	}
}

// TestRunnerErrorRecord (T095, T094 gap 1): a refused API call's answer reaches the run record. A
// failed tofu call's stderr, and the error diagnostics of a failed `apply -json` (whose detail
// holds the API's answer and reaches the terminal only as its one-line summary), are appended to
// errors.txt in the run record, redacted.
func TestRunnerErrorRecord(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "tofu", "apply-fail.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	const body = `OVHcloud API error (status code 400): Client::BadRequest: \"s3:GetObjectVersion is not valid\"`
	if !strings.Contains(string(raw), "Error running command 'exit 1'") {
		t.Fatal("fixture apply-fail.jsonl changed: no diagnostic detail to replace")
	}
	// Review r1: a secret JSON escapes differently ("<" as <, `"` as \") must not survive
	// redaction in its escaped form.
	const odd = `odd<"SECRET\&>7c41`
	esc, _ := json.Marshal(odd)
	oddEscaped := strings.Trim(string(esc), `"`)
	stream := filepath.Join(w.bin, "apply-fail-body.jsonl")
	if err := os.WriteFile(stream, []byte(strings.ReplaceAll(string(raw), "Error running command 'exit 1'", body+" token="+seedSecret+" odd="+oddEscaped)), 0o600); err != nil {
		t.Fatal(err)
	}
	a := w.stack(t, "a", true, tofuStack{DestroyErrors: []string{"Error: fake destroy refusal marker"}})
	b := w.stack(t, "b", true, tofuStack{ApplyStream: stream, ApplyExit: 1})
	r := w.runner(t, a, b)
	r.Creds = maps.Clone(r.Creds)
	r.Creds["TF_VAR_odd_secret"] = odd
	if err, _ := execute(t, r); err == nil {
		t.Fatal("a run whose apply failed passed")
	}
	rec, err := os.ReadFile(filepath.Join(w.runDir, "errors.txt"))
	if err != nil {
		t.Fatalf("no errors.txt in the run record: %v", err)
	}
	for _, want := range []string{"s3:GetObjectVersion is not valid", "fake-tofu-stderr-marker", "fake destroy refusal marker"} {
		if !strings.Contains(string(rec), want) {
			t.Errorf("errors.txt lacks %q:\n%s", want, rec)
		}
	}
	for _, s := range []string{seedSecret, odd, oddEscaped, "SECRET"} {
		if strings.Contains(string(rec), s) {
			t.Errorf("errors.txt holds %q: a secret not redacted", s)
		}
	}
	if fi, err := os.Stat(filepath.Join(w.runDir, "errors.txt")); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("errors.txt mode %o, want 600", fi.Mode().Perm())
	}
}

// ---------------------------------------------------------------- T018 (spec 006 FR-002)

// TestRunnerDestroyBudget: the probe core, as the chain (TestChainDestroyBudget): runs with a
// deadline shorter than DefaultDeadline whose apply of b fails destroy b then a. from-destroy-start:
// b's apply takes budgetForward; the destroy-on-exit's context is bounded by DefaultDeadline from
// the destroy's start. outlives-run-deadline: run deadline budgetShortRun, a's destroy takes
// budgetSlowDestroy; it is not stopped and completes under the same budget. above-default: run
// deadline budgetLongRun; the destroy's budget is that, not DefaultDeadline.
func TestRunnerDestroyBudget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		deadline     time.Duration // 0: the run world's (testWaitCap)
		forward      time.Duration // b's failing apply takes this long
		slowDestroy  time.Duration // a's destroy takes this long
		spentInDelay time.Duration
	}{
		{name: "from-destroy-start", forward: budgetForward},
		{name: "outlives-run-deadline", deadline: budgetShortRun, slowDestroy: budgetSlowDestroy, spentInDelay: budgetSlowDestroy},
		{name: "above-default", deadline: budgetLongRun}, // a pin today: the budget stays the longer run deadline
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newRunWorld(t)
			s := []Stack{w.stack(t, "a", true, tofuStack{DestroyDelay: tc.slowDestroy}), w.stack(t, "b", true, tofuStack{ApplyExit: 1, ApplyDelay: tc.forward})}
			r := w.runner(t, s...)
			if tc.deadline > 0 {
				r.Deadline = tc.deadline
			}
			b := &budgetLister{inner: r.Leftovers.Lister}
			r.Leftovers.Lister = b
			err, _ := execute(t, r)
			if ExitCode(err) == 0 {
				t.Error("a failed apply exited 0")
			}
			calls := w.calls(t, "tofu.log")
			if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"b", "a"}) {
				t.Errorf("destroys %v, want [b a] (reverse order)", got)
			}
			if got := signals(calls, "a"); len(got) != 0 {
				t.Errorf("the destroy of a got %v: the destroy-on-exit was bounded by the run's --deadline %v", got, r.Deadline)
			}
			if tc.slowDestroy > 0 && !slices.Contains(sequence(calls, "delay-done"), "a") {
				t.Errorf("the destroy of a (%v) did not complete", tc.slowDestroy)
			}
			if s := w.summary(t); s["outcome"] != "fail" {
				t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
			}
			destroyBudgetJudge(t, b, r.Deadline, tc.spentInDelay)
		})
	}
}

// TestProbeDestroyBudgetExceeded: a probe whose destroy-on-exit outlives its budget (the destroy
// hangs; Runner.destroyAt ends the budget there, as 45 minutes would) fails: the hung destroy is
// interrupted, the run reports fail, and the inventory, the state and the passphrase stay for
// `--cleanup`. It holds today (the budget is the run's deadline) and pins that T023's longer
// budget keeps it.
func TestProbeDestroyBudgetExceeded(t *testing.T) {
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.probe(t, root, tofuStack{DestroyHang: true})
	p.Run.destroyAt = w.hanging(t, "p")
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- p.Start(context.Background()) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(45 * time.Second):
		t.Fatal("Start did not return within 45 s")
	}
	if took := time.Since(start); took > testWaitCap {
		t.Errorf("Start took %v: the exhausted budget did not stop the hung destroy", took)
	}
	if ExitCode(err) == 0 {
		t.Errorf("a destroy past its budget exited 0 (%v)", err)
	}
	calls := w.calls(t, "tofu.log")
	if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"p"}) {
		t.Errorf("destroys %v, want [p]", got)
	}
	// The budget's end interrupts the hung destroy (it may write its state), once; the fake's own
	// give-up logs hang-timeout instead.
	if got := signals(calls, "p"); !slices.Equal(got, []string{"interrupt"}) {
		t.Errorf("the hung destroy of p received %v when its budget ended, want [interrupt]", got)
	}
	if s := w.summary(t); s["outcome"] != "fail" {
		t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
	}
	if !strings.Contains(w.term.String(), "LZ-LIVE summary 20261006T120000Z-a1b2 fail") {
		t.Errorf("terminal lacks the fail summary:\n%s", w.term.String())
	}
	var ids []string
	for _, e := range readInventory(t, w.runDir) {
		if e.Stack == "p" {
			ids = append(ids, e.ID)
		}
	}
	if !slices.Equal(ids, []string{"pn-p-1", "pn-p-2"}) {
		t.Errorf("inventory of p %v, want [pn-p-1 pn-p-2] kept for --cleanup", ids)
	}
	_, state, pass := probeFiles(t, root)
	if !exists(state) || !exists(pass) {
		t.Errorf("after a destroy past its budget: state kept %v, passphrase kept %v; want both for --cleanup", exists(state), exists(pass))
	}
}

// ---------------------------------------------------------------- T023 (spec 006 FR-002)

// TestRunnerDestroyContext (FR-002, T018's deferred check): the probe core, as TestChainDestroyContext:
// stacks a and b, b's apply fails; every destroy call runs under the destroy-on-exit's one context,
// the one the leftover check lists under, and that context has the budget.
func TestRunnerDestroyContext(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{ApplyExit: 1}))
	b, children := &budgetLister{inner: r.Leftovers.Lister}, &childDeadlines{}
	r.Leftovers.Lister, r.childStarted = b, children.started
	err, _ := execute(t, r)
	if ExitCode(err) == 0 {
		t.Error("a failed apply exited 0")
	}
	if got := sequence(w.calls(t, "tofu.log"), "destroy"); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("destroys %v, want [b a] (reverse order)", got)
	}
	oneDestroyContext(t, children, b, r.Deadline, 0)
}

// TestProbeDestroyBudgetEndsInCheck (FR-002, constitution V): the probe core, as
// TestChainDestroyBudgetEndsInCheck: both stacks apply and are destroyed, and the destroy budget
// runs out during the leftover check while the listings still answer. The run fails on the
// context's state, not only on an error a call returned.
func TestProbeDestroyBudgetEndsInCheck(t *testing.T) {
	t.Parallel()
	w := newRunWorld(t)
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{}))
	children, end := &childDeadlines{}, make(chan struct{})
	r.Leftovers.Lister = endingLister{inner: r.Leftovers.Lister, children: children, end: end, once: &sync.Once{}}
	r.childStarted, r.destroyAt = children.started, end
	err, _ := execute(t, r)
	if got := sequence(w.calls(t, "tofu.log"), "destroy"); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("destroys %v, want [b a] (the destroys complete before the budget ends)", got)
	}
	if ExitCode(err) == 0 {
		t.Error("a destroy-on-exit whose budget ran out during the leftover check exited 0")
	}
	if s := w.summary(t); s["outcome"] != "fail" {
		t.Errorf("summary.json outcome = %v, want fail", s["outcome"])
	}
}
