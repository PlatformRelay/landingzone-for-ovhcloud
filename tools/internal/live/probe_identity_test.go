package live

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Second identity of a probe (T075; found in T008, evidence/T008.md decision request 1, option A):
// some observations need a credential a probe root creates, used by a second client in the same
// run — the storage-iam probe tenant identity (P9 allowlist usage, P25 tag-conditioned access, P26
// tenant binding) and a second writer of one state (P1–P3). OpenTofu cannot configure a provider
// from a resource of the same run, so a probe root may carry a companion root in its `companion/`
// directory (tests/live/probes/storage-iam/companion/). The root publishes the companion's
// credentials as the sensitive map output `companion_env` (variable name → value); lz-live reads
// it under the admin credential after the root's apply, files.go writes it to a 0600 file in the
// run's probe directory (accounts/<account>/state/probes/<run-id>/, 0700, never the checkout's run
// record), and the companion runs with those variables in place of the admin credential. While the
// companion's apply holds its state lock, a second writer of the companion's state is started and
// refused by the lock; its refusal goes to the run record. On every way out — success, failure,
// interrupt — the companion is destroyed under the probe identity first, the credential file is
// removed, and the root's destroy then removes the identity. The tests are black-box on
// Probe.Start: the directory name `companion` and the output name `companion_env` are the contract
// between the probe roots and lz-live.

const (
	identityClientID  = "fake-probe-tenant-client-id"
	identityS3Access  = "fake-probe-p1-access-key"
	identityP25ID     = "fake-probe-p25-client-id"
	identityClient    = "probe-SECRET-tenant-4be1c07a93d2"
	identityS3Secret  = "probe-SECRET-s3-9d02aa7e51c6"
	identityP25Secret = "probe-SECRET-p25-61f3e0b8c27d"
)

// identityEnv is what the companion must run with: the published variables.
var identityEnv = map[string]string{
	"OVH_CLIENT_ID":            identityClientID,
	"OVH_CLIENT_SECRET":        identityClient,
	"AWS_ACCESS_KEY_ID":        identityS3Access,
	"AWS_SECRET_ACCESS_KEY":    identityS3Secret,
	"TF_VAR_p25_client_id":     identityP25ID,
	"TF_VAR_p25_client_secret": identityP25Secret,
}

var identitySecrets = []string{identityClient, identityS3Secret, identityP25Secret}

// identityProbe is a probe of root p whose stage publishes the probe identity (the captured
// `tofu output -json` of testdata/tofu/output-companion.json with the identity's values) and
// whose companion root p/companion behaves as given. Every companion call lists the files holding
// any published secret under the config root, the run records, the stack roots and its HOME.
func (w *runWorld) identityProbe(t *testing.T, root string, companion tofuStack) Probe {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tofu", "output-companion.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.NewReplacer("@PROBE_CLIENT_ID@", identityClientID, "@PROBE_CLIENT_SECRET@", identityClient,
		"@P1_ACCESS_KEY@", identityS3Access, "@P1_SECRET_KEY@", identityS3Secret,
		"@P25_CLIENT_ID@", identityP25ID, "@P25_CLIENT_SECRET@", identityP25Secret).Replace(string(raw))
	outputs := filepath.Join(w.bin, "outputs-p.json")
	if err := os.WriteFile(outputs, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	p := w.probe(t, root, tofuStack{Outputs: outputs})
	if err := os.MkdirAll(filepath.Join(w.stacks, "p", "companion"), 0o755); err != nil {
		t.Fatal(err)
	}
	if companion.ApplyStream == "" {
		companion.ApplyStream = w.stream(t, "companion",
			resourceLine{"ovh_cloud_project_network_private", "p9", "pn-companion-1"},
			resourceLine{"ovh_cloud_project_network_private_subnet", "p9", "sn-companion-1"})
	}
	companion.ScanRoots = []string{root, w.runDir, w.stacks}
	companion.ScanFor = identitySecrets
	w.scenario.Stacks["companion"] = companion
	w.save(t)
	return p
}

// holders lists the regular files under roots whose content holds any of needles (fake tofu,
// ScanRoots).
func holders(roots []string, needles []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range roots {
		if r == "" {
			continue
		}
		_ = filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() || seen[p] {
				return nil
			}
			seen[p] = true
			raw, err := os.ReadFile(p)
			if err == nil && slices.ContainsFunc(needles, func(n string) bool { return bytes.Contains(raw, []byte(n)) }) {
				fi, _ := os.Lstat(p)
				di, _ := os.Stat(filepath.Dir(p))
				var dm fs.FileMode
				if di != nil {
					dm = di.Mode().Perm()
				}
				out = append(out, fmt.Sprintf("%s|%o|%o", p, fi.Mode().Perm(), dm))
			}
			return nil
		})
	}
	return out
}

