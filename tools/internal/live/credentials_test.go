package live

// T058: credential selection by authority (spec 005 FR-010, research R6, data-model *Stage table*
// and *Account binding and local files*; contracts/checks.md V007 "tenant stack with platform
// credentials", guard G1 "always load sandbox.env"). Every instance gets its stage's authority and
// exactly that authority's two files of the bound account, never another authority's file, another
// tenant's or another account's, and never sandbox.env as a fallback.

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

const (
	credAccount      = "xx000058-ovh"
	credOtherAccount = "xx000059-ovh"
)

// credManifest is the repository's sandbox manifest, plus (two) a second tenant `ops` with its
// tenant-state, project and runtime rows (decoded only, never generated).
func credManifest(t *testing.T, two bool) *stacks.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(laneRepoRoot, stacks.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	data := string(raw)
	if two {
		replace := func(old, new string) {
			if strings.Count(data, old) != 1 {
				t.Fatalf("sandbox manifest drifted: %q not found once", old)
			}
			data = strings.Replace(data, old, new, 1)
		}
		replace("\n    ],\n    \"instances\": [",
			",\n      {\"name\": \"ops\", \"environments\": [{\"name\": \"dev\", \"project\": {\"mode\": \"reference\", \"ref\": \"OPS_DEV\"}, "+
				"\"regions\": [{\"name\": \"GRA11\", \"network\": {\"cidr\": \"10.30.0.0/24\", \"vlan_id\": 1}}], "+
				"\"budget_alert\": {\"enabled\": false}, \"quota_guard\": {\"enabled\": false}}]}\n    ],\n    \"instances\": [")
		row := `{"id": "demo-dev-gra11-runtime", "stage": "runtime", "tenant": "demo", "environment": "dev", "region": "GRA11"}`
		replace(row, row+`,
      {"id": "ops-state", "stage": "tenant-state", "tenant": "ops"},
      {"id": "ops-dev-project", "stage": "project", "tenant": "ops", "environment": "dev"},
      {"id": "ops-dev-gra11-runtime", "stage": "runtime", "tenant": "ops", "environment": "dev", "region": "GRA11"}`)
	}
	m, err := stacks.DecodeManifest([]byte(data))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m
}

// credFile is one credential file of the fixture: its path below the config root and its values.
// Every value is unique to its file and account, so a variable read from the wrong file shows.
type credFile struct {
	rel    string
	values map[string]string
}

func oauthValues(tag string) map[string]string {
	return map[string]string{"OVH_ENDPOINT": "ovh-eu", "OVH_CLIENT_ID": "EU.client-" + tag, "OVH_CLIENT_SECRET": "lz-seed-t058-oauth-secret-" + tag}
}

func s3Values(tag string) map[string]string {
	return map[string]string{"AWS_ACCESS_KEY_ID": "AK-" + tag, "AWS_SECRET_ACCESS_KEY": "lz-seed-t058-s3-secret-" + tag}
}

// credFiles are every credential file of the fixture config root: both accounts, both tenants.
// sandbox.env is the one file outside accounts/ (the active admin, data-model).
func credFiles() []credFile {
	files := []credFile{{"sandbox.env", oauthValues("admin")}}
	for _, a := range []string{credAccount, credOtherAccount} {
		files = append(files,
			credFile{filepath.Join("accounts", a, "state.env"), s3Values(a + "-account")},
			credFile{filepath.Join("accounts", a, "platform-deployer.env"), oauthValues(a + "-platform")})
		for _, tn := range []string{"demo", "ops"} {
			files = append(files,
				credFile{filepath.Join("accounts", a, "tenants", tn, "deployer.env"), oauthValues(a + "-" + tn + "-deployer")},
				credFile{filepath.Join("accounts", a, "tenants", tn, "state.env"), s3Values(a + "-" + tn + "-tenant")},
				credFile{filepath.Join("accounts", a, "tenants", tn, "platform-state.env"), s3Values(a + "-" + tn + "-platform")})
		}
	}
	return files
}

// credRoot writes every credential file through files.go (0600 below 0700 directories) and
// returns the config root.
func credRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ovh-lz")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range credFiles() {
		if err := WriteCredentialFile(root, f.rel, f.values); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func credValues(t *testing.T, rel string) map[string]string {
	t.Helper()
	for _, f := range credFiles() {
		if f.rel == rel {
			return f.values
		}
	}
	t.Fatalf("no fixture file %s", rel)
	return nil
}

// credExpected is the authority of each row (data-model *Stage table*, research R6) and its two
// files, OAuth2 first.
var credExpected = map[string]struct {
	authority Authority
	files     [2]string
}{
	"account-bootstrap":      {AuthorityBootstrap, [2]string{"sandbox.env", "accounts/" + credAccount + "/state.env"}},
	"account-governance":     {AuthorityBootstrap, [2]string{"sandbox.env", "accounts/" + credAccount + "/state.env"}},
	"demo-state":             {AuthorityBootstrap, [2]string{"sandbox.env", "accounts/" + credAccount + "/state.env"}},
	"demo-dev-project":       {AuthorityPlatform, [2]string{"accounts/" + credAccount + "/platform-deployer.env", "accounts/" + credAccount + "/tenants/demo/platform-state.env"}},
	"demo-dev-gra11-network": {AuthorityTenant, [2]string{"accounts/" + credAccount + "/tenants/demo/deployer.env", "accounts/" + credAccount + "/tenants/demo/state.env"}},
	"demo-dev-gra11-runtime": {AuthorityTenant, [2]string{"accounts/" + credAccount + "/tenants/demo/deployer.env", "accounts/" + credAccount + "/tenants/demo/state.env"}},
	"ops-state":              {AuthorityBootstrap, [2]string{"sandbox.env", "accounts/" + credAccount + "/state.env"}},
	"ops-dev-project":        {AuthorityPlatform, [2]string{"accounts/" + credAccount + "/platform-deployer.env", "accounts/" + credAccount + "/tenants/ops/platform-state.env"}},
	"ops-dev-gra11-runtime":  {AuthorityTenant, [2]string{"accounts/" + credAccount + "/tenants/ops/deployer.env", "accounts/" + credAccount + "/tenants/ops/state.env"}},
}

