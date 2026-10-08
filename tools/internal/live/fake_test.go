package live

// Shared fakes for the live-lane tests. The test binary doubles as two fake executables, so no
// shell and no real git or OVHcloud API is involved:
//
//   - invoked through a symlink named "git", it is a fake git that answers the commands the host
//     guard needs (research R11) from testdata/git/<scenario>.json, modelled on git 2.53.0's
//     output (rev-parse prints ".git" for the git dir of the current directory, absolute paths
//     otherwise; status prints ignored files only with --ignored, and hides untracked files
//     when the scenario's git config says status.showUntrackedFiles=no and no -u flag is given;
//     merge-base --is-ancestor exits 1 for a known non-ancestor, 128 for an unknown commit;
//     worktree list --porcelain lists the main worktree, then every entry of the common dir's
//     worktrees/ on disk that has a gitdir file, then the scenario's linked_worktrees; config
//     --get answers from the scenario's config and exits 1 for an unset key);
//   - invoked as "<binary> lz-fake-child <file>", it writes its own environment to <file>;
//   - invoked through a symlink named "tofu" or "ovhcloud", or as "<binary> lz-fake-run <config>",
//     it is the fake tofu, the fake ovhcloud or a fresh lz-live process of runner_test.go.
//
// The fake OVHcloud API (newFakeAPI) serves the OAuth2 client-credentials token endpoint,
// GET /auth/details and GET /me, and enforces each credential's IAM policy actions from
// testdata/api/credentials.json (research R13 *Account binding*, premise P26).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const fakeChildArg = "lz-fake-child"

