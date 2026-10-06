package live

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Defence-in-depth clauses the implementation claims (T053 review round 1), one control each.

// TestEnvCredsCannotReplacePathOrHome: a credential file cannot redirect which binary or home
// directory a child uses.
func TestEnvCredsCannotReplacePathOrHome(t *testing.T) {
	creds := map[string]string{"PATH": "/evil/bin", "HOME": "/evil/home", "PATH=/evil/bin:": "v", "OVH_CLIENT_ID": "id"}
	cmd := Command(AuthorityTenant, creds, "true")
	for _, want := range []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "OVH_CLIENT_ID=id"} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("child env lacks %s: %v", want, cmd.Env)
		}
	}
	for _, kv := range cmd.Env {
		if strings.Contains(kv, "/evil") {
			t.Errorf("credential replaced %s", kv)
		}
	}
}

// TestBindingNoRedirect: a redirect from the token endpoint never carries the secret elsewhere.
func TestBindingNoRedirect(t *testing.T) {
	var leaked []string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		leaked = append(leaked, string(body))
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"access_token":"stolen"}`)
	}))
	t.Cleanup(sink.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL+"/token", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	api := API{TokenURL: redirect.URL + "/token", BaseURL: sink.URL, HTTP: redirect.Client()}
	if _, err := api.Account(context.Background(), Credential{Endpoint: "ovh-eu", ClientID: "id", ClientSecret: "the-secret"}); err == nil {
		t.Error("Account() followed a token redirect without error")
	}
	for _, b := range leaked {
		if strings.Contains(b, "the-secret") {
			t.Fatalf("secret re-sent to the redirect target: %q", b)
		}
	}
	if len(leaked) != 0 {
		t.Fatalf("redirect followed (%d requests at the target)", len(leaked))
	}
}

// TestBindingSendsNothingOnMismatch: endpoint and org are compared before the credential is sent
// anywhere.
func TestBindingSendsNothingOnMismatch(t *testing.T) {
	f := newFakeAPI(t)
	admin := credentialOf(f.credential("admin"))
	ca := admin
	ca.Endpoint = "ovh-ca"
	assertRefused(t, Bind(context.Background(), f.API(), ca, bound, "lz"), []string{CondEndpoint})
	assertRefused(t, Bind(context.Background(), f.API(), admin, bound, "acme"), []string{CondOrg})
	if calls := f.Calls(); len(calls) != 0 {
		t.Fatalf("credential sent on a mismatch: %+v", calls)
	}
}

// TestBindingErrorOmitsAnswer: neither the API's answer nor the secret enters an error.
func TestBindingErrorOmitsAnswer(t *testing.T) {
	f := newFakeAPI(t)
	c := credentialOf(f.credential("admin"))
	c.ClientSecret = "wrong-secret-value"
	err := Bind(context.Background(), f.API(), c, bound, "lz")
	if err == nil {
		t.Fatal("rejected credential bound")
	}
	for _, leak := range []string{"invalid_client", "wrong-secret-value", c.ClientID} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q carries %q", err, leak)
		}
	}
}

// TestFilesWriteRefusesOpenDirectory: data-model "directories 700" holds for directories that
// already exist too, the root included.
func TestFilesWriteRefusesOpenDirectory(t *testing.T) {
	withUmask(t)
	for name, open := range map[string]string{"root": ".", "accounts": "accounts"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "ovh-lz")
			if err := os.MkdirAll(filepath.Join(root, "accounts"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, d := range []string{root, filepath.Join(root, "accounts")} {
				if err := os.Chmod(d, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(filepath.Join(root, open), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := WriteCredentialFile(root, "accounts/x.env", map[string]string{"K": "v"}); err == nil {
				t.Fatalf("written below a 0755 %s directory, want refused", name)
			}
			if _, err := os.Lstat(filepath.Join(root, "accounts", "x.env")); !os.IsNotExist(err) {
				t.Fatalf("x.env exists after a refused write (%v)", err)
			}
		})
	}
}