func envOf(c tofuCall) map[string]string {
	env := map[string]string{}
	for _, kv := range c.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return env
}

// childCall: a call the fake made as a child (not the Protect hook or a fake's own event).
func childCall(c tofuCall) bool {
	switch c.Cmd {
	case "protect", "signal", "hang-timeout", "lock-timeout":
		return false
	}
	return true
}

// checkIdentityRun pins the clauses every way out shares.
func checkIdentityRun(t *testing.T, w *runWorld, root string) []tofuCall {
	t.Helper()
	calls := w.calls(t, "tofu.log")
	probeDir, _, _ := probeFiles(t, root)

	// A probe stage creates the probe identity under the admin credential: the root's apply ran
	// with the admin credential, before any companion call.
	stageApply, firstCompanion := -1, -1
	for i, c := range calls {
		if !childCall(c) {
			continue
		}
		env := envOf(c)
		switch c.Stack {
		case "p":
			if env["OVH_CLIENT_ID"] != runCreds["OVH_CLIENT_ID"] || env["OVH_CLIENT_SECRET"] != runCreds["OVH_CLIENT_SECRET"] {
				t.Errorf("probe stage: tofu %s without the admin credential (OVH_CLIENT_ID %q)", c.Cmd, env["OVH_CLIENT_ID"])
			}
			if c.Cmd == "apply" && stageApply < 0 {
				stageApply = i
			}
		case "companion":
			if firstCompanion < 0 {
				firstCompanion = i
			}
		}
	}
	if stageApply < 0 {
		t.Fatal("the probe stage never applied")
	}
	if firstCompanion < 0 {
		t.Fatalf("the companion root never ran: no tofu call in p/companion (calls %v)", sequence(calls, "init"))
	}
	if firstCompanion < stageApply {
		t.Error("the companion ran before the probe stage's apply created the identity")
	}

	// The companion runs under the probe identity alone: every companion child (init, plan, apply,
	// the second writer, destroy) has the published variables, and no value or argument of it holds
	// the admin client id or secret.
	cmds := map[string]int{}
	for _, c := range calls {
		if c.Stack != "companion" || !childCall(c) {
			continue
		}
		cmds[c.Cmd]++
		env := envOf(c)
		for k, want := range identityEnv {
			if env[k] != want {
				t.Errorf("companion tofu %s: %s = %q, want the probe identity's", c.Cmd, k, env[k])
			}
		}
		for _, admin := range []string{runCreds["OVH_CLIENT_ID"], runCreds["OVH_CLIENT_SECRET"]} {
			for _, kv := range c.Env {
				if strings.Contains(kv, admin) {
					k, _, _ := strings.Cut(kv, "=")
					t.Errorf("companion tofu %s: the admin credential reached the child in %s", c.Cmd, k)
				}
			}
			if strings.Contains(strings.Join(c.Args, " "), admin) {
				t.Errorf("companion tofu %s: the admin credential is in its argv", c.Cmd)
			}
		}
		// files.go wrote the credential to a 0600 file in the run's probe directory (0700), and
		// nowhere else: not the run record, the stack roots, the child's HOME or elsewhere under
		// the config root.
		for _, h := range c.Holders {
			parts := strings.Split(h, "|")
			if !within(parts[0], probeDir) {
				t.Errorf("companion tofu %s: the probe credential is in %s, outside the run's probe directory %s", c.Cmd, parts[0], probeDir)
			} else if parts[1] != "600" || parts[2] != "700" {
				t.Errorf("companion tofu %s: credential file %s mode %s in a directory %s, want 600 in 700", c.Cmd, parts[0], parts[1], parts[2])
			}
		}
	}
	for _, want := range []string{"init", "apply", "destroy"} {
		if cmds[want] == 0 {
			t.Errorf("the companion ran no tofu %s (calls %v)", want, cmds)
		}
	}
	for _, c := range calls {
		if c.Stack == "companion" && c.Cmd == "init" {
			if len(c.Holders) == 0 {
				t.Error("when the companion initialised, no file held the probe credential: files.go never wrote it")
			}
			break
		}
	}

	// On exit the companion is destroyed (under the identity: above) before the root's destroy
	// removes the identity, and no file holds a probe secret any more.
	if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"companion", "p"}) {
		t.Errorf("destroys %v, want [companion p]: the companion first, then the root that holds the identity", got)
	}
	for _, dir := range []string{root, w.runDir, w.stacks} {
		if left := holders([]string{dir}, identitySecrets); len(left) > 0 {
			t.Errorf("after the run, %v still hold a probe secret: the credential was not removed", left)
		}
	}
	// Redaction: the published secrets are secrets of the run.
	for _, s := range identitySecrets {
		if strings.Contains(w.term.String(), s) {
			t.Error("a probe secret reached the terminal")
		}
	}
	return calls
}

