package live

// T058: `lz-live plan|apply -- <instance|all>` against fake tofu, a fake object store and real
// run locks (spec 005 FR-009, FR-010, FR-011, SC-005; research R6, R11, R12, R21; data-model
// *Stage table*, *Envelope-to-input adapter*, *Account binding and local files*, *Live run
// record*; contracts/checks.md V007, guards G1, G2, G7, G14 caller part). The lane acts on the
// selected set in run order, each stack under its own authority; holds stacks.HoldRun's locks from
// before the first plan to the end of the run on every path; judges every saved plan with
// protect.go before applying exactly that file; writes the deployer and tenant S3 credential
// files through files.go after account-governance and tenant-state; reads consumed inputs through
// stacks.Adapt (a consumer without its producer's artefact is blocked, exit 2); refuses `destroy`
// of every retained instance; and never lets a seeded secret reach a stream, argv, a rendered
// plan, a record, an input file or an artefact.
//
// The fake tofu models the generated roots (stacks/…/_lz_backend.tf, _lz_variables.tf): init
// needs S3 keys the state bucket admits; plan needs the provider credential, the state passphrase
// and every required root variable without a default (state_project_id, tenants, project_id,
// project) with the bound values; show and apply need the passphrase (plan and state encryption
// enforced); apply needs a saved plan of the same stack that is not stale; output -json prints
// the stage's outputs, sensitive ones included. Every call repeats its environment's secrets on
// stderr and stdout, and the rendered plan shows them: the lane must redact every stream.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

const (
	laneRepoRoot   = "../../.."
	laneWorldFile  = "lane-world.json"
	laneAccount    = "xx000058-ovh"
	laneProject    = "f0000000000000000000000000000058" // KD-1: STATE and DEMO_DEV resolve to one project
	laneRevision   = "5858585858585858585858585858585858585858"
	laneAccountBkt = "lz-bkt-state"
	laneTenantBkt  = "lz-demo-bkt-state"
	// Fixture project id of the envelope fixtures, replaced by laneProject.
	laneFixtureProject = "fedcba9876543210fedcba9876543210"
)

// The seeded credentials. Client ids and access keys are not secret; every *Secret is.
const (
	laneAdminID        = "EU.lane-admin-58"
	laneAdminSecret    = "lz-seed-t058-admin-client-secret"
	laneAccountAK      = "AK-lane-account-58"
	laneAccountSK      = "lz-seed-t058-account-s3-secret"
	lanePassphrase     = "lz-seed-t058-state-passphrase-0123456789"
	lanePlatformID     = "EU.lzplatformdeployer01" // envelopes/account-governance.json
	lanePlatformSecret = "lz-seed-t058-platform-client-secret"
	laneTenantID       = "EU.lzdemodeployer01"
	laneTenantSecret   = "lz-seed-t058-tenant-client-secret"
	laneTenantAK       = "AK-lane-demo-tenant-58"
	laneTenantSK       = "lz-seed-t058-tenant-s3-secret"
	lanePlatformAK     = "AK-lane-demo-platform-58"
	lanePlatformSK     = "lz-seed-t058-platform-s3-secret"
	laneAmbientOVH     = "lz-seed-t058-ambient-ovh-secret"
	laneAmbientAWS     = "lz-seed-t058-ambient-aws-secret"
)

// laneAmbient are further ambient variables of the caller (root-bound application keys, an AWS
// session or profile, a TF_VAR_): none may reach a child (FR-010, G12; review r1).
var laneAmbient = map[string]string{
	"OVH_APPLICATION_KEY":    "lz-seed-t058-ambient-app-key",
	"OVH_APPLICATION_SECRET": "lz-seed-t058-ambient-app-secret",
	"OVH_CONSUMER_KEY":       "lz-seed-t058-ambient-consumer-key",
	"AWS_SESSION_TOKEN":      "lz-seed-t058-ambient-session-token",
	"AWS_PROFILE":            "lz-seed-t058-ambient-profile",
	"TF_VAR_project_id":      "lz-seed-t058-ambient-tf-var",
}

// laneOrder is the sandbox manifest's run order without account-bootstrap (levels, then path:
// evidence/T040.md reading 1, `stacks:order -- all` in T041).
var laneOrder = []string{"account-governance", "demo-state", "demo-dev-project", "demo-dev-gra11-network", "demo-dev-gra11-runtime"}

// laneAuthority is each row's authority (data-model *Stage table*, research R6).
var laneAuthority = map[string]Authority{
	"account-bootstrap": AuthorityBootstrap, "account-governance": AuthorityBootstrap, "demo-state": AuthorityBootstrap,
	"demo-dev-project": AuthorityPlatform, "demo-dev-gra11-network": AuthorityTenant, "demo-dev-gra11-runtime": AuthorityTenant,
}

// laneCreds are the five child variables of each authority.
var laneCreds = map[Authority]map[string]string{
	AuthorityBootstrap: {"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": laneAdminID, "OVH_CLIENT_SECRET": laneAdminSecret,
		"AWS_ACCESS_KEY_ID": laneAccountAK, "AWS_SECRET_ACCESS_KEY": laneAccountSK},
	AuthorityPlatform: {"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": lanePlatformID, "OVH_CLIENT_SECRET": lanePlatformSecret,
		"AWS_ACCESS_KEY_ID": lanePlatformAK, "AWS_SECRET_ACCESS_KEY": lanePlatformSK},
	AuthorityTenant: {"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": laneTenantID, "OVH_CLIENT_SECRET": laneTenantSecret,
		"AWS_ACCESS_KEY_ID": laneTenantAK, "AWS_SECRET_ACCESS_KEY": laneTenantSK},
}

// laneDeployerFiles are the files the lane writes (data-model *Account binding and local files*):
// the deployer clients after account-governance, the tenant's S3 keys after tenant-state.
var laneDeployerFiles = map[string]struct {
	after  string
	values map[string]string
}{
	"platform-deployer.env":           {"account-governance", map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": lanePlatformID, "OVH_CLIENT_SECRET": lanePlatformSecret}},
	"tenants/demo/deployer.env":       {"account-governance", map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": laneTenantID, "OVH_CLIENT_SECRET": laneTenantSecret}},
	"tenants/demo/state.env":          {"demo-state", map[string]string{"AWS_ACCESS_KEY_ID": laneTenantAK, "AWS_SECRET_ACCESS_KEY": laneTenantSK}},
	"tenants/demo/platform-state.env": {"demo-state", map[string]string{"AWS_ACCESS_KEY_ID": lanePlatformAK, "AWS_SECRET_ACCESS_KEY": lanePlatformSK}},
}

// laneSecrets maps every seeded secret to the one file below the config root that may hold it
// ("" for the ambient ones, which no file may hold).
var laneSecrets = map[string]string{
	laneAdminSecret:                      "sandbox.env",
	laneAccountSK:                        "accounts/" + laneAccount + "/state.env",
	lanePassphrase:                       "accounts/" + laneAccount + "/state-passphrase.env",
	lanePlatformSecret:                   "accounts/" + laneAccount + "/platform-deployer.env",
	laneTenantSecret:                     "accounts/" + laneAccount + "/tenants/demo/deployer.env",
	laneTenantSK:                         "accounts/" + laneAccount + "/tenants/demo/state.env",
	lanePlatformSK:                       "accounts/" + laneAccount + "/tenants/demo/platform-state.env",
	laneAmbientOVH:                       "",
	laneAmbientAWS:                       "",
	"lz-seed-t058-ambient-app-secret":    "",
	"lz-seed-t058-ambient-consumer-key":  "",
	"lz-seed-t058-ambient-session-token": "",
}

// laneBucketKeys: which S3 keys each state bucket admits (the account bucket the platform S3 user
// of bootstrap; a tenant bucket its tenant and platform users, research R6, G6).
var laneBucketKeys = map[string]map[string]string{
	laneAccountBkt: {laneAccountAK: laneAccountSK},
	laneTenantBkt:  {laneTenantAK: laneTenantSK, lanePlatformAK: lanePlatformSK},
}

// ---------------------------------------------------------------- fake world and fake tofu

type laneWorld struct {
	Checkout   string               `json:"checkout"`
	Log        string               `json:"log"`    // calls of the fake tofu, the store and the lock store, in order
	States     string               `json:"states"` // fake remote state per instance: <id>.json
	LockDir    string               `json:"lock_dir"`
	Passphrase string               `json:"passphrase"`
	Project    string               `json:"project"`       // bound project of DEMO_DEV
	State      string               `json:"state_project"` // bound project of STATE (KD-1: the same id unless a row changes DEMO_DEV)
	Stacks     map[string]laneStack `json:"stacks"`        // by checkout-relative stack path
}

type laneStack struct {
	ID         string            `json:"id"`
	Stage      string            `json:"stage"`
	Bucket     string            `json:"bucket"`
	BucketKeys map[string]string `json:"bucket_keys"`
	Locks      []string          `json:"locks"`   // lock names every plan/apply of the stack finds held
	Plan       string            `json:"plan"`    // file: the `show -json` document
	Outputs    string            `json:"outputs"` // file: the `output -json` document
	ApplyExit  int               `json:"apply_exit"`
	// Fail names the call of the stack that fails (init, plan, show-json, show, output; T059,
	// coordinator decision 3b: the locks are released after any of them).
	Fail string `json:"fail,omitempty"`
	// Block names the call of the stack that waits for a signal (review r1 of T059: cancellation);
	// "apply+events" makes the apply print its apply_complete events first (T046: inventory).
	Block string `json:"block,omitempty"`
	// T046 (chain, destroy): DestroyPlan is the `show -json` document of a `plan -destroy` file;
	// StateDoc the `show -json` document of the stack's state (no plan file argument), "" when it
	// holds nothing the test names; Creates the resources an apply reports as apply_complete events.
	DestroyPlan string       `json:"destroy_plan,omitempty"`
	StateDoc    string       `json:"state_doc,omitempty"`
	Creates     []laneCreate `json:"creates,omitempty"`
}

// laneCreate is one resource an apply creates: one `apply_complete` event of `tofu apply -json`
// (hook.resource.addr, hook.resource.resource_type, hook.id_value; premise P24, inventory.go).
type laneCreate struct {
	Addr string `json:"addr"`
	Type string `json:"type"`
	ID   string `json:"id"`
}

// laneApplyComplete is c's `apply_complete` event line under `-json`; without it, the human line
// OpenTofu prints (no event to parse: T046 review r1).
func laneApplyComplete(c laneCreate, flags map[string][]string) string {
	if _, ok := flags["-json"]; !ok {
		return fmt.Sprintf("%s: Creation complete after 1s [id=%s]", c.Addr, c.ID)
	}
	ev := map[string]any{"@level": "info", "@message": c.Addr + ": Creation complete", "type": "apply_complete",
		"hook": map[string]any{"resource": map[string]any{"addr": c.Addr, "resource_type": c.Type}, "action": "create", "id_key": "id", "id_value": c.ID}}
	raw, _ := json.Marshal(ev)
	return string(raw)
}

// laneCall is one logged event: a fake tofu call, a store operation, a lock event or a binding.
type laneCall struct {
	Cmd      string   `json:"cmd"`   // init plan show-json show apply output other | put get | lock release | bind
	Stack    string   `json:"stack"` // instance id (tofu), bucket/key (store), lock name
	Args     []string `json:"args,omitempty"`
	Env      []string `json:"env,omitempty"`
	Plan     string   `json:"plan,omitempty"`
	Nonce    string   `json:"nonce,omitempty"`
	VarFiles []string `json:"var_files,omitempty"`
	Unlocked []string `json:"unlocked,omitempty"` // run locks found free during the call
	Signal   string   `json:"signal,omitempty"`   // the signal a blocked call received
	Key      string   `json:"key,omitempty"`      // store: the access key the store was opened with
	Destroy  bool     `json:"destroy,omitempty"`  // plan -destroy, or a show/apply of such a plan file (T046)
	Exit     int      `json:"exit"`
}

// laneNotTofu are the logged events that are not a tofu call (store, locks, binding, and the
// leftover listings of T046).
var laneNotTofu = []string{"put", "get", "lock", "release", "lock-refused", "bind", "list"}

func laneAppend(path string, c laneCall) {
	line, _ := json.Marshal(c)
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, string(line))
		f.Close()
	}
}

// laneFakeWorld returns bin/lane-world.json when the test binary runs as that bin's tofu.
func laneFakeWorld() string {
	if filepath.Base(os.Args[0]) != "tofu" {
		return ""
	}
	w := filepath.Join(filepath.Dir(os.Args[0]), laneWorldFile)
	if _, err := os.Stat(w); err != nil {
		return ""
	}
	return w
}

type laneState struct {
	PassSHA string `json:"pass_sha"`
	Serial  int    `json:"serial"`
	Empty   bool   `json:"empty,omitempty"` // destroyed: the state holds no resource (T046)
}

type lanePlanFile struct {
	Stack   string `json:"stack"`
	Nonce   string `json:"nonce"`
	Serial  int    `json:"serial"`
	PassSHA string `json:"pass_sha"`
	Destroy bool   `json:"destroy,omitempty"`
}

// laneRequired are the root variables without a default per stage (stacks/…/_lz_variables.tf).
var laneRequired = map[string][]string{
	"tenant-state":       {"state_project_id"},
	"account-governance": {"tenants"},
	"project":            {"project_id"},
	"project-network":    {"project"},
	"runtime":            {"project"},
}