func TestMain(m *testing.M) {
	switch {
	case filepath.Base(os.Args[0]) == "git":
		os.Exit(fakeGit(os.Args[1:]))
	case bssFakeWorld() != "":
		// The fake tofu of the bootstrap state phases (bootstrap_state_test.go, T056).
		os.Exit(fakeBootstrapTofu(bssFakeWorld(), os.Args[1:]))
	case laneFakeWorld() != "":
		// The fake tofu of the plan/apply lane (apply_test.go, T058).
		os.Exit(fakeLaneTofu(laneFakeWorld(), os.Args[1:]))
	case filepath.Base(os.Args[0]) == "tofu":
		os.Exit(fakeTofu(os.Args[1:]))
	case filepath.Base(os.Args[0]) == "ovhcloud":
		os.Exit(fakeOvhcloud())
	case len(os.Args) == 3 && os.Args[1] == fakeRunArg:
		os.Exit(fakeRun(os.Args[2]))
	case len(os.Args) == 3 && os.Args[1] == fakeChildArg:
		if err := os.WriteFile(os.Args[2], []byte(strings.Join(os.Environ(), "\n")), 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// gitScenario is what the fake git knows about the repository it is asked about.
type gitScenario struct {
	GitDir    string   `json:"git_dir"`
	CommonDir string   `json:"common_dir"`
	Head      string   `json:"head"`
	Modified  []string `json:"modified"`
	Untracked []string `json:"untracked"`
	Ignored   []string `json:"ignored"`
	// OriginMain lists the commits reachable from origin/main; Known lists other existing commits.
	OriginMain []string `json:"origin_main"`
	Known      []string `json:"known"`
	// ShowUntracked models status.showUntrackedFiles from the user's git config ("no" hides
	// untracked files unless the command passes -u/--untracked-files).
	ShowUntracked string `json:"show_untracked_config"`
	// Index is what ls-files -v prints (default "H README.md"): a lowercase tag is
	// assume-unchanged, "S" skip-worktree; status hides changes to either.
	Index []string `json:"index"`
	// TopLevel is the work tree (core.worktree); empty means the directory git runs in.
	TopLevel string `json:"toplevel"`
	// Fail lists subcommands that exit 128 (a git failure in one call only).
	Fail []string `json:"fail"`
	// LinkedWorktrees are listed by worktree list after the entries found on disk (T071).
	LinkedWorktrees []string `json:"linked_worktrees"`
	// Config is the repository configuration config --get reads; keys in lower case.
	Config map[string]string `json:"config"`
}

// fakeGit reads scenario.json next to its own path, appends the call to git.log there, and
// answers. Anything it does not model exits 128, which the guard must treat as a refusal.
func fakeGit(args []string) int {
	dir := filepath.Dir(os.Args[0])
	cwd, _ := os.Getwd()
	for len(args) > 0 {
		switch {
		case args[0] == "-C" && len(args) > 1:
			cwd = filepath.Join(cwd, args[1])
			if filepath.IsAbs(args[1]) {
				cwd = args[1]
			}
			args = args[2:]
		case args[0] == "-c" && len(args) > 1:
			args = args[2:]
		case args[0] == "--no-pager" || args[0] == "--no-optional-locks":
			args = args[1:]
		default:
			goto parsed
		}
	}
parsed:
	var gitEnv []string // the caller-controlled git environment, for TestGuardGitEnvironment
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") {
			gitEnv = append(gitEnv, kv)
		}
	}
	logLine, _ := json.Marshal(map[string]any{"args": args, "cwd": cwd, "argv": os.Args[1:], "env": gitEnv})
	if f, err := os.OpenFile(filepath.Join(dir, "git.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, string(logLine))
		f.Close()
	}
	raw, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake git: no scenario:", err)
		return 128
	}
	var s gitScenario
	if err := json.Unmarshal(raw, &s); err != nil {
		fmt.Fprintln(os.Stderr, "fake git: bad scenario:", err)
		return 128
	}
	unsupported := func() int {
		fmt.Fprintf(os.Stderr, "fake git: unsupported: %q\n", args)
		return 128
	}
	if len(args) == 0 {
		return unsupported()
	}
	if slices.Contains(s.Fail, args[0]) {
		fmt.Fprintf(os.Stderr, "fatal: %s failed (scenario)\n", args[0])
		return 128
	}
	switch args[0] {
	case "rev-parse":
		return s.revParse(args[1:], cwd, unsupported)
	case "status":
		return s.status(args[1:], unsupported)
	case "ls-files":
		return s.lsFiles(args[1:], unsupported)
	case "worktree":
		if len(args) != 3 || args[1] != "list" || args[2] != "--porcelain" {
			return unsupported()
		}
		return s.worktreeList()
	case "config":
		return s.config(args[1:], unsupported)
	case "merge-base":
		if len(args) != 4 || args[1] != "--is-ancestor" {
			return unsupported()
		}
		if args[3] != "origin/main" && args[3] != "refs/remotes/origin/main" {
			return unsupported()
		}
		commit := args[2]
		if commit == "HEAD" {
			commit = s.Head
		}
		switch {
		case slices.Contains(s.OriginMain, commit):
			return 0
		case commit == s.Head || slices.Contains(s.Known, commit):
			return 1
		default:
			fmt.Fprintf(os.Stderr, "fatal: Not a valid commit name %s\n", commit)
			return 128
		}
	}
	return unsupported()
}

func (s gitScenario) revParse(args []string, cwd string, unsupported func() int) int {
	absolute := slices.Contains(args, "--path-format=absolute")
	here := canonical(filepath.Join(cwd, ".git"))
	show := func(p string) string {
		if !absolute && canonical(p) == here {
			return ".git"
		}
		return p
	}
	var out []string
	for _, a := range args {
		switch a {
		case "--path-format=absolute", "--verify", "-q", "--quiet":
		case "--git-dir":
			out = append(out, show(s.GitDir))
		case "--absolute-git-dir":
			out = append(out, s.GitDir)
		case "--git-common-dir":
			out = append(out, show(s.CommonDir))
		case "--show-toplevel":
			top := s.TopLevel
			if top == "" {
				top = canonical(cwd)
			}
			out = append(out, top)
		case "HEAD", "HEAD^{commit}":
			out = append(out, s.Head)
		default:
			return unsupported()
		}
	}
	if len(out) == 0 {
		return unsupported()
	}
	fmt.Println(strings.Join(out, "\n"))
	return 0
}

func (s gitScenario) status(args []string, unsupported func() int) int {
	porcelain, ignored, nul := false, false, false
	untracked := s.ShowUntracked != "no"
	for _, a := range args {
		switch a {
		case "--porcelain", "--porcelain=v1", "--short", "-s":
			porcelain = true
		case "--ignored", "--ignored=traditional", "--ignored=matching":
			ignored = true
		case "-uno", "--untracked-files=no":
			untracked = false
		case "-u", "-uall", "-unormal", "--untracked-files", "--untracked-files=all", "--untracked-files=normal":
			untracked = true
		case "-z":
			nul = true
		case "--ignore-submodules=none":
		default:
			return unsupported()
		}
	}
	if !porcelain {
		return unsupported()
	}
	lines := slices.Clone(s.Modified)
	if untracked {
		lines = append(lines, s.Untracked...)
	}
	if ignored {
		lines = append(lines, s.Ignored...)
	}
	sep := "\n"
	if nul {
		sep = "\x00"
	}
	for _, l := range lines {
		fmt.Print(l, sep)
	}
	return 0
}

func (s gitScenario) lsFiles(args []string, unsupported func() int) int {
	verbose, nul := false, false
	for _, a := range args {
		switch a {
		case "-v":
			verbose = true
		case "-z":
			nul = true
		default:
			return unsupported()
		}
	}
	lines := s.Index
	if len(lines) == 0 {
		lines = []string{"H README.md"}
	}
	sep := "\n"
	if nul {
		sep = "\x00"
	}
	for _, l := range lines {
		if !verbose {
			l = l[2:]
		}
		fmt.Print(l, sep)
	}
	return 0
}

// worktreeList prints what git 2.53.0 prints for worktree list --porcelain (t071-gitprobe.sh):
// the main worktree (the git dir itself when it is not named .git, as for --separate-git-dir),
// then each linked worktree. An entry of worktrees/ without a gitdir file is not listed
// (t071-gitprobe2.sh); one whose directory is gone is marked prunable.
func (s gitScenario) worktreeList() int {
	main := s.CommonDir
	if filepath.Base(main) == ".git" {
		main = filepath.Dir(main)
	}
	entry := func(path string, prunable bool) {
		fmt.Printf("worktree %s\nHEAD %s\nbranch refs/heads/main\n", path, s.Head)
		if prunable {
			fmt.Println("prunable gitdir file points to non-existent location")
		}
		fmt.Println()
	}
	entry(main, false)
	dirs, _ := os.ReadDir(filepath.Join(s.CommonDir, "worktrees"))
	for _, d := range dirs {
		raw, err := os.ReadFile(filepath.Join(s.CommonDir, "worktrees", d.Name(), "gitdir"))
		if err != nil {
			continue
		}
		path := filepath.Dir(strings.TrimSpace(string(raw)))
		_, err = os.Stat(path)
		entry(path, err != nil)
	}
	for _, path := range s.LinkedWorktrees {
		entry(path, false)
	}
	return 0
}

// config answers config [--local] --get <key> (exit 1 when unset) and config [--local] --list.
func (s gitScenario) config(args []string, unsupported func() int) int {
	if len(args) > 0 && args[0] == "--local" {
		args = args[1:]
	}
	switch {
	case len(args) == 2 && args[0] == "--get":
		v, ok := s.Config[strings.ToLower(args[1])]
		if !ok {
			return 1
		}
		fmt.Println(v)
		return 0
	case len(args) == 1 && (args[0] == "--list" || args[0] == "-l"):
		keys := make([]string, 0, len(s.Config))
		for k := range s.Config {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			fmt.Printf("%s=%s\n", k, s.Config[k])
		}
		return 0
	}
	return unsupported()
}

func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// fakeGitDir installs the fake git with a scenario in a fresh directory and returns the path of
// the git executable. replace substitutes placeholders such as @MAIN@ in the fixture.
func fakeGitDir(t *testing.T, scenario string, replace map[string]string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "git", scenario+".json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for k, v := range replace {
		text = strings.ReplaceAll(text, k, v)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "scenario.json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(bin, "git")
	if err := os.Symlink(self, git); err != nil {
		t.Fatal(err)
	}
	return git
}

// gitCalls returns the logged fake-git invocations next to git.
func gitCalls(t *testing.T, git string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(git), "git.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// apiCredential is one credential the fake API knows.
type apiCredential struct {
	Name         string   `json:"name"`
	Class        string   `json:"class"`
	Account      string   `json:"account"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Policy       []string `json:"policy"`
}

type apiFixture struct {
	Credentials []apiCredential `json:"credentials"`
}

type apiCall struct {
	Method, Path, Credential string
	Status                   int
}

// fakeAPI is an OVHcloud API double for the account binding.
type fakeAPI struct {
	*httptest.Server
	creds  map[string]apiCredential // by client id
	tokens map[string]apiCredential // by access token
	mu     sync.Mutex
	calls  []apiCall
}

// requiredAction is the IAM action each modelled route needs: kb/api/v1/me.json (GET /me:
// account:apiovh:me/get, required) and kb/api/v1/auth.json (GET /auth/details: no iamActions).
var requiredAction = map[string]string{
	"/me":           "account:apiovh:me/get",
	"/auth/details": "",
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	var fx apiFixture
	readJSON(t, filepath.Join("testdata", "api", "credentials.json"), &fx)
	f := &fakeAPI{creds: map[string]apiCredential{}, tokens: map[string]apiCredential{}}
	for _, c := range fx.Credentials {
		f.creds[c.ClientID] = c
		f.tokens["fake-token-"+c.Name] = c
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/oauth2/token", f.token)
	mux.HandleFunc("/v1/", f.api)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// API returns the client configuration pointing at the fake.
func (f *fakeAPI) API() API {
	return API{TokenURL: f.URL + "/auth/oauth2/token", BaseURL: f.URL + "/v1", HTTP: f.Client()}
}

func (f *fakeAPI) credential(name string) apiCredential {
	for _, c := range f.creds {
		if c.Name == name {
			return c
		}
	}
	panic("no fake credential " + name)
}

func (f *fakeAPI) record(r *http.Request, cred string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, apiCall{Method: r.Method, Path: r.URL.Path, Credential: cred, Status: status})
}

func (f *fakeAPI) Calls() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// token implements the client-credentials grant of
// docs/en/guides/account-and-service-management/account-information/authenticate-api-with-service-account.mdx:
// form fields grant_type, client_id, client_secret (or HTTP basic auth), scope.
func (f *fakeAPI) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		// A secret in the URL would reach proxy and server logs.
		f.record(r, "", http.StatusBadRequest)
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "client_credentials" {
		f.record(r, "", http.StatusBadRequest)
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	c, known := f.creds[id]
	if !known || secret != c.ClientSecret {
		f.record(r, id, http.StatusUnauthorized)
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	f.record(r, c.Name, http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "fake-token-" + c.Name, "token_type": "Bearer", "expires_in": 3599, "scope": "all",
	})
}

func (f *fakeAPI) api(w http.ResponseWriter, r *http.Request) {
	route := strings.TrimPrefix(r.URL.Path, "/v1")
	c, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		f.record(r, "", http.StatusUnauthorized)
		http.Error(w, `{"message":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	action, modelled := requiredAction[route]
	if r.Method != http.MethodGet || !modelled {
		f.record(r, c.Name, http.StatusNotFound)
		http.Error(w, `{"message":"not modelled by the fake"}`, http.StatusNotFound)
		return
	}
	if action != "" && !allows(c.Policy, action) {
		f.record(r, c.Name, http.StatusForbidden)
		http.Error(w, fmt.Sprintf(`{"message":"This call has not been granted","action":%q}`, action), http.StatusForbidden)
		return
	}
	body, err := os.ReadFile(filepath.Join("testdata", "api", strings.ReplaceAll(strings.Trim(route, "/"), "/", "-")+".json"))
	if err != nil {
		f.record(r, c.Name, http.StatusInternalServerError)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f.record(r, c.Name, http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(strings.NewReplacer("@ACCOUNT@", c.Account, "@CLIENT@", c.ClientID).Replace(string(body))))
}

// allows matches an IAM action against policy actions, where a trailing * matches any suffix.
func allows(policy []string, action string) bool {
	for _, p := range policy {
		if p == action || (strings.HasSuffix(p, "*") && strings.HasPrefix(action, strings.TrimSuffix(p, "*"))) {
			return true
		}
	}
	return false
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// readEnvFile parses a KEY=value fixture (comments and blank lines skipped).
func readEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("%s: bad line %q", path, line)
		}
		out[k] = v
	}
	return out
}
