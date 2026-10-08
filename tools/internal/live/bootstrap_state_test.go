package live

// T056: the bootstrap phases state, publish and verify (spec 005 FR-004, FR-008, FR-010, FR-011,
// FR-012, SC-005; research R13 phases 5–7 and *Partial states tested*; contracts/checks.md V008,
// guards G2, G3, G7 bootstrap part), run through T042's harness (fake OVHcloud API, fake terminal)
// with a fake tofu, a fake cloud and a fake bucket store. Nothing here reaches the real API or
// reads the real ~/.config/ovh-lz/.
//
// The fake tofu is the test binary called through bin/tofu while bin/bs-world.json exists
// (TestMain). It models the two roots the phases use, each through the generated stack in the
// checkout (stacks/account/bootstrap, stacks/account/account-governance), against the fake cloud
// (fake/cloud.json: buckets, S3 users, an event log) and the bucket objects (fake/s3/<bucket>/<key>):
//
//   - `bootstrap`: local encrypted state at $TF_VAR_lz_account_dir/state/account-bootstrap.tfstate
//     (the generated _lz_backend.tf); only the passphrase that wrote it reads it. Plan compares
//     the state with the cloud: the account bucket and the platform S3 user (with its credential
//     and policy) are created when absent from the state, or from the cloud (refresh); -destroy
//     deletes them. `show -json` prints a plan document (format 1.2) or, when a recorded plan is
//     set, that document; apply executes a saved plan's changes (a create of a bucket name that
//     exists anywhere fails with 409, P21), or plans afresh with -auto-approve and no plan file;
//     `import <bucket address> <project>/<region>/<name>` adopts a bucket of that project;
//     `output -json` gives the stage outputs (platform_s3 sensitive).
//   - `account-governance`: S3 backend on the account bucket (init needs state.env's keys of a
//     user bound to an existing bucket); plan takes the S3 lockfile
//     `account/account-governance/terraform.tfstate.tflock` and removes it (a held one refuses
//     the plan), reads no state it cannot decrypt, and changes nothing.
//
// The OpenTofu S3 backend, its lockfile and client-side encryption on OVHcloud Object Storage are
// premises P1 (lockfile), P2 (backend options), P3 (encryption with the lockfile), all UNVERIFIED
// until T010: the fake follows research R5 and the generated backend, not a live observation. The
// 409 on a bucket name taken elsewhere follows P21 (names are global); its text is UNVERIFIED.
// Every call echoes the secrets of its environment on stderr, so code that forwards a child stream
// unredacted leaks.
//
// The storage routes are on T042's fake API (bootstrapAPI.routes): GET
// /cloud/project/{serviceName}/region/{regionName}/storage[/{name}] (kb/api/v1/cloud.json, IAM
// action publicCloudProject:apiovh:region/storage/get), answered from the fake cloud.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

const (
	bssRepoRoot   = "../../.."
	bssRevision   = "0d1e2f3a4b5c6d7e8f90a1b2c3d4e5f60718293a" // the reviewed commit the envelope names
	bssWorldFile  = "bs-world.json"
	bssBucket     = "lz-bkt-state" // spec.org lz (D87), account scope
	bssRegion     = "GRA"
	bssStateFile  = "account-bootstrap.tfstate"
	bssGovState   = "account/account-governance/terraform.tfstate"
	bssLock       = bssGovState + ".tflock"
	bssForeign    = "xx000009-ovh"
	bssForeignPrj = "f0000000000000000000000000000009"
	bssPrefix     = "module.bootstrap.module.state_backend."
	bssBucketAddr = bssPrefix + "module.bucket.ovh_cloud_project_storage.this"
	bssUserModule = bssPrefix + `module.s3_user["platform"]`
	bssUserAddr   = bssUserModule + `.ovh_cloud_project_user.this`
	bssCredAddr   = bssPrefix + `module.s3_user["platform"].ovh_cloud_project_user_s3_credential.this`
	bssPolicyAddr = bssPrefix + `module.s3_user["platform"].ovh_cloud_project_user_s3_policy.this`
)

var bssTypes = map[string]string{
	bssBucketAddr: "ovh_cloud_project_storage",
	bssUserAddr:   "ovh_cloud_project_user",
	bssCredAddr:   "ovh_cloud_project_user_s3_credential",
	bssPolicyAddr: "ovh_cloud_project_user_s3_policy",
}

// ---------------------------------------------------------------- fake world

// bssWorld is what the fake tofu knows (bin/bs-world.json).
type bssWorld struct {
	Account     string `json:"account"` // the bound account, owner of what the roots create
	Project     string `json:"project"` // its state project (LZ_PROJECT_ID_STATE)
	Region      string `json:"region"`
	Bucket      string `json:"bucket"`
	Cloud       string `json:"cloud"`        // fake/cloud.json
	S3          string `json:"s3"`           // fake/s3: <bucket>/<key>
	Log         string `json:"log"`          // fake/tofu.jsonl
	Recorded    string `json:"recorded"`     // a recorded plan document the bootstrap root's plan yields
	FailAfter   int    `json:"fail_after"`   // > 0: the bootstrap apply stops (exit 1) after this many creates
	BucketFirst bool   `json:"bucket_first"` // the bootstrap apply creates the bucket before the S3 user
	FailShow    bool   `json:"fail_show"`    // `show -json` fails: the run stops between plan and guard
	// UserDescription is the platform S3 user's description, from the pinned-tool capture of the
	// bootstrap stage (bssCapturedUserDescription), never written by hand.
	UserDescription string `json:"user_description"`
	// PolicyConflict: the bootstrap apply creates what it plans, then fails with a 409 on the
	// platform user's S3 policy (a 409 that is not the bucket's name).
	PolicyConflict bool `json:"policy_conflict"`
	// OtherRunUser: while the apply runs, another run creates a platform S3 user (id 4800) in the
	// project, which no state of this run knows.
	OtherRunUser bool `json:"other_run_user"`
	// OutputChange: the plan changes only an output (the stage's outputs.tf changed): no resource
	// change, but an apply is needed for `tofu output` to return the new outputs.
	OutputChange bool `json:"output_change"`
	// OtherRunStateUser: between this run's user listing and its plan, another run created the
	// platform S3 user (id 4800) and recorded it in the shared bootstrap state (review r2).
	OtherRunStateUser bool `json:"other_run_state_user"`
	// MixedErrors: the bucket create fails with a 500 while a separate diagnostic reports a 409 of
	// the user's policy (review r2).
	MixedErrors bool `json:"mixed_errors"`
	// WrapConflict: the taken name's 409 diagnostic is word-wrapped across lines (review r2).
	WrapConflict bool `json:"wrap_conflict"`
	// Tenants is the resolved project of each manifest tenant: account-governance's required
	// `tenants` input (data-model *Resolved-reference input*; stacks.Adapt writes it).
	Tenants map[string]string `json:"tenants"`
}

type bssCloud struct {
	Buckets map[string]bssBucketRec `json:"buckets"` // name → bucket (names are global, P21)
	Users   map[string]bssUser      `json:"users"`   // id → S3 user
	Next    int                     `json:"next"`
	Events  []string                `json:"events"` // "create bucket <n>", "delete user <id>", "import bucket <n>"
}

type bssBucketRec struct {
	Account string `json:"account"`
	Project string `json:"project"`
	Region  string `json:"region"`
}

type bssUser struct {
	Account     string `json:"account"`
	Project     string `json:"project"`
	Description string `json:"description"`
	Bucket      string `json:"bucket"` // the bucket its S3 policy covers
	AccessKey   string `json:"access_key"`
	Secret      string `json:"secret"`
}

// bssLocal is the fake encrypted local state of the bootstrap root.
type bssLocal struct {
	Envelope string `json:"envelope"` // never plaintext state
	PassSHA  string `json:"pass_sha"`
	Bucket   string `json:"bucket,omitempty"`
	User     string `json:"user,omitempty"`
}

// bssCall is one fake tofu invocation.
type bssCall struct {
	Root   string   `json:"root"`
	Cmd    string   `json:"cmd"` // init plan show-json show apply import output state-list other
	Args   []string `json:"args"`
	Env    []string `json:"env"`
	Plan   string   `json:"plan"`  // plan file written, shown or applied
	Nonce  string   `json:"nonce"` // its nonce ("" for an apply without a plan file)
	Locked bool     `json:"locked"`
	Exit   int      `json:"exit"`
}

type bssPlanFile struct {
	Root     string      `json:"root"`
	Nonce    string      `json:"nonce"`
	Destroy  bool        `json:"destroy"`
	Recorded bool        `json:"recorded"`
	Changes  []bssChange `json:"changes"`
}

type bssChange struct {
	Address string   `json:"address"`
	Actions []string `json:"actions"`
}

func bssSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func bssLoad(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func bssSave(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// bssFakeWorld returns bin/bs-world.json when the test binary runs as that bin's tofu.
func bssFakeWorld() string {
	if filepath.Base(os.Args[0]) != "tofu" {
		return ""
	}
	p := os.Args[0]
	if !filepath.IsAbs(p) {
		lp, err := exec.LookPath(p)
		if err != nil {
			return ""
		}
		p = lp
	}
	w := filepath.Join(filepath.Dir(p), bssWorldFile)
	if _, err := os.Stat(w); err != nil {
		return ""
	}
	return w
}

// bssValueFlags take the next argument as their value when written without "=".
var bssValueFlags = []string{"-out", "-var", "-var-file", "-lock-timeout", "-backend-config", "-target", "-state", "-parallelism"}

// fakeBootstrapTofu is the fake tofu of the bootstrap state phases.
func fakeBootstrapTofu(worldPath string, args []string) int {
	var w bssWorld
	if bssLoad(worldPath, &w) != nil {
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
	call := bssCall{Root: filepath.Base(dir), Args: os.Args[1:], Env: os.Environ(), Cmd: "other"}
	done := func(code int) int {
		call.Exit = code
		line, _ := json.Marshal(call)
		if f, err := os.OpenFile(w.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintln(f, string(line))
			f.Close()
		}
		return code
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
		return done(1)
	}
	// Every call repeats its environment's secrets on stderr: a forwarded stream must be redacted.
	fmt.Fprintf(os.Stderr, "fake-tofu-echo client_secret=%s passphrase=%s s3=%s\n",
		os.Getenv("OVH_CLIENT_SECRET"), os.Getenv("TF_VAR_state_passphrase"), os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if len(args) == 0 {
		return done(0)
	}
	sub, rest := args[0], args[1:]
	var positional []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if slices.Contains(bssValueFlags, a) {
			i++
			continue
		}
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
		}
	}
	if sub == "state" && len(positional) > 0 {
		sub, positional = "state "+positional[0], positional[1:]
	}
	flag := func(name string) (string, bool) {
		for i, a := range rest {
			if v, ok := strings.CutPrefix(a, name+"="); ok {
				return v, true
			}
			if a == name && i+1 < len(rest) {
				return rest[i+1], true
			}
		}
		return "", false
	}
	has := func(f string) bool { return slices.Contains(rest, f) }
	resolve := func(p string) string {
		if p != "" && !filepath.IsAbs(p) {
			return filepath.Join(dir, p)
		}
		return p
	}
	if os.Getenv("TF_VAR_state_passphrase") == "" {
		return fail("No value for required variable state_passphrase")
	}
	var cloud bssCloud
	if bssLoad(w.Cloud, &cloud) != nil {
		return fail("fake cloud unreadable")
	}
	saveCloud := func() { _ = bssSave(w.Cloud, cloud) }
	dataDir := fakeDataDir(dir)
	marker := filepath.Join(dataDir, "bss-init-"+call.Root)
	initialised := func() bool { _, err := os.Stat(marker); return err == nil }
	providerOK := func() bool {
		return os.Getenv("OVH_ENDPOINT") == "ovh-eu" && os.Getenv("OVH_CLIENT_ID") != "" && os.Getenv("OVH_CLIENT_SECRET") != ""
	}
	pass := bssSHA(os.Getenv("TF_VAR_state_passphrase"))

	switch call.Root {
	case "bootstrap":
		acctDir := os.Getenv("TF_VAR_lz_account_dir")
		if !filepath.IsAbs(acctDir) || filepath.Base(acctDir) != w.Account || filepath.Base(filepath.Dir(acctDir)) != "accounts" {
			return fail("Invalid value for variable lz_account_dir")
		}
		statePath := filepath.Join(acctDir, "state", bssStateFile)
		var st bssLocal
		if raw, err := os.ReadFile(statePath); err == nil {
			if json.Unmarshal(raw, &st) != nil || st.PassSHA != pass {
				return fail("decrypting the state of %s: the passphrase does not match", statePath)
			}
		}
		saveState := func() int {
			st.Envelope, st.PassSHA = "fake-aes-gcm", pass
			if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
				return fail("cannot write the state")
			}
			if bssSave(statePath, st) != nil {
				return fail("cannot write the state")
			}
			return 0
		}
		needs := func() int {
			if !initialised() {
				return fail(`Backend initialization required, please run "tofu init"`)
			}
			if !providerOK() {
				return fail("ovh provider: no credentials")
			}
			if os.Getenv("TF_VAR_state_project_id") != w.Project {
				return fail("project %q not found", os.Getenv("TF_VAR_state_project_id"))
			}
			return 0
		}
		inCloud := func() (bucket, user bool) {
			b, ok := cloud.Buckets[st.Bucket]
			bucket = ok && st.Bucket != "" && b.Account == w.Account && b.Project == w.Project
			_, user = cloud.Users[st.User]
			return bucket, user && st.User != ""
		}
		compute := func(destroy bool) []bssChange {
			b, u := inCloud()
			act := func(present bool) []string {
				switch {
				case destroy && present:
					return []string{"delete"}
				case destroy:
					return []string{"no-op"}
				case present:
					return []string{"no-op"}
				}
				return []string{"create"}
			}
			return []bssChange{{bssBucketAddr, act(b)}, {bssUserAddr, act(u)}, {bssCredAddr, act(u)}, {bssPolicyAddr, act(u)}}
		}
		recorded := func() ([]bssChange, map[string]any, bool) {
			if w.Recorded == "" {
				return nil, nil, false
			}
			var doc map[string]any
			if bssLoad(w.Recorded, &doc) != nil {
				return nil, nil, false
			}
			var out []bssChange
			rcs, _ := doc["resource_changes"].([]any)
			for _, rc := range rcs {
				m, _ := rc.(map[string]any)
				ch, _ := m["change"].(map[string]any)
				addr, _ := m["address"].(string)
				out = append(out, bssChange{Address: addr, Actions: bsStrings(ch["actions"])})
			}
			return out, doc, true
		}
		switch sub {
		case "init":
			call.Cmd = "init"
			if err := os.MkdirAll(dataDir, 0o700); err != nil || os.WriteFile(marker, []byte("local "+statePath), 0o600) != nil {
				return fail("cannot write the data directory")
			}
			return done(0)
		case "plan":
			call.Cmd = "plan"
			if c := needs(); c != 0 {
				return c
			}
			if w.OtherRunStateUser && st.User == "" {
				cloud.Users["4800"] = bssUser{Account: w.Account, Project: w.Project, Bucket: w.Bucket, Description: w.UserDescription,
					AccessKey: "FAKEAK0800", Secret: "fakeS3Secret0800Qx7v"}
				cloud.Events = append(cloud.Events, "other run creates user 4800")
				st.User = "4800"
				saveCloud()
				if c := saveState(); c != 0 {
					return c
				}
			}
			pf := bssPlanFile{Root: call.Root, Destroy: has("-destroy"), Nonce: bssSHA(fmt.Sprint(os.Getpid(), statePath, len(cloud.Events), os.Args))[:16]}
			if ch, _, ok := recorded(); ok {
				pf.Changes, pf.Recorded = ch, true
			} else {
				pf.Changes = compute(pf.Destroy)
			}
			call.Nonce = pf.Nonce
			if out, ok := flag("-out"); ok {
				call.Plan = resolve(out)
				if bssSave(call.Plan, pf) != nil {
					return fail("cannot write the plan file")
				}
			}
			fmt.Println("Plan: fake.")
			return done(0)
		case "show":
			call.Plan = resolve(strings.Join(positional, ""))
			var pf bssPlanFile
			if bssLoad(call.Plan, &pf) != nil || pf.Root != call.Root {
				call.Cmd = "show"
				return fail("not a saved plan file")
			}
			call.Nonce = pf.Nonce
			if !has("-json") {
				call.Cmd = "show"
				fmt.Printf("fake rendered plan, token=%s\n", os.Getenv("OVH_CLIENT_SECRET"))
				return done(0)
			}
			call.Cmd = "show-json"
			if w.FailShow {
				return fail("fake show failure")
			}
			if _, doc, ok := recorded(); ok && pf.Recorded {
				raw, _ := json.Marshal(doc)
				fmt.Println(string(raw))
				return done(0)
			}
			var rcs []any
			for _, c := range pf.Changes {
				rcs = append(rcs, map[string]any{"address": c.Address, "mode": "managed", "type": bssTypes[c.Address], "name": "this",
					"change": map[string]any{"actions": c.Actions}})
			}
			plan := map[string]any{"format_version": "1.2", "terraform_version": "1.13.0", "planned_values": map[string]any{},
				"resource_changes": rcs, "errored": false}
			if w.OutputChange {
				plan["output_changes"] = map[string]any{"state_endpoint": map[string]any{"actions": []string{"update"},
					"before": "https://s3.old.example", "after": "https://s3." + strings.ToLower(w.Region) + ".io.cloud.ovh.net",
					"after_unknown": false, "before_sensitive": false, "after_sensitive": false}}
			}
			raw, _ := json.Marshal(plan)
			fmt.Println(string(raw))
			return done(0)
		case "apply", "destroy":
			call.Cmd = sub
			if c := needs(); c != 0 {
				return c
			}
			var changes []bssChange
			if len(positional) > 0 {
				call.Plan = resolve(positional[0])
				var pf bssPlanFile
				if bssLoad(call.Plan, &pf) != nil || pf.Root != call.Root {
					return fail("not a saved plan file")
				}
				call.Nonce, changes = pf.Nonce, pf.Changes
			} else if has("-auto-approve") {
				changes = compute(sub == "destroy" || has("-destroy"))
			} else {
				return fail("apply needs a saved plan or -auto-approve")
			}
			bucketOps := []bssChange{}
			userOps := []bssChange{}
			for _, c := range changes {
				if c.Address == bssBucketAddr {
					bucketOps = append(bucketOps, c)
				} else if c.Address == bssUserAddr {
					userOps = append(userOps, c)
				}
			}
			order := append(userOps, bucketOps...)
			if w.BucketFirst {
				order = append(bucketOps, userOps...)
			}
			if w.OtherRunUser {
				cloud.Users["4800"] = bssUser{Account: w.Account, Project: w.Project, Bucket: w.Bucket, Description: w.UserDescription,
					AccessKey: "FAKEAK0800", Secret: "fakeS3Secret0800Qx7v"}
				cloud.Events = append(cloud.Events, "other run creates user 4800")
			}
			creates := 0
			for _, c := range order {
				for _, a := range c.Actions {
					switch {
					case a == "delete" && c.Address == bssBucketAddr && st.Bucket != "":
						delete(cloud.Buckets, st.Bucket)
						_ = os.RemoveAll(filepath.Join(w.S3, st.Bucket))
						cloud.Events = append(cloud.Events, "delete bucket "+st.Bucket)
						st.Bucket = ""
					case a == "delete" && c.Address == bssUserAddr && st.User != "":
						delete(cloud.Users, st.User)
						cloud.Events = append(cloud.Events, "delete user "+st.User)
						st.User = ""
					case a == "create" && c.Address == bssBucketAddr:
						if _, taken := cloud.Buckets[w.Bucket]; taken {
							saveCloud()
							saveState()
							code := "(status code 409)"
							if w.WrapConflict {
								code = "(status code\n409)"
							}
							return fail("calling Post /cloud/project/%s/region/%s/storage: OVHcloud API error %s: Client::Conflict: bucket name %q is already in use (client_secret=%s)",
								w.Project, w.Region, code, w.Bucket, os.Getenv("OVH_CLIENT_SECRET"))
						}
						if w.MixedErrors {
							saveCloud()
							saveState()
							fmt.Fprintf(os.Stderr, "Error: calling Post /cloud/project/%s/user/%s/policy: OVHcloud API error (status code 409): Client::Conflict\n", w.Project, st.User)
							return fail("calling Post /cloud/project/%s/region/%s/storage: OVHcloud API error (status code 500): Server::InternalServerError", w.Project, w.Region)
						}
						cloud.Buckets[w.Bucket] = bssBucketRec{Account: w.Account, Project: w.Project, Region: w.Region}
						cloud.Events = append(cloud.Events, "create bucket "+w.Bucket)
						st.Bucket = w.Bucket
						creates++
					case a == "create" && c.Address == bssUserAddr:
						cloud.Next++
						id := strconv.Itoa(4700 + cloud.Next)
						cloud.Users[id] = bssUser{Account: w.Account, Project: w.Project, Bucket: w.Bucket, Description: w.UserDescription,
							AccessKey: fmt.Sprintf("FAKEAK%04d", cloud.Next), Secret: fmt.Sprintf("fakeS3Secret%04dQx7v", cloud.Next)}
						cloud.Events = append(cloud.Events, "create user "+id)
						st.User = id
						creates++
					}
					if w.FailAfter > 0 && creates >= w.FailAfter && a == "create" {
						saveCloud()
						saveState()
						return fail("fake apply interrupted after %d created resources", creates)
					}
				}
			}
			saveCloud()
			if c := saveState(); c != 0 {
				return c
			}
			if w.PolicyConflict && creates > 0 {
				return fail("calling Post /cloud/project/%s/user/%s/policy with params: OVHcloud API error (status code 409): Client::Conflict: policy already exists",
					w.Project, st.User)
			}
			fmt.Println("Apply complete! (fake)")
			return done(0)
		case "import":
			call.Cmd = "import"
			if c := needs(); c != 0 {
				return c
			}
			if len(positional) != 2 || positional[0] != bssBucketAddr {
				return fail("fake import: only the account bucket, got %v", positional)
			}
			if st.Bucket != "" {
				return fail("Resource already managed by OpenTofu")
			}
			parts := strings.Split(positional[1], "/")
			b, ok := cloud.Buckets[parts[len(parts)-1]]
			if len(parts) != 3 || !ok || b.Project != parts[0] || b.Region != parts[1] {
				return fail("Cannot import non-existent remote object %s", positional[1])
			}
			st.Bucket = parts[2]
			cloud.Events = append(cloud.Events, "import bucket "+positional[1])
			saveCloud()
			if c := saveState(); c != 0 {
				return c
			}
			return done(0)
		case "output":
			call.Cmd = "output"
			if !initialised() {
				return fail(`Backend initialization required, please run "tofu init"`)
			}
			u, okU := cloud.Users[st.User]
			if st.Bucket == "" || !okU || !has("-json") {
				fmt.Println("{}")
				return done(0)
			}
			str := func(v string) map[string]any { return map[string]any{"sensitive": false, "type": "string", "value": v} }
			out := map[string]any{
				"state_bucket":        str(st.Bucket),
				"state_project_id":    str(w.Project),
				"state_region":        str(w.Region),
				"state_endpoint":      str("https://s3." + strings.ToLower(w.Region) + ".io.cloud.ovh.net"),
				"platform_s3_user_id": str(st.User),
				"unlabelled": map[string]any{"sensitive": false, "type": []any{"tuple", []any{"string", "string", "string"}}, "value": []string{
					strings.TrimPrefix(bssUserAddr, "module.bootstrap."),
					strings.TrimPrefix(bssCredAddr, "module.bootstrap."),
					strings.TrimPrefix(bssPolicyAddr, "module.bootstrap.")}},
				"platform_s3": map[string]any{"sensitive": true, "type": []any{"object", map[string]any{"access_key_id": "string", "secret_access_key": "string"}},
					"value": map[string]any{"access_key_id": u.AccessKey, "secret_access_key": u.Secret}},
			}
			raw, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(raw))
			return done(0)
		case "state list":
			call.Cmd = "state-list"
			if !initialised() {
				return fail(`Backend initialization required, please run "tofu init"`)
			}
			// As OpenTofu does on a local backend whose file does not exist yet (observed with
			// tofu 1.10.3 on the host, 2026-10-08: exit 1; the pinned 1.13.0 believed the same).
			if _, err := os.Stat(statePath); err != nil {
				return fail("No state file was found!")
			}
			if st.Bucket != "" {
				fmt.Println(bssBucketAddr)
			}
			if st.User != "" {
				fmt.Println(bssUserAddr + "\n" + bssCredAddr + "\n" + bssPolicyAddr)
			}
			return done(0)
		case "state rm":
			call.Cmd = "state-rm"
			if !initialised() {
				return fail(`Backend initialization required, please run "tofu init"`)
			}
			if _, err := os.Stat(statePath); err != nil {
				return fail("No state file was found!")
			}
			for _, a := range positional {
				if a != bssUserModule || st.User == "" {
					return fail("Invalid target address %s", a)
				}
				st.User = ""
			}
			if c := saveState(); c != 0 {
				return c
			}
			return done(0)
		}
		return done(0)
	case "account-governance":
		s3OK := func() bool {
			b, ok := cloud.Buckets[w.Bucket]
			if !ok || b.Account != w.Account {
				return false
			}
			for _, u := range cloud.Users {
				if u.Bucket == w.Bucket && u.AccessKey == os.Getenv("AWS_ACCESS_KEY_ID") && u.Secret == os.Getenv("AWS_SECRET_ACCESS_KEY") {
					return true
				}
			}
			return false
		}
		switch sub {
		case "init":
			call.Cmd = "init"
			if !s3OK() {
				return fail("Failed to get existing workspaces: S3 bucket %q: AccessDenied (s3=%s)", w.Bucket, os.Getenv("AWS_SECRET_ACCESS_KEY"))
			}
			if err := os.MkdirAll(dataDir, 0o700); err != nil || os.WriteFile(marker, []byte("s3 "+w.Bucket), 0o600) != nil {
				return fail("cannot write the data directory")
			}
			return done(0)
		case "plan", "apply", "destroy", "import":
			call.Cmd = sub
			if !initialised() {
				return fail(`Backend initialization required, please run "tofu init"`)
			}
			if !s3OK() || !providerOK() {
				return fail("credentials refused")
			}
			if sub != "plan" {
				return fail("fake: the bootstrap never changes account-governance")
			}
			// The required `tenants` input (no default in the generated _lz_variables.tf): from a
			// -var-file (the adapter's resolved input) or TF_VAR_tenants.
			var tenants map[string]struct {
				ProjectID  string `json:"project_id"`
				ProjectURN string `json:"project_urn"`
			}
			found := false
			if v := os.Getenv("TF_VAR_tenants"); v != "" {
				found = json.Unmarshal([]byte(v), &tenants) == nil
			}
			for i, a := range rest {
				f, ok := strings.CutPrefix(a, "-var-file=")
				if !ok && a == "-var-file" && i+1 < len(rest) {
					f, ok = rest[i+1], true
				}
				var doc map[string]json.RawMessage
				if ok && bssLoad(resolve(f), &doc) == nil && doc["tenants"] != nil {
					found = json.Unmarshal(doc["tenants"], &tenants) == nil
				}
			}
			if !found {
				return fail(`No value for required variable "tenants"`)
			}
			for name, project := range w.Tenants {
				got := tenants[name]
				if got.ProjectID != project || got.ProjectURN != "urn:v1:eu:resource:publicCloudProject:"+project {
					return fail("Invalid value for variable \"tenants\": tenant %s", name)
				}
			}
			lock := filepath.Join(w.S3, w.Bucket, filepath.FromSlash(bssLock))
			lockOff := false
			if v, ok := flag("-lock"); ok {
				b, err := strconv.ParseBool(v)
				lockOff = err != nil || !b
			}
			for _, k := range []string{"TF_CLI_ARGS", "TF_CLI_ARGS_plan"} {
				lockOff = lockOff || strings.Contains(os.Getenv(k), "-lock")
			}
			if lockOff {
				call.Args = append(call.Args, "-lock=false")
			}
			if !lockOff {
				if _, err := os.Stat(lock); err == nil {
					return fail("Error acquiring the state lock: the lock object %s exists", bssLock)
				}
				if os.MkdirAll(filepath.Dir(lock), 0o700) != nil || os.WriteFile(lock, []byte(`{"ID":"fake"}`), 0o600) != nil {
					return fail("cannot write the lock object")
				}
				call.Locked = true
				defer os.Remove(lock)
			}
			var gs bssLocal
			if raw, err := os.ReadFile(filepath.Join(w.S3, w.Bucket, filepath.FromSlash(bssGovState))); err == nil {
				if json.Unmarshal(raw, &gs) != nil || gs.PassSHA != pass {
					return fail("decrypting the state of account-governance: the passphrase does not match")
				}
			}
			if out, ok := flag("-out"); ok {
				call.Plan = resolve(out)
				if bssSave(call.Plan, bssPlanFile{Root: call.Root, Nonce: "governance"}) != nil {
					return fail("cannot write the plan file")
				}
			}
			fmt.Println("Plan: fake account-governance.")
			return done(0)
		}
		return done(0)
	}
	return fail("fake tofu: unknown root %s", call.Root)
}

