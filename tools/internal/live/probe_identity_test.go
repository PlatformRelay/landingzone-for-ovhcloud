package live

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
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
		// T076 (coordinator, T075 gap "live timing"): the second writer's data directory is
		// initialised before the companion's apply starts, so live it reaches its plan while a short
		// apply still holds the lock instead of initialising a fresh directory first.
		writerInit, companionApply := -1, -1
		for i, c := range calls {
			if c.Stack == "companion" && c.Cmd == "apply" && !c.Locked && companionApply < 0 {
				companionApply = i
			}
		}
		for _, c := range calls {
			if c.Stack != "companion" || !c.Locked || companionApply < 0 {
				continue
			}
			for i, d := range calls[:companionApply+1] {
				if d.Stack == "companion" && d.Cmd == "init" && d.DataDir == c.DataDir {
					writerInit = i
					break
				}
			}
			if c.DataDir == calls[companionApply].DataDir {
				t.Error("the second writer used the companion's own data directory: want one of its own")
			}
		}
		if locked > 0 && writerInit < 0 {
			t.Error("the second writer's data directory was not initialised before the companion's apply started")
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
		// Review r2: the writer's init output (stderr included) is in its record, not on the terminal.
		if raw, _ := os.ReadFile(filepath.Join(w.runDir, "second-writer-p-companion.txt")); !bytes.Contains(raw, []byte("fake-tofu-init-stderr")) {
			t.Error("the second writer's init stderr is not in its record")
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

// TestProbeIdentities (T076, T080): every published identity — a <prefix>client_id with the
// <prefix>client_secret of the same prefix, the suffixes in any case — is one to bind; the default
// one, under exactly OVH_CLIENT_ID and OVH_CLIENT_SECRET (the provider reads only those), is
// required; half an identity, two ids of one prefix and an unpaired credential-shaped key (ending
// in key, token, password or secret, any case) are refused; the S3 keys and other names pass.
func TestProbeIdentities(t *testing.T) {
	got, err := identities(identityEnv, "ovh-eu")
	want := []Credential{{Endpoint: "ovh-eu", ClientID: identityClientID, ClientSecret: identityClient},
		{Endpoint: "ovh-eu", ClientID: identityP25ID, ClientSecret: identityP25Secret}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("identities(published) = %v, %v; want the tenant and the P25 identity", got, err)
	}
	for name, env := range map[string]map[string]string{
		"p25 id without its secret": {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_p25_client_id": "c"},
		"p25 secret without its id": {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_p25_client_secret": "d"},
		"empty p25 secret":          {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_p25_client_id": "c", "TF_VAR_p25_client_secret": ""},
		"no default identity":       {"TF_VAR_p25_client_id": "c", "TF_VAR_p25_client_secret": "d"},
		"default id only":           {"OVH_CLIENT_ID": "a"},
		// T080 (review r1): the default pair under other names leaves the companion without them.
		"default secret in another case": {"OVH_CLIENT_ID": "a", "OVH_Client_Secret": "b"},
		"default id in another case":     {"OVH_Client_Id": "a", "OVH_CLIENT_SECRET": "b"},
		"two ids of one prefix":          {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_p25_CLIENT_ID": "c", "TF_VAR_p25_client_id": "e", "TF_VAR_p25_client_secret": "d"},
		"unpaired key":                   {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_signing_Key": "d"},
		"unpaired token":                 {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_api_TOKEN": "d"},
		"unpaired password":              {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_db_password": "d"},
		"unpaired secret":                {"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_api_Secret": "d"},
	} {
		if got, err := identities(env, "ovh-eu"); err == nil {
			t.Errorf("%s: identities = %v, want a refusal", name, got)
		}
	}
	// T080: suffixes in any case pair; the S3 keys and non-credential names pass, unbound.
	mixed := map[string]string{"OVH_CLIENT_ID": "a", "OVH_CLIENT_SECRET": "b", "TF_VAR_p25_Client_Id": "c", "TF_VAR_p25_cLiEnT_sEcReT": "d",
		"AWS_ACCESS_KEY_ID": "e", "AWS_SECRET_ACCESS_KEY": "f", "TF_VAR_tenant_client_identity": "urn:v1:eu:identity:credential:x"}
	want = []Credential{{Endpoint: "ovh-eu", ClientID: "a", ClientSecret: "b"}, {Endpoint: "ovh-eu", ClientID: "c", ClientSecret: "d"}}
	if got, err := identities(mixed, "ovh-eu"); err != nil || !slices.Equal(got, want) {
		t.Errorf("identities(mixed case) = %v, %v; want the default and the P25 identity", got, err)
	}
}

// TestApplyStartWatch (T076 review r1): the second writer starts at the apply stream's first
// apply_start event however the stream is cut into reads — ConsumeApply reads through a 4096-byte
// buffer, so the event name can fall across two reads — and only once.
func TestApplyStartWatch(t *testing.T) {
	stream := strings.Repeat(`{"type":"version"}`+"\n", 300) + `{"type":"apply_start"}` + "\n" + `{"type":"apply_start"}` + "\n"
	for name, r := range map[string]io.Reader{
		"one byte per read": iotest.OneByteReader(strings.NewReader(stream)),
		"half reads":        iotest.HalfReader(strings.NewReader(stream)),
		"whole":             strings.NewReader(stream),
	} {
		fired := 0
		w := &applyStartWatch{r: r, fire: func() { fired++ }}
		if _, err := io.Copy(io.Discard, w); err != nil {
			t.Fatal(err)
		}
		if fired != 1 {
			t.Errorf("%s: the watch fired %d times, want once", name, fired)
		}
	}
}

// TestProbeIdentityWriterNeverStarted (T076 review r1): a companion apply whose stream reports no
// resource operation starts no second writer; the run record says so, so T010 does not read a
// missing record as anything else.
// Review r2: the same when the companion stops before its apply (here its plan fails).
func TestProbeIdentityWriterNeverStarted(t *testing.T) {
	t.Run("no-resource-operation", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{ApplyStream: w.stream(t, "companion-empty")})
		if err := p.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v\n%s", err, w.term)
		}
		checkNeverStarted(t, w, "the apply reported no resource operation")
	})
	t.Run("companion-plan-fails", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{PlanExit: 1})
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Error("a probe whose companion plan failed exited 0")
		}
		checkNeverStarted(t, w, "the companion stopped before its apply")
	})
}

func checkNeverStarted(t *testing.T, w *runWorld, why string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.runDir, "second-writer-p-companion.txt"))
	if err != nil || !strings.Contains(string(raw), "never started: "+why) {
		t.Errorf("the run record does not say the second writer never started because %s (%v): %q", why, err, raw)
	}
	for _, c := range w.calls(t, "tofu.log") {
		if c.Stack == "companion" && c.Cmd == "plan" && c.Plan == "" {
			t.Error("a second writer ran although the companion's apply reported no resource operation")
		}
	}
}

// TestProbeIdentityPlanOnly (T076, coordinator): `--plan-only` on a root with a companion plans the
// root only. Nothing is applied, so no identity exists: the companion never runs, the published
// output is never read and no credential file is written.
func TestProbeIdentityPlanOnly(t *testing.T) {
	withUmask(t)
	root := tempPrivate(t)
	w := newRunWorld(t)
	p := w.identityProbe(t, root, tofuStack{})
	p.Run.PlanOnly = true
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("plan-only Start: %v\n%s", err, w.term)
	}
	calls := w.calls(t, "tofu.log")
	for _, c := range calls {
		switch {
		case c.Stack == "companion" && childCall(c):
			t.Errorf("a plan-only run ran tofu %s in the companion root", c.Cmd)
		case c.Cmd == "output":
			t.Errorf("a plan-only run read the published identity (tofu output in %s)", c.Stack)
		case c.Cmd == "apply" || c.Cmd == "destroy":
			t.Errorf("a plan-only run ran tofu %s in %s", c.Cmd, c.Stack)
		}
	}
	if got := sequence(calls, "plan"); !slices.Equal(got, []string{"p"}) {
		t.Errorf("plans %v, want [p]: the root only", got)
	}
	for _, dir := range []string{root, w.runDir, w.stacks} {
		if left := holders([]string{dir}, identitySecrets); len(left) > 0 {
			t.Errorf("a plan-only run left %v holding a probe secret", left)
		}
	}
}