func credWant(t *testing.T, id string) map[string]string {
	t.Helper()
	e := credExpected[id]
	want := map[string]string{}
	maps.Copy(want, credValues(t, filepath.FromSlash(e.files[0])))
	maps.Copy(want, credValues(t, filepath.FromSlash(e.files[1])))
	return want
}

// credNoValue fails when err's text carries any fixture value of a secret.
func credNoValue(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	for _, f := range credFiles() {
		for k, v := range f.values {
			if strings.Contains(k, "SECRET") && strings.Contains(err.Error(), v) {
				t.Errorf("the error carries the value of %s from %s: %v", k, f.rel, err)
			}
		}
	}
}

// Every row's authority is its stage's principal; an unknown or external id is an error.
func TestCredentialsAuthorityOf(t *testing.T) {
	t.Parallel()
	m := credManifest(t, true)
	for _, in := range m.Instances {
		e, ok := credExpected[in.ID]
		if !ok {
			t.Fatalf("fixture has no expectation for row %s", in.ID)
		}
		got, err := AuthorityOf(m, in.ID)
		if err != nil || got != e.authority {
			t.Errorf("AuthorityOf(%s) = %q, %v; want %q", in.ID, got, err, e.authority)
		}
	}
	for _, id := range []string{"", "unknown-stack", "demo-dev"} {
		if got, err := AuthorityOf(m, id); err == nil {
			t.Errorf("AuthorityOf(%q) = %q, <nil>; want an error (not a row)", id, got)
		}
	}
}

// Every row gets exactly the five variables of its authority's two files of the bound account:
// the bootstrap rows sandbox.env + the account state.env, the platform `project` the platform
// deployer + its tenant's platform-state.env, the tenant rows their tenant's deployer + state.env.
// Every other file of the config root exists with distinct values, so a variable from another
// authority, tenant or account (or sandbox.env for every row, G1's mutant) shows.
func TestCredentialsSelectByAuthority(t *testing.T) {
	t.Parallel()
	root := credRoot(t)
	m := credManifest(t, true)
	for _, in := range m.Instances {
		t.Run(in.ID, func(t *testing.T) {
			a, got, err := LoadCredentials(root, credAccount, m, in.ID)
			if err != nil {
				t.Fatalf("LoadCredentials: %v", err)
			}
			if want := credExpected[in.ID].authority; a != want {
				t.Errorf("authority %q, want %q", a, want)
			}
			want := credWant(t, in.ID)
			if !maps.Equal(got, want) {
				keys := slices.Sorted(maps.Keys(got))
				t.Errorf("variables %v (client %q, access key %q); want exactly those of %v (client %q, access key %q)",
					keys, got["OVH_CLIENT_ID"], got["AWS_ACCESS_KEY_ID"], credExpected[in.ID].files, want["OVH_CLIENT_ID"], want["AWS_ACCESS_KEY_ID"])
			}
		})
	}
}

// A variable a file holds beyond the authority's five never reaches the children: the selection
// passes the five, not the file.
func TestCredentialsOnlyAuthorityVariables(t *testing.T) {
	t.Parallel()
	root := credRoot(t)
	m := credManifest(t, false)
	rel := filepath.Join("accounts", credAccount, "tenants", "demo", "deployer.env")
	extra := maps.Clone(credValues(t, rel))
	extra["TF_VAR_state_passphrase"] = "lz-seed-t058-extra-passphrase"
	extra["AWS_SECRET_ACCESS_KEY"] = "lz-seed-t058-extra-s3-secret"
	if err := WriteCredentialFile(root, rel, extra); err != nil {
		t.Fatal(err)
	}
	_, got, err := LoadCredentials(root, credAccount, m, "demo-dev-gra11-runtime")
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if want := credWant(t, "demo-dev-gra11-runtime"); !maps.Equal(got, want) {
		t.Errorf("variables %v, want exactly the deployer's OAuth2 client and the tenant state.env keys (extra variables of a file dropped)", slices.Sorted(maps.Keys(got)))
	}
}

// A missing file of the selected authority blocks the stack (exit 2): its producer has not run.
// Nothing is returned, and no other file (sandbox.env above all, G1) stands in for it.
func TestCredentialsMissingFileBlocks(t *testing.T) {
	t.Parallel()
	m := credManifest(t, false)
	for _, id := range []string{"account-governance", "demo-dev-project", "demo-dev-gra11-runtime"} {
		for _, rel := range credExpected[id].files {
			t.Run(id+"/"+filepath.Base(rel), func(t *testing.T) {
				root := credRoot(t)
				if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
					t.Fatal(err)
				}
				_, got, err := LoadCredentials(root, credAccount, m, id)
				var b *Blocked
				if !errors.As(err, &b) || ExitCode(err) != BlockedExit {
					t.Errorf("err %v (exit %d), want *Blocked (exit %d) naming the missing file", err, ExitCode(err), BlockedExit)
				} else if !strings.Contains(err.Error(), filepath.Base(rel)) {
					t.Errorf("err %v does not name %s", err, filepath.Base(rel))
				}
				if len(got) != 0 {
					t.Errorf("variables %v returned with a missing file", slices.Sorted(maps.Keys(got)))
				}
				credNoValue(t, err)
			})
		}
	}
}