// fakeLaneTofu is the fake tofu of the plan/apply lane.
func fakeLaneTofu(worldPath string, args []string) int {
	raw, err := os.ReadFile(worldPath)
	var w laneWorld
	if err != nil || json.Unmarshal(raw, &w) != nil {
		fmt.Fprintln(os.Stderr, "fake tofu: no world")
		return 1
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
	rel, _ := filepath.Rel(w.Checkout, dir)
	st, known := w.Stacks[filepath.ToSlash(rel)]
	call := laneCall{Cmd: "other", Stack: st.ID, Args: os.Args[1:], Env: os.Environ()}
	if !known {
		call.Stack = "unknown:" + filepath.ToSlash(rel)
	}
	done := func(code int) int {
		call.Exit = code
		laneAppend(w.Log, call)
		return code
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
		return done(1)
	}
	// Every call repeats its environment's secrets on both streams.
	echo := fmt.Sprintf("fake-lane-tofu client_secret=%s s3=%s passphrase=%s\n",
		os.Getenv("OVH_CLIENT_SECRET"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("TF_VAR_state_passphrase"))
	fmt.Fprint(os.Stderr, echo)
	if len(args) == 0 || !known {
		return done(1)
	}
	sub, rest := args[0], args[1:]
	call.Cmd = sub
	// Run locks: every call of the stack finds them held by the run (flock on the lock file).
	for _, n := range st.Locks {
		f, err := os.OpenFile(filepath.Join(w.LockDir, n+".lock"), os.O_RDWR, 0)
		if err != nil {
			call.Unlocked = append(call.Unlocked, n)
			continue
		}
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			call.Unlocked = append(call.Unlocked, n)
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		}
		f.Close()
	}
	var positional []string
	flags := map[string][]string{}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name, v, ok := strings.Cut(a, "=")
		if !ok && slices.Contains(bssValueFlags, a) && i+1 < len(rest) {
			v, ok = rest[i+1], true
			i++
		}
		flags[name] = append(flags[name], v)
	}
	call.VarFiles = flags["-var-file"]
	failing := sub
	if _, ok := flags["-json"]; ok && sub == "show" {
		failing = "show-json"
	}
	if st.Fail != "" && st.Fail == failing {
		call.Cmd = failing
		return fail("fake %s failure of %s", failing, st.ID)
	}
	// Block (review r1): the call announces itself next to the log and waits for SIGINT, as tofu
	// stops gracefully on it; a kill is never logged.
	// A destroy (a call on a `plan -destroy` file, or the plan itself) never blocks (T046).
	var blockPlan lanePlanFile
	if len(positional) == 1 {
		if raw, err := os.ReadFile(positional[0]); err == nil && json.Unmarshal(raw, &blockPlan) == nil {
			call.Plan, call.Nonce, call.Destroy = positional[0], blockPlan.Nonce, blockPlan.Destroy
		}
	}
	_, destroying := flags["-destroy"]
	if st.Block != "" && (st.Block == failing || st.Block == "apply+events" && failing == "apply") && !blockPlan.Destroy && !destroying {
		call.Cmd = failing
		if st.Block == "apply+events" {
			for _, c := range st.Creates {
				fmt.Println(laneApplyComplete(c, flags))
			}
		}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		_ = os.WriteFile(w.Log+".blocked", []byte(st.ID), 0o600)
		select {
		case s := <-sig:
			call.Signal = s.String()
			return fail("interrupted by %s", s)
		// Bounded so a run that never stops its child fails in seconds, inside the entry's time (T046).
		case <-time.After(10 * time.Second):
			return fail("fake: never interrupted")
		}
	}
	dataDir := fakeDataDir(dir)
	marker := filepath.Join(dataDir, "lane-init-"+st.ID)
	initialised := func() bool { _, err := os.Stat(marker); return err == nil }
	pass := passSHA(os.Getenv("TF_VAR_state_passphrase"))
	statePath := filepath.Join(w.States, st.ID+".json")
	var state laneState
	hasState := false
	if raw, err := os.ReadFile(statePath); err == nil && json.Unmarshal(raw, &state) == nil {
		hasState = true
	}
	stateOK := func() bool {
		if os.Getenv("TF_VAR_state_passphrase") == "" {
			return false
		}
		return !hasState || state.PassSHA == pass
	}
	readPlan := func(p string) (lanePlanFile, bool) {
		var pf lanePlanFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		raw, err := os.ReadFile(p)
		return pf, err == nil && json.Unmarshal(raw, &pf) == nil
	}
	switch sub {
	case "init":
		// account-bootstrap has the local backend: no bucket to open (T046 reads its state).
		if sk, ok := st.BucketKeys[os.Getenv("AWS_ACCESS_KEY_ID")]; st.Bucket != "" && (!ok || sk != os.Getenv("AWS_SECRET_ACCESS_KEY")) {
			return fail("Failed to get existing workspaces: AccessDenied on bucket %s", st.Bucket)
		}
		if os.MkdirAll(dataDir, 0o700) != nil || os.WriteFile(marker, []byte(st.Bucket), 0o600) != nil {
			return fail("cannot write the data directory")
		}
		return done(0)
	case "plan":
		if !initialised() {
			return fail(`Backend initialization required, please run "tofu init"`)
		}
		if !stateOK() {
			return fail("decrypting the state of %s: the passphrase does not match", st.ID)
		}
		if os.Getenv("OVH_ENDPOINT") != "ovh-eu" || os.Getenv("OVH_CLIENT_ID") == "" || os.Getenv("OVH_CLIENT_SECRET") == "" {
			return fail("ovh provider: missing credentials")
		}
		vars := map[string]json.RawMessage{}
		for _, f := range call.VarFiles {
			raw, err := os.ReadFile(f)
			var m map[string]json.RawMessage
			if err != nil || json.Unmarshal(raw, &m) != nil {
				return fail("Failed to read variables file %s", f)
			}
			maps.Copy(vars, m)
		}
		for _, name := range laneRequired[st.Stage] {
			v, ok := vars[name]
			if !ok {
				if e := os.Getenv("TF_VAR_" + name); e != "" {
					v, ok = json.RawMessage(e), true
				}
			}
			if !ok {
				return fail("No value for required variable %q", name)
			}
			project := w.Project
			if name == "state_project_id" {
				project = w.State
			}
			if msg := laneVarCheck(name, v, project); msg != "" {
				return fail("Invalid value for variable %q: %s", name, msg)
			}
		}
		out := ""
		if o := flags["-out"]; len(o) > 0 {
			out = o[len(o)-1]
		}
		if out == "" {
			return fail("fake: plan without -out")
		}
		if !filepath.IsAbs(out) {
			out = filepath.Join(dir, out)
		}
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		call.Plan, call.Nonce = out, hex.EncodeToString(b)
		_, call.Destroy = flags["-destroy"]
		raw, _ := json.Marshal(lanePlanFile{Stack: st.ID, Nonce: call.Nonce, Serial: state.Serial, PassSHA: pass, Destroy: call.Destroy})
		if os.WriteFile(out, raw, 0o600) != nil {
			return fail("cannot write the plan file")
		}
		fmt.Print("Plan: 1 to add, 0 to change, 0 to destroy.\n" + echo)
		return done(0)
	case "show":
		if _, ok := flags["-json"]; ok && len(positional) == 0 {
			// The state as `show -json` prints it (T046: the retained instances' states the
			// leftover check exempts); it carries the state's secrets.
			call.Cmd = "show-state"
			if !initialised() {
				return fail(`Backend initialization required, please run "tofu init"`)
			}
			if !stateOK() {
				return fail("decrypting the state of %s: the passphrase does not match", st.ID)
			}
			doc := []byte(`{"format_version":"1.0"}`)
			if hasState && !state.Empty && st.StateDoc != "" {
				if doc, err = os.ReadFile(st.StateDoc); err != nil {
					return fail("fake: no state document")
				}
			}
			fmt.Println(strings.TrimSpace(string(doc)))
			return done(0)
		}
		if len(positional) != 1 {
			return fail("fake: show without a plan file")
		}
		pf, ok := readPlan(positional[0])
		call.Plan, call.Nonce, call.Destroy = positional[0], pf.Nonce, pf.Destroy
		if !ok || pf.Stack != st.ID {
			return fail("Failed to read the given file as a state or plan file")
		}
		if pf.PassSHA != pass {
			return fail("decrypting the plan file: the passphrase does not match")
		}
		if _, ok := flags["-json"]; ok {
			call.Cmd = "show-json"
			planDoc := st.Plan
			if pf.Destroy {
				planDoc = st.DestroyPlan
			}
			raw, err := os.ReadFile(planDoc)
			var doc map[string]any
			if err != nil || json.Unmarshal(raw, &doc) != nil {
				return fail("fake: no plan document")
			}
			// The plan document carries the root's variable values, the sensitive passphrase
			// among them (believed for OpenTofu 1.13's `show -json`): keeping the raw document is
			// keeping a secret.
			doc["variables"] = map[string]any{"state_passphrase": map[string]any{"value": os.Getenv("TF_VAR_state_passphrase")}}
			out, _ := json.Marshal(doc)
			fmt.Println(string(out))
			return done(0)
		}
		fmt.Printf("fake-rendered-plan %s\n  + client_secret = %q\n  + s3_secret = %q\n  + passphrase = %q\n", st.ID,
			os.Getenv("OVH_CLIENT_SECRET"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("TF_VAR_state_passphrase"))
		return done(0)
	case "apply":
		if len(positional) != 1 {
			return fail("fake: apply without a saved plan file")
		}
		pf, ok := readPlan(positional[0])
		call.Plan, call.Nonce, call.Destroy = positional[0], pf.Nonce, pf.Destroy
		if !initialised() {
			return fail(`Backend initialization required, please run "tofu init"`)
		}
		if !ok || pf.Stack != st.ID || pf.PassSHA != pass || !stateOK() {
			return fail("Failed to load the saved plan file")
		}
		if pf.Serial != state.Serial {
			return fail("Saved plan is stale")
		}
		if pf.Destroy {
			fmt.Print(`{"@level":"info","@message":"Destroy complete!","type":"change_summary"}` + "\n" + echo)
			raw, _ := json.Marshal(laneState{PassSHA: pass, Serial: state.Serial + 1, Empty: true})
			if os.WriteFile(statePath, raw, 0o600) != nil {
				return fail("cannot write the state")
			}
			return done(0)
		}
		for _, c := range st.Creates {
			fmt.Println(laneApplyComplete(c, flags))
		}
		fmt.Print(`{"@level":"info","@message":"Apply complete!","type":"change_summary"}` + "\n" + echo)
		// The outputs event as OpenTofu writes it: a sensitive output's value omitted (verified with
		// OpenTofu 1.10.3, evidence/T059.md iteration 7), every other value shown (T059 review r2).
		if raw, err := os.ReadFile(st.Outputs); err == nil {
			var outs map[string]map[string]any
			if json.Unmarshal(raw, &outs) == nil {
				for _, o := range outs {
					if s, _ := o["sensitive"].(bool); s {
						delete(o, "value")
					}
				}
				ev, _ := json.Marshal(map[string]any{"@level": "info", "@message": fmt.Sprintf("Outputs: %d", len(outs)), "type": "outputs", "outputs": outs})
				fmt.Println(string(ev))
			}
		}
		if st.ApplyExit != 0 {
			return fail("fake apply failure")
		}
		raw, _ := json.Marshal(laneState{PassSHA: pass, Serial: state.Serial + 1})
		if os.WriteFile(statePath, raw, 0o600) != nil {
			return fail("cannot write the state")
		}
		return done(0)
	case "output":
		if !initialised() {
			return fail(`Backend initialization required, please run "tofu init"`)
		}
		if !stateOK() {
			return fail("decrypting the state: the passphrase does not match")
		}
		if _, ok := flags["-json"]; !ok {
			return fail("fake: output without -json")
		}
		if !hasState || state.Empty {
			fmt.Println("{}")
			return done(0)
		}
		doc, err := os.ReadFile(st.Outputs)
		if err != nil {
			return fail("fake: no outputs")
		}
		fmt.Print(string(doc))
		return done(0)
	}
	return fail("fake: unsupported subcommand %s", sub)
}

// laneVarCheck checks a required root variable against the bound account (KD-1: STATE and
// DEMO_DEV are one project) and returns what is wrong, "" when it holds.
func laneVarCheck(name string, raw json.RawMessage, project string) string {
	urn := "urn:v1:eu:resource:publicCloudProject:" + project
	switch name {
	case "state_project_id", "project_id":
		var s string
		if json.Unmarshal(raw, &s) != nil || s != project {
			return "not the bound project id"
		}
	case "tenants":
		var m map[string]struct {
			ProjectID  string `json:"project_id"`
			ProjectURN string `json:"project_urn"`
		}
		if json.Unmarshal(raw, &m) != nil || len(m) != 1 || m["demo"].ProjectID != project || m["demo"].ProjectURN != urn {
			return "not every manifest tenant with its bound project"
		}
	case "project":
		var p struct {
			Tenant     string `json:"tenant"`
			ProjectID  string `json:"project_id"`
			ProjectURN string `json:"project_urn"`
		}
		if json.Unmarshal(raw, &p) != nil || p.Tenant != "demo" || p.ProjectID != project || p.ProjectURN != urn {
			return "not the published project of demo/dev"
		}
	}
	return ""
}

// ---------------------------------------------------------------- store and locks

// laneStore is the state buckets: objects by bucket/key, opened with one pair of S3 keys that a
// bucket must admit (laneBucketKeys). Every operation is logged with the access key.
type laneStore struct {
	mu      sync.Mutex
	log     string
	objects map[string][]byte
}

type laneBucket struct {
	s  *laneStore
	ak string
}

func (s *laneStore) open(keys map[string]string) (ObjectStore, error) {
	ak := keys["AWS_ACCESS_KEY_ID"]
	for _, admitted := range laneBucketKeys {
		if sk, ok := admitted[ak]; ok && sk == keys["AWS_SECRET_ACCESS_KEY"] {
			return &laneBucket{s: s, ak: ak}, nil
		}
	}
	return nil, errors.New("fake store: InvalidAccessKeyId")
}

func (b *laneBucket) Put(bucket, key string, data []byte) error {
	b.s.mu.Lock()
	defer b.s.mu.Unlock()
	laneAppend(b.s.log, laneCall{Cmd: "put", Stack: bucket + "/" + key, Key: b.ak})
	if _, ok := laneBucketKeys[bucket][b.ak]; !ok {
		return errors.New("fake store: AccessDenied")
	}
	b.s.objects[bucket+"/"+key] = bytes.Clone(data)
	return nil
}

func (b *laneBucket) Get(bucket, key string) ([]byte, error) {
	b.s.mu.Lock()
	defer b.s.mu.Unlock()
	laneAppend(b.s.log, laneCall{Cmd: "get", Stack: bucket + "/" + key, Key: b.ak})
	if _, ok := laneBucketKeys[bucket][b.ak]; !ok {
		return nil, errors.New("fake store: AccessDenied")
	}
	data, ok := b.s.objects[bucket+"/"+key]
	if !ok {
		return nil, fmt.Errorf("fake store: NoSuchKey %s: %w", key, fs.ErrNotExist)
	}
	return bytes.Clone(data), nil
}

func (s *laneStore) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.objects {
		out[k] = string(v)
	}
	return out
}

