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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// quietSecret is the client secret the hostile fakes below echo. It starts with four characters a
// JSON escape error quotes, so a decode error that quotes the answer quotes part of it. It holds
// no English, so no four characters of it occur in an honest message ("binding", "decoding").
const quietSecret = "s3cr7Qx9Zk4vW0j8"

// quietLocation is an unparseable redirect target ("%zz" is no URL escape) that quotes the secret.
// Go's client parses a 3xx answer's Location before it consults CheckRedirect, and quotes the
// header in the error it returns (net/http client.go, "failed to parse Location header").
const quietLocation = "http://evil.example/%zz/" + quietSecret

// leaksSecretPart reports a four-character window of quietSecret inside s: a fragment of the
// secret is as much a leak as the whole of it.
func leaksSecretPart(s string) string {
	for i := 0; i+4 <= len(quietSecret); i++ {
		if strings.Contains(s, quietSecret[i:i+4]) {
			return quietSecret[i : i+4]
		}
	}
	return ""
}

// hostileBindingAPI answers the token request and GET /auth/details well, except the call named
// by hostile, which answer answers instead.
func hostileBindingAPI(t *testing.T, hostile string, answer http.HandlerFunc) API {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := r.Method + " " + r.URL.Path
		if call == hostile {
			answer(w, r)
			return
		}
		switch call {
		case "POST /token":
			_ = r.ParseForm()
			if r.PostForm.Get("client_secret") != quietSecret {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, `{"access_token":"quiet-token","token_type":"Bearer","expires_in":3600}`)
		case "GET /v1/auth/details":
			if r.Header.Get("Authorization") != "Bearer quiet-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, `{"account":"`+boundAccount+`"}`)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	t.Cleanup(srv.Close)
	return API{TokenURL: srv.URL + "/token", BaseURL: srv.URL + "/v1", HTTP: srv.Client()}
}

var quietCredential = Credential{Endpoint: "ovh-eu", ClientID: "EU.quiet", ClientSecret: quietSecret}

// TestBindingErrorsQuiet (FR-003, guard G16): an error from the binding client never quotes what
// the answer chose (a redirect's Location, a body) nor the secret; it names the call and the
// status class.
func TestBindingErrorsQuiet(t *testing.T) {
	t.Parallel()
	redirect := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", quietLocation)
		w.WriteHeader(http.StatusFound)
	}
	// An escape error quotes "\u" and the next four characters of the body.
	undecodable := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"\u`+quietSecret+`","account":"\u`+quietSecret+`"}`)
	}
	for _, tc := range []struct {
		name, hostile, call string
		answer              http.HandlerFunc
		class               string // the status class the error names; "" when not asserted
	}{
		// Defect exposed: the %w transport path (binding.go:54) quotes the unparseable Location.
		{"token-redirect", "POST /token", "token endpoint", redirect, "3xx"},
		{"auth-details-redirect", "GET /v1/auth/details", "GET /auth/details", redirect, "3xx"},
		// Defect exposed: the %w decode path (binding.go:63) quotes the body.
		{"token-undecodable", "POST /token", "token endpoint", undecodable, ""},
		{"auth-details-undecodable", "GET /v1/auth/details", "GET /auth/details", undecodable, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := hostileBindingAPI(t, tc.hostile, tc.answer)
			err := Bind(context.Background(), api, quietCredential, bound, "lz")
			if err == nil {
				t.Fatal("Bind() bound against a hostile answer")
			}
			msg := err.Error()
			if part := leaksSecretPart(msg); part != "" {
				t.Errorf("error %q quotes the secret (%q)", msg, part)
			}
			for _, leak := range []string{"evil.example", "%zz"} {
				if strings.Contains(msg, leak) {
					t.Errorf("error %q quotes the redirect URL (%q)", msg, leak)
				}
			}
			if !strings.Contains(msg, tc.call) {
				t.Errorf("error %q does not name the call %q", msg, tc.call)
			}
			if tc.class != "" && !strings.Contains(msg, tc.class) {
				t.Errorf("error %q does not name the status class %q", msg, tc.class)
			}
		})
	}
	// Positive control (passes from the start): a well-behaved API binds, so a fix that refuses
	// every answer shows here.
	t.Run("well-behaved-binds", func(t *testing.T) {
		t.Parallel()
		api := hostileBindingAPI(t, "", nil)
		if err := Bind(context.Background(), api, quietCredential, bound, "lz"); err != nil {
			t.Fatalf("Bind() = %v, want bound", err)
		}
	})
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