// A credential file others may read is refused (exit 3, CondFileMode), whichever of the two.
func TestCredentialsFileModeRefused(t *testing.T) {
	t.Parallel()
	m := credManifest(t, false)
	for _, id := range []string{"demo-state", "demo-dev-project", "demo-dev-gra11-network"} {
		for _, rel := range credExpected[id].files {
			t.Run(id+"/"+filepath.Base(rel), func(t *testing.T) {
				root := credRoot(t)
				if err := os.Chmod(filepath.Join(root, filepath.FromSlash(rel)), 0o644); err != nil {
					t.Fatal(err)
				}
				_, got, err := LoadCredentials(root, credAccount, m, id)
				var r *Refusal
				if !errors.As(err, &r) || r.Condition != CondFileMode {
					t.Errorf("err %v, want a %s refusal (exit 3)", err, CondFileMode)
				}
				if len(got) != 0 {
					t.Errorf("variables returned from a world-readable file")
				}
			})
		}
	}
}

// A file without one of the authority's variables is refused (CondCredentials); nothing returned.
func TestCredentialsIncompleteFileRefused(t *testing.T) {
	t.Parallel()
	m := credManifest(t, false)
	cases := []struct{ id, rel, drop string }{
		{"demo-dev-gra11-runtime", "accounts/" + credAccount + "/tenants/demo/deployer.env", "OVH_CLIENT_SECRET"},
		{"demo-dev-gra11-runtime", "accounts/" + credAccount + "/tenants/demo/state.env", "AWS_ACCESS_KEY_ID"},
		{"demo-dev-project", "accounts/" + credAccount + "/platform-deployer.env", "OVH_ENDPOINT"},
		{"account-governance", "sandbox.env", "OVH_CLIENT_ID"},
	}
	for _, c := range cases {
		t.Run(c.id+"/"+filepath.Base(c.rel)+"-"+c.drop, func(t *testing.T) {
			root := credRoot(t)
			rel := filepath.FromSlash(c.rel)
			v := maps.Clone(credValues(t, rel))
			delete(v, c.drop)
			if err := WriteCredentialFile(root, rel, v); err != nil {
				t.Fatal(err)
			}
			_, got, err := LoadCredentials(root, credAccount, m, c.id)
			var r *Refusal
			if !errors.As(err, &r) || r.Condition != CondCredentials {
				t.Errorf("err %v, want a %s refusal naming %s", err, CondCredentials, c.drop)
			} else if !strings.Contains(err.Error(), c.drop) {
				t.Errorf("err %v does not name %s", err, c.drop)
			}
			if len(got) != 0 {
				t.Errorf("variables returned from an incomplete file")
			}
			credNoValue(t, err)
		})
	}
}

// FR-010 "refuses a mismatch", V007 "tenant stack with platform credentials": a selected file
// that holds another authority's client or S3 key of the same account (copied over, or the admin
// pasted into a deployer file) is refused (CondCredentials) and nothing is returned; the refusal
// names the files, never a value.
func TestCredentialsMismatchRefused(t *testing.T) {
	t.Parallel()
	m := credManifest(t, false)
	acct := "accounts/" + credAccount + "/"
	cases := []struct{ name, id, rel, from string }{
		{"platform-deployer-in-tenant-file", "demo-dev-gra11-runtime", acct + "tenants/demo/deployer.env", acct + "platform-deployer.env"},
		{"admin-in-tenant-file", "demo-dev-gra11-network", acct + "tenants/demo/deployer.env", "sandbox.env"},
		{"platform-s3-in-tenant-state", "demo-dev-gra11-runtime", acct + "tenants/demo/state.env", acct + "tenants/demo/platform-state.env"},
		{"account-s3-in-tenant-state", "demo-dev-gra11-runtime", acct + "tenants/demo/state.env", acct + "state.env"},
		{"admin-in-platform-file", "demo-dev-project", acct + "platform-deployer.env", "sandbox.env"},
		{"tenant-deployer-in-platform-file", "demo-dev-project", acct + "platform-deployer.env", acct + "tenants/demo/deployer.env"},
		{"account-s3-in-platform-state", "demo-dev-project", acct + "tenants/demo/platform-state.env", acct + "state.env"},
		{"tenant-s3-in-platform-state", "demo-dev-project", acct + "tenants/demo/platform-state.env", acct + "tenants/demo/state.env"},
		{"platform-deployer-as-admin", "account-governance", "sandbox.env", acct + "platform-deployer.env"},
		{"tenant-s3-as-account-state", "demo-state", acct + "state.env", acct + "tenants/demo/state.env"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := credRoot(t)
			if err := WriteCredentialFile(root, filepath.FromSlash(c.rel), credValues(t, filepath.FromSlash(c.from))); err != nil {
				t.Fatal(err)
			}
			_, got, err := LoadCredentials(root, credAccount, m, c.id)
			var r *Refusal
			if !errors.As(err, &r) || r.Condition != CondCredentials || ExitCode(err) != RefusalExit {
				t.Errorf("err %v, want a %s refusal (exit 3): %s holds the credential of %s", err, CondCredentials, c.rel, c.from)
			}
			if len(got) != 0 {
				t.Errorf("variables returned (client %q, access key %q) from a mismatched file", got["OVH_CLIENT_ID"], got["AWS_ACCESS_KEY_ID"])
			}
			credNoValue(t, err)
		})
	}
}