// TestProbeIdentityCleanup (T076, coordinator): `lz-live probe --cleanup` of a run whose root has a
// companion re-reads the published identity from the root's retained state (under the admin
// credential), has files.go write it to the run's probe directory, destroys the companion under
// that identity first, then the root, and removes the credential; it plans and applies nothing.
// A run whose root published no identity (it failed before) has no companion to destroy.
func TestProbeIdentityCleanup(t *testing.T) {
	failedRun := func(t *testing.T, w *runWorld, p Probe, root string, mut func(*tofuStack)) {
		t.Helper()
		st := w.scenario.Stacks["p"]
		st.DestroyExit = 1
		mut(&st)
		w.scenario.Stacks["p"] = st
		w.save(t)
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a probe whose root destroy failed exited 0")
		}
		_, state, pass := probeFiles(t, root)
		if !exists(state) || !exists(pass) {
			t.Fatalf("after a failed root destroy: state kept %v, passphrase kept %v; want both", exists(state), exists(pass))
		}
		for _, dir := range []string{root, w.runDir, w.stacks} {
			if left := holders([]string{dir}, identitySecrets); len(left) > 0 {
				t.Errorf("after the failed run, %v still hold a probe secret: the credential outlives the run", left)
			}
		}
		st.DestroyExit = 0
		w.scenario.Stacks["p"] = st
		w.save(t)
	}

	t.Run("companion-then-root", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{})
		failedRun(t, w, p, root, func(*tofuStack) {})
		before := len(w.calls(t, "tofu.log"))
		if err := p.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup: %v\n%s", err, w.term)
		}
		calls := w.calls(t, "tofu.log")[before:]
		probeDir, state, pass := probeFiles(t, root)
		readIdentity, firstCompanion := -1, -1
		for i, c := range calls {
			if !childCall(c) {
				continue
			}
			env := envOf(c)
			switch c.Stack {
			case "p":
				if env["OVH_CLIENT_ID"] != runCreds["OVH_CLIENT_ID"] || env["OVH_CLIENT_SECRET"] != runCreds["OVH_CLIENT_SECRET"] {
					t.Errorf("cleanup: root tofu %s without the admin credential", c.Cmd)
				}
				if c.Cmd == "output" && readIdentity < 0 {
					readIdentity = i
				}
			case "companion":
				if firstCompanion < 0 {
					firstCompanion = i
				}
				for k, want := range identityEnv {
					if env[k] != want {
						t.Errorf("cleanup: companion tofu %s: %s = %q, want the probe identity's", c.Cmd, k, env[k])
					}
				}
				for _, kv := range c.Env {
					if strings.Contains(kv, runCreds["OVH_CLIENT_SECRET"]) || strings.Contains(kv, runCreds["OVH_CLIENT_ID"]) {
						t.Errorf("cleanup: the admin credential reached companion tofu %s", c.Cmd)
					}
				}
				if c.Cmd == "destroy" {
					if len(c.Holders) == 0 {
						t.Error("cleanup: when the companion was destroyed, no file held the probe credential")
					}
					for _, h := range c.Holders {
						parts := strings.Split(h, "|")
						if !within(parts[0], probeDir) || parts[1] != "600" || parts[2] != "700" {
							t.Errorf("cleanup: the probe credential is in %s (mode %s, directory %s), want 600 in the run's probe directory (700)", parts[0], parts[1], parts[2])
						}
					}
				}
			}
			if c.Cmd == "plan" || c.Cmd == "apply" {
				t.Errorf("cleanup ran tofu %s in %s: it must only destroy", c.Cmd, c.Stack)
			}
		}
		if firstCompanion < 0 {
			t.Fatalf("cleanup never ran the companion (destroys %v)", sequence(calls, "destroy"))
		}
		if readIdentity < 0 || readIdentity > firstCompanion {
			t.Error("cleanup: the identity was not read from the root's state before the companion ran")
		}
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"companion", "p"}) {
			t.Errorf("cleanup destroys %v, want [companion p]", got)
		}
		for _, dir := range []string{root, w.runDir, w.stacks} {
			if left := holders([]string{dir}, identitySecrets); len(left) > 0 {
				t.Errorf("after the cleanup, %v still hold a probe secret", left)
			}
		}
		if exists(state) || exists(pass) {
			t.Errorf("after a passing cleanup: state kept %v, passphrase kept %v", exists(state), exists(pass))
		}
		for _, s := range identitySecrets {
			if strings.Contains(w.term.String(), s) {
				t.Error("a probe secret reached the terminal during the cleanup")
			}
		}
	})

	t.Run("no-published-identity", func(t *testing.T) {
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		p := w.identityProbe(t, root, tofuStack{})
		none := filepath.Join(w.bin, "outputs-none.json")
		if err := os.WriteFile(none, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		failedRun(t, w, p, root, func(st *tofuStack) { st.ApplyExit, st.Outputs = 1, none })
		before := len(w.calls(t, "tofu.log"))
		if err := p.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup of a root that published no identity: %v\n%s", err, w.term)
		}
		calls := w.calls(t, "tofu.log")[before:]
		for _, c := range calls {
			if c.Stack == "companion" && childCall(c) {
				t.Errorf("cleanup ran tofu %s in the companion of a root that published no identity", c.Cmd)
			}
		}
		if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"p"}) {
			t.Errorf("cleanup destroys %v, want [p]", got)
		}
	})
}