// TestProbeIdentity: the second-identity stage of a probe (T075 Verify line).
func TestProbeIdentity(t *testing.T) {
	t.Run("stage-companion-second-writer", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{HoldLock: true})
		if err := p.Start(context.Background()); err != nil {
			t.Errorf("Start: %v (the lock refusing the second writer is the expected observation, not a failure)\n%s", err, w.term)
		}
		calls := checkIdentityRun(t, w, root)

		// A second writer of the companion's state, started while the companion's apply holds the
		// lock, is refused by it: a plan, which takes the lock (not -lock=false, not a lock-free
		// command such as output or state list, which the lock never refuses) and changes no state;
		// its refusal is in the run record.
		locked, applies := 0, 0
		for _, c := range calls {
			if c.Stack != "companion" {
				continue
			}
			switch {
			case c.Cmd == "lock-timeout":
				t.Error("the companion's apply held the lock for 10 s and no second writer came")
			case c.Locked && c.Cmd != "plan":
				t.Errorf("the second writer ran tofu %s: want a plan, which takes the lock and changes no state", c.Cmd)
			case c.Locked:
				locked++
			case c.Cmd == "apply":
				applies++
			}
		}
		if locked == 0 {
			t.Error("no second writer of the companion's state was refused by the lock the companion's apply held (P1–P3)")
		}
		if applies != 1 {
			t.Errorf("the companion applied %d times, want once", applies)
		}
		recorded := false
		_ = filepath.WalkDir(w.runDir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if raw, _ := os.ReadFile(p); bytes.Contains(raw, []byte("fake-lock-marker")) {
					recorded = true
				}
			}
			return nil
		})
		if !recorded {
			t.Error("the second writer's lock error is not in the run record")
		}
	})

	t.Run("companion-apply-fails", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{ApplyExit: 1})
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Error("a probe whose companion apply failed exited 0")
		}
		checkIdentityRun(t, w, root)
	})

	// A companion that cannot be destroyed (e.g. the P9 allowlist lacks a delete) fails the run;
	// the root is still destroyed, so the identity does not outlive the run, and the credential goes.
	// What the companion left is the leftover check's to report.
	t.Run("companion-destroy-fails", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{DestroyExit: 1})
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Error("a probe whose companion destroy failed exited 0")
		}
		checkIdentityRun(t, w, root)
	})

	t.Run("interrupt-during-companion", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{HangAfter: 2})
		sig := make(chan os.Signal, 1)
		p.Run.Signals = sig
		done := make(chan error, 1)
		go func() { done <- p.Start(context.Background()) }()
		if !waitFor(20*time.Second, func() bool {
			for _, c := range w.calls(t, "tofu.log") {
				if c.Stack == "companion" && c.Cmd == "apply" && !c.Locked {
					return true
				}
			}
			return false
		}) {
			sig <- syscall.SIGINT
			<-done
			t.Fatalf("the companion's apply never started\n%s", w.term)
		}
		sig <- syscall.SIGINT
		select {
		case err := <-done:
			if ExitCode(err) == 0 {
				t.Error("an interrupted probe exited 0")
			}
		case <-time.After(45 * time.Second):
			t.Fatal("Start did not return within 45 s of the interrupt")
		}
		calls := checkIdentityRun(t, w, root)
		if got := signals(calls, "companion"); !slices.Equal(got, []string{"interrupt"}) {
			t.Errorf("the companion's apply received %v, want the interrupt forwarded once", got)
		}
	})
}
