package live

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Bounds of the read-only API-client listing (T084, from the T083 review proposals): a listed id
// that becomes a path segment cannot re-route the GET, a cursor that never ends is bounded, and
// an answer over the client's size limit is an error. The fake answers one project whose account
// is empty except for what a row puts in `answers` (keyed by the escaped path as sent).

type boundsAPI struct {
	*httptest.Server
	mu      sync.Mutex
	answers map[string]func(cursor string) (body, next string)
	sent    []string // escaped paths as received, version included
}

func newBoundsAPI(t *testing.T) *boundsAPI {
	t.Helper()
	b := &boundsAPI{answers: map[string]func(string) (string, string){}}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_ = r.ParseForm()
			if r.Method != http.MethodPost || r.PostForm.Get("client_id") != listClientID || r.PostForm.Get("client_secret") != listSecret {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"access_token":%q}`, listToken)
			return
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+listToken {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		path := r.URL.EscapedPath()
		b.mu.Lock()
		b.sent = append(b.sent, path)
		answer, ok := b.answers[path]
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case ok:
			body, next := answer(r.Header.Get("X-Pagination-Cursor"))
			if next != "" {
				w.Header().Set("X-Pagination-Cursor-Next", next)
			}
			fmt.Fprint(w, body)
		case strings.HasPrefix(path, "/v2/iam/resource/"):
			fmt.Fprint(w, `{"urn":"x","tags":{}}`)
		case strings.HasPrefix(path, "/v1/") && strings.HasSuffix(path, "/policy"):
			fmt.Fprint(w, `{"policy":""}`)
		default:
			fmt.Fprint(w, "[]")
		}
	}))
	t.Cleanup(b.Close)
	return b
}

func (b *boundsAPI) set(path, body string) {
	b.answers[path] = func(string) (string, string) { return body, "" }
}

func (b *boundsAPI) Sent() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.sent...)
}

func (b *boundsAPI) check() LeftoverCheck {
	return LeftoverCheck{
		API:      API{TokenURL: b.URL + "/token", BaseURL: b.URL + "/v1", HTTP: b.Client()},
		Cred:     listCred,
		Projects: []Project{{ID: "p1", URN: "urn:v1:eu:resource:publicCloudProject:p1"}},
		Prefix:   "lzprobe-",
	}
}

// TestLeftoversAPIChildPathEscaped: a listed id becomes exactly one escaped path segment of its
// child listing (a `/`, `?`, `#` or `%` in it cannot change the path, add a query or cut it), and
// an id that is a dot segment is refused with an error naming the listing, never requested.
func TestLeftoversAPIChildPathEscaped(t *testing.T) {
	const nets = "/v1/cloud/project/p1/network/private"
	for _, id := range []string{"pn-a/../../../me", "pn-a?x=1", "pn-a#frag", "pn-a%2Fb", "pn a"} {
		t.Run(url.PathEscape(id), func(t *testing.T) {
			b := newBoundsAPI(t)
			raw, _ := json.Marshal([]map[string]string{{"id": id, "name": "lzprobe-net"}})
			b.set(nets, string(raw))
			rep := b.check().Check(context.Background(), nil)
			want := nets + "/" + url.PathEscape(id) + "/subnet"
			var children []string
			for _, p := range b.Sent() {
				if strings.HasPrefix(p, nets+"/") {
					children = append(children, p)
				}
			}
			if len(children) != 1 || children[0] != want {
				t.Errorf("child requests %v, want exactly [%s]", children, want)
			}
			if len(rep.Errors) != 0 || len(rep.Leftovers) != 1 || rep.Leftovers[0].ID != id {
				t.Errorf("errors %v, leftovers %v; want no error and the network %q as the one leftover", rep.Errors, rep.Leftovers, id)
			}
		})
	}
	for _, id := range []string{".", ".."} {
		t.Run("dot-segment-"+id, func(t *testing.T) {
			b := newBoundsAPI(t)
			raw, _ := json.Marshal([]map[string]string{{"id": id, "name": "lzprobe-net"}})
			b.set(nets, string(raw))
			rep := b.check().Check(context.Background(), nil)
			for _, p := range b.Sent() {
				if strings.HasPrefix(p, nets+"/") || strings.HasSuffix(p, "/subnet") {
					t.Errorf("requested %s for the id %q", p, id)
				}
			}
			if rep.Outcome != "fail" || !strings.Contains(strings.Join(rep.Errors, "\n"), "/cloud/project/p1/network/private") {
				t.Errorf("outcome %q, errors %v; want fail naming the network listing", rep.Outcome, rep.Errors)
			}
		})
	}
}

// TestLeftoversAPIEndlessCursor: a listing whose next cursor never ends — the same cursor again,
// or a new one on every page — is an error naming the listing after a bounded number of pages,
// never a hang until the deadline.
func TestLeftoversAPIEndlessCursor(t *testing.T) {
	const pol = "/v2/iam/policy"
	for name, next := range map[string]func(cursor string, n int) string{
		"same-cursor":      func(string, int) string { return "opaque-same" },
		"new-cursor-again": func(_ string, n int) string { return fmt.Sprintf("opaque-%d", n) },
	} {
		t.Run(name, func(t *testing.T) {
			b := newBoundsAPI(t)
			n := 0
			b.answers[pol] = func(cursor string) (string, string) {
				n++
				return "[]", next(cursor, n)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			rep := b.check().Check(ctx, nil)
			if ctx.Err() != nil {
				t.Fatalf("the listing ran until the deadline (%d pages)", n)
			}
			asked := 0
			for _, p := range b.Sent() {
				if p == pol {
					asked++
				}
			}
			if asked > maxListingPages+1 {
				t.Errorf("asked %s %d times, want at most %d", pol, asked, maxListingPages+1)
			}
			if rep.Outcome != "fail" || !strings.Contains(strings.Join(rep.Errors, "\n"), "/iam/policy") {
				t.Errorf("outcome %q, errors %v; want fail naming /iam/policy", rep.Outcome, rep.Errors)
			}
		})
	}
	t.Run("cursor-repeat-detected-early", func(t *testing.T) {
		b := newBoundsAPI(t)
		n := 0
		b.answers[pol] = func(string) (string, string) { n++; return "[]", "opaque-same" }
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = b.check().Check(ctx, nil)
		if n > 2 {
			t.Errorf("a repeated cursor was followed %d times, want it refused on its first repeat", n)
		}
	})
	t.Run("many-pages-pass", func(t *testing.T) { // bound control: a long but finite listing passes
		b := newBoundsAPI(t)
		b.answers[pol] = func(cursor string) (string, string) {
			var k int
			fmt.Sscanf(cursor, "opaque-%d", &k)
			if k+1 >= 50 {
				return "[]", ""
			}
			return "[]", fmt.Sprintf("opaque-%d", k+1)
		}
		if rep := b.check().Check(context.Background(), nil); rep.Outcome != "pass" {
			t.Errorf("a 50-page listing: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
		}
	})
}

// TestLeftoversAPIOversize: an answer longer than the client's limit is an error naming the
// listing, also when its first maxListing bytes would parse (an array padded with whitespace); an
// answer of exactly the limit is read.
func TestLeftoversAPIOversize(t *testing.T) {
	const groups = "/v1/me/identity/group"
	pad := func(n int) string { return "[]" + strings.Repeat(" ", n-2) }
	t.Run("over-the-limit", func(t *testing.T) {
		b := newBoundsAPI(t)
		b.set(groups, pad(maxListing+1))
		rep := b.check().Check(context.Background(), nil)
		if rep.Outcome != "fail" || !strings.Contains(strings.Join(rep.Errors, "\n"), "/me/identity/group") {
			t.Errorf("outcome %q, errors %v; want fail naming /me/identity/group", rep.Outcome, rep.Errors)
		}
	})
	t.Run("at-the-limit", func(t *testing.T) {
		b := newBoundsAPI(t)
		b.set(groups, pad(maxListing))
		if rep := b.check().Check(context.Background(), nil); rep.Outcome != "pass" {
			t.Errorf("an answer of exactly %d bytes: outcome %q, errors %v; want pass", maxListing, rep.Outcome, rep.Errors)
		}
	})
}