// Identity pairing (T079; found in T076, evidence/T076.md gaps): every identity a root publishes in
// companion_env — a key whose name ends in client_id in any case (…_CLIENT_ID, …_Client_Id,
// …_client_id) with the key of the same prefix ending in client_secret, in any case — is bound to
// the run's account (Probe.Bind) with that pair's secret before the companion's first child, and
// the companion gets every published key under its own name. A key that looks like a credential
// but pairs with no identity — half a pair in any case, a pair with an empty half, or a name
// ending in key, token, password or secret in any case (T080, coordinator: the narrow reading) —
// is refused before the companion starts: the run fails, no companion child runs, the root is
// still destroyed and no file, terminal or returned error keeps a published secret. The S3 keys
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY) are no OVHcloud identity and pass unbound (T076); so
// does any other name, e.g. an identity URN holding "client" (T080). Not pinned: whether two keys
// that differ in the case of their prefix pair (they are either refused or bound, never passed on
// unbound).

const (
	pairDefaultID     = "fake-pair-default-client-id"
	pairDefaultSecret = "pair-SECRET-default-5c1e09a7"
	pairID            = "fake-pair-extra-client-id"
	pairOtherID       = "fake-pair-other-client-id"
	pairSecret        = "pair-SECRET-extra-83d4f62b"
	pairOtherSecret   = "pair-SECRET-other-2a6d50f9"
	pairS3Secret      = "pair-SECRET-s3-0b7e91c4"
)