// laneLocks records every lock event of the real DirLocks store in the shared log.
type laneLocks struct {
	inner stacks.LockStore
	log   string
	after func(name string) // called after a lock is taken (T059 review r1: another run finished meanwhile)
}

func (l laneLocks) TryLock(name string) (func() error, error) {
	release, err := l.inner.TryLock(name)
	if err != nil {
		laneAppend(l.log, laneCall{Cmd: "lock-refused", Stack: name})
		return nil, err
	}
	laneAppend(l.log, laneCall{Cmd: "lock", Stack: name})
	if l.after != nil {
		l.after(name)
	}
	var once sync.Once
	return func() error {
		var rerr error
		once.Do(func() {
			rerr = release()
			laneAppend(l.log, laneCall{Cmd: "release", Stack: name})
		})
		return rerr
	}, nil
}

// ---------------------------------------------------------------- harness

type laneHarness struct {
	t        *testing.T
	base     string
	checkout string
	root     string // config root
	acctDir  string
	bin      string
	tmp      string
	world    laneWorld
	m        *stacks.Manifest
	store    *laneStore
	api      *fakeAPI
	// afterLock runs after each run lock is taken (nil: nothing).
	afterLock func(name string)
	// destroys: the runs under test may destroy ephemeral stacks and read retained states (T046).
	destroys bool
	// chainRun: the run under judgement is a chain, which reads account-bootstrap's state for the
	// leftover check; plan, apply and destroy never do (T046 review r2).
	chainRun bool
	term     bytes.Buffer
	runs     int
}

// newLaneHarness: a checkout copy (generated stacks and their stage, component and module
// closure), the config root as bootstrap:account leaves it (sandbox.env; account.env, state.env,
// state-passphrase.env of the bound account), the fake tofu on its own bin, the fake store and
// the real lock directory accounts/<a>/locks. Ambient OVH_*/AWS_* hold seeded secrets.
func newLaneHarness(t *testing.T) *laneHarness {
	t.Helper()
	base := t.TempDir()
	h := &laneHarness{t: t, base: base, checkout: filepath.Join(base, "checkout"), root: filepath.Join(base, "home", ".config", "ovh-lz"),
		bin: filepath.Join(base, "bin"), tmp: filepath.Join(base, "tmp")}
	for _, d := range []string{h.bin, h.tmp, filepath.Join(base, "fake", "states")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(h.root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", h.tmp)
	// A fake HOME (review r2): a lane that ignores ConfigRoot finds no real ~/.config/ovh-lz/.
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "home", ".config"))
	t.Setenv("OVH_CLIENT_ID", "EU.ambient")
	t.Setenv("OVH_CLIENT_SECRET", laneAmbientOVH)
	t.Setenv("AWS_ACCESS_KEY_ID", "AK-ambient")
	t.Setenv("AWS_SECRET_ACCESS_KEY", laneAmbientAWS)
	for k, v := range laneAmbient {
		t.Setenv(k, v)
	}
	for _, rel := range []string{"stacks", "stages", "components", "modules"} {
		laneCopyTree(t, filepath.Join(laneRepoRoot, rel), filepath.Join(h.checkout, rel))
	}
	raw, err := os.ReadFile(filepath.Join(h.checkout, stacks.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	if h.m, err = stacks.DecodeManifest(raw); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(h.bin, "tofu")); err != nil {
		t.Fatal(err)
	}
	h.acctDir = filepath.Join(h.root, "accounts", laneAccount)
	for rel, v := range map[string]map[string]string{
		"sandbox.env": {"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": laneAdminID, "OVH_CLIENT_SECRET": laneAdminSecret},
		"accounts/" + laneAccount + "/account.env": {"LZ_ACCOUNT_ID": laneAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz",
			"LZ_PROJECT_ID_STATE": laneProject, "LZ_PROJECT_ID_DEMO_DEV": laneProject},
		"accounts/" + laneAccount + "/state.env":            {"AWS_ACCESS_KEY_ID": laneAccountAK, "AWS_SECRET_ACCESS_KEY": laneAccountSK},
		"accounts/" + laneAccount + "/state-passphrase.env": {"TF_VAR_state_passphrase": lanePassphrase},
	} {
		if err := WriteCredentialFile(h.root, filepath.FromSlash(rel), v); err != nil {
			t.Fatal(err)
		}
	}
	h.world = laneWorld{Checkout: h.checkout, Log: filepath.Join(base, "fake", "calls.jsonl"), States: filepath.Join(base, "fake", "states"),
		LockDir: filepath.Join(h.acctDir, "locks"), Passphrase: lanePassphrase, Project: laneProject, State: laneProject, Stacks: map[string]laneStack{}}
	h.store = &laneStore{log: h.world.Log, objects: map[string][]byte{}}
	// The OVHcloud API (T059, coordinator decision 3a): every lane credential belongs to the bound
	// account; GET /auth/details answers which (binding.go, premise P26).
	h.api = newFakeAPI(t)
	for a, name := range laneAPINames {
		h.api.add(apiCredential{Name: name, Class: string(a), Account: laneAccount, ClientID: laneCreds[a]["OVH_CLIENT_ID"], ClientSecret: laneCreds[a]["OVH_CLIENT_SECRET"]})
	}
	h.api.add(apiCredential{Name: "lane-foreign", Class: "admin", Account: laneForeignAccount, ClientID: laneForeignID, ClientSecret: laneForeignSecret})
	for _, in := range h.m.Instances {
		ls := laneStack{ID: in.ID, Stage: in.Stage, Bucket: in.StateBucket, BucketKeys: laneBucketKeys[in.StateBucket]}
		locks, err := stacks.RunLocks(h.m, []string{in.ID})
		if err != nil {
			t.Fatal(err)
		}
		ls.Locks = locks
		ls.Plan = h.planDoc(in, "create")
		ls.Outputs = h.outputsDoc(in, laneProject)
		h.world.Stacks[in.Path] = ls
	}
	h.save()
	return h
}

// laneCopyTree copies the regular files of a tree (no .terraform, no links).
func laneCopyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".terraform" {
			return fs.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(from, p)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (h *laneHarness) save() {
	h.t.Helper()
	raw, err := json.MarshalIndent(h.world, "", " ")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.bin, laneWorldFile), raw, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *laneHarness) row(id string) stacks.Instance {
	h.t.Helper()
	in, err := h.m.Row(id)
	if err != nil {
		h.t.Fatal(err)
	}
	return in
}

// planDoc derives a stack plan from T063's captured plan fixture: every address (and previous and
// module address) moved under the generated root's one module call, module.<stage, - → _>.
// The pseudo-fixture "errored" is the `create` capture marked errored (review r2: the guard runs on
// every stack's plan, not only a retained one's).
func (h *laneHarness) planDoc(in stacks.Instance, fixture string) string {
	h.t.Helper()
	source := fixture
	if fixture == "errored" {
		source = "create"
	}
	raw, err := os.ReadFile(filepath.Join(laneRepoRoot, "tests", "fixtures", "tofu-probes", "protect", source+".json"))
	if err != nil {
		h.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		h.t.Fatal(err)
	}
	if fixture == "errored" {
		doc["errored"] = true
	}
	prefix := "module." + strings.ReplaceAll(in.Stage, "-", "_")
	rcs, _ := doc["resource_changes"].([]any)
	if len(rcs) == 0 {
		h.t.Fatalf("fixture %s has no resource changes", fixture)
	}
	for _, rc := range rcs {
		m := rc.(map[string]any)
		for _, k := range []string{"address", "previous_address"} {
			if a, ok := m[k].(string); ok {
				m[k] = prefix + "." + a
			}
		}
		if a, ok := m["module_address"].(string); ok {
			m["module_address"] = prefix + "." + a
		} else {
			m["module_address"] = prefix
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		h.t.Fatal(err)
	}
	p := filepath.Join(h.base, "fake", "plan-"+in.ID+"-"+fixture+".json")
	if err := os.WriteFile(p, out, 0o600); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// setPlan makes id's next plans the derived fixture.
func (h *laneHarness) setPlan(id, fixture string) {
	in := h.row(id)
	st := h.world.Stacks[in.Path]
	st.Plan = h.planDoc(in, fixture)
	h.world.Stacks[in.Path] = st
	h.save()
}

func (h *laneHarness) setApplyExit(id string, code int) {
	in := h.row(id)
	st := h.world.Stacks[in.Path]
	st.ApplyExit = code
	h.world.Stacks[in.Path] = st
	h.save()
}

// outputsDoc is the stage's `tofu output -json`: the envelope fixture's values (project id and
// runtime instance bound to this manifest) as plain outputs, and the stage's sensitive outputs
// (stages/*/outputs.tf) holding the seeded secrets.
func (h *laneHarness) outputsDoc(in stacks.Instance, project string) string {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Join(laneRepoRoot, "tests", "fixtures", "outputs", "envelopes", in.Stage+".json"))
	if err != nil {
		h.t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), laneFixtureProject, project)
	text = strings.ReplaceAll(text, "0123456789abcdef0123456789abcdef", laneProject)
	text = strings.ReplaceAll(text, "demo-dev-gra11-runtime-blue", "demo-dev-gra11-runtime")
	text = strings.ReplaceAll(text, "lz-demo-dev-gra11-bkt-runtime-blue", "lz-demo-dev-gra11-bkt-runtime")
	var env struct {
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		h.t.Fatal(err)
	}
	delete(env.Values, "slot")
	outs := map[string]any{}
	for k, v := range env.Values {
		outs[k] = map[string]any{"sensitive": false, "type": "dynamic", "value": v}
	}
	sens := func(name string, v any) {
		outs[name] = map[string]any{"sensitive": true, "type": "dynamic", "value": v}
	}
	switch in.Stage {
	case "account-governance":
		sens("platform_deployer_secret", lanePlatformSecret)
		sens("tenant_deployer_secrets", map[string]string{"demo": laneTenantSecret})
	case "tenant-state":
		sens("tenant_s3", map[string]string{"access_key_id": laneTenantAK, "secret_access_key": laneTenantSK})
		sens("platform_s3", map[string]string{"access_key_id": lanePlatformAK, "secret_access_key": lanePlatformSK})
	}
	out, err := json.Marshal(outs)
	if err != nil {
		h.t.Fatal(err)
	}
	p := filepath.Join(h.base, "fake", "outputs-"+in.ID+".json")
	if err := os.WriteFile(p, out, 0o600); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *laneHarness) recordsDir() string {
	return filepath.Join(h.checkout, ".local", "live", "records")
}

// steady seeds the state a complete earlier run leaves (independent of the lane under test):
// every stack applied (fake state serial 1), every artefact published through Publish, the
// deployer and tenant S3 files written through files.go, and a record per row with the current
// code digest, the consumed artefact digests and the resolved-reference digest as stacks.Adapt
// computes them.
func (h *laneHarness) steady() {
	t := h.t
	t.Helper()
	for _, in := range h.m.Instances {
		raw, _ := json.Marshal(laneState{PassSHA: passSHA(lanePassphrase), Serial: 1})
		if err := os.WriteFile(filepath.Join(h.world.States, in.ID+".json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for rel, f := range laneDeployerFiles {
		if err := WriteCredentialFile(h.root, filepath.Join("accounts", laneAccount, filepath.FromSlash(rel)), f.values); err != nil {
			t.Fatal(err)
		}
	}
	acct := h.boundAccount()
	order, err := stacks.Order(h.m)
	if err != nil {
		t.Fatal(err)
	}
	digests := map[string]string{}
	for _, id := range order {
		in := h.row(id)
		outs, err := os.ReadFile(h.world.Stacks[in.Path].Outputs)
		if err != nil {
			t.Fatal(err)
		}
		store, err := h.store.open(laneCreds[laneAuthority[id]])
		if err != nil {
			t.Fatal(err)
		}
		if digests[id], err = Publish(PublishOptions{Manifest: h.m, Instance: id, Revision: laneRevision, TofuOutput: outs, Schemas: laneSchemas(), Account: acct, Store: store}); err != nil {
			t.Fatalf("seed publish %s: %v", id, err)
		}
	}
	if err := os.MkdirAll(h.recordsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range order {
		in := h.row(id)
		code, err := stacks.CodeDigest(h.checkout, in)
		if err != nil {
			t.Fatal(err)
		}
		rec := Record{AppliedAt: "2026-10-08T00:00:00Z", SourceRevision: laneRevision, CodeDigest: code, Consumed: map[string]string{}}
		for _, e := range in.Edges {
			if e.Kind == stacks.EdgeData {
				rec.Consumed[e.Producer] = digests[e.Producer]
			}
		}
		rec.Resolved = h.resolvedDigest(id)
		if err := WriteRecord(h.recordsDir(), id, rec); err != nil {
			t.Fatal(err)
		}
	}
}

// resolvedDigest is what stacks.Adapt reports for id's resolved-reference input ("" when its stage
// takes none), adapted into a scratch directory with the tenant's keys.
func (h *laneHarness) resolvedDigest(id string) string {
	h.t.Helper()
	store, err := h.store.open(laneCreds[AuthorityTenant])
	if err != nil {
		h.t.Fatal(err)
	}
	in, err := stacks.Adapt(stacks.AdaptOptions{Manifest: h.m, Consumer: id, Store: store, Schemas: laneSchemas(), Account: h.boundAccount(),
		Dir: filepath.Join(h.base, "adapt", id)})
	if err != nil {
		h.t.Fatalf("seed adapt %s: %v", id, err)
	}
	return in.Resolved
}

func laneSchemas() fs.FS { return os.DirFS(filepath.Join(laneRepoRoot, "schemas", "outputs")) }

func (h *laneHarness) boundAccount() stacks.BoundAccount {
	return stacks.BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"STATE": laneProject, "DEMO_DEV": laneProject}}
}

// rebind points LZ_PROJECT_ID_DEMO_DEV of account.env at project (STATE keeps laneProject): the
// resolved-reference input of account-governance and demo-dev-project changes, nothing else of
// the checkout; the fake roots then expect and produce that project.
func (h *laneHarness) rebind(project string) {
	h.t.Helper()
	rel := filepath.Join("accounts", laneAccount, "account.env")
	if err := WriteCredentialFile(h.root, rel, map[string]string{"LZ_ACCOUNT_ID": laneAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz",
		"LZ_PROJECT_ID_STATE": laneProject, "LZ_PROJECT_ID_DEMO_DEV": project}); err != nil {
		h.t.Fatal(err)
	}
	h.world.Project = project
	for p, st := range h.world.Stacks {
		if in := h.row(st.ID); in.Tenant != "" && in.Stage != "tenant-state" {
			st.Outputs = h.outputsDoc(in, project)
			h.world.Stacks[p] = st
		}
	}
	h.save()
}

// touch changes id's generated code (a comment in its root), so its code digest changes.
func (h *laneHarness) touch(id string) {
	h.t.Helper()
	p := filepath.Join(h.checkout, filepath.FromSlash(h.row(id).Path), "_lz_main.tf")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "# changed by %s\n", h.t.Name())
}

func (h *laneHarness) record(id string) []byte {
	raw, _ := os.ReadFile(filepath.Join(h.recordsDir(), id+".json"))
	return raw
}

func (h *laneHarness) configFiles() map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(h.root, func(p string, d fs.DirEntry, err error) error {
		if d != nil && d.IsDir() && d.Name() == "locks" {
			return fs.SkipDir // run locks: created by every run and by the harness's lock check
		}
		if err == nil && d.Type().IsRegular() {
			raw, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(h.root, p)
			out[filepath.ToSlash(rel)] = string(raw)
		}
		return nil
	})
	return out
}