// The same authority across tenants (review r1): with two tenants, ops' deployer or S3 keys copied
// into demo's file is refused for demo's tenant stacks and its platform `project`.
func TestCredentialsCrossTenantMismatchRefused(t *testing.T) {
	t.Parallel()
	m := credManifest(t, true)
	acct := "accounts/" + credAccount + "/tenants/"
	cases := []struct{ name, id, rel, from string }{
		{"ops-deployer-in-demo", "demo-dev-gra11-runtime", acct + "demo/deployer.env", acct + "ops/deployer.env"},
		{"ops-tenant-s3-in-demo", "demo-dev-gra11-runtime", acct + "demo/state.env", acct + "ops/state.env"},
		{"ops-platform-s3-in-demo", "demo-dev-project", acct + "demo/platform-state.env", acct + "ops/platform-state.env"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := credRoot(t)
			if err := WriteCredentialFile(root, filepath.FromSlash(c.rel), credValues(t, filepath.FromSlash(c.from))); err != nil {
				t.Fatal(err)
			}
			_, got, err := LoadCredentials(root, credAccount, m, c.id)
			var r *Refusal
			if !errors.As(err, &r) || r.Condition != CondCredentials {
				t.Errorf("err %v, want a %s refusal: %s holds the credential of %s", err, CondCredentials, c.rel, c.from)
			}
			if len(got) != 0 {
				t.Errorf("variables returned from another tenant's credential")
			}
			credNoValue(t, err)
		})
	}
}

// The bound account's directory only: with the other account's files and no file of the bound
// account's tenant, a tenant row is blocked, never served from the other account.
func TestCredentialsBoundAccountOnly(t *testing.T) {
	t.Parallel()
	m := credManifest(t, false)
	root := credRoot(t)
	for _, rel := range []string{"deployer.env", "state.env"} {
		if err := os.Remove(filepath.Join(root, "accounts", credAccount, "tenants", "demo", rel)); err != nil {
			t.Fatal(err)
		}
	}
	_, got, err := LoadCredentials(root, credAccount, m, "demo-dev-gra11-runtime")
	if ExitCode(err) != BlockedExit || len(got) != 0 {
		t.Errorf("err %v, variables %v; want blocked with nothing (the other account's files are never read)", err, slices.Sorted(maps.Keys(got)))
	}
	// "x/../<other>" resolves to the other account's existing directory (review r1: a raw path
	// would serve its files): refused as an account id, not blocked and not served.
	bad := "x/../" + credOtherAccount
	if _, got, err := LoadCredentials(root, bad, m, "account-governance"); err == nil || ExitCode(err) == BlockedExit || len(got) != 0 {
		t.Errorf("account %q: err %v, variables %v; want an error that is not blocked: an account id is one path segment", bad, err, slices.Sorted(maps.Keys(got)))
	}
}

// T059, decision 2: a missing file names what writes it, so the operator knows what to run:
// sandbox.env and the account's state.env bootstrap:account, the deployer files
// account-governance, a tenant's S3 files that tenant's tenant-state instance.
func TestCredentialsMissingFileNamesProducer(t *testing.T) {
	t.Parallel()
	m := credManifest(t, true)
	acct := "accounts/" + credAccount + "/"
	for _, c := range []struct{ id, rel, producer string }{
		{"account-governance", "sandbox.env", "bootstrap:account"},
		{"demo-state", acct + "state.env", "bootstrap:account"},
		{"demo-dev-project", acct + "platform-deployer.env", "account-governance"},
		{"demo-dev-project", acct + "tenants/demo/platform-state.env", "demo-state"},
		{"demo-dev-gra11-network", acct + "tenants/demo/deployer.env", "account-governance"},
		{"demo-dev-gra11-network", acct + "tenants/demo/state.env", "demo-state"},
		{"ops-dev-gra11-runtime", acct + "tenants/ops/state.env", "ops-state"},
	} {
		t.Run(c.id+"/"+c.rel, func(t *testing.T) {
			root := credRoot(t)
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(c.rel))); err != nil {
				t.Fatal(err)
			}
			_, _, err := LoadCredentials(root, credAccount, m, c.id)
			var b *Blocked
			if !errors.As(err, &b) {
				t.Fatalf("err %v, want *Blocked", err)
			}
			if !strings.Contains(err.Error(), c.producer) {
				t.Errorf("err %v does not name %s, which writes %s", err, c.producer, c.rel)
			}
		})
	}
}

// T008 (spec 006 FR-005, data-model *Credential set*): the readers of credential sets and the
// migration from 005's flat files. A set lives under accounts/<a>/sets/<instance>/; `current` names
// its generation gen-<run_id>/, which holds the set's files at the paths readers use today. While
// `current` does not exist, readers use the flat files. Every generation here is laid down by hand,
// so a reader case judges the reader alone; the binding case judges the writer.