var pairSecrets = []string{pairDefaultSecret, pairSecret, pairOtherSecret, pairS3Secret}

// bindCall is one Probe.Bind call and the number of tofu calls logged before it.
type bindCall struct {
	cred Credential
	at   int
}

// pairingProbe is identityProbe whose stage publishes the default identity, the S3 keys and extra,
// with a Bind that records every identity it is given. It returns the published environment.
func (w *runWorld) pairingProbe(t *testing.T, root string, extra map[string]string, binds *[]bindCall) (Probe, map[string]string) {
	t.Helper()
	p := w.identityProbe(t, root, tofuStack{})
	env := map[string]string{"OVH_CLIENT_ID": pairDefaultID, "OVH_CLIENT_SECRET": pairDefaultSecret,
		"AWS_ACCESS_KEY_ID": "fake-pair-s3-access-key", "AWS_SECRET_ACCESS_KEY": pairS3Secret}
	maps.Copy(env, extra)
	raw, err := json.Marshal(map[string]any{companionOutput: map[string]any{"sensitive": true, "value": env}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.bin, "outputs-p.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	st := w.scenario.Stacks["companion"]
	st.ScanFor = pairSecrets
	w.scenario.Stacks["companion"] = st
	w.save(t)
	log := filepath.Join(w.bin, "tofu.log")
	p.Bind = func(_ context.Context, c Credential) error {
		raw, _ := os.ReadFile(log)
		*binds = append(*binds, bindCall{c, bytes.Count(raw, []byte("\n"))})
		return nil
	}
	return p, env
}

// companionChildren returns the indexes of the companion's child calls.
func companionChildren(calls []tofuCall) []int {
	var out []int
	for i, c := range calls {
		if c.Stack == "companion" && childCall(c) {
			out = append(out, i)
		}
	}
	return out
}

// checkNoUnbound: when the companion ran, every published key ending in client_id (any case) was
// bound before the companion's first child with a secret published under that key's prefix.
func checkNoUnbound(t *testing.T, calls []tofuCall, env map[string]string, binds []bindCall) {
	t.Helper()
	comp := companionChildren(calls)
	if len(comp) == 0 {
		return
	}
	for k, id := range env {
		if !strings.HasSuffix(strings.ToLower(k), "client_id") {
			continue
		}
		prefix := strings.ToLower(k[:len(k)-len("client_id")])
		bound := false
		for _, b := range binds {
			if b.cred.ClientID != id || b.at > comp[0] {
				continue
			}
			for k2, v := range env {
				if v == b.cred.ClientSecret && strings.EqualFold(k2, prefix+"client_secret") {
					bound = true
				}
			}
		}
		if !bound {
			var ids []string
			for _, b := range binds {
				ids = append(ids, fmt.Sprintf("%s@%d", b.cred.ClientID, b.at))
			}
			t.Errorf("%s reached the companion unbound: no bind of its client id with its pair's secret before the companion's first call %d (binds %v)", k, comp[0], ids)
		}
	}
}

// checkNoPairSecret: no file under the run's directories and nothing on the terminal holds a
// published secret after the run.
func checkNoPairSecret(t *testing.T, w *runWorld, root string) {
	t.Helper()
	for _, dir := range []string{root, w.runDir, w.stacks} {
		if left := holders([]string{dir}, pairSecrets); len(left) > 0 {
			t.Errorf("after the run, %v still hold a published secret", left)
		}
	}
	for _, s := range pairSecrets {
		if strings.Contains(w.term.String(), s) {
			t.Error("a published secret reached the terminal")
		}
	}
}

// checkRefused: the run failed before any companion child; the root was still destroyed.
func checkRefused(t *testing.T, err error, calls []tofuCall) {
	t.Helper()
	if ExitCode(err) == 0 {
		t.Error("the run exited 0: a credential-looking key that pairs with no identity was not refused")
	} else if slices.ContainsFunc(pairSecrets, func(s string) bool { return strings.Contains(err.Error(), s) }) {
		t.Error("the refusal's error holds a published secret (lz-live prints it)")
	}
	if comp := companionChildren(calls); len(comp) > 0 {
		t.Errorf("the companion ran %d tofu calls (first: %s) with a credential that pairs with no bound identity", len(comp), calls[comp[0]].Cmd)
	}
	if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"p"}) {
		t.Errorf("destroys %v, want [p]: the root that holds the identities is destroyed", got)
	}
}