func (h *laneHarness) logCalls() []laneCall {
	h.t.Helper()
	raw, err := os.ReadFile(h.world.Log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	var out []laneCall
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l == "" {
			continue
		}
		var c laneCall
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			h.t.Fatalf("log line %q: %v", l, err)
		}
		out = append(out, c)
	}
	return out
}

// laneRun is one run of the lane: its error and every event it caused, in order.
type laneRun struct {
	err     error
	calls   []laneCall
	runDir  string
	process string // what the process itself wrote on stdout and stderr during the run
}

func (r laneRun) tofu() []laneCall {
	var out []laneCall
	for _, c := range r.calls {
		if !slices.Contains(laneNotTofu, c.Cmd) {
			out = append(out, c)
		}
	}
	return out
}

// stacksOf lists the stacks of the run's calls of cmd, in order, each once.
func (r laneRun) stacksOf(cmd string) []string {
	var out []string
	for _, c := range r.tofu() {
		if c.Cmd == cmd && !slices.Contains(out, c.Stack) {
			out = append(out, c.Stack)
		}
	}
	return out
}

func (r laneRun) events(cmd string) []string {
	var out []string
	for _, c := range r.calls {
		if c.Cmd == cmd {
			out = append(out, c.Stack)
		}
	}
	return out
}

func (r laneRun) puts() map[string]string {
	out := map[string]string{}
	for _, c := range r.calls {
		if c.Cmd == "put" {
			out[c.Stack] = c.Key
		}
	}
	return out
}

func laneTree(t *testing.T, dir string, skip string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && p == skip {
			return fs.SkipDir
		}
		rel, _ := filepath.Rel(dir, p)
		if d.Type().IsRegular() {
			raw, _ := os.ReadFile(p)
			out[rel] = string(raw)
		} else if !d.IsDir() {
			out[rel] = "non-regular"
		} else {
			out[rel+"/"] = ""
		}
		return nil
	})
	return out
}

// exec runs the lane once and returns the run and the checkout as it was before the run.
func (h *laneHarness) exec(verb, target string) (laneRun, map[string]string) {
	t := h.t
	t.Helper()
	h.runs++
	runID := fmt.Sprintf("20261008T1200%02dZ-%04x", h.runs, h.runs)
	runDir := filepath.Join(h.checkout, ".local", "live", runID)
	from := len(h.logCalls())
	before := laneTree(t, h.checkout, filepath.Join(h.checkout, ".local"))
	h.term.Reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// A blocked call (laneStack.Block) cancels the run once it announced itself (T059 review r1).
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				if _, err := os.Stat(h.world.Log + ".blocked"); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	// The process's own stdout and stderr are captured too (review r1): a child stream or a print
	// that bypasses Terminal lands there.
	streams := laneCaptureStd(t)
	err := Apply(ctx, ApplyOptions{Verb: verb, Target: target, Checkout: h.checkout, Manifest: h.m, ConfigRoot: h.root, Account: laneAccount,
		RunID: runID, RunDir: runDir, Records: h.recordsDir(), Revision: laneRevision, Tofu: filepath.Join(h.bin, "tofu"), Schemas: laneSchemas(),
		Locks: laneLocks{inner: stacks.DirLocks(h.world.LockDir), log: h.world.Log, after: h.afterLock}, Store: h.store.open, Terminal: &h.term, API: h.laneAPI()})
	return laneRun{err: err, calls: h.logCalls()[from:], runDir: runDir, process: streams()}, before
}

// laneCaptureStd points os.Stdout and os.Stderr at one pipe until the returned function restores
// them and returns what was written.
func laneCaptureStd(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = buf.ReadFrom(r)
		close(done)
	}()
	return func() string {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = w.Close()
		<-done
		_ = r.Close()
		return buf.String()
	}
}

// run runs the lane and judges what every run must hold, whatever the row (the harness's own
// checks, so setup and refused runs are judged too).
func (h *laneHarness) run(verb, target string) laneRun {
	h.t.Helper()
	r, before := h.exec(verb, target)
	h.judge(r, before)
	return r
}

func (h *laneHarness) judge(r laneRun, checkoutBefore map[string]string) {
	t := h.t
	t.Helper()
	// The checkout: nothing changed outside .local/ (no .terraform, plan file or lock file in a root).
	after := laneTree(t, h.checkout, filepath.Join(h.checkout, ".local"))
	for p, v := range after {
		if b, ok := checkoutBefore[p]; !ok || b != v {
			t.Errorf("the run wrote %s in the checkout", p)
		}
	}
	for p := range checkoutBefore {
		if _, ok := after[p]; !ok {
			t.Errorf("the run removed %s from the checkout", p)
		}
	}
	// G2: no seeded secret on the terminal, in the error, in any child's argv, in any file of the
	// checkout (.local/live: rendered plans, inputs, records), TMPDIR or the store; below the
	// config root only in its own credential file. No saved plan file is left behind.
	streams := map[string]string{"terminal": h.term.String(), "process stdout/stderr": r.process}
	if r.err != nil {
		streams["error"] = r.err.Error()
	}
	for name, text := range streams {
		for s := range laneSecrets {
			if strings.Contains(text, s) {
				t.Errorf("a seeded secret (%.16s…) reached the %s", s, name)
			}
		}
	}
	for _, c := range r.tofu() {
		for s := range laneSecrets {
			if strings.Contains(strings.Join(c.Args, " "), s) {
				t.Errorf("a seeded secret (%.16s…) in the argv of tofu %s (%s)", s, c.Cmd, c.Stack)
			}
		}
	}
	scan := func(dir string, allowed func(rel string, secret string) bool) {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			raw, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			for s := range laneSecrets {
				if strings.Contains(string(raw), s) && !allowed(filepath.ToSlash(rel), s) {
					t.Errorf("a seeded secret (%.16s…) was written to %s", s, p)
				}
			}
			if strings.HasSuffix(p, ".tfplan") {
				t.Errorf("saved plan %s left behind", p)
			}
			return nil
		})
	}
	never := func(string, string) bool { return false }
	scan(h.checkout, never)
	scan(h.tmp, never)
	scan(h.root, func(rel, s string) bool { return laneSecrets[s] == rel })
	for k, v := range h.store.snapshot() {
		for s := range laneSecrets {
			if strings.Contains(v, s) {
				t.Errorf("a seeded secret (%.16s…) in the object %s", s, k)
			}
		}
	}
	// FR-010, G1, G12: every child holds exactly its stack's authority's credential and the state
	// passphrase; no other authority's secret, no ambient OVH_*/AWS_* value.
	for _, c := range r.tofu() {
		a, ok := laneAuthority[c.Stack]
		if !ok {
			t.Errorf("tofu %s on %s: not a stack of the manifest", c.Cmd, c.Stack)
			continue
		}
		env := map[string]string{}
		for _, kv := range c.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		for k, want := range laneCreds[a] {
			if env[k] != want {
				t.Errorf("tofu %s on %s (%s authority): %s = %q, want %q", c.Cmd, c.Stack, a, k, laneRedacted(k, env[k]), laneRedacted(k, want))
			}
		}
		if env["TF_VAR_state_passphrase"] != lanePassphrase {
			t.Errorf("tofu %s on %s: TF_VAR_state_passphrase is not the account's passphrase", c.Cmd, c.Stack)
		}
		own := slices.Collect(maps.Values(laneCreds[a]))
		for _, kv := range c.Env {
			k, v, _ := strings.Cut(kv, "=")
			// Exactly the authority's five OVH_*/AWS_* variables; no ambient one (review r1).
			if (strings.HasPrefix(k, "OVH_") || strings.HasPrefix(k, "AWS_")) && laneCreds[a][k] == "" {
				t.Errorf("tofu %s on %s carries %s, not a variable of its authority", c.Cmd, c.Stack, k)
			}
			if amb, ok := laneAmbient[k]; ok && v == amb {
				t.Errorf("tofu %s on %s inherited the caller's %s", c.Cmd, c.Stack, k)
			}
			for s := range laneSecrets {
				if strings.Contains(v, s) && !slices.Contains(own, s) && s != lanePassphrase {
					t.Errorf("tofu %s on %s (%s authority) holds another credential's secret (%.16s…) in %s", c.Cmd, c.Stack, a, s, strings.SplitN(kv, "=", 2)[0])
				}
			}
		}
	}
	// FR-010 (T059, decision 3a): every credential is bound to the account before use: each tofu
	// call of a stack follows a successful GET /auth/details with its authority's credential in
	// this run.
	boundAs := map[string]bool{}
	for _, c := range r.calls {
		if c.Cmd == "bind" && c.Exit == http.StatusOK {
			boundAs[c.Stack] = true
		}
		if a, ok := laneAuthority[c.Stack]; ok && !slices.Contains(laneNotTofu, c.Cmd) && !boundAs[laneAPINames[a]] {
			t.Errorf("tofu %s on %s before its %s credential was bound to the account (GET /auth/details)", c.Cmd, c.Stack, a)
		}
	}
	// account-bootstrap is bootstrap:account's: the lane never runs tofu in its root, except that
	// a run that destroys (T046) reads its state for the leftover check: init and `show -json`.
	for _, c := range r.tofu() {
		if c.Stack == "account-bootstrap" && !(h.chainRun && (c.Cmd == "init" || c.Cmd == "show-state")) {
			t.Errorf("tofu %s in the account-bootstrap root: bootstrap:account owns it", c.Cmd)
		}
	}
	// G7: every apply applies the saved plan that the last `show -json` of the same stack in this
	// run read, which the last plan of that stack wrote; nothing is destroyed; no lock bypass.
	lastPlan, lastShown := map[string]laneCall{}, map[string]laneCall{}
	for _, c := range r.tofu() {
		for _, a := range c.Args {
			// A destroy is a saved `plan -destroy` of an ephemeral stack, judged and applied as
			// every plan (T046); never of a retained one, never `destroy -auto-approve`.
			if a == "-destroy" && h.destroys && c.Cmd == "plan" && laneEphemeral[c.Stack] {
				continue
			}
			if strings.HasPrefix(a, "-lock=") || a == "-destroy" || a == "-auto-approve" || strings.HasPrefix(a, "-target") || strings.HasPrefix(a, "-replace") {
				t.Errorf("tofu %s on %s with %s", c.Cmd, c.Stack, a)
			}
		}
		switch c.Cmd {
		case "plan":
			lastPlan[c.Stack] = c
			delete(lastShown, c.Stack)
		case "show-json":
			if p, ok := lastPlan[c.Stack]; ok && p.Nonce == c.Nonce && c.Exit == 0 {
				lastShown[c.Stack] = c
			}
		case "apply":
			s, ok := lastShown[c.Stack]
			if !ok || s.Nonce != c.Nonce || s.Plan != c.Plan {
				t.Errorf("apply of %s applies plan %s (nonce %s) that no `show -json` of its last plan in this run read", c.Stack, c.Plan, c.Nonce)
			}
		case "destroy", "import", "state", "force-unlock", "other":
			t.Errorf("tofu %s on %s", c.Cmd, c.Stack)
		}
	}
	// G2: every saved plan the run wrote is gone when it ends, whatever its name (review r1).
	for _, c := range r.tofu() {
		if c.Cmd != "plan" || c.Plan == "" {
			continue
		}
		if _, err := os.Lstat(c.Plan); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("saved plan %s of %s left behind (%v)", c.Plan, c.Stack, err)
		}
	}
	// Run locks (stacks.HoldRun, G14 caller part): taken before the first tofu call, held during
	// every call (each stack's own locks found held), released only after the last call, and all
	// released when the run returns, on every path.
	firstTofu, lastTofu, lastLock, firstRelease, lastPut := -1, -1, -1, -1, -1
	for i, c := range r.calls {
		switch c.Cmd {
		case "lock":
			lastLock = i
		case "release":
			if firstRelease < 0 {
				firstRelease = i
			}
		case "put":
			lastPut = i
		case "get", "lock-refused", "bind", "list":
		default:
			if firstTofu < 0 {
				firstTofu = i
			}
			lastTofu = i
		}
	}
	if firstTofu >= 0 {
		if lastLock < 0 || lastLock > firstTofu {
			t.Errorf("tofu ran before the run locks were taken (events %v)", laneEvents(r.calls))
		}
		if firstRelease >= 0 && firstRelease < lastTofu {
			t.Errorf("a run lock was released before the last tofu call (events %v)", laneEvents(r.calls))
		}
		// ... and after the last publication (review r2).
		if firstRelease >= 0 && firstRelease < lastPut {
			t.Errorf("a run lock was released before the last artefact was published (events %v)", laneEvents(r.calls))
		}
	}
	for _, c := range r.tofu() {
		if len(c.Unlocked) > 0 {
			t.Errorf("tofu %s on %s ran while its run locks %v were free", c.Cmd, c.Stack, c.Unlocked)
		}
	}
	taken, released := r.events("lock"), r.events("release")
	slices.Sort(taken)
	slices.Sort(released)
	if !slices.Equal(taken, released) {
		t.Errorf("locks taken %v, released %v: every lock is released when the run ends", taken, released)
	}
	h.locksFree()
}

