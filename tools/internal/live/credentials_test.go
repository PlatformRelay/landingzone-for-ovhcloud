package live

// T058: credential selection by authority (spec 005 FR-010, research R6, data-model *Stage table*
// and *Account binding and local files*; contracts/checks.md V007 "tenant stack with platform
// credentials", guard G1 "always load sandbox.env"). Every instance gets its stage's authority and
// exactly that authority's two files of the bound account, never another authority's file, another
// tenant's or another account's, and never sandbox.env as a fallback.

import (
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