const (
	csetGov     = "account-governance"
	csetPrev    = "gen-20261014T090000Z-0aa0"
	csetCur     = "gen-20261015T101500Z-1a2b"
	csetPartial = "gen-20261016T090000Z-0bb0" // sorts last: a crashed write that never switched
	csetTenant  = "gen-20261015T111500Z-3c4d" // a tenant-state write after account-governance's
)

// csetGovFiles is the account-governance set (paths relative to the account directory); every
// value carries tag.
func csetGovFiles(tag string) map[string]map[string]string {
	return map[string]map[string]string{
		"platform-deployer.env":                          oauthValues(tag + "-platform"),
		filepath.Join("tenants", "demo", "deployer.env"): oauthValues(tag + "-demo-deployer"),
		filepath.Join("tenants", "ops", "deployer.env"):  oauthValues(tag + "-ops-deployer"),
	}
}

// csetStateFiles is the set of tenant's tenant-state instance.
func csetStateFiles(tenant, tag string) map[string]map[string]string {
	return map[string]map[string]string{
		filepath.Join("tenants", tenant, "state.env"):          s3Values(tag + "-" + tenant + "-tenant"),
		filepath.Join("tenants", tenant, "platform-state.env"): s3Values(tag + "-" + tenant + "-platform"),
	}
}

func csetDir(root, instance string) string {
	return filepath.Join(root, "accounts", credAccount, "sets", instance)
}

// csetLay writes generation gen of instance's set by hand (through WriteCredentialFile: 0600
// files below 0700 directories), not through a writer under test.
func csetLay(t *testing.T, root, instance, gen string, files map[string]map[string]string) {
	t.Helper()
	for rel, values := range files {
		if err := WriteCredentialFile(root, filepath.Join("accounts", credAccount, "sets", instance, gen, rel), values); err != nil {
			t.Fatal(err)
		}
	}
}

// csetCrashed leaves the leftovers of crashed later writes of instance at once (data-model steps 1
// and 3): a newer generation holding only the first file of files, and a stale `current.tmp`
// naming it. Neither may be followed while `current` names another generation or is absent.
func csetCrashed(t *testing.T, root, instance string, files map[string]map[string]string) {
	t.Helper()
	first := slices.Sorted(maps.Keys(files))[0]
	csetLay(t, root, instance, csetPartial, map[string]map[string]string{first: files[first]})
	if err := os.WriteFile(filepath.Join(csetDir(root, instance), "current.tmp"), []byte(csetPartial), 0o600); err != nil {
		t.Fatal(err)
	}
}

// csetView is what a reader must see: the flat files of credRoot, with each listed set's files
// (relative to the account directory) in place of the flat ones.
func csetView(sets ...map[string]map[string]string) map[string]map[string]string {
	view := map[string]map[string]string{}
	for _, f := range credFiles() {
		view[filepath.ToSlash(f.rel)] = f.values
	}
	for _, s := range sets {
		for rel, v := range s {
			view["accounts/"+credAccount+"/"+filepath.ToSlash(rel)] = v
		}
	}
	return view
}

// csetLoadAll checks LoadCredentials for every row of the two-tenant manifest against view.
func csetLoadAll(t *testing.T, root string, view map[string]map[string]string) {
	t.Helper()
	m := credManifest(t, true)
	for _, in := range m.Instances {
		_, got, err := LoadCredentials(root, credAccount, m, in.ID)
		credNoValue(t, err)
		if err != nil {
			t.Errorf("%s: LoadCredentials: %v", in.ID, err)
			continue
		}
		files := credExpected[in.ID].files
		want := map[string]string{}
		maps.Copy(want, view[files[0]])
		maps.Copy(want, view[files[1]])
		if !maps.Equal(got, want) {
			t.Errorf("%s: client %q, access key %q; want client %q (%s), access key %q (%s)",
				in.ID, got["OVH_CLIENT_ID"], got["AWS_ACCESS_KEY_ID"], want["OVH_CLIENT_ID"], files[0], want["AWS_ACCESS_KEY_ID"], files[1])
		}
	}
}

// CredentialSetPath resolves `current` to its generation, and falls back to the flat path only
// while `current` does not exist (a first write that crashed before its switch included). A
// previous generation, and a newer one that `current.tmp` names (a later write that crashed), are
// not followed.
func TestCredentialSetPathResolves(t *testing.T) {
	t.Parallel()
	rels := []string{"platform-deployer.env", filepath.Join("tenants", "demo", "deployer.env")}
	cases := []struct {
		name  string
		lay   func(t *testing.T, root string)
		inGen string // "" for the flat path
	}{
		{"current-names-generation", func(t *testing.T, root string) {
			csetLay(t, root, csetGov, csetPrev, csetGovFiles("prev"))
			csetLay(t, root, csetGov, csetCur, csetGovFiles("cur"))
			csPoint(t, csetDir(root, csetGov), csetCur)
			csetCrashed(t, root, csetGov, csetGovFiles("partial"))
		}, csetCur},
		{"no-set-flat", func(t *testing.T, root string) {}, ""},
		{"first-write-crashed-flat", func(t *testing.T, root string) {
			csetCrashed(t, root, csetGov, csetGovFiles("partial"))
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := credRoot(t)
			c.lay(t, root)
			for _, rel := range rels {
				want := filepath.Join(root, "accounts", credAccount, rel)
				if c.inGen != "" {
					want = filepath.Join(csetDir(root, csetGov), c.inGen, rel)
				}
				got, err := CredentialSetPath(root, credAccount, csetGov, rel)
				if err != nil || filepath.Clean(got) != want {
					t.Errorf("CredentialSetPath(%s) = %q, %v; want %q", rel, got, err, want)
				}
			}
		})
	}
}

