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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	ApplyStream string   `json:"apply_stream"` // file whose lines `apply -json` prints
	ApplyExit   int      `json:"apply_exit"`
	HangAfter   int      `json:"hang_after"` // >0: after that many stream lines, hang until killed
	PlanExit    int      `json:"plan_exit"`
	DestroyExit int      `json:"destroy_exit"`
	StateList   []string `json:"state_list"`
}

type tofuScenario struct {
	Stacks map[string]tofuStack `json:"stacks"`
}

// tofuCall is one logged invocation of a fake child (tofu, ovhcloud) or of the Protect hook.
type tofuCall struct {
	Cmd      string   `json:"cmd"` // init plan show-json show apply destroy state-list protect ovhcloud other
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
	}
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
	call := tofuCall{Stack: filepath.Base(dir), Args: os.Args[1:]}
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
	switch {
	case sub == "init":
		call.Cmd = "init"
		logCall(bin, "tofu.log", call)
		return 0
	case sub == "plan":
		call.Cmd = "plan"
		out := ""
		for i, a := range rest {
			if v, ok := strings.CutPrefix(a, "-out="); ok {
				out = v
			} else if a == "-out" && i+1 < len(rest) {
				out = rest[i+1]
			}
		}
		call.Plan = resolve(out)
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
			raw, _ := json.Marshal(map[string]any{"format_version": "1.2", "terraform_version": "1.13.0", "stack": pf.Stack, "nonce": pf.Nonce, "resource_changes": []any{}})
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
			if statePath != "" {
				var s fakeState
				raw, err := os.ReadFile(statePath)
				ok := err == nil && json.Unmarshal(raw, &s) == nil && s.PassSHA == passSHA(os.Getenv("TF_VAR_state_passphrase"))
				call.PassOK = &ok
				if !ok {
					logCall(bin, "tofu.log", call)
					fmt.Fprintln(os.Stderr, "Error: cannot decrypt state")
					return 1
				}
			}
			logCall(bin, "tofu.log", call)
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
		if st.ApplyStream != "" {
			raw, _ := os.ReadFile(st.ApplyStream)
			for i, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
				fmt.Println(l)
				if st.HangAfter > 0 && i+1 == st.HangAfter {
					time.Sleep(60 * time.Second)
					return 9
				}
			}
		}
		if st.ApplyExit != 0 {
			fmt.Fprintf(os.Stderr, "Error: fake apply failure token=%s\n", secret)
		}
		return st.ApplyExit
	case sub == "state" && len(rest) > 0 && rest[0] == "list":
		call.Cmd = "state-list"
		logCall(bin, "tofu.log", call)
		for _, l := range st.StateList {
			fmt.Println(l)
		}
		return 0
	}
	call.Cmd = "other"
	logCall(bin, "tofu.log", call)
	return 0
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
		ID:        "20261006T120000Z-a1b2",
		Dir:       w.runDir,
		Tofu:      w.tofu,
		Authority: AuthorityTenant,
		Creds:     runCreds,
		Stacks:    stacks,
		Deadline:  time.Minute,
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

// ---------------------------------------------------------------- controls

func TestRunnerDefaultDeadline(t *testing.T) {
	if DefaultDeadline != 45*time.Minute {
		t.Errorf("DefaultDeadline = %v, want 45m (FR-011)", DefaultDeadline)
	}
}

