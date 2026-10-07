package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// `lz-live probe` end to end (T055): the guard admits (fake git), the sandbox credential binds
// to its account through a fake API, the probe root runs through the run core with a fake tofu
// (the test binary through symlinks, as the fake git), and the leftover check lists through the
// same fake API (T084). A fake ovhcloud on PATH logs any start: none is expected (P18). HOME is a
// temporary directory: no test reads the owner's ~/.config/ovh-lz/, calls the OVHcloud API or
// runs a real tofu or ovhcloud.

const (
	probeSecret  = "probe-admin-secret-0f9e8d7c6b5a"
	probeAccount = "ab12345-ovh"
)

// fakeTofu logs each call (argv and environment) and behaves like a tofu whose probe creates one
// private network; it prints the credential on stderr, which must never reach lz-live's output.
// Its backend model is host tofu's (T073, t073-reinit.sh): init records the backend path in the
// data directory (TF_DATA_DIR, relative to -chdir, else <root>/.terraform) and fails "Backend
// configuration changed" when that directory holds another one; plan and destroy need an init of
// this backend in their data directory. Each call's log line carries the data directory with
// symlinks resolved.
func fakeTofu(args []string) int {
	bin := filepath.Dir(os.Args[0])
	sub, out, positional, root := "", "", "", "."
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-chdir="):
			root = strings.TrimPrefix(a, "-chdir=")
		case strings.HasPrefix(a, "-out="):
			out = strings.TrimPrefix(a, "-out=")
		case strings.HasPrefix(a, "-"):
		case sub == "":
			sub = a
		default:
			positional = a
		}
	}
	state := os.Getenv("TF_VAR_state_path")
	data := os.Getenv("TF_DATA_DIR")
	if data == "" {
		data = ".terraform"
	}
	if !filepath.IsAbs(data) {
		data = filepath.Join(root, data)
	}
	logChildIn(bin, "tofu.log", args, resolvedPath(data))
	fmt.Fprintf(os.Stderr, "fake-tofu-stderr token=%s\n", os.Getenv("OVH_CLIENT_SECRET"))
	backend := filepath.Join(data, "terraform.tfstate")
	recorded, rerr := os.ReadFile(backend)
	switch sub {
	case "init":
		if rerr == nil && string(recorded) != state && !slices.Contains(args, "-reconfigure") {
			fmt.Fprintln(os.Stderr, "Error: Backend configuration changed")
			return 1
		}
		if os.MkdirAll(data, 0o755) != nil {
			return 1
		}
		return writeOK(backend, state)
	case "plan", "destroy":
		if rerr != nil || string(recorded) != state {
			fmt.Fprintln(os.Stderr, `Error: Backend initialization required, please run "tofu init"`)
			return 1
		}
	}
	switch sub {
	case "output":
		// T075: what a root publishes for its companion (companion-env.json next to the fake).
		raw, err := os.ReadFile(filepath.Join(bin, "companion-env.json"))
		if err != nil || positional != "companion_env" {
			return 1
		}
		fmt.Println(string(raw))
		return 0
	case "plan":
		return writeOK(out, "fake plan")
	case "show":
		if slices.Contains(args, "-json") {
			fmt.Println(`{"format_version":"1.2","terraform_version":"1.13.0","planned_values":{},"resource_changes":[{"address":"ovh_cloud_project_network_private.p","change":{"actions":["create"]}}]}`)
		} else {
			fmt.Println("  + ovh_cloud_project_network_private.p")
		}
		return 0
	case "apply":
		if _, err := os.Stat(positional); err != nil {
			return 1
		}
		fmt.Println(`{"@level":"info","@message":"ovh_cloud_project_network_private.p: Creation complete after 1s [id=pn-1]","type":"apply_complete","hook":{"resource":{"addr":"ovh_cloud_project_network_private.p","resource_type":"ovh_cloud_project_network_private"},"action":"create","id_key":"id","id_value":"pn-1"}}`)
		// The local backend keeps the previous state as <path>.backup. A companion has no local
		// state path (its backend is its own, T076): nothing to write.
		if state == "" {
			return 0
		}
		return writeOK(state, "state") + writeOK(state+".backup", "previous state")
	case "destroy":
		if _, err := os.Stat(filepath.Join(bin, "destroy-fails")); err == nil {
			fmt.Fprintln(os.Stderr, "Error: fake destroy failure")
			return 1
		}
		return 0
	}
	return 0
}