// A `current` that does not name a gen- directory beside it is refused, never followed and never
// replaced by the flat files (which exist here): a missing generation, a parent path (`../x` with
// a set there, `../..` which is the account directory holding the flat files), a name outside the
// pattern with a directory of that name, an absolute path, an empty pointer, a generation that is
// a file, and a generation that is a symlink to a directory outside the set.
func TestCredentialSetPathPointerInvalid(t *testing.T) {
	t.Parallel()
	rel := "platform-deployer.env"
	cases := []struct {
		name string
		lay  func(t *testing.T, root string) string // returns the pointer's content
	}{
		{"missing-directory", func(t *testing.T, root string) string {
			csetLay(t, root, csetGov, csetPrev, csetGovFiles("prev"))
			return csetCur
		}},
		{"parent-escape", func(t *testing.T, root string) string {
			csetLay(t, root, "x", "", csetGovFiles("escaped"))
			return "../x"
		}},
		{"account-directory", func(t *testing.T, root string) string { return "../.." }},
		{"not-a-generation-name", func(t *testing.T, root string) string {
			csetLay(t, root, csetGov, "x", csetGovFiles("other"))
			return "x"
		}},
		{"absolute", func(t *testing.T, root string) string {
			csetLay(t, root, csetGov, csetCur, csetGovFiles("cur"))
			return filepath.Join(csetDir(root, csetGov), csetCur)
		}},
		{"empty", func(t *testing.T, root string) string {
			csetLay(t, root, csetGov, csetCur, csetGovFiles("cur"))
			return ""
		}},
		{"generation-is-a-file", func(t *testing.T, root string) string {
			csetLay(t, root, csetGov, csetPrev, csetGovFiles("prev"))
			if err := os.WriteFile(filepath.Join(csetDir(root, csetGov), csetCur), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return csetCur
		}},
		{"generation-is-a-symlink", func(t *testing.T, root string) string {
			csetLay(t, root, "outside", "", csetGovFiles("outside"))
			csetLay(t, root, csetGov, csetPrev, csetGovFiles("prev"))
			if err := os.Symlink(filepath.Join(root, "accounts", credAccount, "sets", "outside"), filepath.Join(csetDir(root, csetGov), csetCur)); err != nil {
				t.Fatal(err)
			}
			return csetCur
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := credRoot(t)
			if err := os.MkdirAll(csetDir(root, csetGov), 0o700); err != nil {
				t.Fatal(err)
			}
			csPoint(t, csetDir(root, csetGov), c.lay(t, root))
			got, err := CredentialSetPath(root, credAccount, csetGov, rel)
			if err == nil || !strings.HasPrefix(err.Error(), "CREDENTIAL_POINTER_INVALID") {
				t.Errorf("CredentialSetPath = %q, %v; want an error starting CREDENTIAL_POINTER_INVALID", got, err)
			}
		})
	}
}

// Migration and per-writer pointers through LoadCredentials (two tenants): every row gets its
// authority's variables from the set its writer owns, and from the flat files while that set has
// no `current`. The bootstrap rows keep reading sandbox.env and the account state.env, which no set
// holds.
func TestCredentialsFromSets(t *testing.T) {
	t.Parallel()
	// Today's flat files, no `current` (a first write that crashed before its switch included):
	// the flat files are read.
	t.Run("flat-files-without-current", func(t *testing.T) {
		t.Parallel()
		root := credRoot(t)
		csetCrashed(t, root, csetGov, csetGovFiles("partial"))
		csetLoadAll(t, root, csetView())
	})
	// Flat files and a `current` for every writer (a crash after the switch, before the flat files
	// were removed): the generations win; neither the previous generation nor a newer one that
	// `current.tmp` names is read.
	t.Run("generation-after-switch", func(t *testing.T) {
		t.Parallel()
		root := credRoot(t)
		gov, demo, ops := csetGovFiles("cur"), csetStateFiles("demo", "cur"), csetStateFiles("ops", "cur")
		csetLay(t, root, csetGov, csetPrev, csetGovFiles("prev"))
		csetLay(t, root, csetGov, csetCur, gov)
		csPoint(t, csetDir(root, csetGov), csetCur)
		csetCrashed(t, root, csetGov, csetGovFiles("partial"))
		for inst, files := range map[string]map[string]map[string]string{"demo-state": demo, "ops-state": ops} {
			csetLay(t, root, inst, csetCur, files)
			csPoint(t, csetDir(root, inst), csetCur)
		}
		csetLoadAll(t, root, csetView(gov, demo, ops))
	})
	// demo's tenant-state wrote its set after account-governance: each set has its own pointer and
	// generation name, and each also holds a stale generation under the other's current name, so a
	// reader that takes one pointer for all writers reads stale values. ops-state has written no set
	// yet: its files stay flat while the others are sets.
	t.Run("each-writer-its-own-pointer", func(t *testing.T) {
		t.Parallel()
		root := credRoot(t)
		gov, demo := csetGovFiles("gov"), csetStateFiles("demo", "demo")
		csetLay(t, root, csetGov, csetCur, gov)
		csetLay(t, root, csetGov, csetTenant, csetGovFiles("stale"))
		csPoint(t, csetDir(root, csetGov), csetCur)
		csetLay(t, root, "demo-state", csetTenant, demo)
		csetLay(t, root, "demo-state", csetCur, csetStateFiles("demo", "stale"))
		csPoint(t, csetDir(root, "demo-state"), csetTenant)
		csetLoadAll(t, root, csetView(gov, demo))
	})
}

// A set write never touches the account binding (account.env), the passphrase, sandbox.env or the
// bootstrap's state.env, nor the flat files of a writer that has no set yet: after a first
// account-governance write (the migration), demo's tenant-state write and a second
// account-governance write, each is byte-identical and still a 0600 regular file.
func TestCredentialSetWriteKeepsBinding(t *testing.T) {
	t.Parallel()
	root := credRoot(t)
	acct := filepath.Join("accounts", credAccount)
	for rel, v := range map[string]map[string]string{
		"account.env":          {"OVH_ACCOUNT": credAccount, "OVH_ENDPOINT": "ovh-eu"},
		"state-passphrase.env": {"TF_VAR_state_passphrase": "lz-seed-t008-passphrase"},
	} {
		if err := WriteCredentialFile(root, filepath.Join(acct, rel), v); err != nil {
			t.Fatal(err)
		}
	}
	kept := []string{
		"sandbox.env",
		filepath.Join(acct, "account.env"),
		filepath.Join(acct, "state-passphrase.env"),
		filepath.Join(acct, "state.env"),
		filepath.Join(acct, "tenants", "ops", "state.env"),
		filepath.Join(acct, "tenants", "ops", "platform-state.env"),
	}
	before := map[string][]byte{}
	for _, rel := range kept {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		before[rel] = b
	}
	writes := []CredentialSet{
		{Root: root, Account: credAccount, Instance: csetGov, RunID: "20261015T101500Z-1a2b", Files: csetGovFiles("first")},
		{Root: root, Account: credAccount, Instance: "demo-state", RunID: "20261015T111500Z-3c4d", Files: csetStateFiles("demo", "first")},
		{Root: root, Account: credAccount, Instance: csetGov, RunID: "20261015T121500Z-5e6f", Files: csetGovFiles("second")},
	}
	for _, w := range writes {
		if err := WriteCredentialSet(w, 0); err != nil {
			t.Fatalf("WriteCredentialSet(%s, run %s): %v", w.Instance, w.RunID, err)
		}
		for _, rel := range kept {
			p := filepath.Join(root, rel)
			b, err := os.ReadFile(p)
			switch {
			case err != nil:
				t.Errorf("after the %s write of run %s: %s: %v", w.Instance, w.RunID, rel, err)
			case !bytes.Equal(b, before[rel]):
				t.Errorf("after the %s write of run %s: %s changed", w.Instance, w.RunID, rel)
			default:
				if fi, err := os.Lstat(p); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
					t.Errorf("after the %s write of run %s: %s is no longer a 0600 regular file", w.Instance, w.RunID, rel)
				}
			}
		}
	}
}

