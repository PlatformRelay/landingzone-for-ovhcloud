package live

import (
	"context"
	"net/http"
	"testing"
)

// Account binding G13 (FR-010, research R13 *Account binding*, premise P26): every credential is
// checked against account.env through GET /auth/details, which needs no IAM action; GET /me needs
// account:apiovh:me/get, which neither deployer policy holds.

const boundAccount = "zz00000-ovh"

var bound = Binding{AccountID: boundAccount, Endpoint: "ovh-eu", Org: "lz"}

func credentialOf(c apiCredential) Credential {
	return Credential{Endpoint: "ovh-eu", ClientID: c.ClientID, ClientSecret: c.ClientSecret}
}

// TestBindingFakeAPIEnforcesPolicy pins the fake itself: without it, a binding calling GET /me
// would pass for every class.
func TestBindingFakeAPIEnforcesPolicy(t *testing.T) {
	f := newFakeAPI(t)
	for name, wantMe := range map[string]int{"admin": 200, "platform-deployer": 403, "tenant-deployer": 403} {
		t.Run(name, func(t *testing.T) {
			for path, want := range map[string]int{"/v1/me": wantMe, "/v1/auth/details": 200, "/v1/cloud/project": 404} {
				req, _ := http.NewRequest(http.MethodGet, f.URL+path, nil)
				req.Header.Set("Authorization", "Bearer fake-token-"+name)
				resp, err := f.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != want {
					t.Errorf("GET %s as %s = %d, want %d", path, name, resp.StatusCode, want)
				}
			}
		})
	}
}

func TestBindingAllClassesBind(t *testing.T) {
	f := newFakeAPI(t)
	for _, name := range []string{"admin", "platform-deployer", "tenant-deployer"} {
		t.Run(name, func(t *testing.T) {
			c := credentialOf(f.credential(name))
			got, err := f.API().Account(context.Background(), c)
			if err != nil || got != boundAccount {
				t.Fatalf("Account() = %q, %v; want %q", got, err, boundAccount)
			}
			if err := Bind(context.Background(), f.API(), c, bound, "lz"); err != nil {
				t.Fatalf("Bind() = %v, want bound", err)
			}
			asked := false
			for _, call := range f.Calls() {
				if call.Credential == name && call.Method == http.MethodGet && call.Path == "/v1/auth/details" && call.Status == 200 {
					asked = true
				}
			}
			if !asked {
				t.Fatalf("no successful GET /auth/details as %s; calls: %+v", name, f.Calls())
			}
			assertNoMe(t, f)
		})
	}
}

// assertNoMe: binding never calls GET /me, not even with a fallback after its 403 (P26).
func assertNoMe(t *testing.T, f *fakeAPI) {
	t.Helper()
	for _, call := range f.Calls() {
		if call.Path == "/v1/me" {
			t.Fatalf("binding called GET /me (as %s, status %d)", call.Credential, call.Status)
		}
	}
}

func TestBindingRefuses(t *testing.T) {
	f := newFakeAPI(t)
	admin := credentialOf(f.credential("admin"))
	otherAccount := credentialOf(f.credential("other-account-admin"))
	caEndpoint := admin
	caEndpoint.Endpoint = "ovh-ca"
	rows := []struct {
		name string
		cred Credential
		org  string
		want string
	}{
		{"account-differs", otherAccount, "lz", CondAccount},
		{"manifest-org-differs", admin, "acme", CondOrg},
		{"endpoint-differs", caEndpoint, "lz", CondEndpoint},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			assertRefused(t, Bind(context.Background(), f.API(), r.cred, bound, r.org), []string{r.want})
			assertNoMe(t, f)
		})
	}
	t.Run("deployer-account-differs", func(t *testing.T) {
		other := bound
		other.AccountID = "yy99999-ovh"
		c := credentialOf(f.credential("tenant-deployer"))
		assertRefused(t, Bind(context.Background(), f.API(), c, other, "lz"), []string{CondAccount})
	})
	t.Run("rejected-credential", func(t *testing.T) {
		c := admin
		c.ClientSecret = "wrong"
		if err := Bind(context.Background(), f.API(), c, bound, "lz"); err == nil || ExitCode(err) == 0 {
			t.Fatalf("Bind() with a rejected credential = %v (exit %d), want an error", err, ExitCode(err))
		}
	})
}

// TestBindingRefusesIncompleteBinding: an account.env missing a field binds nothing, even when
// the credential or the manifest is empty in the same place.
func TestBindingRefusesIncompleteBinding(t *testing.T) {
	f := newFakeAPI(t)
	admin := credentialOf(f.credential("admin"))
	noEndpoint := admin
	noEndpoint.Endpoint = ""
	for name, r := range map[string]struct {
		cred Credential
		b    Binding
		org  string
	}{
		"no-account":  {admin, Binding{Endpoint: "ovh-eu", Org: "lz"}, "lz"},
		"no-endpoint": {noEndpoint, Binding{AccountID: boundAccount, Org: "lz"}, "lz"},
		"no-org":      {admin, Binding{AccountID: boundAccount, Endpoint: "ovh-eu"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			assertRefused(t, Bind(context.Background(), f.API(), r.cred, r.b, r.org), []string{CondAccount, CondEndpoint, CondOrg})
		})
	}
}