// TestRunnerAppliesInOrderDestroysEphemeralInReverse: a run that succeeds still destroys its
// ephemeral stacks on exit, last applied first; a retained stack is never destroyed.
func TestRunnerAppliesInOrderDestroysEphemeralInReverse(t *testing.T) {
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

// TestRunnerDeadline: G8 — the deadline stops a hanging apply, and the destroy still runs (with
// its own time, not the expired one), in reverse order; what was created before the hang is in
// the inventory.
func TestRunnerDeadline(t *testing.T) {
	w := newRunWorld(t)
	s := []Stack{w.stack(t, "a", true, tofuStack{}), w.stack(t, "b", true, tofuStack{HangAfter: 3})}
	r := w.runner(t, s...)
	r.Deadline = 3 * time.Second
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
}

// TestRunnerSignals: G8 — SIGINT and SIGTERM to a fresh lz-live process (no injected channel)
// stop the hanging apply and fire the destroy in reverse order; the process exits non-zero.
func TestRunnerSignals(t *testing.T) {
	for name, sig := range map[string]syscall.Signal{"SIGINT": syscall.SIGINT, "SIGTERM": syscall.SIGTERM} {
		t.Run(name, func(t *testing.T) {
			w := newRunWorld(t)
			w.stack(t, "a", true, tofuStack{})
			w.stack(t, "b", true, tofuStack{HangAfter: 3})
			cmd := w.subprocess(t, fakeRunConfig{Mode: "execute", Stacks: []fakeRunStack{{"a", true}, {"b", true}}, DeadlineMS: 60000})
			out := &syncBuffer{}
			cmd.Stdout, cmd.Stderr = out, out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if !waitFor(20*time.Second, func() bool { return slices.Contains(sequence(w.calls(t, "tofu.log"), "apply"), "b") }) {
				_ = cmd.Process.Kill()
				t.Fatalf("apply of b never started:\n%s", out)
			}
			time.Sleep(200 * time.Millisecond)
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
		})
	}
}

// TestRunnerProtectBeforeApply: every plan goes through the retained-resource guard (given
// `tofu show -json` of the saved plan) before exactly that plan file is applied.
func TestRunnerProtectBeforeApply(t *testing.T) {
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
}

// TestRunnerStateListFallback: with P24 refuted, `tofu state list` after each stack fills the
// inventory.
func TestRunnerStateListFallback(t *testing.T) {
	w := newRunWorld(t)
	noEvents := w.stream(t, "a-none")
	a := w.stack(t, "a", true, tofuStack{ApplyStream: noEvents, StateList: []string{"ovh_cloud_project_network_private.a", "module.m.ovh_cloud_project_network_private_subnet.s"}})
	r := w.runner(t, a)
	r.StateListFallback = true
	if err, _ := execute(t, r); err != nil {
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
}

// TestRunnerRedactsEveryOutput: G2 — the seeded secret a child prints on stdout and stderr, in a
// created id, in a rendered plan and in a listing reaches neither the terminal nor any file of the
// run record (inventory.jsonl, listings/, summary.json, plan-<id>.txt), nor any child's argv;
// the non-secret part of each output does arrive.
func TestRunnerRedactsEveryOutput(t *testing.T) {
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
			if !strings.Contains(term, "fake-tofu-stderr-marker") {
				t.Errorf("the child's stderr never reached the terminal:\n%s", term)
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

// TestRunnerChildHome: coordinator decision — every child (tofu and ovhcloud) runs with a
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
	r.Leftovers.Lister = nil
	r.Leftovers.Ovhcloud = w.ovhcloud
	_, _ = execute(t, r)

	calls := append(w.calls(t, "tofu.log"), w.calls(t, "ovhcloud.log")...)
	homes := map[string]bool{}
	sawOvhcloud := false
	for _, c := range calls {
		if c.Cmd == "protect" {
			continue
		}
		sawOvhcloud = sawOvhcloud || c.Cmd == "ovhcloud"
		homes[c.Home] = true
		if c.Home == "" || c.Home == callerHome || strings.HasPrefix(c.Home, callerHome+string(filepath.Separator)) {
			t.Errorf("%s ran with HOME %q (caller's HOME %q)", c.Cmd, c.Home, callerHome)
		}
		if c.HomeMode != 0o700 {
			t.Errorf("%s: HOME mode %#o, want 0700", c.Cmd, c.HomeMode)
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
	if !sawOvhcloud {
		t.Error("the leftover check never ran ovhcloud")
	}
	if len(homes) != 1 {
		t.Errorf("children ran with %d different HOMEs, want one per run: %v", len(homes), homes)
	}
	for h := range homes {
		if _, err := os.Stat(h); h != "" && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("scratch HOME %s still exists after the run (%v)", h, err)
		}
	}
}

// ---------------------------------------------------------------- probe state

const probeAccount = "ab12345-ovh"

func (w *runWorld) probe(t *testing.T, root string, st tofuStack) Probe {
	t.Helper()
	s := w.stack(t, "p", true, st)
	r := w.runner(t, s)
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
			if strings.Contains(strings.Join(c.Args, " "), phrase) {
				t.Error("the passphrase is in tofu's argv")
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
		lister := filepath.Join(w.bin, "lister.log")
		out, err := w.subprocess(t, fakeRunConfig{Mode: "probe-cleanup", ConfigRoot: root, Stacks: []fakeRunStack{{"p", true}}, ListerLog: lister}).CombinedOutput()
		if err != nil {
			t.Fatalf("cleanup process: %v\n%s", err, out)
		}
		if passing() == before {
			t.Error("the cleanup process never destroyed the probe with the retained passphrase")
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
	ConfigRoot string
	Seed       string
	ListerLog  string
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
	check, lister := cleanCheck(&fakeTB{}, c.Seed)
	lister.log = c.ListerLog
	r := Runner{
		ID: "20261006T120000Z-a1b2", Dir: c.RunDir, Tofu: filepath.Join(c.Bin, "tofu"), Authority: AuthorityTenant,
		Creds: runCreds, Deadline: time.Duration(c.DeadlineMS) * time.Millisecond, Leftovers: check, Terminal: os.Stdout,
		Protect: func(Stack, []byte) error { return nil },
	}
	if r.Deadline == 0 {
		r.Deadline = time.Minute
	}
	for _, s := range c.Stacks {
		r.Stacks = append(r.Stacks, Stack{ID: s.ID, Dir: filepath.Join(c.StacksDir, s.ID), Ephemeral: s.Ephemeral})
	}
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

// fakeTB lets the fixture helpers run outside a test: a fatal error panics (fakeRun recovers).
type fakeTB struct{ testing.TB }

func (fakeTB) Helper()                        {}
func (fakeTB) Fatal(a ...any)                 { panic(fmt.Sprint(a...)) }
func (fakeTB) Fatalf(format string, a ...any) { panic(fmt.Sprintf(format, a...)) }