// ---------------------------------------------------------------- harness

type bssHarness struct {
	*bsHarness
	world     bssWorld
	worldPath string
	m         *stacks.Manifest
	state     *BootstrapState // the last run's phases
	tmp       string          // TMPDIR of the runs
	outFrom   int             // stdout offset of the current run
	callsFrom int             // fake tofu calls before the current run
	evFrom    int             // cloud events before the current run
	stores    []string        // access key of every store opened
	// storeFor, when set, opens the account bucket instead of bssStore (T089: the S3 store against
	// a fake S3 endpoint).
	storeFor func(keys map[string]string) (ObjectStore, error)
}

// newBSSHarness is T042's harness with the fake tofu, the fake cloud and the checkout's generated
// roots and manifest (copied from the repository), for the current sandbox account by default.
func newBSSHarness(t *testing.T) *bssHarness {
	t.Helper()
	h := &bssHarness{bsHarness: newBSHarness(t)}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tofu := filepath.Join(h.bin, "tofu")
	if err := os.Remove(tofu); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, tofu); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(h.base, "fake")
	if err := os.MkdirAll(filepath.Join(fake, "s3"), 0o700); err != nil {
		t.Fatal(err)
	}
	h.tmp = filepath.Join(h.base, "tmp")
	bsMkdirPrivate(t, h.tmp)
	t.Setenv("TMPDIR", h.tmp)
	for _, rel := range []string{"stacks/deployments.yaml", "stacks/account/bootstrap", "stacks/account/account-governance"} {
		bssCopy(t, filepath.Join(bssRepoRoot, rel), filepath.Join(h.checkout, rel))
	}
	raw, err := os.ReadFile(filepath.Join(h.checkout, stacks.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	if h.m, err = stacks.DecodeManifest(raw); err != nil {
		t.Fatal(err)
	}
	h.worldPath = filepath.Join(h.bin, bssWorldFile)
	h.world = bssWorld{Account: bsOldAccount, Project: bsOldProject, Region: bssRegion, Bucket: bssBucket,
		Cloud: filepath.Join(fake, "cloud.json"), S3: filepath.Join(fake, "s3"), Log: filepath.Join(fake, "tofu.jsonl"),
		Tenants:         map[string]string{"demo": bsOldProject}, // KD-1: DEMO_DEV is the state project
		UserDescription: bssCapturedUserDescription(t)}
	h.saveWorld()
	if err := bssSave(h.world.Cloud, bssCloud{Buckets: map[string]bssBucketRec{}, Users: map[string]bssUser{}}); err != nil {
		t.Fatal(err)
	}
	h.api.routes = h.storageRoutes
	return h
}

// bssCopy copies a file, or a directory's regular files (not its subdirectories).
func bssCopy(t *testing.T, from, to string) {
	t.Helper()
	fi, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	files := []string{from}
	if fi.IsDir() {
		entries, err := os.ReadDir(from)
		if err != nil {
			t.Fatal(err)
		}
		files = nil
		for _, e := range entries {
			if e.Type().IsRegular() {
				files = append(files, filepath.Join(from, e.Name()))
			}
		}
	}
	for _, f := range files {
		dst := to
		if fi.IsDir() {
			dst = filepath.Join(to, filepath.Base(f))
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *bssHarness) saveWorld() {
	h.t.Helper()
	if err := bssSave(h.worldPath, h.world); err != nil {
		h.t.Fatal(err)
	}
}

func (h *bssHarness) cloud() bssCloud {
	h.t.Helper()
	var c bssCloud
	if err := bssLoad(h.world.Cloud, &c); err != nil {
		h.t.Fatal(err)
	}
	return c
}

func (h *bssHarness) setCloud(c bssCloud) {
	h.t.Helper()
	if err := bssSave(h.world.Cloud, c); err != nil {
		h.t.Fatal(err)
	}
}

// storageRoutes answers the storage reads and the project S3 user routes from the fake cloud
// (kb/api/v1/cloud.json).
func (h *bssHarness) storageRoutes(acct *bsAccount, client *bsClient, may func(string) bool, method, path string) (int, []byte, bool) {
	rest, ok := strings.CutPrefix(path, "/v1/cloud/project/")
	if !ok {
		return 0, nil, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) >= 2 && parts[1] == "user" {
		return h.userRoutes(acct, client, may, method, parts)
	}
	if method != http.MethodGet {
		return 0, nil, false
	}
	if len(parts) < 4 || parts[1] != "region" || parts[3] != "storage" || len(parts) > 5 {
		return 0, nil, false
	}
	project, region := parts[0], parts[2]
	if client == nil {
		// The state phases run as the admin client, never with the root keys (R13).
		h.issue("storage read %s with the root keys", path)
		return http.StatusForbidden, h.api.errBody("root keys refused by the fake"), true
	}
	if !slices.Contains(acct.Projects, project) {
		return http.StatusNotFound, h.api.errBody("project not found"), true
	}
	if !may("publicCloudProject:apiovh:region/storage/get") {
		return http.StatusForbidden, h.api.errBody("not allowed"), true
	}
	var c bssCloud
	if bssLoad(h.world.Cloud, &c) != nil {
		return http.StatusInternalServerError, h.api.errBody("fake cloud"), true
	}
	container := func(name string) map[string]any {
		return map[string]any{"name": name, "region": region, "createdAt": "2026-10-08T07:00:00Z", "objectsCount": 0, "objectsSize": 0,
			"ownerId": 0, "tags": map[string]string{}, "virtualHost": name + ".s3." + strings.ToLower(region) + ".io.cloud.ovh.net"}
	}
	if len(parts) == 5 {
		b, found := c.Buckets[parts[4]]
		if !found || b.Project != project || b.Region != region {
			return http.StatusNotFound, h.api.errBody("container not found"), true
		}
		raw, _ := json.Marshal(container(parts[4]))
		return http.StatusOK, raw, true
	}
	list := []map[string]any{}
	for name, b := range c.Buckets {
		if b.Project == project && b.Region == region {
			list = append(list, container(name))
		}
	}
	raw, _ := json.Marshal(list)
	return http.StatusOK, raw, true
}

// userRoutes: GET /cloud/project/{serviceName}/user (cloud.user.User[], id long; IAM
// publicCloudProject:apiovh:user/get) and DELETE /cloud/project/{serviceName}/user/{userId} (IAM
// publicCloudProject:apiovh:user/delete), kb/api/v1/cloud.json. A delete is a cloud event.
func (h *bssHarness) userRoutes(acct *bsAccount, client *bsClient, may func(string) bool, method string, parts []string) (int, []byte, bool) {
	project := parts[0]
	if client == nil {
		h.issue("user route %s %v with the root keys", method, parts)
		return http.StatusForbidden, h.api.errBody("root keys refused by the fake"), true
	}
	if !slices.Contains(acct.Projects, project) {
		return http.StatusNotFound, h.api.errBody("project not found"), true
	}
	var c bssCloud
	if bssLoad(h.world.Cloud, &c) != nil {
		return http.StatusInternalServerError, h.api.errBody("fake cloud"), true
	}
	switch {
	case method == http.MethodGet && len(parts) == 2:
		if !may("publicCloudProject:apiovh:user/get") {
			return http.StatusForbidden, h.api.errBody("not allowed"), true
		}
		list := []map[string]any{}
		for _, id := range slices.Sorted(maps.Keys(c.Users)) {
			u := c.Users[id]
			if u.Account != acct.Account || u.Project != project {
				continue
			}
			n, _ := strconv.Atoi(id)
			list = append(list, map[string]any{"id": n, "description": u.Description, "username": "user-" + id,
				"status": "ok", "creationDate": "2026-10-08T07:00:00Z", "openstackId": "os-" + id, "roles": []any{}})
		}
		raw, _ := json.Marshal(list)
		return http.StatusOK, raw, true
	case method == http.MethodDelete && len(parts) == 3:
		if !may("publicCloudProject:apiovh:user/delete") {
			return http.StatusForbidden, h.api.errBody("not allowed"), true
		}
		u, found := c.Users[parts[2]]
		if !found || u.Account != acct.Account || u.Project != project {
			return http.StatusNotFound, h.api.errBody("user not found"), true
		}
		delete(c.Users, parts[2])
		c.Events = append(c.Events, "delete user "+parts[2])
		if bssSave(h.world.Cloud, c) != nil {
			return http.StatusInternalServerError, h.api.errBody("fake cloud"), true
		}
		return http.StatusOK, []byte("null"), true
	}
	return 0, nil, false
}

// bssCapturedUserDescription is the platform S3 user's description in the plan pinned OpenTofu
// 1.13.0 made of the bootstrap stage (org lz).
func bssCapturedUserDescription(t *testing.T) string {
	t.Helper()
	var capture struct {
		TestPlan struct {
			ResourceChanges []struct {
				Address string `json:"address"`
				Change  struct {
					After map[string]any `json:"after"`
				} `json:"change"`
			} `json:"resource_changes"`
		} `json:"test_plan"`
	}
	readJSON(t, filepath.Join(bssRepoRoot, "tests/fixtures/outputs/captures/bootstrap-plan.json"), &capture)
	for _, rc := range capture.TestPlan.ResourceChanges {
		if "module.bootstrap."+rc.Address == bssUserAddr {
			if d, _ := rc.Change.After["description"].(string); d != "" {
				return d
			}
		}
	}
	t.Fatal("no platform S3 user description in the captured bootstrap plan")
	return ""
}

// bssStore is the account bucket as state.env's S3 keys reach it: a key pair of a user bound to
// the bucket, else AccessDenied (the error repeats the secret it was given).
type bssStore struct {
	h      *bssHarness
	ak, sk string
}

func (s bssStore) allowed(bucket string) error {
	var c bssCloud
	if err := bssLoad(s.h.world.Cloud, &c); err != nil {
		return err
	}
	if b, ok := c.Buckets[bucket]; ok && b.Account == s.h.world.Account {
		for _, u := range c.Users {
			if u.Bucket == bucket && u.AccessKey == s.ak && u.Secret == s.sk {
				return nil
			}
		}
	}
	return fmt.Errorf("AccessDenied: %s with key %s/%s", bucket, s.ak, s.sk)
}

func (s bssStore) Put(bucket, key string, data []byte) error {
	if err := s.allowed(bucket); err != nil {
		return err
	}
	p := filepath.Join(s.h.world.S3, bucket, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func (s bssStore) Get(bucket, key string) ([]byte, error) {
	if err := s.allowed(bucket); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(s.h.world.S3, bucket, filepath.FromSlash(key)))
}

func (h *bssHarness) stateOptions() StateOptions {
	return StateOptions{
		Checkout: h.checkout,
		Manifest: h.m,
		Region:   bssRegion,
		Schemas:  os.DirFS(filepath.Join(bssRepoRoot, "schemas", "outputs")),
		Revision: bssRevision,
		Tofu:     filepath.Join(h.bin, "tofu"),
		API:      h.api.API(),
		Store: func(keys map[string]string) (ObjectStore, error) {
			h.mu.Lock()
			h.stores = append(h.stores, keys["AWS_ACCESS_KEY_ID"])
			h.mu.Unlock()
			if h.storeFor != nil {
				return h.storeFor(keys)
			}
			return bssStore{h: h, ak: keys["AWS_ACCESS_KEY_ID"], sk: keys["AWS_SECRET_ACCESS_KEY"]}, nil
		},
		Stdout: h.stdout,
	}
}

// run runs the bootstrap with the state phases as Rest (after T042's own Rest checks: passphrase
// present, no root key in the environment or handed over) and returns every phase's result in run
// order: Bootstrap's, the state phases' before revoke.
func (h *bssHarness) run(fresh bool) ([]PhaseResult, error) {
	h.t.Helper()
	h.saveWorld()
	h.outFrom = len(h.stdout.String())
	h.callsFrom = len(h.calls())
	h.evFrom = len(h.cloud().Events)
	h.state = NewBootstrapState(h.stateOptions())
	h.stateFn = func(ctx context.Context) error {
		rest, _ := h.bsHarness.Rest()
		return h.state.Rest(ctx, rest[len(rest)-1])
	}
	rs, err := h.bsHarness.run(fresh)
	var out []PhaseResult
	for _, r := range rs {
		if r.Phase == PhaseRevoke {
			out = append(out, h.state.Results()...)
		}
		out = append(out, r)
	}
	if !slices.ContainsFunc(rs, func(r PhaseResult) bool { return r.Phase == PhaseRevoke }) {
		out = append(out, h.state.Results()...)
	}
	// Every run, setup and interrupted runs included: its own applies guarded, nothing leaked.
	bssGuarded(h.t, h.runCalls())
	bssNoLeak(h.t, h, out, err)
	bssNoDeployerFile(h.t, h)
	return out, err
}

// mustRun runs without --fresh-account and fails the test unless the run passes.
func (h *bssHarness) mustRun() []PhaseResult {
	h.t.Helper()
	rs, err := h.run(false)
	if err != nil {
		h.t.Fatalf("setup run: %v", err)
	}
	return rs
}

// calls lists every fake tofu call so far.
func (h *bssHarness) calls() []bssCall {
	h.t.Helper()
	raw, err := os.ReadFile(h.world.Log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	var out []bssCall
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l == "" {
			continue
		}
		var c bssCall
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// runCalls lists the fake tofu calls of the current run.
func (h *bssHarness) runCalls() []bssCall { return h.calls()[h.callsFrom:] }

// runEvents lists the cloud events of the current run.
func (h *bssHarness) runEvents() []string { return h.cloud().Events[h.evFrom:] }

// lines returns the LZ-LIVE phase lines of the current run as "phase status", in order.
func (h *bssHarness) lines() []string {
	var out []string
	for _, l := range strings.Split(h.stdout.String()[h.outFrom:], "\n") {
		f := strings.Fields(l)
		// A phase's result line; `info` lines (identify's project listing, the move) are not results.
		if len(f) >= 4 && f[0] == "LZ-LIVE" && f[2] == "bootstrap" && f[3] != "info" {
			out = append(out, f[1]+" "+f[3])
		}
	}
	return out
}

func (h *bssHarness) linePhases() []string {
	var out []string
	for _, l := range h.lines() {
		out = append(out, strings.Fields(l)[0])
	}
	return out
}

func (h *bssHarness) accountPath(rel string) string {
	return filepath.Join(h.accountDir(h.world.Account), rel)
}

func (h *bssHarness) object(key string) ([]byte, bool) {
	raw, err := os.ReadFile(filepath.Join(h.world.S3, h.world.Bucket, filepath.FromSlash(key)))
	return raw, err == nil
}

// secrets lists every secret the runs may hold, by what it is: the admin secrets, every
// passphrase, every S3 secret of the cloud, the root keys.
func (h *bssHarness) secrets() map[string]string {
	out := map[string]string{bsOldSecret: "admin secret"}
	created, _ := h.api.Created()
	for _, c := range created {
		out[c.Secret] = "admin secret"
	}
	for _, a := range []string{bsOldAccount, bsNewAccount} {
		if vals, err := ReadCredentialFile(filepath.Join(h.accountDir(a), "state-passphrase.env")); err == nil {
			if vals[bsPassphrase] != "" {
				out[vals[bsPassphrase]] = "passphrase"
			}
		}
	}
	for _, u := range h.cloud().Users {
		out[u.Secret] = "S3 secret"
	}
	for _, s := range h.rootSecrets() {
		out[s] = "root key"
	}
	return out
}

// allowedFiles: where a secret may be written (data-model *Account binding and local files*).
var bssAllowed = map[string][]string{
	"admin secret": {"sandbox.env", "accounts/" + bsOldAccount + "/sandbox.env"},
	"passphrase":   {"accounts/" + bsOldAccount + "/state-passphrase.env", "accounts/" + bsNewAccount + "/state-passphrase.env"},
	"S3 secret":    {"accounts/" + bsOldAccount + "/state.env", "accounts/" + bsNewAccount + "/state.env"},
}

// ---------------------------------------------------------------- assertions

// bssNoLeak (G2, G3, SC-005): no secret on the run's stdout, the process's stdout or stderr, the
// error or the phase results; in no child's argv; no root key in a child's environment; no
// secret in any file the run could write (config root outside the allowed files, the checkout,
// TMPDIR, the bucket objects); no recorded child but tofu with a secret in argv; and T042's
// harness saw nothing wrong (API wire, state-phase environment).
func bssNoLeak(t *testing.T, h *bssHarness, rs []PhaseResult, runErr error) {
	t.Helper()
	secrets := h.secrets()
	streams := map[string]string{"stdout": h.stdout.String(), "phase results": fmt.Sprintf("%+v", rs)}
	for _, name := range []string{"process-stdout.txt", "process-stderr.txt"} {
		raw, _ := os.ReadFile(filepath.Join(h.base, name))
		streams[name] = string(raw)
	}
	if runErr != nil {
		streams["error"] = runErr.Error()
	}
	for name, text := range streams {
		for s, kind := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("a %s (%.6s…) reached the %s", kind, s, name)
			}
		}
	}
	for _, c := range h.calls() {
		argv := strings.Join(c.Args, " ")
		env := strings.Join(c.Env, "\n")
		for s, kind := range secrets {
			if strings.Contains(argv, s) {
				t.Errorf("a %s (%.6s…) in the argv of tofu %s (%s)", kind, s, c.Cmd, c.Root)
			}
			if kind == "root key" && strings.Contains(env, s) {
				t.Errorf("a root key in the environment of tofu %s (%s)", c.Cmd, c.Root)
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(h.bin, "children.log")); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(l, "argv: ") && !strings.Contains(l, "/ovhcloud ") && !strings.HasSuffix(l, "/ovhcloud") {
				t.Errorf("a child other than tofu and ovhcloud ran: %s", strings.Fields(l)[1])
			}
			for s, kind := range secrets {
				if strings.Contains(l, s) && (strings.HasPrefix(l, "argv: ") || kind == "root key") {
					t.Errorf("a %s (%.6s…) reached a recorded child (%.40s)", kind, s, l)
				}
			}
		}
	}
	scan := func(dir string, rel func(string) string) {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for s, kind := range secrets {
				if strings.Contains(string(raw), s) && !slices.Contains(bssAllowed[kind], rel(p)) {
					t.Errorf("a %s (%.6s…) was written to %s", kind, s, p)
				}
			}
			return nil
		})
	}
	none := func(string) string { return "" }
	scan(filepath.Join(h.base, "home"), func(p string) string { r, _ := filepath.Rel(h.root, p); return r })
	// The checkout outside the config root (TestBootstrapStateCredentialInRepo nests the root in it;
	// the root's own files are judged above).
	scan(h.checkout, func(p string) string {
		if within(p, h.root) {
			r, _ := filepath.Rel(h.root, p)
			return r
		}
		return ""
	})
	scan(h.tmp, none)
	scan(h.world.S3, none)
	if issues := h.api.Issues(); len(issues) > 0 {
		t.Errorf("protocol issues on the wire:\n%s", strings.Join(issues, "\n"))
	}
	if _, issues := h.bsHarness.Rest(); len(issues) > 0 {
		t.Errorf("harness saw (state phase environment, admin requests):\n%s", strings.Join(issues, "\n"))
	}
}

// bssGuarded (G7, FR-011): on the bootstrap root nothing is destroyed and every apply applies a
// saved plan file that a plan of the same run wrote and that `show -json` read after that plan
// and before the apply (the document the guard judges); no apply without a plan file, no
// -auto-approve on an unchecked plan, no -destroy.
func bssGuarded(t *testing.T, calls []bssCall) {
	t.Helper()
	for i, c := range calls {
		if c.Root != "bootstrap" {
			continue
		}
		if c.Cmd == "destroy" || slices.Contains(c.Args, "-destroy") {
			t.Errorf("tofu %s %v on the bootstrap root: the retained bucket and S3 user are never destroyed", c.Cmd, c.Args)
		}
		if c.Cmd != "apply" {
			continue
		}
		if c.Plan == "" || c.Nonce == "" {
			t.Errorf("apply %v without a saved plan file: unchecked", c.Args)
			continue
		}
		planned := slices.IndexFunc(calls[:i], func(p bssCall) bool { return p.Cmd == "plan" && p.Root == c.Root && p.Nonce == c.Nonce })
		if planned < 0 {
			t.Errorf("apply of plan %s that no plan of this run wrote", c.Plan)
			continue
		}
		if !slices.ContainsFunc(calls[planned+1:i], func(s bssCall) bool { return s.Cmd == "show-json" && s.Nonce == c.Nonce && s.Exit == 0 }) {
			t.Errorf("apply of plan %s that `show -json` did not read before it: the guard never saw it", c.Plan)
		}
	}
}

// bssNeverDestroyed: no event of the whole test deletes a retained resource, the account bucket
// or the platform S3 user (FR-004).
func bssNeverDestroyed(t *testing.T, h *bssHarness) {
	t.Helper()
	for _, e := range h.cloud().Events {
		if strings.HasPrefix(e, "delete ") {
			t.Errorf("cloud event %q: a retained resource (account bucket, platform S3 user) was destroyed (FR-004)", e)
		}
	}
}

func bssCount(events []string, prefix string) int {
	n := 0
	for _, e := range events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// bssNoTofu: the run started no tofu at all (a phase before state stopped it).
func bssNoTofu(t *testing.T, h *bssHarness) {
	t.Helper()
	if calls := h.runCalls(); len(calls) > 0 {
		t.Errorf("tofu ran (%s %s, …) though the run stopped before the state phase", calls[0].Root, calls[0].Cmd)
	}
}

// bssStateEnv checks state.env (0600 in a 0700 directory) against the platform S3 user of the
// bucket and returns it.
func bssStateEnv(t *testing.T, h *bssHarness) map[string]string {
	t.Helper()
	p := h.accountPath("state.env")
	fi, err := os.Stat(p)
	if err != nil {
		t.Errorf("state.env: %v", err)
		return nil
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("state.env mode %04o, want 0600", fi.Mode().Perm())
	}
	if di, err := os.Stat(filepath.Dir(p)); err != nil || di.Mode().Perm() != 0o700 {
		t.Errorf("account directory not 0700")
	}
	vals := readEnvFile(t, p)
	ok := slices.ContainsFunc(slices.Collect(maps.Values(h.cloud().Users)), func(u bssUser) bool {
		return u.Bucket == h.world.Bucket && vals["AWS_ACCESS_KEY_ID"] == u.AccessKey && vals["AWS_SECRET_ACCESS_KEY"] == u.Secret
	})
	if !ok {
		t.Errorf("state.env holds %v, not the keys of an S3 user bound to %s", slices.Sorted(maps.Keys(vals)), h.world.Bucket)
	}
	return vals
}

// bssEnvelope checks the published envelope: account-bootstrap's outputs at its key in the account
// bucket, with the bootstrap's values and no sensitive output.
func bssEnvelope(t *testing.T, h *bssHarness) []byte {
	t.Helper()
	raw, ok := h.object(stacks.ArtifactKey("account-bootstrap"))
	if !ok {
		t.Errorf("no %s in %s", stacks.ArtifactKey("account-bootstrap"), h.world.Bucket)
		return nil
	}
	var env struct {
		InstanceID     string         `json:"instance_id"`
		Stage          string         `json:"stage"`
		SourceRevision string         `json:"source_revision"`
		Values         map[string]any `json:"values"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Errorf("envelope not JSON: %v", err)
		return raw
	}
	if env.InstanceID != "account-bootstrap" || env.Stage != "bootstrap" || env.SourceRevision != bssRevision {
		t.Errorf("envelope producer %s/%s/%s, want account-bootstrap/bootstrap/%s", env.InstanceID, env.Stage, env.SourceRevision, bssRevision)
	}
	if env.Values["state_bucket"] != h.world.Bucket || env.Values["state_project_id"] != h.world.Project {
		t.Errorf("envelope values %v, want bucket %s in project %s", env.Values, h.world.Bucket, h.world.Project)
	}
	if _, ok := env.Values["platform_s3"]; ok {
		t.Error("the envelope publishes the sensitive platform_s3 output")
	}
	// Current, not stale: it names the S3 user whose keys state.env holds.
	if vals, err := ReadCredentialFile(h.accountPath("state.env")); err == nil {
		for id, u := range h.cloud().Users {
			if u.AccessKey == vals["AWS_ACCESS_KEY_ID"] && env.Values["platform_s3_user_id"] != id {
				t.Errorf("envelope names S3 user %v, want %s (the user of state.env): a stale envelope", env.Values["platform_s3_user_id"], id)
			}
		}
	}
	return raw
}

// bssVerified: verify initialised and planned account-governance against the account bucket with
// the state lock taken (P1 UNVERIFIED), released afterwards, and never applied it.
func bssVerified(t *testing.T, h *bssHarness) {
	t.Helper()
	calls := h.runCalls()
	if !slices.ContainsFunc(calls, func(c bssCall) bool { return c.Root == "account-governance" && c.Cmd == "init" && c.Exit == 0 }) {
		t.Error("verify did not initialise account-governance against the account bucket")
	}
	if !slices.ContainsFunc(calls, func(c bssCall) bool {
		return c.Root == "account-governance" && c.Cmd == "plan" && c.Exit == 0 && c.Locked
	}) {
		t.Error("verify did not plan account-governance with the state lock taken")
	}
	if slices.ContainsFunc(calls, func(c bssCall) bool {
		return c.Root == "account-governance" && c.Cmd != "init" && c.Cmd != "plan" && c.Cmd != "show-json" && c.Cmd != "show" && c.Cmd != "other" && c.Cmd != "output"
	}) {
		t.Error("verify changed account-governance (it only plans)")
	}
	if _, held := h.object(bssLock); held {
		t.Errorf("the lock object %s is left in the bucket", bssLock)
	}
}

// bssCheckout snapshots the checkout: the phases write nothing inside it (G3).
func bssCheckout(t *testing.T, h *bssHarness, before map[string]string) {
	t.Helper()
	after := treeSnapshot(t, h.checkout)
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("the run wrote %s inside the checkout", k)
		} else if before[k] != after[k] {
			t.Errorf("the run changed %s inside the checkout", k)
		}
	}
}

// bssNoDeployerFile: bootstrap writes no deployer credential (data-model: those are the live
// lane's after account-governance), and nothing but the files of the table.
func bssNoDeployerFile(t *testing.T, h *bssHarness) {
	t.Helper()
	acct := "accounts/" + h.world.Account + "/"
	allowed := []string{"live.env", "sandbox.env", acct + "account.env", acct + "state-passphrase.env", acct + "state.env"}
	for _, e := range bsFiles(t, h.root) {
		rel := strings.Fields(e)[0]
		if strings.Contains(rel, "deployer") || strings.HasPrefix(rel, acct+"tenants/") {
			t.Errorf("bootstrap wrote %s: deployer files are the live lane's", rel)
			continue
		}
		if strings.HasSuffix(rel, "/") || slices.Contains(allowed, rel) || strings.HasPrefix(rel, acct+"state/") ||
			strings.HasPrefix(rel, "accounts/"+bsOldAccount+"/") && h.world.Account != bsOldAccount {
			continue
		}
		t.Errorf("bootstrap wrote %s, which is not a file of the account table", rel)
	}
}

// bssDropBucket deletes the account bucket and its objects out of band.
func bssDropBucket(h *bssHarness) {
	h.t.Helper()
	c := h.cloud()
	delete(c.Buckets, h.world.Bucket)
	h.setCloud(c)
	if err := os.RemoveAll(filepath.Join(h.world.S3, h.world.Bucket)); err != nil {
		h.t.Fatal(err)
	}
}

// bssBucketLost (coordinator decision 1): the run refuses with state-bucket-lost (exit 3), names
// the bucket and how to recover, creates or imports nothing, applies nothing, leaves state.env as
// it was, and neither publishes nor verifies.
func bssBucketLost(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
	t.Helper()
	bsExit(t, err, RefusalExit)
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Condition != "state-bucket-lost" {
		t.Errorf("err %v, want the state-bucket-lost refusal", err)
	}
	bsWantStatus(t, rs, PhaseState, StatusFail)
	r, _ := bsResult(rs, PhaseState)
	if !strings.Contains(r.Detail, h.world.Bucket) || !strings.Contains(r.Detail, "restore") {
		t.Errorf("state detail %q: want the bucket named and the recovery steps (restore …)", r.Detail)
	}
	if ev := h.runEvents(); len(ev) > 0 {
		t.Errorf("events %v: a lost bucket is never created or imported again", ev)
	}
	if slices.ContainsFunc(h.runCalls(), func(c bssCall) bool { return c.Cmd == "apply" || c.Cmd == "import" }) {
		t.Error("tofu applied or imported after the bucket was found lost")
	}
	if _, serr := os.Stat(h.accountPath("state.env")); serr != nil {
		t.Errorf("state.env: %v (it is the record of the lost bucket, kept)", serr)
	}
	for _, p := range []string{PhasePublish, PhaseVerify} {
		if r, ok := bsResult(rs, p); ok && r.Status != StatusFail {
			t.Errorf("%s %s after the lost bucket's refusal", p, r.Status)
		}
	}
}

func bssExitNonZero(t *testing.T, err error) {
	t.Helper()
	if ExitCode(err) == 0 {
		t.Errorf("exit 0 (%v), want a failure", err)
	}
}

// ---------------------------------------------------------------- recorded plans

// bssRecorded derives a plan of the generated root from the plan pinned OpenTofu 1.13.0 made of
// the bootstrap stage (tests/fixtures/outputs/captures/bootstrap-plan.json, sidecar provenance):
// every address is prefixed with the stack's module call `module.bootstrap.` and the actions of
// the named addresses are set; the rest become no-op. Returns the document's path.
func bssRecorded(t *testing.T, h *bssHarness, name string, actions map[string][]string, reason string) string {
	t.Helper()
	var capture struct {
		TestPlan map[string]any `json:"test_plan"`
	}
	readJSON(t, filepath.Join(bssRepoRoot, "tests/fixtures/outputs/captures/bootstrap-plan.json"), &capture)
	doc := capture.TestPlan
	if doc == nil || doc["format_version"] != "1.2" {
		t.Fatal("captured bootstrap plan missing")
	}
	rcs := doc["resource_changes"].([]any)
	for _, rc := range rcs {
		m := rc.(map[string]any)
		m["address"] = "module.bootstrap." + m["address"].(string)
		if ma, ok := m["module_address"].(string); ok {
			m["module_address"] = "module.bootstrap." + ma
		}
		ch := m["change"].(map[string]any)
		ch["actions"] = []string{"no-op"}
		if a, ok := actions[m["address"].(string)]; ok {
			ch["actions"] = a
			if reason != "" {
				m["action_reason"] = reason
			}
		}
	}
	for addr := range actions {
		if !slices.ContainsFunc(rcs, func(rc any) bool { return rc.(map[string]any)["address"] == addr }) {
			t.Fatalf("no %s in the captured plan", addr)
		}
	}
	p := filepath.Join(h.base, "fake", "recorded-"+name+".json")
	if err := bssSave(p, doc); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---------------------------------------------------------------- tests

// TestBootstrapStateFirstAndSecondRun (V008, T044's shape offline): on the current sandbox the
// first run reports guard, identify, passphrase, admin, state, publish, verify in that order;
// state creates the account bucket and the platform S3 user once, writes state.env (0600, the S3
// user's keys) and nothing in the checkout; publish writes the bootstrap envelope to the account
// bucket; verify plans account-governance with the lock taken and released. The second run reports
// every phase unchanged and changes nothing.
func TestBootstrapStateFirstAndSecondRun(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	before := treeSnapshot(t, h.checkout)
	rs, err := h.run(false)
	if err != nil {
		t.Errorf("first run: %v", err)
	}
	want := []string{PhaseGuard, PhaseIdentify, PhasePassphrase, PhaseAdmin, PhaseState, PhasePublish, PhaseVerify}
	if got := h.linePhases(); !slices.Equal(got, want) {
		t.Errorf("LZ-LIVE phases %v, want %v", got, want)
	}
	bsWantStatus(t, rs, PhaseState, StatusRan)
	bsWantStatus(t, rs, PhasePublish, StatusRan)
	if r, _ := bsResult(rs, PhaseVerify); r.Status != StatusRan && r.Status != StatusUnchanged {
		t.Errorf("verify %s (%s), want it to pass", r.Status, r.Detail)
	}
	ev := h.runEvents()
	if bssCount(ev, "create bucket "+bssBucket) != 1 || bssCount(ev, "create user") != 1 || len(ev) != 2 {
		t.Errorf("cloud events %v, want the account bucket and the platform S3 user created once each", ev)
	}
	calls := h.runCalls()
	for _, c := range calls {
		env := strings.Join(c.Env, "\n")
		for _, kv := range []string{"OVH_CLIENT_ID=" + bsOldClient, "TF_VAR_state_project_id=" + bsOldProject, "TF_VAR_lz_account_dir=" + h.accountDir(bsOldAccount)} {
			if c.Root == "bootstrap" && (c.Cmd == "plan" || c.Cmd == "apply" || c.Cmd == "import") && !strings.Contains(env, kv) {
				t.Errorf("tofu %s on bootstrap without %s", c.Cmd, strings.SplitN(kv, "=", 2)[0])
			}
		}
	}
	bssGuarded(t, calls)
	keys := bssStateEnv(t, h)
	envelope := bssEnvelope(t, h)
	bssVerified(t, h)
	bssCheckout(t, h, before)
	bssNoDeployerFile(t, h)
	bssNoLeak(t, h, rs, err)

	stateEnv, _ := os.ReadFile(h.accountPath("state.env"))
	rs, err = h.run(false)
	if err != nil {
		t.Errorf("second run: %v", err)
	}
	for _, p := range []string{PhaseIdentify, PhasePassphrase, PhaseAdmin, PhaseState, PhasePublish, PhaseVerify} {
		bsWantStatus(t, rs, p, StatusUnchanged)
	}
	if got := h.linePhases(); !slices.Equal(got, want) {
		t.Errorf("second run LZ-LIVE phases %v, want %v", got, want)
	}
	if ev := h.runEvents(); len(ev) > 0 {
		t.Errorf("second run changed the cloud: %v", ev)
	}
	if now, _ := os.ReadFile(h.accountPath("state.env")); string(now) != string(stateEnv) {
		t.Error("second run rewrote state.env")
	}
	if now, _ := h.object(stacks.ArtifactKey("account-bootstrap")); envelope != nil && string(now) != string(envelope) {
		t.Error("second run changed the published envelope")
	}
	if len(keys) > 0 && !slices.Contains(h.stores, keys["AWS_ACCESS_KEY_ID"]) {
		t.Error("publish did not reach the bucket with state.env's keys")
	}
	bssGuarded(t, h.runCalls())
	bssVerified(t, h)
	bssCheckout(t, h, before)
	bssNeverDestroyed(t, h)
	bssNoLeak(t, h, rs, err)
}

// TestBootstrapStateFreshAccount (V008, research R13 fresh journey): from an empty directory with
// --fresh-account the run reports every phase in order with revoke last; the state phases run as
// the created admin, never with a root key; a second run without the flag reports every phase
// unchanged.
func TestBootstrapStateFreshAccount(t *testing.T) {
	h := newBSSHarness(t)
	h.world.Account, h.world.Project = bsNewAccount, bsNewProjectA
	h.world.Tenants = map[string]string{"demo": bsNewProjectB}
	h.term.queueRoot(h.api.rootKey(bsRootFirst))
	h.answerRefs()
	before := treeSnapshot(t, h.checkout)
	rs, err := h.run(true)
	if err != nil {
		t.Errorf("fresh run: %v", err)
	}
	want := []string{PhaseGuard, PhaseIdentify, PhasePassphrase, PhaseAdmin, PhaseState, PhasePublish, PhaseVerify, PhaseRevoke}
	if got := h.linePhases(); !slices.Equal(got, want) {
		t.Errorf("LZ-LIVE phases %v, want %v", got, want)
	}
	bsWantStatus(t, rs, PhaseState, StatusRan)
	bsWantStatus(t, rs, PhasePublish, StatusRan)
	created, _ := h.api.Created()
	if len(created) != 1 {
		t.Fatalf("%d admin clients created, want 1", len(created))
	}
	for _, c := range h.runCalls() {
		if c.Root == "bootstrap" && c.Cmd == "apply" && !slices.Contains(c.Env, "OVH_CLIENT_ID="+created[0].ClientID) {
			t.Error("the state apply did not run as the created admin client")
		}
	}
	bssGuarded(t, h.runCalls())
	bssStateEnv(t, h)
	bssEnvelope(t, h)
	bssVerified(t, h)
	bssNoDeployerFile(t, h)
	bssNoLeak(t, h, rs, err)

	rs, err = h.run(false)
	if err != nil {
		t.Errorf("second run: %v", err)
	}
	for _, p := range []string{PhaseIdentify, PhasePassphrase, PhaseAdmin, PhaseState, PhasePublish, PhaseVerify} {
		bsWantStatus(t, rs, p, StatusUnchanged)
	}
	if ev := h.runEvents(); len(ev) > 0 {
		t.Errorf("second run changed the cloud: %v", ev)
	}
	bssCheckout(t, h, before)
	bssNeverDestroyed(t, h)
	bssNoLeak(t, h, rs, err)
}

// TestBootstrapStateBucketTaken (V008 negative, P21): the account bucket's name belongs to another
// account: the state phase fails with a message naming spec.org (the override), adopts and
// touches nothing of the other account, writes no state.env and publishes nothing.
func TestBootstrapStateBucketTaken(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	c := h.cloud()
	c.Buckets[bssBucket] = bssBucketRec{Account: bssForeign, Project: bssForeignPrj, Region: bssRegion}
	// A platform S3 user of an earlier, lost bootstrap: not this run's, so never deleted (decision 3).
	const earlier = "4690"
	c.Users[earlier] = bssUser{Account: h.world.Account, Project: h.world.Project, Bucket: bssBucket,
		Description: h.world.UserDescription, AccessKey: "FAKEAK0690", Secret: "fakeS3Secret0690Qx7v"}
	h.setCloud(c)
	rs, err := h.run(false)
	bssExitNonZero(t, err)
	bsWantStatus(t, rs, PhaseState, StatusFail)
	r, _ := bsResult(rs, PhaseState)
	text := r.Detail
	if err != nil {
		text += " " + err.Error()
	}
	if !strings.Contains(text, "spec.org") || !strings.Contains(text, bssBucket) {
		t.Errorf("state failure %q: want the message naming spec.org and the taken bucket %s", text, bssBucket)
	}
	for _, p := range []string{PhasePublish, PhaseVerify} {
		if r, ok := bsResult(rs, p); ok && r.Status != StatusFail && r.Status != StatusBlocked {
			t.Errorf("%s %s after a failed state phase", p, r.Status)
		}
	}
	after := h.cloud()
	if b := after.Buckets[bssBucket]; b.Account != bssForeign || b.Project != bssForeignPrj {
		t.Errorf("the other account's bucket changed: %+v", b)
	}
	if n := bssCount(after.Events, "import "); n > 0 {
		t.Errorf("events %v: a bucket of another account was imported", after.Events)
	}
	if _, err := os.Stat(h.accountPath("state.env")); err == nil {
		t.Error("state.env written though the state phase failed")
	}
	if _, ok := h.object(stacks.ArtifactKey("account-bootstrap")); ok {
		t.Error("an envelope was written into the other account's bucket")
	}
	// Coordinator decision 3 (2026-10-08): the S3 user the failed apply created before the bucket's
	// 409 is deleted again in the same run (as T043 deletes its admin client); nothing else is.
	var created []string
	for _, e := range h.runEvents() {
		if id, ok := strings.CutPrefix(e, "create user "); ok {
			created = append(created, id)
		}
	}
	if len(created) == 0 {
		t.Error("the fake apply created no S3 user before the 409: the row tests nothing")
	}
	for _, e := range h.runEvents() {
		if id, ok := strings.CutPrefix(e, "delete user "); strings.HasPrefix(e, "delete ") && (!ok || !slices.Contains(created, id)) {
			t.Errorf("cloud event %q: only the S3 user this run created may be deleted", e)
		}
	}
	for id, u := range h.cloud().Users {
		if u.Account == h.world.Account && id != earlier {
			t.Errorf("S3 user %s (%s) this run created is left behind with keys bound to %s", id, u.Description, u.Bucket)
		}
	}
	if _, ok := h.cloud().Users[earlier]; !ok {
		t.Errorf("the earlier bootstrap's S3 user %s was deleted: only this run's user may be", earlier)
	}
	if !strings.Contains(text, earlier) {
		t.Errorf("state failure %q does not name the earlier platform S3 user %s", text, earlier)
	}
	// The deleted user leaves the bootstrap state too, so the next run plans a clean create.
	if !slices.ContainsFunc(h.runCalls(), func(c bssCall) bool {
		return c.Cmd == "state-rm" && c.Exit == 0 && slices.Contains(c.Args, bssUserModule)
	}) {
		t.Errorf("the deleted S3 user was not removed from the bootstrap state (tofu state rm %s)", bssUserModule)
	}
	bssGuarded(t, h.runCalls())
	bssNoLeak(t, h, rs, err)
}

// TestBootstrapStateTakenOtherRunUser (decision 3, review r1): while the apply ran, a platform S3
// user of another run appeared in the project. Nothing proves which new user is this run's, so
// none is deleted; both are named.
func TestBootstrapStateTakenOtherRunUser(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	c := h.cloud()
	c.Buckets[bssBucket] = bssBucketRec{Account: bssForeign, Project: bssForeignPrj, Region: bssRegion}
	h.setCloud(c)
	h.world.OtherRunUser = true
	rs, err := h.run(false)
	bssExitNonZero(t, err)
	r, _ := bsResult(rs, PhaseState)
	for _, e := range h.runEvents() {
		if strings.HasPrefix(e, "delete ") {
			t.Errorf("cloud event %q: a user that cannot be attributed to this run was deleted", e)
		}
	}
	for id, u := range h.cloud().Users {
		if u.Account == h.world.Account && !strings.Contains(r.Detail, id) {
			t.Errorf("state detail %q does not name the new platform S3 user %s", r.Detail, id)
		}
	}
	bssGuarded(t, h.runCalls())
	bssNoLeak(t, h, rs, err)
}

// bssTakenWorld seeds the current sandbox with the account bucket's name owned by another account.
func bssTakenWorld(t *testing.T) *bssHarness {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	c := h.cloud()
	c.Buckets[bssBucket] = bssBucketRec{Account: bssForeign, Project: bssForeignPrj, Region: bssRegion}
	h.setCloud(c)
	return h
}

// bssNoDelete: the run deleted nothing.
func bssNoDelete(t *testing.T, h *bssHarness) {
	t.Helper()
	for _, e := range h.runEvents() {
		if strings.HasPrefix(e, "delete ") {
			t.Errorf("cloud event %q: nothing this run cannot prove its own is deleted", e)
		}
	}
}

// TestBootstrapStateTakenUserOfOtherRun (decision 3, review r2): between this run's user listing
// and its plan, another run created the platform S3 user and recorded it in the shared bootstrap
// state; this run's plan does not create it, so the taken-name failure deletes nothing.
func TestBootstrapStateTakenUserOfOtherRun(t *testing.T) {
	h := bssTakenWorld(t)
	h.world.OtherRunStateUser = true
	rs, err := h.run(false)
	bssExitNonZero(t, err)
	bssNoDelete(t, h)
	if _, ok := h.cloud().Users["4800"]; !ok {
		t.Error("the other run's S3 user 4800 was deleted")
	}
	bssGuarded(t, h.runCalls())
	bssNoLeak(t, h, rs, err)
}

// TestBootstrapStateMixedDiagnostics (review r2): the bucket create fails with a 500 while another
// diagnostic of the same apply is a 409: not a taken name, nothing deleted, no spec.org advice.
func TestBootstrapStateMixedDiagnostics(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	h.world.MixedErrors = true
	rs, err := h.run(false)
	bssExitNonZero(t, err)
	if r, _ := bsResult(rs, PhaseState); strings.Contains(r.Detail, "spec.org") {
		t.Errorf("state detail %q: a 500 on the bucket read as a taken name", r.Detail)
	}
	bssNoDelete(t, h)
	bssNoLeak(t, h, rs, err)
}

// TestBootstrapStateBucketTakenWrapped (review r2): the 409 diagnostic is word-wrapped; it is still
// the taken name, and this run's S3 user is deleted again.
func TestBootstrapStateBucketTakenWrapped(t *testing.T) {
	h := bssTakenWorld(t)
	h.world.WrapConflict = true
	rs, err := h.run(false)
	bssExitNonZero(t, err)
	if r, _ := bsResult(rs, PhaseState); !strings.Contains(r.Detail, "spec.org") {
		t.Errorf("state detail %q: a wrapped 409 not recognised as the taken name", r.Detail)
	}
	if bssCount(h.runEvents(), "delete user ") != 1 {
		t.Errorf("events %v: want this run's S3 user deleted once", h.runEvents())
	}
	bssNoLeak(t, h, rs, err)
}

// TestBootstrapStateScratchInCheckout (G3, review r2): a TMPDIR inside the checkout is refused
// before any child starts: plan files and data directories go under it.
func TestBootstrapStateScratchInCheckout(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	inner := filepath.Join(h.checkout, ".tmp")
	bsMkdirPrivate(t, inner)
	before := treeSnapshot(t, h.checkout)
	t.Setenv("TMPDIR", inner)
	rs, err := h.run(false)
	bsExit(t, err, RefusalExit)
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Condition != CondScratch {
		t.Errorf("err %v, want the scratch refusal", err)
	}
	bssNoTofu(t, h)
	bsWantStatus(t, rs, PhaseState, StatusFail)
	bssCheckout(t, h, before)
}

// TestBootstrapStateUnrelatedConflict (review r1): a 409 that is not the bucket's name (here the
// S3 policy's, after the bucket was created) is a plain failure: no spec.org advice, nothing
// deleted, and the next run resumes.
func TestBootstrapStateUnrelatedConflict(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	h.world.PolicyConflict = true
	rs, err := h.run(false)
	bssExitNonZero(t, err)
	r, _ := bsResult(rs, PhaseState)
	if strings.Contains(r.Detail, "spec.org") {
		t.Errorf("state detail %q: a policy conflict read as a taken bucket name", r.Detail)
	}
	for _, e := range h.runEvents() {
		if strings.HasPrefix(e, "delete ") {
			t.Errorf("cloud event %q after a conflict that is not the bucket's", e)
		}
	}
	h.world.PolicyConflict = false
	if _, err := h.run(false); err != nil {
		t.Errorf("the next run did not resume: %v", err)
	}
	if all := h.cloud().Events; bssCount(all, "create bucket") != 1 || bssCount(all, "create user") != 1 {
		t.Errorf("events %v: one bucket and one S3 user over both runs", all)
	}
	bssNeverDestroyed(t, h)
}

// TestBootstrapStateCredentialInRepo (G3): a config root inside the checkout is refused before the
// state phase starts anything.
func TestBootstrapStateCredentialInRepo(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	inner := filepath.Join(h.base, "home")
	for _, rel := range []string{"stacks/deployments.yaml", "stacks/account/bootstrap", "stacks/account/account-governance"} {
		bssCopy(t, filepath.Join(bssRepoRoot, rel), filepath.Join(inner, rel))
	}
	h.checkout = inner
	rs, err := h.run(false)
	bsExit(t, err, RefusalExit)
	bssNoTofu(t, h)
	for _, p := range []string{PhaseState, PhasePublish, PhaseVerify} {
		if r, ok := bsResult(rs, p); ok {
			t.Errorf("%s %s with the config root inside the checkout", p, r.Status)
		}
	}
	if _, err := os.Stat(h.accountPath("state.env")); err == nil {
		t.Error("state.env written inside the checkout")
	}
}

// TestBootstrapProtect (G7 bootstrap part, FR-004, FR-011): the state phase plans first and
// passes the saved plan through the retained-resource guard (the account bucket and the platform
// S3 user retained) before any apply. Recorded plans that remove the bucket's block, replace the
// bucket (`org` change) or replace the platform S3 user are refused with exit 3 and no apply; a
// recorded plan that only creates is admitted and applied (control).
func TestBootstrapProtect(t *testing.T) {
	refused := []struct {
		name    string
		actions map[string][]string
		reason  string
	}{
		{"removed-bucket-block", map[string][]string{bssBucketAddr: {"delete"}}, "delete_because_no_resource_config"},
		{"bucket-replaced-org", map[string][]string{bssBucketAddr: {"delete", "create"}}, "replace_because_cannot_update"},
		{"bucket-replaced-create-first", map[string][]string{bssBucketAddr: {"create", "delete"}}, "replace_because_cannot_update"},
		{"s3-user-replaced", map[string][]string{bssUserAddr: {"delete", "create"}, bssCredAddr: {"delete", "create"}, bssPolicyAddr: {"delete", "create"}}, "replace_because_cannot_update"},
		// Each address of the platform S3 user on its own: a retained set naming only one of them
		// must not pass the row of another.
		{"s3-user-only-replaced", map[string][]string{bssUserAddr: {"delete", "create"}}, "replace_because_cannot_update"},
		{"s3-credential-replaced", map[string][]string{bssCredAddr: {"delete", "create"}}, "replace_because_cannot_update"},
		{"s3-policy-replaced", map[string][]string{bssPolicyAddr: {"delete", "create"}}, "replace_because_cannot_update"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			h := newBSSHarness(t)
			h.seedCurrentSandbox()
			h.mustRun()
			stateEnv, _ := os.ReadFile(h.accountPath("state.env"))
			h.world.Recorded = bssRecorded(t, h, tc.name, tc.actions, tc.reason)
			rs, err := h.run(false)
			bsExit(t, err, RefusalExit)
			var ref *Refusal
			if !errors.As(err, &ref) || ref.Condition != CondRetained {
				t.Errorf("err %v, want the retained-resource refusal", err)
			}
			bsWantStatus(t, rs, PhaseState, StatusFail)
			calls := h.runCalls()
			if !slices.ContainsFunc(calls, func(c bssCall) bool { return c.Root == "bootstrap" && c.Cmd == "plan" }) {
				t.Error("the state phase did not plan")
			}
			if i := slices.IndexFunc(calls, func(c bssCall) bool { return c.Root == "bootstrap" && (c.Cmd == "apply" || c.Cmd == "destroy") }); i >= 0 {
				t.Errorf("tofu %s %v after a plan the guard refuses", calls[i].Cmd, calls[i].Args)
			}
			if ev := h.runEvents(); len(ev) > 0 {
				t.Errorf("cloud changed: %v", ev)
			}
			if now, _ := os.ReadFile(h.accountPath("state.env")); string(now) != string(stateEnv) {
				t.Error("state.env changed on a refused plan")
			}
			for _, p := range []string{PhasePublish, PhaseVerify} {
				if r, ok := bsResult(rs, p); ok && r.Status != StatusFail {
					t.Errorf("%s %s after the guard's refusal", p, r.Status)
				}
			}
			bssNeverDestroyed(t, h)
			bssNoLeak(t, h, rs, err)
		})
	}
	t.Run("create-only-admitted", func(t *testing.T) {
		h := newBSSHarness(t)
		h.seedCurrentSandbox()
		h.world.Recorded = bssRecorded(t, h, "create", map[string][]string{bssBucketAddr: {"create"}, bssUserAddr: {"create"}, bssCredAddr: {"create"}, bssPolicyAddr: {"create"}}, "")
		rs, err := h.run(false)
		if err != nil {
			t.Errorf("err %v: a create-only plan is admitted", err)
		}
		bsWantStatus(t, rs, PhaseState, StatusRan)
		if ev := h.runEvents(); bssCount(ev, "create bucket") != 1 {
			t.Errorf("events %v: the admitted recorded plan was not applied", ev)
		}
		bssGuarded(t, h.runCalls())
		bssNoLeak(t, h, rs, err)
	})
}

// TestBootstrapPartial (V008, research R13 *Partial states tested*, FR-012): every interruption
// point leaves a state the next run detects and resumes (only the missing phases do work, the done
// ones report unchanged) or refuses; a run never duplicates the bucket or the S3 user, never loses
// or replaces the passphrase or the encrypted state, and never destroys the bucket.
func TestBootstrapPartial(t *testing.T) {
	type row struct {
		name  string
		setup func(h *bssHarness)
		check func(t *testing.T, h *bssHarness, rs []PhaseResult, err error)
	}
	resumed := func(state, publish string) func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
		return func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			if err != nil {
				t.Errorf("err %v, want the run to resume", err)
			}
			bsWantStatus(t, rs, PhaseState, state)
			if publish != "" {
				bsWantStatus(t, rs, PhasePublish, publish)
			}
			bssStateEnv(t, h)
			bssEnvelope(t, h)
			bssVerified(t, h)
		}
	}
	rows := []row{
		{"prefix-identify", func(h *bssHarness) {}, resumed(StatusRan, StatusRan)},
		{"prefix-admin", func(h *bssHarness) {
			h.seedPreviousAccount()
		}, resumed(StatusRan, StatusRan)},
		{"prefix-passphrase", func(h *bssHarness) {
			bsWrite(h.t, h.accountPath("state-passphrase.env"), bsPassphrase+"=fakeSeededPassphrase8Vd2kQ0zR7wYb3nT5mLc1Xa9Ue4Hs6Jp\n")
		}, resumed(StatusRan, StatusRan)},
		{"prefix-state", func(h *bssHarness) {
			h.mustRun()
			if err := os.Remove(filepath.Join(h.world.S3, h.world.Bucket, filepath.FromSlash(stacks.ArtifactKey("account-bootstrap")))); err != nil {
				h.t.Fatal(err)
			}
		}, resumed(StatusUnchanged, StatusRan)},
		{"state-env-missing", func(h *bssHarness) {
			h.mustRun()
			if err := os.Remove(h.accountPath("state.env")); err != nil {
				h.t.Fatal(err)
			}
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			resumed(StatusRan, StatusUnchanged)(t, h, rs, err)
			if ev := h.runEvents(); len(ev) > 0 {
				t.Errorf("events %v: state.env is rewritten from the state, nothing is created", ev)
			}
		}},
		// Coordinator decision 1 (2026-10-08): a missing account bucket that the bootstrap state or
		// state.env records is refused (its remote states are lost; an apply would recreate
		// resources that still exist), never created again.
		{"bucket-missing", func(h *bssHarness) {
			h.mustRun()
			bssDropBucket(h)
		}, bssBucketLost},
		{"bucket-missing-state-lost", func(h *bssHarness) {
			h.mustRun()
			bssDropBucket(h)
			if err := os.Remove(h.accountPath("state/" + bssStateFile)); err != nil {
				h.t.Fatal(err)
			}
		}, bssBucketLost},
		{"bucket-not-in-state", func(h *bssHarness) {
			h.mustRun()
			if err := os.Remove(h.accountPath("state/" + bssStateFile)); err != nil {
				h.t.Fatal(err)
			}
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			resumed(StatusRan, "")(t, h, rs, err)
			ev := h.runEvents()
			if bssCount(ev, "create bucket") != 0 || !slices.Contains(ev, "import bucket "+bsOldProject+"/"+bssRegion+"/"+bssBucket) {
				t.Errorf("events %v: want the existing bucket imported by name (%s/%s/%s), not created", ev, bsOldProject, bssRegion, bssBucket)
			}
			// Decision 3: the earlier run's S3 user, no longer in the state, is named, not deleted.
			keys, _ := ReadCredentialFile(h.accountPath("state.env"))
			var old []string
			for id, u := range h.cloud().Users {
				if u.Account == h.world.Account && u.AccessKey != keys["AWS_ACCESS_KEY_ID"] {
					old = append(old, id)
				}
			}
			if len(old) != 1 {
				t.Fatalf("users %v: want the earlier run's S3 user kept beside the new one", old)
			}
			if r, _ := bsResult(rs, PhaseState); !strings.Contains(r.Detail, old[0]) {
				t.Errorf("state detail %q does not name the leftover S3 user %s", r.Detail, old[0])
			}
		}},
		// Review r1: a plan that changes only an output is applied, or `tofu output` (and the
		// envelope) would keep the outputs of the earlier apply.
		{"output-only-change", func(h *bssHarness) {
			h.mustRun()
			h.world.OutputChange = true
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			if err != nil {
				t.Errorf("err %v", err)
			}
			bsWantStatus(t, rs, PhaseState, StatusRan)
			if !slices.ContainsFunc(h.runCalls(), func(c bssCall) bool { return c.Root == "bootstrap" && c.Cmd == "apply" && c.Exit == 0 }) {
				t.Error("a plan with an output change was not applied")
			}
			bssVerified(t, h)
		}},
		{"orphan-user-named", func(h *bssHarness) {
			c := h.cloud()
			// A platform S3 user no state knows (an earlier, lost bootstrap) and, in the same
			// project (KD-1), a tenant's S3 user: only the first is a leftover of the bootstrap.
			c.Users["4690"] = bssUser{Account: h.world.Account, Project: h.world.Project, Bucket: h.world.Bucket,
				Description: h.world.UserDescription, AccessKey: "FAKEAK0690", Secret: "fakeS3Secret0690Qx7v"}
			c.Users["4691"] = bssUser{Account: h.world.Account, Project: h.world.Project, Bucket: "lz-demo-bkt-state",
				Description: strings.Replace(h.world.UserDescription, "lz-", "lz-demo-", 1), AccessKey: "FAKEAK0691", Secret: "fakeS3Secret0691Qx7v"}
			h.setCloud(c)
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			resumed(StatusRan, StatusRan)(t, h, rs, err)
			r, _ := bsResult(rs, PhaseState)
			if !strings.Contains(r.Detail, "4690") {
				t.Errorf("state detail %q does not name the leftover platform S3 user 4690", r.Detail)
			}
			if strings.Contains(r.Detail, "4691") {
				t.Errorf("state detail %q names the tenant's S3 user 4691 as a leftover", r.Detail)
			}
			if c := h.cloud(); c.Users["4690"].AccessKey == "" || c.Users["4691"].AccessKey == "" {
				t.Error("a pre-existing S3 user was deleted")
			}
		}},
		{"apply-stopped-after-user", func(h *bssHarness) {
			h.world.FailAfter = 1
			h.seedCurrentSandbox()
			rs, err := h.run(false)
			if err == nil {
				h.t.Fatal("the interrupted apply passed")
			}
			if r, _ := bsResult(rs, PhaseState); r.Status != StatusFail {
				h.t.Errorf("interrupted state %s, want fail", r.Status)
			}
			bssNoLeak(h.t, h, rs, err)
			h.world.FailAfter = 0
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			resumed(StatusRan, StatusRan)(t, h, rs, err)
			if all := h.cloud().Events; bssCount(all, "create bucket") != 1 || bssCount(all, "create user") != 1 {
				t.Errorf("events %v: one bucket and one S3 user over both runs", all)
			}
		}},
		{"apply-stopped-after-bucket", func(h *bssHarness) {
			h.world.FailAfter, h.world.BucketFirst = 1, true
			h.seedCurrentSandbox()
			rs, err := h.run(false)
			if err == nil {
				h.t.Fatal("the interrupted apply passed")
			}
			bssNoLeak(h.t, h, rs, err)
			h.world.FailAfter = 0
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			resumed(StatusRan, StatusRan)(t, h, rs, err)
			if all := h.cloud().Events; bssCount(all, "create bucket") != 1 || bssCount(all, "create user") != 1 {
				t.Errorf("events %v: one bucket and one S3 user over both runs", all)
			}
		}},
		{"stopped-between-plan-and-guard", func(h *bssHarness) {
			h.world.FailShow = true
			h.seedCurrentSandbox()
			rs, err := h.run(false)
			if err == nil {
				h.t.Fatal("a run whose plan could not be read passed")
			}
			bssNoLeak(h.t, h, rs, err)
			if slices.ContainsFunc(h.runCalls(), func(c bssCall) bool { return c.Cmd == "apply" }) {
				h.t.Error("applied a plan the guard could not read")
			}
			h.world.FailShow = false
		}, resumed(StatusRan, StatusRan)},
		{"verify-lock-held", func(h *bssHarness) {
			h.mustRun()
			bsWrite(h.t, filepath.Join(h.world.S3, h.world.Bucket, filepath.FromSlash(bssLock)), `{"ID":"held"}`)
			rs, err := h.run(false)
			bssExitNonZero(h.t, err)
			bsWantStatus(h.t, rs, PhaseState, StatusUnchanged)
			bsWantStatus(h.t, rs, PhasePublish, StatusUnchanged)
			bsWantStatus(h.t, rs, PhaseVerify, StatusFail)
			bssNoLeak(h.t, h, rs, err)
			if _, held := h.object(bssLock); !held {
				h.t.Error("the bootstrap removed a lock it did not take")
			}
			if slices.ContainsFunc(h.runCalls(), func(c bssCall) bool {
				return slices.Contains(c.Args, "force-unlock") || slices.Contains(c.Args, "-lock=false")
			}) {
				h.t.Error("verify broke or bypassed a held lock")
			}
			if err := os.Remove(filepath.Join(h.world.S3, h.world.Bucket, filepath.FromSlash(bssLock))); err != nil {
				h.t.Fatal(err)
			}
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			resumed(StatusUnchanged, StatusUnchanged)(t, h, rs, err)
			bsWantStatus(t, rs, PhaseVerify, StatusUnchanged)
		}},
		{"passphrase-lost", func(h *bssHarness) {
			h.mustRun()
			if err := os.Remove(h.accountPath("state-passphrase.env")); err != nil {
				h.t.Fatal(err)
			}
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			// Coordinator decision 2 (2026-10-08): `passphrase` refuses; no new passphrase is
			// generated beside state it cannot read.
			bsExit(t, err, RefusalExit)
			var ref *Refusal
			if !errors.As(err, &ref) || ref.Condition != "passphrase-lost" {
				t.Errorf("err %v, want the passphrase-lost refusal", err)
			}
			bsWantStatus(t, rs, PhasePassphrase, StatusFail)
			bssNoTofu(t, h)
			if _, serr := os.Stat(h.accountPath("state-passphrase.env")); serr == nil {
				t.Error("a new passphrase was written beside encrypted state it cannot read")
			}
			if ev := h.runEvents(); len(ev) > 0 {
				t.Errorf("events %v with the passphrase of the encrypted state lost", ev)
			}
		}},
		{"credential-rejected", func(h *bssHarness) {
			h.mustRun()
			h.api.rejected[bsOldClient] = true
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			bsExit(t, err, BlockedExit)
			bssNoTofu(t, h)
		}},
		{"admin-drifted", func(h *bssHarness) {
			h.mustRun()
			for _, p := range h.api.account(bsOldAccount).Policies {
				if p["id"] == bsOldPolicy {
					perms := p["permissions"].(map[string]any)
					perms["allow"] = append(perms["allow"].([]any), map[string]any{"action": "account:apiovh:*"})
				}
			}
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			bsExit(t, err, 1)
			bsWantStatus(t, rs, PhaseAdmin, StatusFail)
			bssNoTofu(t, h)
		}},
		{"binding-other-account", func(h *bssHarness) {
			h.mustRun()
			p := h.accountPath("account.env")
			bsWrite(h.t, p, strings.Replace(bsRead(h.t, p), "LZ_ACCOUNT_ID="+bsOldAccount, "LZ_ACCOUNT_ID="+bssForeign, 1))
		}, func(t *testing.T, h *bssHarness, rs []PhaseResult, err error) {
			bsExit(t, err, RefusalExit)
			bssNoTofu(t, h)
		}},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			h := newBSSHarness(t)
			if !strings.HasPrefix(tc.name, "apply-stopped") && tc.name != "stopped-between-plan-and-guard" {
				h.seedCurrentSandbox()
			}
			tc.setup(h)
			checkout := treeSnapshot(t, h.checkout)
			passBefore, passErr := os.ReadFile(h.accountPath("state-passphrase.env"))
			stateBefore, stateErr := os.ReadFile(h.accountPath("state/" + bssStateFile))
			bucketsBefore := len(h.cloud().Buckets)
			rs, err := h.run(false)
			tc.check(t, h, rs, err)
			// Never lost or replaced: an existing passphrase and an existing encrypted state stay
			// readable by it (the state may change only under the same passphrase).
			if passErr == nil {
				if now, _ := os.ReadFile(h.accountPath("state-passphrase.env")); string(now) != string(passBefore) {
					t.Error("the passphrase file changed")
				}
			}
			if stateErr == nil {
				var before, after bssLocal
				_ = json.Unmarshal(stateBefore, &before)
				nowRaw, rerr := os.ReadFile(h.accountPath("state/" + bssStateFile))
				if rerr != nil || json.Unmarshal(nowRaw, &after) != nil || after.PassSHA != before.PassSHA {
					t.Error("the encrypted state was lost or re-encrypted under another passphrase")
				}
			}
			if c := h.cloud(); len(c.Buckets) < bucketsBefore || bssCount(c.Events, "create bucket") > 1+bssCount(c.Events, "delete bucket") {
				t.Errorf("events %v: a bucket was lost or created twice", c.Events)
			}
			bssCheckout(t, h, checkout)
			bssNeverDestroyed(t, h)
			bssNoLeak(t, h, rs, err)
		})
	}
}