// resolvedPath is p with symlinks resolved as far as p exists.
func resolvedPath(p string) string {
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if parent := filepath.Dir(p); parent != p {
		return filepath.Join(resolvedPath(parent), filepath.Base(p))
	}
	return p
}

func writeOK(path, content string) int {
	if path == "" {
		return 0
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}
	fmt.Fprint(f, content)
	f.Close()
	return 0
}

// fakeOvhcloud logs that it was started (lz-live must not start it since T084) and answers an
// empty listing.
func fakeOvhcloud(args []string) int {
	logChild(filepath.Dir(os.Args[0]), "ovhcloud.log", args)
	fmt.Println("[]")
	return 0
}

// apiListing is one leftover listing the fake API answered: its path (version included) and the
// client id the bearer token was issued to.
type apiListing struct{ Path, Client string }

// tokenClients maps each token the fake API issues to the client id it was issued to.
var tokenClients = map[string]string{"tok-1": "EU.sandboxadmin", "tok-identity": "EU.probeidentity", "tok-p25": "EU.probep25"}

type childCall struct {
	Args    []string `json:"args"`
	Env     []string `json:"env"`
	DataDir string   `json:"data_dir,omitempty"` // tofu: its data directory, symlinks resolved
}

func logChild(dir, name string, args []string) { logChildIn(dir, name, args, "") }

func logChildIn(dir, name string, args []string, dataDir string) {
	raw, _ := json.Marshal(childCall{Args: args, Env: os.Environ(), DataDir: dataDir})
	if f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, string(raw))
		f.Close()
	}
}