// T014 (FR-005, data-model *Credential set*): the reader takes `current`'s whole content as the
// generation name, as the writer does: a pointer with a line break or surrounding whitespace,
// beside a directory of the trimmed name, is refused by both, never trimmed by one of them.
func TestCredentialSetPointerExact(t *testing.T) {
	t.Parallel()
	for name, pointer := range map[string]string{
		"trailing-newline": csetCur + "\n",
		"trailing-crlf":    csetCur + "\r\n",
		"leading-space":    " " + csetCur,
		"trailing-space":   csetCur + " ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := credRoot(t)
			csetLay(t, root, csetGov, csetCur, csetGovFiles("cur"))
			csPoint(t, csetDir(root, csetGov), pointer)
			got, err := CredentialSetPath(root, credAccount, csetGov, "platform-deployer.env")
			if err == nil || !strings.HasPrefix(err.Error(), "CREDENTIAL_POINTER_INVALID") {
				t.Errorf("reader: CredentialSetPath = %q, %v; want an error starting CREDENTIAL_POINTER_INVALID", got, err)
			}
			w := CredentialSet{Root: root, Account: credAccount, Instance: csetGov, RunID: "20261016T101500Z-7a7b", Files: csetGovFiles("next")}
			if err := WriteCredentialSet(w, 0); err == nil || !strings.HasPrefix(err.Error(), "CREDENTIAL_POINTER_INVALID") {
				t.Errorf("writer: WriteCredentialSet = %v; want an error starting CREDENTIAL_POINTER_INVALID", err)
			}
		})
	}
}

// One load resolves each set's `current` once (data-model: "readers resolve `current` once per
// load"): a pointer switched between two of its files would otherwise give one load files of two
// generations. Every row of the two-tenant manifest is loaded with each writer's set in place.
func TestCredentialsResolveCurrentOnce(t *testing.T) {
	notParallel(t)
	root := credRoot(t)
	csetLay(t, root, csetGov, csetCur, csetGovFiles("gov"))
	csPoint(t, csetDir(root, csetGov), csetCur)
	for _, tenant := range []string{"demo", "ops"} {
		csetLay(t, root, tenant+"-state", csetCur, csetStateFiles(tenant, tenant))
		csPoint(t, csetDir(root, tenant+"-state"), csetCur)
	}
	old := setCurrent
	t.Cleanup(func() { setCurrent = old })
	resolved := map[string]int{}
	setCurrent = func(setDir string) (string, error) {
		resolved[filepath.Base(setDir)]++
		return old(setDir)
	}
	m := credManifest(t, true)
	for _, in := range m.Instances {
		clear(resolved)
		if _, _, err := LoadCredentials(root, credAccount, m, in.ID); err != nil {
			t.Errorf("%s: LoadCredentials: %v", in.ID, err)
			continue
		}
		a, _ := AuthorityOf(m, in.ID)
		var own []string
		if a != AuthorityBootstrap {
			own = []string{csetGov, in.Tenant + "-state"}
		}
		for _, set := range own {
			if resolved[set] != 1 {
				t.Errorf("%s: the %s set's current resolved %d times in one load, want once", in.ID, set, resolved[set])
			}
		}
		for set, n := range resolved {
			if n > 1 {
				t.Errorf("%s: the %s set's current resolved %d times in one load, want at most once", in.ID, set, n)
			}
		}
	}
}