// locksFree fails when a run lock is still held (by this process: a lost release function).
func (h *laneHarness) locksFree() {
	h.t.Helper()
	for _, n := range []string{"account", "tenant-demo"} {
		release, err := stacks.DirLocks(h.world.LockDir).TryLock(n)
		if err != nil {
			h.t.Errorf("lock %s still held after the run: %v", n, err)
			continue
		}
		_ = release()
	}
}

func laneEvents(calls []laneCall) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c.Cmd+":"+c.Stack)
	}
	return out
}

// laneRedacted is what a failure message may print of a value: a seeded secret by its prefix,
// any other value of a secret variable never (review r2: it could be a real one).
func laneRedacted(k, v string) string {
	for s := range laneSecrets {
		if v == s {
			return fmt.Sprintf("<secret %.16s…>", s)
		}
	}
	if strings.Contains(k, "SECRET") || strings.Contains(k, "PASSPHRASE") || strings.Contains(k, "passphrase") {
		return "<unseeded value withheld>"
	}
	return v
}

func laneExit(t *testing.T, r laneRun, want int) {
	t.Helper()
	if got := ExitCode(r.err); got != want {
		t.Errorf("exit %d (%v), want %d", got, r.err, want)
	}
}

func laneCondition(t *testing.T, r laneRun, cond string) {
	t.Helper()
	var ref *Refusal
	if !errors.As(r.err, &ref) || ref.Condition != cond {
		t.Errorf("err %v, want a %s refusal (exit 3)", r.err, cond)
	}
}

// ---------------------------------------------------------------- tests

// A first run after bootstrap:account (no record, no artefact, no deployer file): `apply -- all`
// acts on every stack but account-bootstrap, in run order, each under its own authority (the
// harness checks every child's environment); writes the platform and tenant deployer files after
// account-governance and the tenant's S3 files after tenant-state through files.go (0600); holds
// the account and tenant locks, in that order, for the whole run; publishes every envelope with
// the stack's own keys; writes a record per stack that selection reads back; then a second run
// selects nothing and runs nothing.
func TestApplyFirstRunAllInOrder(t *testing.T) {
	h := newLaneHarness(t)
	r := h.run(VerbApply, TargetAll)
	laneExit(t, r, 0)
	if got := r.stacksOf("apply"); !slices.Equal(got, laneOrder) {
		t.Fatalf("applied %v, want %v (the selected set in run order, account-bootstrap left to bootstrap:account)", got, laneOrder)
	}
	// Strictly one stack after the other: a stack's first call follows the previous stack's apply.
	seen, applied := map[string]bool{}, map[string]bool{}
	prev := ""
	for _, c := range r.tofu() {
		if !seen[c.Stack] {
			if prev != "" && !applied[prev] {
				t.Errorf("%s started before %s was applied", c.Stack, prev)
			}
			seen[c.Stack] = true
			prev = c.Stack
		}
		if c.Cmd == "apply" && c.Exit == 0 {
			applied[c.Stack] = true
		}
	}
	if got := r.events("lock"); !slices.Equal(got, []string{"account", "tenant-demo"}) {
		t.Errorf("locks taken %v, want [account tenant-demo] (stacks.HoldRun over the acted stacks)", got)
	}
	for rel, f := range laneDeployerFiles {
		p := filepath.Join(h.acctDir, filepath.FromSlash(rel))
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("%s not written after %s: %v", rel, f.after, err)
			continue
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %04o, want 0600", rel, fi.Mode().Perm())
		}
		got, err := ReadCredentialFile(p)
		if err != nil || !maps.Equal(got, f.values) {
			t.Errorf("%s holds %v, want the %s outputs as %v", rel, slices.Sorted(maps.Keys(got)), f.after, slices.Sorted(maps.Keys(f.values)))
		}
	}
	objects := h.store.snapshot()
	puts := r.puts()
	for _, id := range laneOrder {
		in := h.row(id)
		key := in.StateBucket + "/" + stacks.ArtifactKey(id)
		if _, ok := objects[key]; !ok {
			t.Errorf("no artefact %s published for %s", key, id)
		}
		if got, want := puts[key], laneCreds[laneAuthority[id]]["AWS_ACCESS_KEY_ID"]; got != want {
			t.Errorf("artefact of %s published with access key %q, want its %s authority's %q", id, got, laneAuthority[id], want)
		}
		if !strings.Contains(string(objects[key]), laneRevision) {
			t.Errorf("artefact of %s does not carry the source revision", id)
		}
		var rec Record
		if err := json.Unmarshal(h.record(id), &rec); err != nil {
			t.Errorf("record of %s: %v", id, err)
			continue
		}
		code, err := stacks.CodeDigest(h.checkout, in)
		if err != nil {
			t.Fatal(err)
		}
		if rec.CodeDigest != code || rec.SourceRevision != laneRevision {
			t.Errorf("record of %s: code digest %q, revision %q; want %q, %q", id, rec.CodeDigest, rec.SourceRevision, code, laneRevision)
		}
		if slices.Contains([]string{"demo-dev-gra11-network", "demo-dev-gra11-runtime"}, id) {
			pkey := laneTenantBkt + "/" + stacks.ArtifactKey("demo-dev-project")
			if want := sha256Hex([]byte(objects[pkey])); rec.Consumed["demo-dev-project"] != want {
				t.Errorf("record of %s consumed %q, want the sha256 of the published project artefact %q", id, rec.Consumed["demo-dev-project"], want)
			}
		}
		laneRenderedPlan(t, r, id)
	}
	// Consumed inputs come from stacks.Adapt: exactly its files, in the run's inputs/<id>/
	// directory (data-model *Envelope-to-input adapter* step 3, *Resolved-reference input*).
	wantInputs := map[string][]string{
		"account-governance": {stacks.ResolvedFile}, "demo-state": {stacks.ResolvedFile}, "demo-dev-project": {stacks.ResolvedFile},
		"demo-dev-gra11-network": {"project.tfvars.json"}, "demo-dev-gra11-runtime": {"project.tfvars.json"},
	}
	counts := map[string]int{}
	for _, c := range r.tofu() {
		counts[c.Stack+" "+c.Cmd]++
		if c.Cmd != "plan" {
			continue
		}
		var want []string
		for _, f := range wantInputs[c.Stack] {
			want = append(want, filepath.Join(r.runDir, "inputs", c.Stack, f))
		}
		if got := slices.Sorted(slices.Values(c.VarFiles)); !slices.Equal(got, want) {
			t.Errorf("plan of %s reads var files %v, want exactly %v", c.Stack, got, want)
		}
	}
	for _, id := range laneOrder {
		for _, cmd := range []string{"plan", "show-json", "apply", "output"} {
			if n := counts[id+" "+cmd]; n != 1 {
				t.Errorf("%s: %d `%s` calls, want exactly one", id, n, cmd)
			}
		}
	}
	if t.Failed() {
		return
	}
	again := h.run(VerbApply, TargetAll)
	laneExit(t, again, 0)
	if n := len(again.tofu()); n != 0 || len(again.puts()) != 0 {
		t.Errorf("second run with nothing changed: %d tofu calls %v, puts %v; want nothing selected", n, again.stacksOf("plan"), again.puts())
	}
}

// Selection integration (FR-009, R21) from a complete earlier run: a code change selects that
// stack and its transitive data consumers only; authority edges order but never select; the run
// holds only the locks of what it acts on; `-- <instance>` acts on that instance and refuses when
// a producer it consumes is selected and not applied.
func TestApplySelection(t *testing.T) {
	cases := []struct {
		name, touch, target string
		applied             []string
		locks               []string
	}{
		{"project-and-consumers", "demo-dev-project", TargetAll, []string{"demo-dev-project", "demo-dev-gra11-network", "demo-dev-gra11-runtime"}, []string{"tenant-demo"}},
		{"governance-alone", "account-governance", TargetAll, []string{"account-governance"}, []string{"account"}},
		{"tenant-state-alone", "demo-state", TargetAll, []string{"demo-state"}, []string{"account", "tenant-demo"}},
		{"runtime-alone", "demo-dev-gra11-runtime", TargetAll, []string{"demo-dev-gra11-runtime"}, []string{"tenant-demo"}},
		{"runtime-target", "demo-dev-gra11-runtime", "demo-dev-gra11-runtime", []string{"demo-dev-gra11-runtime"}, []string{"tenant-demo"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			before := map[string][]byte{}
			for _, id := range laneOrder {
				before[id] = h.record(id)
			}
			h.touch(c.touch)
			r := h.run(VerbApply, c.target)
			laneExit(t, r, 0)
			if got := r.stacksOf("apply"); !slices.Equal(got, c.applied) {
				t.Errorf("applied %v, want %v", got, c.applied)
			}
			if got := r.stacksOf("plan"); !slices.Equal(got, c.applied) {
				t.Errorf("planned %v, want %v (only the selected set)", got, c.applied)
			}
			if got := r.events("lock"); !slices.Equal(got, c.locks) {
				t.Errorf("locks taken %v, want %v", got, c.locks)
			}
			for _, id := range laneOrder {
				if !slices.Contains(c.applied, id) && !bytes.Equal(h.record(id), before[id]) {
					t.Errorf("record of %s (not selected) changed", id)
				}
				if slices.Contains(c.applied, id) && bytes.Equal(h.record(id), before[id]) {
					t.Errorf("record of %s (applied) unchanged", id)
				}
			}
			if again := h.run(VerbApply, TargetAll); len(again.tofu()) != 0 {
				t.Errorf("after the run, a second run still acts on %v", again.stacksOf("plan"))
			}
		})
	}
}