func (w world) childCalls(t *testing.T, name string) []childCall {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(w.git), name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []childCall
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var c childCall
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// subcommands returns each tofu call's subcommand (the first argument not a flag).
func subcommands(calls []childCall) []string {
	var out []string
	for _, c := range calls {
		for _, a := range c.Args {
			if !strings.HasPrefix(a, "-") {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

func (c childCall) env() map[string]string {
	m := map[string]string{}
	for _, kv := range c.Env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

type probeWorld struct {
	world
	cfg       string
	api       *httptest.Server
	apiCalls  *int
	stdout    *bytes.Buffer
	apiAcount string    // the account GET /auth/details answers
	now       time.Time // the clock run ids are made from (zero: 2026-10-06 12:00 UTC)
	// The probe identity a root publishes (T075, probe_identity_test.go): its token is refused
	// when identityRefused, else GET /auth/details answers identityAccount; identityAsked holds
	// the number of tofu calls made before each such answer.
	identityAccount string
	identityRefused bool
	identityAsked   *[]int
	// The P25 identity a root publishes next to it (T076): GET /auth/details with its token answers
	// p25Account; p25Asked as identityAsked.
	p25Account string
	p25Asked   *[]int
	// The leftover check's listings (T084): listings answers a path (as asked, /v1/... or
	// /v2/iam/...), else an empty account answers ([] or an untagged resource); listed records
	// every listing request; lookedUp every executable lz-live looked up.
	listings map[string]string
	listed   *[]apiListing
	lookedUp *[]string
}

// newProbeWorld is an admitted host with a sandbox credential bound to probeAccount, one project,
// the admin exemption and a probe root tests/live/probes/net in the checkout.
func newProbeWorld(t *testing.T) *probeWorld {
	t.Helper()
	w := &probeWorld{world: newWorld(t, true), apiAcount: probeAccount, apiCalls: new(int), identityAccount: probeAccount, identityAsked: new([]int),
		p25Account: probeAccount, p25Asked: new([]int), listings: map[string]string{}, listed: new([]apiListing), lookedUp: new([]string)}
	// As in production (os.UserHomeDir), the injected home is the process HOME, which a child
	// must not inherit.
	t.Setenv("HOME", w.home)
	w.cfg = filepath.Join(w.home, ".config", "ovh-lz")
	bin := filepath.Dir(w.git)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tofu", "ovhcloud"} {
		if err := os.Symlink(self, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	writePrivate(t, filepath.Join(w.cfg, "sandbox.env"), "OVH_ENDPOINT=ovh-eu\nOVH_CLIENT_ID=EU.sandboxadmin\nOVH_CLIENT_SECRET="+probeSecret+"\n")
	writePrivate(t, filepath.Join(w.cfg, "accounts", probeAccount, "account.env"),
		"LZ_ACCOUNT_ID="+probeAccount+"\nOVH_ENDPOINT=ovh-eu\nLZ_ORG=demo\nLZ_PROJECT_ID_STATE=p1\nLZ_ADMIN_POLICY_ID=pol-admin\n")
	root := filepath.Join(w.checkout, "tests", "live", "probes", "net")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte("# probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.api = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		*w.apiCalls++
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/token":
			_ = r.ParseForm()
			if r.PostForm.Get("client_id") == "EU.probeidentity" && r.PostForm.Get("client_secret") == identitySecret && !w.identityRefused {
				fmt.Fprint(rw, `{"access_token":"tok-identity","token_type":"Bearer","expires_in":3600}`)
				return
			}
			if r.PostForm.Get("client_id") == "EU.probep25" && r.PostForm.Get("client_secret") == p25Secret {
				fmt.Fprint(rw, `{"access_token":"tok-p25","token_type":"Bearer","expires_in":3600}`)
				return
			}
			if r.PostForm.Get("client_id") != "EU.sandboxadmin" || r.PostForm.Get("client_secret") != probeSecret {
				rw.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(rw, `{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/details" && r.Header.Get("Authorization") == "Bearer tok-1":
			fmt.Fprintf(rw, `{"account":%q}`, w.apiAcount)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/details" && r.Header.Get("Authorization") == "Bearer tok-identity":
			*w.identityAsked = append(*w.identityAsked, countLines(filepath.Join(filepath.Dir(w.git), "tofu.log")))
			fmt.Fprintf(rw, `{"account":%q}`, w.identityAccount)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/details" && r.Header.Get("Authorization") == "Bearer tok-p25":
			*w.p25Asked = append(*w.p25Asked, countLines(filepath.Join(filepath.Dir(w.git), "tofu.log")))
			fmt.Fprintf(rw, `{"account":%q}`, w.p25Account)
		case r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/v2/iam/")) &&
			tokenClients[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] != "":
			*w.listed = append(*w.listed, apiListing{Path: r.URL.EscapedPath(), Client: tokenClients[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]})
			rw.Header().Set("Content-Type", "application/json")
			switch body, ok := w.listings[r.URL.EscapedPath()]; {
			case ok:
				fmt.Fprint(rw, body)
			case strings.HasPrefix(r.URL.Path, "/v2/iam/resource/"):
				fmt.Fprint(rw, `{"urn":"x","name":"p1","type":"publicCloudProject","tags":{}}`)
			default:
				fmt.Fprint(rw, "[]")
			}
		default:
			rw.WriteHeader(http.StatusForbidden)
		}
	}))
	t.Cleanup(w.api.Close)
	return w
}

func writePrivate(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w *probeWorld) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	bin := filepath.Dir(w.git)
	code := run(args, deps{
		Getwd:         func() (string, error) { return w.checkout, nil },
		Home:          func() (string, error) { return w.home, nil },
		Getenv:        func(k string) string { return w.env[k] },
		Git:           w.git,
		OfflineMarker: w.marker,
		Stderr:        &stderr,
		Stdout:        &stdout,
		LookPath: func(name string) (string, error) {
			*w.lookedUp = append(*w.lookedUp, name)
			p := filepath.Join(bin, name)
			if _, err := os.Stat(p); err != nil {
				return "", err
			}
			return p, nil
		},
		API: func(endpoint string) (live.API, error) {
			if endpoint != "ovh-eu" {
				return live.API{}, fmt.Errorf("endpoint %s", endpoint)
			}
			return live.API{TokenURL: w.api.URL + "/token", BaseURL: w.api.URL + "/v1", HTTP: w.api.Client()}, nil
		},
		Now: func() time.Time {
			if !w.now.IsZero() {
				return w.now
			}
			return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
		},
	})
	return code, stdout.String(), stderr.String()
}

var runIDLine = regexp.MustCompile(`LZ-LIVE run (\S+) start`)

func runIDOf(t *testing.T, stdout string) string {
	t.Helper()
	m := runIDLine.FindStringSubmatch(stdout)
	if m == nil || !live.RunIDPattern.MatchString(m[1]) {
		t.Fatalf("no run id line in stdout:\n%s", stdout)
	}
	return m[1]
}

func (w *probeWorld) probeDir(runID string) string {
	return filepath.Join(w.cfg, "accounts", probeAccount, "state", "probes", runID)
}

// noSecret fails when the credential reached lz-live's output or any file of the run record.
func (w *probeWorld) noSecret(t *testing.T, outputs ...string) {
	t.Helper()
	for _, o := range outputs {
		if strings.Contains(o, probeSecret) {
			t.Errorf("lz-live output holds the credential:\n%s", o)
		}
	}
	_ = filepath.WalkDir(filepath.Join(w.checkout, ".local"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if raw, _ := os.ReadFile(p); bytes.Contains(raw, []byte(probeSecret)) {
				t.Errorf("%s holds the credential", p)
			}
		}
		return nil
	})
}

func TestProbeEntry(t *testing.T) {
	t.Run("start-passes", func(t *testing.T) {
		w := newProbeWorld(t)
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		id := runIDOf(t, stdout)
		calls := w.childCalls(t, "tofu.log")
		if got, want := subcommands(calls), []string{"init", "plan", "show", "show", "apply", "destroy"}; !slices.Equal(got, want) {
			t.Errorf("tofu calls %v, want %v", got, want)
		}
		statePath := filepath.Join(w.probeDir(id), "net.tfstate")
		for _, c := range calls {
			env := c.env()
			if env["TF_VAR_state_path"] != statePath {
				t.Errorf("tofu %v: state path %q, want %q", c.Args, env["TF_VAR_state_path"], statePath)
			}
			if p := env["TF_VAR_state_passphrase"]; len(p) < 16 || strings.Contains(strings.Join(c.Args, " "), p) || strings.Contains(stdout, p) {
				t.Errorf("tofu %v: passphrase missing, short, in argv or on the terminal", c.Args)
			}
			if env["OVH_CLIENT_SECRET"] != probeSecret || env["OVH_CLIENT_ID"] != "EU.sandboxadmin" {
				t.Errorf("tofu %v runs without the sandbox credential", c.Args)
			}
			if h := env["HOME"]; h == "" || h == w.home || strings.HasPrefix(h, w.home+string(filepath.Separator)) {
				t.Errorf("tofu %v runs with HOME %q, not a scratch HOME", c.Args, h)
			}
			if !strings.HasPrefix(c.Args[0], "-chdir=") || filepath.Clean(strings.TrimPrefix(c.Args[0], "-chdir=")) != filepath.Join(w.checkout, "tests", "live", "probes", "net") {
				t.Errorf("tofu %v does not run in the probe root", c.Args)
			}
		}
		// The leftover check lists the bound project, its tags on the project URN.
		asked := map[string]bool{}
		for _, l := range *w.listed {
			asked[l.Path] = true
		}
		for _, path := range []string{"/v1/cloud/project/p1/network/private", "/v2/iam/resource/urn:v1:eu:resource:publicCloudProject:p1"} {
			if !asked[path] {
				t.Errorf("the leftover check never listed %s (asked %v)", path, asked)
			}
		}
		for _, c := range calls {
			if strings.Contains(strings.Join(c.Args, " "), " init") || slices.Contains(c.Args, "init") {
				if !slices.Contains(c.Args, "-lockfile=readonly") {
					t.Errorf("tofu %v: init may write .terraform.lock.hcl into the reviewed checkout", c.Args)
				}
			}
		}
		for _, want := range []string{"LZ-LIVE summary " + id + " pass", "record approximate cost for run " + id, "fake-tofu-stderr token="} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
		}
		w.noSecret(t, stdout, stderr)
		var sum map[string]any
		raw, err := os.ReadFile(filepath.Join(w.checkout, ".local", "live", id, "summary.json"))
		if err != nil || json.Unmarshal(raw, &sum) != nil || sum["outcome"] != "pass" || sum["deadline"] != live.DefaultDeadline.String() {
			t.Errorf("summary.json %s (%v), want outcome pass, deadline %v", raw, err, live.DefaultDeadline)
		}
		inv, err := live.ReadInventory(filepath.Join(w.checkout, ".local", "live", id))
		if err != nil || len(inv) != 1 || inv[0].ID != "pn-1" {
			t.Errorf("inventory %v (%v), want the one created network", inv, err)
		}
		// The state backup goes with the state, and the run's directory with its files.
		if _, err := os.Stat(w.probeDir(id)); err == nil {
			left, _ := os.ReadDir(w.probeDir(id))
			t.Errorf("a passing probe kept its directory %v", left)
		}
	})

	t.Run("plan-only", func(t *testing.T) {
		w := newProbeWorld(t)
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net", "--plan-only", "--deadline", "10m")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		got := subcommands(w.childCalls(t, "tofu.log"))
		if slices.Contains(got, "apply") || slices.Contains(got, "destroy") || !slices.Contains(got, "plan") {
			t.Errorf("plan-only tofu calls %v: want a plan, no apply, no destroy", got)
		}
		id := runIDOf(t, stdout)
		raw, _ := os.ReadFile(filepath.Join(w.checkout, ".local", "live", id, "summary.json"))
		if !strings.Contains(string(raw), `"deadline":"10m0s"`) {
			t.Errorf("summary.json %s, want deadline 10m0s from --deadline", raw)
		}
	})

	t.Run("failed-destroy-then-cleanup", func(t *testing.T) {
		w := newProbeWorld(t)
		marker := filepath.Join(filepath.Dir(w.git), "destroy-fails")
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code == 0 {
			t.Fatalf("a probe whose destroy failed exited 0:\n%s", stdout)
		}
		id := runIDOf(t, stdout)
		for _, f := range []string{"passphrase.env", "probe.env"} {
			if _, err := os.Stat(filepath.Join(w.probeDir(id), f)); err != nil {
				t.Fatalf("after a failed destroy %s is gone: %v", f, err)
			}
		}
		w.noSecret(t, stdout, stderr)
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		before := len(w.childCalls(t, "tofu.log"))
		code, stdout, stderr = w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
		if code != 0 {
			t.Fatalf("cleanup: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		// A fresh process initialises the root (it may not be in this checkout yet), then only
		// destroys.
		after := w.childCalls(t, "tofu.log")[before:]
		if got := subcommands(after); !slices.Equal(got, []string{"init", "destroy"}) {
			t.Errorf("cleanup tofu calls %v, want [init destroy]: no plan, no apply", got)
		} else if !slices.Contains(after[0].Args, "-lockfile=readonly") {
			t.Errorf("cleanup init %v may write the lock file into the checkout", after[0].Args)
		}
		if left, _ := os.ReadDir(w.probeDir(id)); len(left) != 0 {
			t.Errorf("a passing cleanup kept %v", left)
		}
		if !strings.Contains(stdout, "LZ-LIVE summary "+id+" pass") {
			t.Errorf("cleanup stdout lacks the pass summary:\n%s", stdout)
		}
		w.noSecret(t, stdout, stderr)
	})

	// A listed network carrying the probe prefix that no run recorded is a leftover: the run
	// fails and keeps its files.
	t.Run("leftover-by-prefix", func(t *testing.T) {
		w := newProbeWorld(t)
		w.listings["/v1/cloud/project/p1/network/private"] = `[{"id":"pn-old","name":"lzprobe-old","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`
		code, stdout, _ := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code == 0 {
			t.Fatalf("a probe with an lzprobe- leftover exited 0:\n%s", stdout)
		}
		id := runIDOf(t, stdout)
		raw, _ := os.ReadFile(filepath.Join(w.checkout, ".local", "live", id, "leftovers.json"))
		if !strings.Contains(string(raw), "pn-old") {
			t.Errorf("leftovers.json lacks pn-old:\n%s", raw)
		}
		if _, err := os.Stat(filepath.Join(w.probeDir(id), "passphrase.env")); err != nil {
			t.Errorf("a probe with a leftover deleted its passphrase: %v", err)
		}
	})

	// The probe root a cleanup reads back from probe.env is confined like a fresh run's.
	t.Run("cleanup-root-outside", func(t *testing.T) {
		w := newProbeWorld(t)
		marker := filepath.Join(filepath.Dir(w.git), "destroy-fails")
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, stdout, _ := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		id := runIDOf(t, stdout)
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		elsewhere := t.TempDir()
		if err := os.WriteFile(filepath.Join(w.probeDir(id), "probe.env"), []byte("LZ_PROBE_ROOT="+elsewhere+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := len(w.childCalls(t, "tofu.log"))
		code, _, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
		if code != 3 || !strings.Contains(stderr, "probe root") {
			t.Errorf("cleanup of a root outside %s: exit %d, stderr %q; want 3 naming the probe root", probeRoots, code, stderr)
		}
		if n := len(w.childCalls(t, "tofu.log")) - before; n != 0 {
			t.Errorf("%d tofu calls for a root outside the checkout's probes", n)
		}
	})

	// Refusals and failures before any child runs.
	for name, c := range map[string]struct {
		setup func(*probeWorld)
		root  string
		exit  int
		cond  string
	}{
		"binding-refused":     {setup: func(w *probeWorld) { w.apiAcount = "zz99999-ovh"; writeAccount(w, "zz99999-ovh", probeAccount) }, root: "tests/live/probes/net", exit: 3, cond: "(account)"},
		"root-outside-probes": {root: "tests/live", exit: 3, cond: "probe root"},
		"root-escapes":        {root: "tests/live/probes/../../../..", exit: 3, cond: "probe root"},
		"root-missing":        {root: "tests/live/probes/none", exit: 3, cond: "probe root"},
		"root-nested": {setup: func(w *probeWorld) {
			_ = os.MkdirAll(filepath.Join(w.checkout, "tests", "live", "probes", "net", "sub"), 0o755)
		},
			root: "tests/live/probes/net/sub", exit: 3, cond: "probe root"},
		"no-sandbox-env": {setup: func(w *probeWorld) { os.Remove(filepath.Join(w.cfg, "sandbox.env")) }, root: "tests/live/probes/net", exit: 1, cond: "sandbox.env"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newProbeWorld(t)
			if c.setup != nil {
				c.setup(w)
			}
			code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, c.root)
			if code != c.exit || !strings.Contains(stderr, c.cond) {
				t.Errorf("exit %d, stderr %q; want %d naming %q", code, stderr, c.exit, c.cond)
			}
			if n := len(w.childCalls(t, "tofu.log")); n != 0 {
				t.Errorf("%d tofu calls before the refusal", n)
			}
			w.noSecret(t, stdout, stderr)
		})
	}
}

// writeAccount binds account dir `dir` to account id `id` (a mismatch when they differ).
func writeAccount(w *probeWorld, dir, id string) {
	path := filepath.Join(w.cfg, "accounts", dir, "account.env")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte("LZ_ACCOUNT_ID="+id+"\nOVH_ENDPOINT=ovh-eu\nLZ_ORG=demo\nLZ_PROJECT_ID_STATE=p1\nLZ_ADMIN_POLICY_ID=pol-admin\n"), 0o600)
}

// TestProbeLeftoversThroughAPI (T084; T083 gap "entry wiring", "which credential"): `lz-live
// probe` lists leftovers through lz-live's own API client with the credential the run binds, the
// sandbox.env service account: every listing carries the bearer token issued to its client id
// (EU.sandboxadmin), both API versions are asked, no ovhcloud is looked up or started (a fake one
// is on PATH), and a listing the API refuses fails the run instead of passing it.
func TestProbeLeftoversThroughAPI(t *testing.T) {
	t.Run("sandbox-credential", func(t *testing.T) {
		w := newProbeWorld(t)
		code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code != 0 {
			t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
		}
		if len(*w.listed) == 0 {
			t.Fatal("the leftover check listed nothing through the API")
		}
		versions := map[string]bool{}
		for _, l := range *w.listed {
			if l.Client != "EU.sandboxadmin" {
				t.Errorf("listing %s with the token of %q, want the sandbox.env client EU.sandboxadmin", l.Path, l.Client)
			}
			versions[strings.SplitN(l.Path, "/", 3)[1]] = true
		}
		if !versions["v1"] || !versions["v2"] {
			t.Errorf("listed under %v, want v1 and v2", versions)
		}
		if slices.Contains(*w.lookedUp, "ovhcloud") {
			t.Errorf("lz-live looked up ovhcloud (looked up %v)", *w.lookedUp)
		}
		if n := len(w.childCalls(t, "ovhcloud.log")); n != 0 {
			t.Errorf("the probe started ovhcloud %d times", n)
		}
		w.noSecret(t, stdout, stderr)
	})
	t.Run("listing-refused-fails", func(t *testing.T) {
		w := newProbeWorld(t)
		w.listings["/v2/iam/policy"] = "not json"
		code, stdout, _ := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
		if code == 0 {
			t.Fatalf("a probe whose policy listing is unreadable exited 0:\n%s", stdout)
		}
		raw, _ := os.ReadFile(filepath.Join(w.checkout, ".local", "live", runIDOf(t, stdout), "leftovers.json"))
		if !strings.Contains(string(raw), "/iam/policy") {
			t.Errorf("leftovers.json does not name the unreadable listing /iam/policy:\n%s", raw)
		}
	})
}