// A set is read only from where its writer put it (review round 1): a symlinked `sets/` or
// `sets/<instance>/` (pointing at another account's valid set), a symlinked directory inside the
// generation, or a symlinked file in it is refused, never followed, never replaced by the flat
// files, by CredentialSetPath and by LoadCredentials alike.
func TestCredentialSetNoSymlinks(t *testing.T) {
	t.Parallel()
	rel := filepath.Join("tenants", "demo", "deployer.env")
	other := func(root string) string { return filepath.Join(root, "accounts", credOtherAccount, "sets", csetGov) }
	cases := []struct {
		name string
		lay  func(t *testing.T, root string)
	}{
		{"set-directory", func(t *testing.T, root string) {
			lay := filepath.Join("accounts", credOtherAccount, "sets", csetGov, csetCur)
			for r, v := range csetGovFiles("foreign") {
				if err := WriteCredentialFile(root, filepath.Join(lay, r), v); err != nil {
					t.Fatal(err)
				}
			}
			csPoint(t, other(root), csetCur)
			if err := os.MkdirAll(filepath.Join(root, "accounts", credAccount, "sets"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other(root), csetDir(root, csetGov)); err != nil {
				t.Fatal(err)
			}
		}},
		{"sets-directory", func(t *testing.T, root string) {
			lay := filepath.Join("accounts", credOtherAccount, "sets", csetGov, csetCur)
			for r, v := range csetGovFiles("foreign") {
				if err := WriteCredentialFile(root, filepath.Join(lay, r), v); err != nil {
					t.Fatal(err)
				}
			}
			csPoint(t, other(root), csetCur)
			if err := os.Symlink(filepath.Dir(other(root)), filepath.Join(root, "accounts", credAccount, "sets")); err != nil {
				t.Fatal(err)
			}
		}},
		// Review round 2: the account directory, or accounts/, is a symlink to another account's
		// tree holding a valid set (the writer's credentialDir refuses both).
		{"account-directory", func(t *testing.T, root string) {
			lay := filepath.Join("accounts", credOtherAccount, "sets", csetGov, csetCur)
			for r, v := range csetGovFiles("foreign") {
				if err := WriteCredentialFile(root, filepath.Join(lay, r), v); err != nil {
					t.Fatal(err)
				}
			}
			csPoint(t, other(root), csetCur)
			acct := filepath.Join(root, "accounts", credAccount)
			if err := os.RemoveAll(acct); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "accounts", credOtherAccount), acct); err != nil {
				t.Fatal(err)
			}
		}},
		{"accounts-directory", func(t *testing.T, root string) {
			elsewhere := filepath.Join(root, "elsewhere")
			lay := filepath.Join("elsewhere", credAccount, "sets", csetGov, csetCur)
			if err := os.Mkdir(elsewhere, 0o700); err != nil {
				t.Fatal(err)
			}
			for r, v := range csetGovFiles("foreign") {
				if err := WriteCredentialFile(root, filepath.Join(lay, r), v); err != nil {
					t.Fatal(err)
				}
			}
			csPoint(t, filepath.Join(elsewhere, credAccount, "sets", csetGov), csetCur)
			if err := os.RemoveAll(filepath.Join(root, "accounts")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, filepath.Join(root, "accounts")); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory-in-generation", func(t *testing.T, root string) {
			csetLay(t, root, csetGov, csetCur, map[string]map[string]string{"platform-deployer.env": oauthValues("cur-platform")})
			csPoint(t, csetDir(root, csetGov), csetCur)
			if err := os.Symlink(filepath.Join(root, "accounts", credOtherAccount, "tenants"), filepath.Join(csetDir(root, csetGov), csetCur, "tenants")); err != nil {
				t.Fatal(err)
			}
		}},
		{"file-in-generation", func(t *testing.T, root string) {
			files := csetGovFiles("cur")
			delete(files, rel)
			csetLay(t, root, csetGov, csetCur, files)
			csPoint(t, csetDir(root, csetGov), csetCur)
			if err := os.MkdirAll(filepath.Join(csetDir(root, csetGov), csetCur, filepath.Dir(rel)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "accounts", credOtherAccount, rel), filepath.Join(csetDir(root, csetGov), csetCur, rel)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := credRoot(t)
			c.lay(t, root)
			if got, err := CredentialSetPath(root, credAccount, csetGov, rel); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Errorf("CredentialSetPath(%s) = %q, %v; want a refusal of the symlink", rel, got, err)
			}
			m := credManifest(t, true)
			_, got, err := LoadCredentials(root, credAccount, m, "demo-dev-gra11-runtime")
			credNoValue(t, err)
			if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Errorf("LoadCredentials(demo-dev-gra11-runtime) = client %q, %v through a symlink; want a refusal of the symlink", got["OVH_CLIENT_ID"], err)
			}
		})
	}
}