// Input-driven selection (FR-009, R21; review r1), the code unchanged: a changed resolved
// reference (LZ_PROJECT_ID_DEMO_DEV) selects account-governance and demo-dev-project, which take it,
// and the project's data consumers, never demo-state (it takes STATE); a republished project
// artefact selects exactly its consumers. Each stack is applied once, in run order.
func TestApplySelectionInputs(t *testing.T) {
	cases := []struct {
		name    string
		change  func(h *laneHarness)
		applied []string
		locks   []string
	}{
		{"resolved-reference", func(h *laneHarness) { h.rebind("f00000000000000000000000000000a1") },
			[]string{"account-governance", "demo-dev-project", "demo-dev-gra11-network", "demo-dev-gra11-runtime"}, []string{"account", "tenant-demo"}},
		{"artefact-republished", func(h *laneHarness) {
			key := laneTenantBkt + "/" + stacks.ArtifactKey("demo-dev-project")
			h.store.objects[key] = []byte(strings.Replace(string(h.store.objects[key]), `"unlabelled":[]`, `"unlabelled":["module.project.ovh_cloud_project.this"]`, 1))
			if !strings.Contains(string(h.store.objects[key]), "ovh_cloud_project.this") {
				h.t.Fatal("control: the project artefact was not changed")
			}
		}, []string{"demo-dev-gra11-network", "demo-dev-gra11-runtime"}, []string{"tenant-demo"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			c.change(h)
			r := h.run(VerbApply, TargetAll)
			laneExit(t, r, 0)
			var applies []string
			for _, call := range r.tofu() {
				if call.Cmd == "apply" {
					applies = append(applies, call.Stack)
				}
			}
			if !slices.Equal(applies, c.applied) {
				t.Errorf("applies %v, want %v (each once, in run order)", applies, c.applied)
			}
			if got := r.events("lock"); !slices.Equal(got, c.locks) {
				t.Errorf("locks taken %v, want %v", got, c.locks)
			}
			if again := h.run(VerbApply, TargetAll); len(again.tofu()) != 0 {
				t.Errorf("after the run, a second run still acts on %v", again.stacksOf("plan"))
			}
		})
	}
}

// `-- <instance>` while a producer it consumes is selected and not applied: refused (exit 3),
// nothing runs.
func TestApplySelectionProducerSelected(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	h.touch("demo-dev-project")
	h.touch("demo-dev-gra11-runtime")
	for _, verb := range []string{VerbPlan, VerbApply} {
		r := h.run(verb, "demo-dev-gra11-runtime")
		laneCondition(t, r, CondProducerSelected)
		if n := len(r.tofu()); n != 0 {
			t.Errorf("%s: %d tofu calls (%v), want none", verb, n, r.stacksOf("init"))
		}
	}
}

// `plan -- all` with a selected producer (T090, coordinator 2026-10-08): each selected data
// consumer of a selected producer is reported `blocked-on=<producer>` and runs no tofu, as
// `plan -- <consumer>` refuses the same state (a plan against the producer's old artefact could be
// approved by mistake); the run plans everything else and ends blocked (exit 2). A consumer whose
// producer is not selected still plans, and an authority edge never blocks. `apply -- all` is
// unchanged: TestApplySelection (project-and-consumers) and TestApplySelectionInputs.
func TestApplyPlanAllProducerSelected(t *testing.T) {
	consumers := []string{"demo-dev-gra11-network", "demo-dev-gra11-runtime"}
	cases := []struct {
		name    string
		touch   []string
		planned []string
		blocked []string // reported blocked-on=demo-dev-project
	}{
		{"producer-and-consumers", []string{"demo-dev-project"}, []string{"demo-dev-project"}, consumers},
		{"producer-and-touched-consumer", []string{"demo-dev-project", "demo-dev-gra11-runtime"}, []string{"demo-dev-project"}, consumers},
		{"consumer-alone", []string{"demo-dev-gra11-runtime"}, []string{"demo-dev-gra11-runtime"}, nil},
		{"authority-producer-selected", []string{"account-governance", "demo-dev-gra11-runtime"}, []string{"account-governance", "demo-dev-gra11-runtime"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			for _, id := range c.touch {
				h.touch(id)
			}
			r := h.run(VerbPlan, TargetAll)
			term := h.term.String()
			if got := r.stacksOf("plan"); !slices.Equal(got, c.planned) {
				t.Errorf("planned %v, want %v", got, c.planned)
			}
			for _, call := range r.tofu() {
				if slices.Contains(c.blocked, call.Stack) {
					t.Errorf("tofu %s on %s, which is blocked on its selected producer", call.Cmd, call.Stack)
				}
			}
			for _, id := range consumers {
				line := "LZ-LIVE plan " + id + " blocked-on=demo-dev-project\n"
				if want := slices.Contains(c.blocked, id); strings.Contains(term, line) != want {
					t.Errorf("terminal reports %q: %v, want %v (terminal %q)", strings.TrimSpace(line), !want, want, term)
				}
			}
			if len(c.blocked) == 0 {
				laneExit(t, r, 0)
				return
			}
			var b *Blocked
			if !errors.As(r.err, &b) || ExitCode(r.err) != BlockedExit {
				t.Errorf("err %v (exit %d), want *Blocked (exit 2)", r.err, ExitCode(r.err))
			} else {
				for _, id := range append([]string{"demo-dev-project"}, c.blocked...) {
					if !strings.Contains(r.err.Error(), id) {
						t.Errorf("err %v does not name %s", r.err, id)
					}
				}
			}
			// The producer's plan ran to its end before the run reported the block.
			if !strings.Contains(term, "LZ-LIVE plan demo-dev-project ok\n") {
				t.Errorf("the producer's plan did not complete (terminal %q)", term)
			}
			// The two forms agree: `plan -- <consumer>` refuses the same state.
			for _, id := range c.blocked {
				one := h.run(VerbPlan, id)
				laneCondition(t, one, CondProducerSelected)
			}
		})
	}
}

// account-bootstrap is applied by bootstrap:account only (its state phase imports, guards and
// publishes it, research R13): `plan|apply -- account-bootstrap` is refused and runs nothing.
func TestApplyBootstrapOwned(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	h.touch("account-bootstrap")
	for _, verb := range []string{VerbPlan, VerbApply} {
		r := h.run(verb, "account-bootstrap")
		laneCondition(t, r, CondBootstrapOwned)
		if n := len(r.tofu()); n != 0 {
			t.Errorf("%s: %d tofu calls, want none", verb, n)
		}
	}
	r := h.run(VerbApply, TargetAll)
	laneExit(t, r, 0)
	if n := len(r.tofu()); n != 0 {
		t.Errorf("all with only account-bootstrap changed: %d tofu calls, want none (bootstrap:account owns it)", n)
	}
}

// `plan` judges the selected stacks' saved plans and applies, outputs, publishes, records and
// writes nothing.
// laneRenderedPlan: the run record holds id's rendered plan (`show -no-color`), its secrets
// redacted (the harness's scan), its content kept (review r2: an empty file is no record).
func laneRenderedPlan(t *testing.T, r laneRun, id string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.runDir, "plan-"+id+".txt"))
	if err != nil {
		t.Errorf("no rendered plan plan-%s.txt in the run record: %v", id, err)
		return
	}
	if !strings.Contains(string(raw), "fake-rendered-plan "+id) || !strings.Contains(string(raw), "client_secret") {
		t.Errorf("plan-%s.txt does not hold the rendered plan (%d bytes)", id, len(raw))
	}
}

// The selected credential producers (account-governance, tenant-state) under `plan` read no
// output and write no credential file (review r2).
func TestApplyPlanOnly(t *testing.T) {
	cases := []struct {
		name    string
		touch   []string
		planned []string
		exit    int
	}{
		// The consumers are blocked on the selected project (T090): it alone is planned, exit 2.
		{"project-and-consumers", []string{"demo-dev-project"}, []string{"demo-dev-project"}, BlockedExit},
		{"credential-producers", []string{"account-governance", "demo-state"}, []string{"account-governance", "demo-state"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { lanePlanOnly(t, c.touch, c.planned, c.exit) })
	}
}

func lanePlanOnly(t *testing.T, touch, want []string, exit int) {
	h := newLaneHarness(t)
	h.steady()
	for _, id := range touch {
		h.touch(id)
	}
	records := map[string][]byte{}
	for _, id := range laneOrder {
		records[id] = h.record(id)
	}
	objects, config := h.store.snapshot(), h.configFiles()
	r := h.run(VerbPlan, TargetAll)
	laneExit(t, r, exit)
	if got := r.stacksOf("plan"); !slices.Equal(got, want) {
		t.Errorf("planned %v, want %v", got, want)
	}
	if got := r.stacksOf("show-json"); !slices.Equal(got, want) {
		t.Errorf("plans read for the guard %v, want %v", got, want)
	}
	if got := append(r.stacksOf("apply"), r.stacksOf("output")...); len(got) != 0 {
		t.Errorf("plan applied or read outputs of %v", got)
	}
	if len(r.puts()) != 0 || !maps.Equal(h.store.snapshot(), objects) {
		t.Errorf("plan published %v", r.puts())
	}
	if !maps.Equal(h.configFiles(), config) {
		t.Errorf("plan changed a file under the config root")
	}
	for _, id := range laneOrder {
		if !bytes.Equal(h.record(id), records[id]) {
			t.Errorf("plan changed the record of %s", id)
		}
	}
	for _, id := range want {
		laneRenderedPlan(t, r, id)
	}
}

// A consumer without its producer's artefact is blocked (exit 2) and never planned; with no
// other selected stack nothing runs (FR-009).
func TestApplyBlockedConsumer(t *testing.T) {
	for _, verb := range []string{VerbPlan, VerbApply} {
		t.Run(verb, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			delete(h.store.objects, laneTenantBkt+"/"+stacks.ArtifactKey("demo-dev-project"))
			r := h.run(verb, TargetAll)
			var b *Blocked
			if !errors.As(r.err, &b) || ExitCode(r.err) != BlockedExit {
				t.Errorf("err %v (exit %d), want *Blocked (exit 2)", r.err, ExitCode(r.err))
			} else if !strings.Contains(r.err.Error(), "demo-dev-project") {
				t.Errorf("err %v does not name the producer demo-dev-project", r.err)
			}
			for _, c := range r.tofu() {
				if c.Stack == "demo-dev-gra11-network" || c.Stack == "demo-dev-gra11-runtime" {
					t.Errorf("tofu %s on blocked %s", c.Cmd, c.Stack)
				}
			}
			if len(r.puts()) != 0 {
				t.Errorf("published %v", r.puts())
			}
		})
	}
}

// Inputs come through stacks.Adapt only: a published project artefact naming another project
// (KD-3) is refused for its consumers, which are never planned.
func TestApplyAdaptRefusesUnboundProject(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	key := laneTenantBkt + "/" + stacks.ArtifactKey("demo-dev-project")
	h.store.objects[key] = []byte(strings.ReplaceAll(string(h.store.objects[key]), laneProject, "f00000000000000000000000000000ff"))
	r := h.run(VerbApply, TargetAll)
	var ref *stacks.RefusalError
	if !errors.As(r.err, &ref) || ref.Reason != stacks.ReasonUnboundProject || ExitCode(r.err) == 0 {
		t.Errorf("err %v, want the adapter's %s refusal", r.err, stacks.ReasonUnboundProject)
	}
	for _, c := range r.tofu() {
		if c.Cmd == "plan" && (c.Stack == "demo-dev-project" || c.Stack == "demo-dev-gra11-network" || c.Stack == "demo-dev-gra11-runtime") {
			t.Errorf("%s planned with an unbound project artefact", c.Stack)
		}
	}
}

// G7 (FR-011): every saved plan of a retained instance passes protect.go before its apply. T063's
// captured plans, moved under each stack's module call, that delete, replace or forget a resource
// of account-governance, tenant-state or project are refused (exit 3) and nothing is applied
// after it — the refused stack's record, artefact and credential files stay; a harmless plan of a
// retained instance and a replacement in an ephemeral one are applied.
func TestApplyRetainedGuard(t *testing.T) {
	cases := []struct {
		name, id, fixture string
		refused           bool
	}{
		{"org-change-tenant-state", "demo-state", "org-change", true},
		{"removed-block-tenant-state", "demo-state", "removed-block", true},
		{"tenant-removed-governance", "account-governance", "tenant-removed", true},
		{"module-removed-governance", "account-governance", "module-removed", true},
		{"project-replaced", "demo-dev-project", "project-replaced", true},
		{"project-forget", "demo-dev-project", "forget", true},
		{"project-create-before-destroy", "demo-dev-project", "create-before-destroy", true},
		{"runtime-errored-plan", "demo-dev-gra11-runtime", "errored", true},
		{"project-update-admitted", "demo-dev-project", "update", false},
		{"runtime-replace-admitted", "demo-dev-gra11-runtime", "project-replaced", false},
	}
	// Every refused plan is refused on each path to it (review r1): `apply -- all`, `apply -- <id>`
	// and `plan -- all` (the guard judges the saved plan of every verb, as the run core does).
	variants := []struct{ verb, target string }{{VerbApply, TargetAll}, {VerbApply, ""}, {VerbPlan, TargetAll}}
	for _, c := range cases {
		for _, v := range variants {
			if !c.refused && v != variants[0] {
				continue
			}
			target := v.target
			if target == "" {
				target = c.id
			}
			t.Run(c.name+"/"+v.verb+"-"+target, func(t *testing.T) {
				h := newLaneHarness(t)
				h.steady()
				h.setPlan(c.id, c.fixture)
				h.touch(c.id)
				record, objects, config := h.record(c.id), h.store.snapshot(), h.configFiles()
				r := h.run(v.verb, target)
				if !c.refused {
					laneExit(t, r, 0)
					if !slices.Contains(r.stacksOf("apply"), c.id) {
						t.Errorf("the admitted plan of %s was not applied", c.id)
					}
					return
				}
				laneCondition(t, r, CondRetained)
				if !slices.Contains(r.stacksOf("show-json"), c.id) {
					t.Errorf("the plan of %s was never read for the guard", c.id)
				}
				if got := r.stacksOf("apply"); len(got) != 0 {
					t.Errorf("applied %v after the refused plan of %s", got, c.id)
				}
				// The refusal stops the run, under `plan` too (review r2).
				after := laneOrder[slices.Index(laneOrder, c.id)+1:]
				for _, id := range r.stacksOf("plan") {
					if slices.Contains(after, id) {
						t.Errorf("%s planned after the refused plan of %s", id, c.id)
					}
				}
				if !bytes.Equal(h.record(c.id), record) || !maps.Equal(h.store.snapshot(), objects) || !maps.Equal(h.configFiles(), config) {
					t.Errorf("the refused run changed the record, an artefact or a credential file")
				}
			})
		}
	}
}