func byClientID(a, b Credential) int { return strings.Compare(a.ClientID, b.ClientID) }

// TestProbeIdentityPairing (T079 Verify line).
func TestProbeIdentityPairing(t *testing.T) {
	endpoint := runCreds["OVH_ENDPOINT"]
	defaultID := Credential{Endpoint: endpoint, ClientID: pairDefaultID, ClientSecret: pairDefaultSecret}
	extraID := Credential{Endpoint: endpoint, ClientID: pairID, ClientSecret: pairSecret}
	otherID := Credential{Endpoint: endpoint, ClientID: pairOtherID, ClientSecret: pairOtherSecret}

	// Paired and bound, whatever the case of the suffixes; every published identity, not only two.
	for name, tc := range map[string]struct {
		extra map[string]string
		want  []Credential
	}{
		"lower":                {map[string]string{"TF_VAR_p25_client_id": pairID, "TF_VAR_p25_client_secret": pairSecret}, []Credential{defaultID, extraID}},
		"upper":                {map[string]string{"TF_VAR_p25_CLIENT_ID": pairID, "TF_VAR_p25_CLIENT_SECRET": pairSecret}, []Credential{defaultID, extraID}},
		"title":                {map[string]string{"TF_VAR_p25_Client_Id": pairID, "TF_VAR_p25_Client_Secret": pairSecret}, []Credential{defaultID, extraID}},
		"id title, secret up":  {map[string]string{"TF_VAR_p25_Client_Id": pairID, "TF_VAR_p25_CLIENT_SECRET": pairSecret}, []Credential{defaultID, extraID}},
		"id lower, secret odd": {map[string]string{"TF_VAR_p25_client_id": pairID, "TF_VAR_p25_cLiEnT_sEcReT": pairSecret}, []Credential{defaultID, extraID}},
		"id odd, secret lower": {map[string]string{"TF_VAR_p25_cLiEnT_iD": pairID, "TF_VAR_p25_client_secret": pairSecret}, []Credential{defaultID, extraID}},
		"three identities": {map[string]string{"TF_VAR_p25_Client_Id": pairID, "TF_VAR_p25_Client_Secret": pairSecret,
			"TF_VAR_p26_CLIENT_ID": pairOtherID, "TF_VAR_p26_client_secret": pairOtherSecret}, []Credential{defaultID, extraID, otherID}},
	} {
		t.Run("bound/"+name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds []bindCall
			p, env := w.pairingProbe(t, root, tc.extra, &binds)
			if err := p.Start(context.Background()); err != nil {
				t.Errorf("Start: %v\n%s", err, w.term)
			}
			calls := w.calls(t, "tofu.log")
			comp := companionChildren(calls)
			if len(comp) == 0 {
				t.Fatalf("the companion never ran (inits %v)", sequence(calls, "init"))
			}
			got := make([]Credential, 0, len(binds))
			for _, b := range binds {
				got = append(got, b.cred)
				if b.at > comp[0] {
					t.Errorf("identity %s bound after %d tofu calls, but the companion's first call was call %d: bind before the companion", b.cred.ClientID, b.at, comp[0])
				}
			}
			slices.SortFunc(got, byClientID)
			want := slices.SortedFunc(slices.Values(tc.want), byClientID)
			if !slices.Equal(got, want) {
				var ids []string
				for _, c := range got {
					ids = append(ids, c.ClientID)
				}
				t.Errorf("bound %v, want exactly %d identities, each with its own secret and the run's endpoint", ids, len(want))
			}
			checkNoUnbound(t, calls, env, binds)
			// The companion gets every published key under its own name (a TF_VAR_ name is
			// case-sensitive), from a 0600 file in the run's probe directory (0700) only.
			probeDir, _, _ := probeFiles(t, root)
			for _, i := range comp {
				ce := envOf(calls[i])
				for k, v := range env {
					if ce[k] != v {
						t.Errorf("companion tofu %s: %s is not the published value under the published name", calls[i].Cmd, k)
					}
				}
				for _, h := range calls[i].Holders {
					parts := strings.Split(h, "|")
					if !within(parts[0], probeDir) || parts[1] != "600" || parts[2] != "700" {
						t.Errorf("companion tofu %s: a published secret is in %s (mode %s, directory %s), want 600 in the run's probe directory (700)", calls[i].Cmd, parts[0], parts[1], parts[2])
					}
				}
			}
			if len(calls[comp[0]].Holders) == 0 {
				t.Error("when the companion started, no file held the published credential")
			}
			if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"companion", "p"}) {
				t.Errorf("destroys %v, want [companion p]", got)
			}
			checkNoPairSecret(t, w, root)
		})
	}

	// Credential-looking, pairs with no identity: refused before the companion starts.
	for name, extra := range map[string]map[string]string{
		"title id alone":           {"TF_VAR_p25_Client_Id": pairID},
		"title secret alone":       {"TF_VAR_p25_Client_Secret": pairSecret},
		"upper secret alone":       {"TF_VAR_p25_CLIENT_SECRET": pairSecret},
		"title pair, empty secret": {"TF_VAR_p25_Client_Id": pairID, "TF_VAR_p25_Client_Secret": ""},
		"title pair, empty id":     {"TF_VAR_p25_Client_Id": "", "TF_VAR_p25_Client_Secret": pairSecret},
		"no separator":             {"TF_VAR_p25_clientid": pairID, "TF_VAR_p25_clientsecret": pairSecret},
		"client key":               {"TF_VAR_p25_Client_Key": pairSecret},
		"client token":             {"TF_VAR_P25_CLIENT_TOKEN": pairSecret},
		"secret of another prefix": {"TF_VAR_p25_Client_Id": pairID, "TF_VAR_p26_client_secret": pairSecret},
		// T080: a credential-shaped suffix without "client" (narrow reading).
		"api key":     {"TF_VAR_signing_Key": pairSecret},
		"api token":   {"TF_VAR_api_token": pairSecret},
		"password":    {"TF_VAR_db_PassWord": pairSecret},
		"bare secret": {"TF_VAR_api_SECRET": pairSecret},
	} {
		t.Run("refused/"+name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds []bindCall
			p, env := w.pairingProbe(t, root, extra, &binds)
			err := p.Start(context.Background())
			calls := w.calls(t, "tofu.log")
			checkRefused(t, err, calls)
			checkNoUnbound(t, calls, env, binds)
			checkNoPairSecret(t, w, root)
		})
	}

	// Not credential-shaped (T080, coordinator: the narrow reading): passed to the companion under
	// its own name, unbound; only the default identity is bound.
	for name, extra := range map[string]map[string]string{
		"identity urn holding client": {"TF_VAR_tenant_client_identity": "urn:v1:eu:identity:credential:fake-pair/oauth2-client"},
		"secret not as suffix":        {"TF_VAR_Secret_Name": "fake-pair-secret-name"},
	} {
		t.Run("passed/"+name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds []bindCall
			p, env := w.pairingProbe(t, root, extra, &binds)
			if err := p.Start(context.Background()); err != nil {
				t.Errorf("Start: %v: a key that is not credential-shaped was refused", err)
			}
			calls := w.calls(t, "tofu.log")
			comp := companionChildren(calls)
			if len(comp) == 0 {
				t.Fatalf("the companion never ran (inits %v)", sequence(calls, "init"))
			}
			if len(binds) != 1 || binds[0].cred != defaultID {
				t.Errorf("bound %d identities, want only the default one", len(binds))
			}
			for k, v := range extra {
				if envOf(calls[comp[0]])[k] != v {
					t.Errorf("the companion did not get %s under its own name", k)
				}
			}
			checkNoUnbound(t, calls, env, binds)
			checkNoPairSecret(t, w, root)
		})
	}

	// Not pinned which way, only that no identity passes unbound: refused, or every id bound.
	for name, extra := range map[string]map[string]string{
		"prefix case differs": {"TF_VAR_P25_client_id": pairID, "TF_VAR_p25_client_secret": pairSecret},
		"two ids, one secret": {"TF_VAR_p25_CLIENT_ID": pairID, "TF_VAR_p25_client_id": pairOtherID, "TF_VAR_p25_Client_Secret": pairSecret},
	} {
		t.Run("refused-or-bound/"+name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds []bindCall
			p, env := w.pairingProbe(t, root, extra, &binds)
			err := p.Start(context.Background())
			calls := w.calls(t, "tofu.log")
			if len(companionChildren(calls)) == 0 {
				checkRefused(t, err, calls)
			} else if err != nil {
				t.Errorf("Start: %v\n%s", err, w.term)
			}
			checkNoUnbound(t, calls, env, binds)
			checkNoPairSecret(t, w, root)
		})
	}

	// `--cleanup` re-reads companion_env from the root's retained state: the same pairing applies.
	type cleaned struct {
		err   error
		calls []tofuCall
		env   map[string]string
		binds []bindCall
		w     *runWorld
		root  string
	}
	cleanup := func(t *testing.T, extra map[string]string) cleaned {
		t.Helper()
		withUmask(t)
		root := tempPrivate(t)
		w := newRunWorld(t)
		var binds []bindCall
		p, env := w.pairingProbe(t, root, extra, &binds)
		st := w.scenario.Stacks["p"]
		st.DestroyExit = 1
		w.scenario.Stacks["p"] = st
		w.save(t)
		if err := p.Start(context.Background()); ExitCode(err) == 0 {
			t.Fatal("a probe whose root destroy failed exited 0")
		}
		st.DestroyExit = 0
		w.scenario.Stacks["p"] = st
		w.save(t)
		before := len(w.calls(t, "tofu.log"))
		binds = binds[:0]
		err := p.Cleanup(context.Background())
		for i := range binds {
			binds[i].at -= before
		}
		return cleaned{err, w.calls(t, "tofu.log")[before:], env, binds, w, root}
	}
	t.Run("cleanup/bound", func(t *testing.T) {
		c := cleanup(t, map[string]string{"TF_VAR_p25_Client_Id": pairID, "TF_VAR_p25_Client_Secret": pairSecret})
		if c.err != nil {
			t.Errorf("cleanup: %v\n%s", c.err, c.w.term)
		}
		if len(companionChildren(c.calls)) == 0 {
			t.Fatalf("cleanup never ran the companion (destroys %v)", sequence(c.calls, "destroy"))
		}
		var ids []string
		for _, b := range c.binds {
			ids = append(ids, b.cred.ClientID)
		}
		// The whole credential, endpoint included (T079 review r2).
		if !slices.ContainsFunc(c.binds, func(b bindCall) bool { return b.cred == extraID }) {
			t.Errorf("cleanup bound %v: the title-case identity was not bound with its secret and the run's endpoint", ids)
		}
		checkNoUnbound(t, c.calls, c.env, c.binds)
		if got := sequence(c.calls, "destroy"); !slices.Equal(got, []string{"companion", "p"}) {
			t.Errorf("cleanup destroys %v, want [companion p]", got)
		}
		checkNoPairSecret(t, c.w, c.root)
	})
	t.Run("cleanup/refused", func(t *testing.T) {
		c := cleanup(t, map[string]string{"TF_VAR_p25_Client_Id": pairID})
		checkRefused(t, c.err, c.calls)
		checkNoUnbound(t, c.calls, c.env, c.binds)
		checkNoPairSecret(t, c.w, c.root)
	})
}