// A credential file missing after the locks are taken (the platform deployer of a run that starts
// at project) blocks the run (exit 2) before the stack's first tofu call; the locks are released
// (the harness checks every run).
func TestApplyCredentialMissingBlocks(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	h.touch("demo-dev-project")
	if err := os.Remove(filepath.Join(h.acctDir, "platform-deployer.env")); err != nil {
		t.Fatal(err)
	}
	r := h.run(VerbApply, TargetAll)
	laneExit(t, r, BlockedExit)
	if got := r.events("lock"); !slices.Equal(got, []string{"tenant-demo"}) {
		t.Errorf("locks taken %v, want [tenant-demo] (held before the stack's credential is read, then released)", got)
	}
	if !strings.Contains(fmt.Sprint(r.err), "platform-deployer.env") {
		t.Errorf("err %v does not name platform-deployer.env", r.err)
	}
	if n := len(r.tofu()); n != 0 {
		t.Errorf("%d tofu calls (%v) without the platform credential", n, r.stacksOf("init"))
	}
}

// A failed apply stops the run (exit 1): nothing after it, no record or artefact for it; the
// locks are released (the harness checks every run).
func TestApplyFailureStops(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	h.setApplyExit("demo-dev-project", 1)
	h.touch("demo-dev-project")
	record, objects := h.record("demo-dev-project"), h.store.snapshot()
	r := h.run(VerbApply, TargetAll)
	laneExit(t, r, 1)
	if got := r.stacksOf("apply"); !slices.Equal(got, []string{"demo-dev-project"}) {
		t.Errorf("applied %v, want only the failing demo-dev-project", got)
	}
	if !bytes.Equal(h.record("demo-dev-project"), record) || !maps.Equal(h.store.snapshot(), objects) {
		t.Errorf("a failed apply wrote a record or published")
	}
}

// A run refused on a lock (exit 3, CondLocked) runs nothing and leaves no lock of its own held:
// the account lock it took before the held tenant lock is released.
func TestApplyLockRefused(t *testing.T) {
	cases := []struct{ name, held, touch string }{
		{"tenant-held", "tenant-demo", "demo-dev-project"},
		{"tenant-held-after-account", "tenant-demo", "demo-state"},
		{"account-held", "account", "account-governance"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			h.touch(c.touch)
			release, err := stacks.DirLocks(h.world.LockDir).TryLock(c.held)
			if err != nil {
				t.Fatal(err)
			}
			objects := h.store.snapshot()
			r, before := h.exec(VerbApply, TargetAll)
			// The harness's checks (its lock check among them) once the other holder is gone.
			if err := release(); err != nil {
				t.Fatal(err)
			}
			h.judge(r, before)
			laneCondition(t, r, CondLocked)
			if c.name == "tenant-held-after-account" && !slices.Equal(r.events("lock"), []string{"account"}) {
				t.Errorf("locks taken %v, want [account] (taken before the held tenant lock, then released)", r.events("lock"))
			}
			if n := len(r.tofu()); n != 0 {
				t.Errorf("%d tofu calls on a run refused on its lock", n)
			}
			if len(r.puts()) != 0 || !maps.Equal(h.store.snapshot(), objects) {
				t.Errorf("a refused run published")
			}
		})
	}
}

// `destroy` refuses every retained instance, alone or within `all` (G7, FR-011), before any tofu
// call.
func TestApplyDestroyRetainedRefused(t *testing.T) {
	for _, target := range []string{"account-bootstrap", "demo-state", "account-governance", "demo-dev-project", TargetAll} {
		t.Run(target, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			r := h.run(VerbDestroy, target)
			laneCondition(t, r, CondRetained)
			if target != TargetAll && (r.err == nil || !strings.Contains(r.err.Error(), target)) {
				t.Errorf("err %v does not name %s", r.err, target)
			}
			if n := len(r.tofu()); n != 0 {
				t.Errorf("%d tofu calls on a refused destroy", n)
			}
		})
	}
}

// ---------------------------------------------------------------- T059 additions (decision 3)

// laneAPINames are the fake API's names of the lane credentials, by authority.
var laneAPINames = map[Authority]string{AuthorityBootstrap: "lane-admin", AuthorityPlatform: "lane-platform", AuthorityTenant: "lane-tenant"}

// A credential of another account, known to the fake API.
const (
	laneForeignAccount = "yy000058-ovh"
	laneForeignID      = "EU.lane-foreign-58"
	laneForeignSecret  = "lz-seed-t059-foreign-client-secret"
)

// add registers a credential with the fake API.
func (f *fakeAPI) add(c apiCredential) {
	f.creds[c.ClientID] = c
	f.tokens["fake-token-"+c.Name] = c
}

// laneAPITransport logs every GET /auth/details in the lane's log as a "bind" event of the
// credential's fake name, with the answer's status, so its order against tofu calls shows.
type laneAPITransport struct {
	inner http.RoundTripper
	log   string
}

func (l laneAPITransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := l.inner.RoundTrip(r)
	if r.URL.Path == "/v1/auth/details" {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		laneAppend(l.log, laneCall{Cmd: "bind", Stack: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer fake-token-"), Exit: status})
	}
	return resp, err
}

func (h *laneHarness) laneAPI() API {
	a := h.api.API()
	a.HTTP = &http.Client{Transport: laneAPITransport{inner: h.api.Client().Transport, log: h.world.Log}}
	return a
}

// setFail makes id's call cmd fail (init, plan, show-json, show, output).
func (h *laneHarness) setFail(id, cmd string) {
	in := h.row(id)
	st := h.world.Stacks[in.Path]
	st.Fail = cmd
	h.world.Stacks[in.Path] = st
	h.save()
}

// writeConfig replaces a file below the config root with values.
func (h *laneHarness) writeConfig(rel string, values map[string]string) {
	h.t.Helper()
	if err := WriteCredentialFile(h.root, filepath.FromSlash(rel), values); err != nil {
		h.t.Fatal(err)
	}
}

// Decision 3a (FR-010 "every credential is checked against the bound account before use", G13):
// a credential the API places in another account, one for another endpoint, or a binding whose
// org is not the manifest's or whose account id is not the run's, is refused (exit 3) before any
// tofu call of its stack; nothing is applied or published; the locks are released (the harness
// checks every run, and that every stack's calls follow a successful GET /auth/details with its
// authority's credential).
func TestApplyBindsCredentials(t *testing.T) {
	acct := "accounts/" + laneAccount + "/"
	foreign := map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": laneForeignID, "OVH_CLIENT_SECRET": laneForeignSecret}
	accountEnv := func(id, org string) map[string]string {
		return map[string]string{"LZ_ACCOUNT_ID": id, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": org, "LZ_PROJECT_ID_STATE": laneProject, "LZ_PROJECT_ID_DEMO_DEV": laneProject}
	}
	cases := []struct {
		name, verb, touch, file string
		values                  map[string]string
		cond                    string
		refused                 string // the stack refused: no tofu call of it, nothing after it
	}{
		{"admin-foreign", VerbApply, "", "sandbox.env", foreign, CondAccount, "account-governance"},
		{"platform-foreign", VerbApply, "demo-dev-project", acct + "platform-deployer.env", foreign, CondAccount, "demo-dev-project"},
		{"platform-foreign-plan", VerbPlan, "demo-dev-project", acct + "platform-deployer.env", foreign, CondAccount, "demo-dev-project"},
		{"tenant-foreign", VerbApply, "demo-dev-gra11-network", acct + "tenants/demo/deployer.env", foreign, CondAccount, "demo-dev-gra11-network"},
		{"tenant-endpoint", VerbApply, "demo-dev-gra11-network", acct + "tenants/demo/deployer.env",
			map[string]string{"OVH_ENDPOINT": "ovh-ca", "OVH_CLIENT_ID": laneTenantID, "OVH_CLIENT_SECRET": laneTenantSecret}, CondEndpoint, "demo-dev-gra11-network"},
		{"org-mismatch", VerbApply, "demo-dev-project", acct + "account.env", accountEnv(laneAccount, "zz"), CondOrg, "demo-dev-project"},
		{"account-env-other-id", VerbApply, "demo-dev-project", acct + "account.env", accountEnv(laneForeignAccount, "lz"), CondAccount, "demo-dev-project"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			if c.touch != "" {
				h.steady()
				h.touch(c.touch)
			}
			h.writeConfig(c.file, c.values)
			objects := h.store.snapshot()
			r := h.run(c.verb, TargetAll)
			laneCondition(t, r, c.cond)
			for _, call := range r.tofu() {
				if call.Stack == c.refused {
					t.Errorf("tofu %s on %s, whose credential is not bound to the account", call.Cmd, c.refused)
				}
			}
			if got := r.stacksOf("apply"); len(got) != 0 {
				t.Errorf("applied %v on a refused binding", got)
			}
			if len(r.puts()) != 0 || !maps.Equal(h.store.snapshot(), objects) {
				t.Errorf("a refused run published")
			}
			if c.cond == CondEndpoint {
				// The endpoint is compared before the credential is sent anywhere (Bind).
				for _, call := range h.api.Calls() {
					if call.Credential == "lane-tenant" || call.Credential == laneTenantID {
						t.Errorf("the ovh-ca credential reached the API: %+v", call)
					}
				}
			}
		})
	}
}

// Decision 3b: a failed init, plan, `show -json`, rendered `show` or `output` stops the run (exit
// 1) with nothing applied after it, no record or artefact of the failed stack, its saved plan
// removed, and every run lock released (the harness checks the last two on every run).
func TestApplyChildFailureReleasesLocks(t *testing.T) {
	for _, verb := range []string{VerbApply, VerbPlan} {
		for _, fail := range []string{"init", "plan", "show-json", "show", "output"} {
			if verb == VerbPlan && fail == "output" {
				continue // plan reads no output
			}
			t.Run(verb+"/"+fail, func(t *testing.T) {
				h := newLaneHarness(t)
				h.steady()
				h.touch("demo-dev-project")
				h.setFail("demo-dev-project", fail)
				record, objects := h.record("demo-dev-project"), h.store.snapshot()
				r := h.run(verb, TargetAll)
				laneExit(t, r, 1)
				if got := r.events("lock"); !slices.Equal(got, []string{"tenant-demo"}) {
					t.Errorf("locks taken %v, want [tenant-demo]", got)
				}
				var want []string
				if fail == "output" {
					want = []string{"demo-dev-project"}
				}
				if got := r.stacksOf("apply"); !slices.Equal(got, want) {
					t.Errorf("applied %v, want %v", got, want)
				}
				if got := r.stacksOf(fail); !slices.Equal(got, []string{"demo-dev-project"}) {
					t.Errorf("failing %s ran on %v, want [demo-dev-project]", fail, got)
				}
				for _, call := range r.tofu() {
					if call.Stack != "demo-dev-project" {
						t.Errorf("tofu %s on %s after the failed %s of demo-dev-project", call.Cmd, call.Stack, fail)
					}
				}
				if !bytes.Equal(h.record("demo-dev-project"), record) || !maps.Equal(h.store.snapshot(), objects) {
					t.Errorf("a failed %s wrote a record or published", fail)
				}
			})
		}
	}
}

// account-governance's outputs name the tenants whose deployer files the lane writes: a tenant the
// manifest does not have (a path among them), or a manifest tenant or the platform deployer
// without its client or secret, fails the run (exit 1) before any credential file is written.
func TestApplyDeployerOutputsChecked(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(outs map[string]map[string]any)
	}{
		{"unknown-tenant", func(o map[string]map[string]any) {
			o["tenants"]["value"].(map[string]any)["ghost"] = map[string]any{"deployer_client_id": "EU.ghost"}
			o["tenant_deployer_secrets"]["value"].(map[string]any)["ghost"] = "lz-seed-t059-ghost-secret"
		}},
		{"path-tenant", func(o map[string]map[string]any) {
			o["tenants"]["value"].(map[string]any)["../../escape"] = map[string]any{"deployer_client_id": "EU.escape"}
			o["tenant_deployer_secrets"]["value"].(map[string]any)["../../escape"] = "lz-seed-t059-escape-secret"
		}},
		{"tenant-missing", func(o map[string]map[string]any) {
			delete(o["tenants"]["value"].(map[string]any), "demo")
		}},
		{"secret-missing", func(o map[string]map[string]any) {
			delete(o["tenant_deployer_secrets"]["value"].(map[string]any), "demo")
		}},
		{"client-empty", func(o map[string]map[string]any) {
			o["platform_deployer"]["value"].(map[string]any)["client_id"] = ""
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			in := h.row("account-governance")
			st := h.world.Stacks[in.Path]
			raw, err := os.ReadFile(st.Outputs)
			if err != nil {
				t.Fatal(err)
			}
			var outs map[string]map[string]any
			if err := json.Unmarshal(raw, &outs); err != nil {
				t.Fatal(err)
			}
			c.mutate(outs)
			raw, _ = json.Marshal(outs)
			st.Outputs = filepath.Join(h.base, "fake", "outputs-governance-"+c.name+".json")
			if err := os.WriteFile(st.Outputs, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			h.world.Stacks[in.Path] = st
			h.save()
			before := h.configFiles()
			r := h.run(VerbApply, TargetAll)
			laneExit(t, r, 1)
			if got := r.stacksOf("apply"); !slices.Equal(got, []string{"account-governance"}) {
				t.Errorf("applied %v, want only account-governance", got)
			}
			for p := range h.configFiles() {
				if _, ok := before[p]; !ok {
					t.Errorf("the run wrote %s from rejected outputs", p)
				}
			}
			if len(h.record("account-governance")) != 0 {
				t.Errorf("account-governance recorded with rejected outputs")
			}
		})
	}
}

// The bound account's own files (account.env, state-passphrase.env: bootstrap:account writes them)
// missing stop the run before any lock or tofu call, blocked (exit 2) naming bootstrap:account; a
// passphrase file without the passphrase is refused (exit 3).
func TestApplyAccountFilesMissing(t *testing.T) {
	acct := "accounts/" + laneAccount + "/"
	for _, c := range []struct {
		name, rel string
		empty     bool
	}{
		{"account-env", acct + "account.env", false},
		{"passphrase", acct + "state-passphrase.env", false},
		{"passphrase-empty", acct + "state-passphrase.env", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			if c.empty {
				h.writeConfig(c.rel, map[string]string{"OTHER": "x"})
			} else if err := os.Remove(filepath.Join(h.root, filepath.FromSlash(c.rel))); err != nil {
				t.Fatal(err)
			}
			r := h.run(VerbApply, TargetAll)
			if c.empty {
				laneExit(t, r, RefusalExit)
			} else {
				laneExit(t, r, BlockedExit)
				if !strings.Contains(fmt.Sprint(r.err), "bootstrap:account") {
					t.Errorf("err %v does not name bootstrap:account", r.err)
				}
			}
			if n := len(r.tofu()); n != 0 || len(r.events("lock")) != 0 {
				t.Errorf("%d tofu calls, locks %v without the account's files", n, r.events("lock"))
			}
		})
	}
}

// Selection reads each producer's artefact with that producer's keys: a producer's credential file
// that exists but is refused (here readable by others) refuses the run (exit 3, CondFileMode)
// before any lock or tofu call; only a missing file means "nothing published yet".
func TestApplySelectionCredentialRefused(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	h.touch("demo-dev-gra11-runtime")
	if err := os.Chmod(filepath.Join(h.acctDir, "platform-deployer.env"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := h.run(VerbApply, TargetAll)
	laneCondition(t, r, CondFileMode)
	if n := len(r.tofu()); n != 0 || len(r.events("lock")) != 0 {
		t.Errorf("%d tofu calls, locks %v after a refused producer credential", n, r.events("lock"))
	}
}

// ---------------------------------------------------------------- T059 review round 1

// The bound account is the run's: an account.env naming another account is refused before any
// lock or tofu call, also when the stack's own credential belongs to that other account (the
// binding alone would then pass: the credential answers the account account.env names).
func TestApplyAccountEnvNamesOtherAccount(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	h.touch("demo-dev-project")
	acct := "accounts/" + laneAccount + "/"
	h.writeConfig(acct+"account.env", map[string]string{"LZ_ACCOUNT_ID": laneForeignAccount, "OVH_ENDPOINT": "ovh-eu", "LZ_ORG": "lz",
		"LZ_PROJECT_ID_STATE": laneProject, "LZ_PROJECT_ID_DEMO_DEV": laneProject})
	h.writeConfig(acct+"platform-deployer.env", map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": laneForeignID, "OVH_CLIENT_SECRET": laneForeignSecret})
	r := h.run(VerbApply, "demo-dev-project")
	laneCondition(t, r, CondAccount)
	if n := len(r.tofu()); n != 0 || len(r.events("lock")) != 0 || len(r.events("bind")) != 0 {
		t.Errorf("%d tofu calls, locks %v, bindings %v under another account's account.env", n, r.events("lock"), r.events("bind"))
	}
}

// Credential values from the outputs are checked as files.go would before the first file is
// written (a later value with a line break must not leave earlier files replaced), and the
// secrets the lane writes must be sensitive outputs (a non-sensitive one would also be published).
func TestApplyDeployerOutputValues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(outs map[string]map[string]any)
	}{
		{"tenant-secret-newline", func(o map[string]map[string]any) {
			o["tenant_deployer_secrets"]["value"].(map[string]any)["demo"] = "lz-seed-t059-line\nbreak"
		}},
		{"platform-secret-space", func(o map[string]map[string]any) {
			o["platform_deployer_secret"]["value"] = " lz-seed-t059-padded"
		}},
		{"platform-secret-not-sensitive", func(o map[string]map[string]any) {
			o["platform_deployer_secret"]["sensitive"] = false
		}},
		{"tenant-secrets-not-sensitive", func(o map[string]map[string]any) {
			o["tenant_deployer_secrets"]["sensitive"] = false
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			h.touch("account-governance")
			in := h.row("account-governance")
			st := h.world.Stacks[in.Path]
			raw, err := os.ReadFile(st.Outputs)
			if err != nil {
				t.Fatal(err)
			}
			var outs map[string]map[string]any
			if err := json.Unmarshal(raw, &outs); err != nil {
				t.Fatal(err)
			}
			// A new platform client, so a platform file written before the rejected value shows.
			outs["platform_deployer"]["value"].(map[string]any)["client_id"] = "EU.lz-t059-new-platform"
			c.mutate(outs)
			raw, _ = json.Marshal(outs)
			st.Outputs = filepath.Join(h.base, "fake", "outputs-governance-"+c.name+".json")
			if err := os.WriteFile(st.Outputs, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			h.world.Stacks[in.Path] = st
			h.save()
			before, objects := h.configFiles(), h.store.snapshot()
			r := h.run(VerbApply, "account-governance")
			laneExit(t, r, 1)
			if after := h.configFiles(); !maps.Equal(after, before) {
				t.Errorf("credential files changed from rejected outputs")
			}
			if !maps.Equal(h.store.snapshot(), objects) {
				t.Errorf("published from rejected outputs")
			}
		})
	}
}

// Selection runs again once the run locks are held: another run that finished in between (here:
// it recorded demo-dev-project's new code) changes the set, and the run is refused (exit 3,
// CondLocked) with nothing planned, the locks released.
func TestApplySelectionChangedUnderLock(t *testing.T) {
	h := newLaneHarness(t)
	h.steady()
	h.touch("demo-dev-project")
	var once sync.Once
	h.afterLock = func(string) {
		once.Do(func() {
			var rec Record
			if err := json.Unmarshal(h.record("demo-dev-project"), &rec); err != nil {
				t.Error(err)
				return
			}
			code, err := stacks.CodeDigest(h.checkout, h.row("demo-dev-project"))
			if err != nil {
				t.Error(err)
				return
			}
			rec.CodeDigest = code
			if err := WriteRecord(h.recordsDir(), "demo-dev-project", rec); err != nil {
				t.Error(err)
			}
		})
	}
	r := h.run(VerbApply, TargetAll)
	laneCondition(t, r, CondLocked)
	if n := len(r.tofu()); n != 0 {
		t.Errorf("%d tofu calls after the selection changed under the locks", n)
	}
}

// SIGINT/SIGTERM cancel the run (the entry's context): the running tofu gets SIGINT, as the run
// core stops its children (never an immediate kill), the run fails, nothing after it runs, and the
// locks and the saved plan are released (the harness checks both).
func TestApplyCancelStopsChild(t *testing.T) {
	for _, call := range []string{"plan", "apply"} {
		t.Run(call, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			h.touch("demo-dev-project")
			in := h.row("demo-dev-project")
			st := h.world.Stacks[in.Path]
			st.Block = call
			h.world.Stacks[in.Path] = st
			h.save()
			r := h.run(VerbApply, TargetAll)
			if r.err == nil {
				t.Fatal("a cancelled run returned no error")
			}
			var got string
			for _, c := range r.tofu() {
				if c.Cmd == call && c.Stack == "demo-dev-project" {
					got = c.Signal
				}
				if c.Stack != "demo-dev-project" {
					t.Errorf("tofu %s on %s after the cancellation", c.Cmd, c.Stack)
				}
			}
			if got != syscall.SIGINT.String() {
				t.Errorf("the blocked tofu %s got signal %q, want %q (stopped gracefully, not killed)", call, got, syscall.SIGINT.String())
			}
		})
	}
}

// ---------------------------------------------------------------- T059 review round 2

// tenant-state's outputs carry the tenant's and the platform's S3 keys: each must be a sensitive
// output with both halves present and writable, checked before any file is written (review r2).
func TestApplyTenantStateOutputValues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(outs map[string]map[string]any)
	}{
		{"tenant-s3-not-sensitive", func(o map[string]map[string]any) { o["tenant_s3"]["sensitive"] = false }},
		{"platform-s3-not-sensitive", func(o map[string]map[string]any) { o["platform_s3"]["sensitive"] = false }},
		{"platform-secret-empty", func(o map[string]map[string]any) {
			o["platform_s3"]["value"].(map[string]any)["secret_access_key"] = ""
		}},
		{"tenant-key-empty", func(o map[string]map[string]any) {
			o["tenant_s3"]["value"].(map[string]any)["access_key_id"] = ""
		}},
		{"tenant-secret-newline", func(o map[string]map[string]any) {
			o["tenant_s3"]["value"].(map[string]any)["secret_access_key"] = "lz-seed-t059-s3\nbreak"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newLaneHarness(t)
			h.steady()
			h.touch("demo-state")
			in := h.row("demo-state")
			st := h.world.Stacks[in.Path]
			raw, err := os.ReadFile(st.Outputs)
			if err != nil {
				t.Fatal(err)
			}
			var outs map[string]map[string]any
			if err := json.Unmarshal(raw, &outs); err != nil {
				t.Fatal(err)
			}
			// New keys for both users, so a file written before the rejected value shows.
			outs["platform_s3"]["value"].(map[string]any)["access_key_id"] = "AK-t059-new-platform"
			outs["tenant_s3"]["value"].(map[string]any)["access_key_id"] = "AK-t059-new-tenant"
			c.mutate(outs)
			raw, _ = json.Marshal(outs)
			st.Outputs = filepath.Join(h.base, "fake", "outputs-state-"+c.name+".json")
			if err := os.WriteFile(st.Outputs, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			h.world.Stacks[in.Path] = st
			h.save()
			before := h.configFiles()
			r := h.run(VerbApply, "demo-state")
			laneExit(t, r, 1)
			if after := h.configFiles(); !maps.Equal(after, before) {
				t.Errorf("credential files changed from rejected outputs")
			}
		})
	}
}

// The binding cache holds whole credentials: a credential file replaced during the run with the
// same client id but another endpoint or secret is bound again, not taken as bound (review r2).
func TestApplyBindCacheWholeCredential(t *testing.T) {
	h := newLaneHarness(t)
	r := &laneRunner{o: ApplyOptions{API: h.laneAPI(), Manifest: h.m}, bound: map[Credential]bool{},
		binding: Binding{AccountID: laneAccount, Endpoint: "ovh-eu", Org: h.m.Org}}
	ctx := context.Background()
	creds := maps.Clone(laneCreds[AuthorityTenant])
	if err := r.bind(ctx, creds); err != nil {
		t.Fatalf("bind of the tenant credential: %v", err)
	}
	creds["OVH_ENDPOINT"] = "ovh-ca"
	var ref *Refusal
	if err := r.bind(ctx, creds); !errors.As(err, &ref) || ref.Condition != CondEndpoint {
		t.Errorf("same client id, endpoint ovh-ca: err %v, want an endpoint refusal", err)
	}
	creds["OVH_ENDPOINT"] = "ovh-eu"
	creds["OVH_CLIENT_SECRET"] = "lz-seed-t059-other-secret"
	if err := r.bind(ctx, creds); err == nil {
		t.Error("same client id, another secret: bound without asking the API")
	}
}

// The apply stream reaches the terminal line by line without its `outputs` events; an
// unterminated last line is passed on at the end (review r2).
func TestApplyDropOutputs(t *testing.T) {
	var out bytes.Buffer
	d := &dropOutputs{w: &out}
	for _, chunk := range []string{`{"type":"apply_start"}` + "\n" + `{"type":"out`, `puts","outputs":{"s":{"value":"x"}}}` + "\n", `{"type":"change_summary"}` + "\n", "tail"} {
		if _, err := d.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	d.flush()
	want := `{"type":"apply_start"}` + "\n" + `{"type":"change_summary"}` + "\n" + "tail"
	if out.String() != want {
		t.Errorf("terminal got %q, want %q", out.String(), want)
	}
}
