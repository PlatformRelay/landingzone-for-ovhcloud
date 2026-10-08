package live

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// T089: the live S3 object store of the account bucket (objectstore.go). A minimal SigV4 client:
// GET, PUT and HEAD only, pinned against the published AWS SigV4 vectors with a fixed clock
// (testdata/sigv4/PROVENANCE); the endpoint derived from the region and refused unless it is an
// OVHcloud Object Storage host; no secret outside the signature; answers size-capped and never
// echoed. The fake S3 servers below verify each request's signature from what they received, so
// a request that sends something other than what it signed is refused (403).

// ---------------------------------------------------------------- SigV4 vectors

// sigV4Vector is one vendored vector: the request and the expected canonical request, string to
// sign and Authorization value.
type sigV4Vector struct {
	name                   string
	method, path, rawQuery string
	header                 http.Header
	body                   []byte
	creq, sts, authz       string
}

// readSigV4Vector parses <dir>/<name>.req (the suite's layout: request line, header lines, a
// continuation line starting with white space, an empty line, the body) and its expected files.
func readSigV4Vector(t *testing.T, dir string) sigV4Vector {
	t.Helper()
	name := filepath.Base(dir)
	read := func(ext string) string {
		raw, err := os.ReadFile(filepath.Join(dir, name+"."+ext))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	v := sigV4Vector{name: name, header: http.Header{}, creq: read("creq"), sts: read("sts"), authz: strings.TrimRight(read("authz"), "\n")}
	req := read("req")
	head, body, _ := strings.Cut(req, "\n\n")
	v.body = []byte(body)
	lines := strings.Split(strings.TrimRight(head, "\n"), "\n")
	parts := strings.Split(lines[0], " ")
	if len(parts) != 3 {
		t.Fatalf("%s: request line %q", name, lines[0])
	}
	v.method = parts[0]
	v.path, v.rawQuery, _ = strings.Cut(parts[1], "?")
	var last string
	for _, l := range lines[1:] {
		if l != "" && (l[0] == ' ' || l[0] == '\t') {
			vals := v.header[last]
			vals[len(vals)-1] += "\n" + l
			continue
		}
		k, val, ok := strings.Cut(l, ":")
		if !ok {
			t.Fatalf("%s: header line %q", name, l)
		}
		last = http.CanonicalHeaderKey(k)
		v.header[last] = append(v.header[last], val)
	}
	return v
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// TestS3SigV4Vectors: the signer reproduces the canonical request, the string to sign and the
// Authorization value of every vendored vector: the AWS SigV4 test suite's GET vectors (botocore's
// copy) and the four worked S3 examples (GET Object, PUT Object, two bucket GETs), each with its
// published keys and fixed clock.
func TestS3SigV4Vectors(t *testing.T) {
	suites := []struct {
		dir                string
		ak, sk, region, sv string
		at                 time.Time
		sep                string // the Authorization separator the source prints
	}{
		{"testdata/sigv4/aws4_testsuite", "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "us-east-1", "service",
			time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC), ", "},
		{"testdata/sigv4/s3-examples", "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "s3",
			time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC), ","},
	}
	n := 0
	for _, s := range suites {
		dirs, err := os.ReadDir(s.dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range dirs {
			v := readSigV4Vector(t, filepath.Join(s.dir, d.Name()))
			n++
			t.Run(filepath.Base(s.dir)+"/"+v.name, func(t *testing.T) {
				query, err := url.ParseQuery(v.rawQuery)
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256Hex(v.body)
				if h := v.header.Get("X-Amz-Content-Sha256"); h != "" && h != hash {
					t.Fatalf("fixture: x-amz-content-sha256 %s is not the body's %s", h, hash)
				}
				creq, sts, authz := sigV4{accessKey: s.ak, secret: s.sk, region: s.region, service: s.sv}.sign(v.method, v.path, query, v.header, hash, s.at)
				if creq != v.creq {
					t.Errorf("canonical request\n%s\nwant\n%s", creq, v.creq)
				}
				if sts != v.sts {
					t.Errorf("string to sign\n%s\nwant\n%s", sts, v.sts)
				}
				if got := strings.ReplaceAll(authz, ", ", s.sep); got != v.authz {
					t.Errorf("Authorization\n%s\nwant\n%s", got, v.authz)
				}
			})
		}
	}
	if n != 18 {
		t.Errorf("%d vectors read, want the 18 vendored ones", n)
	}
}

// ---------------------------------------------------------------- the fake S3 server

const (
	s3TestAK     = "AK0EXAMPLE0ACCESS0KEY"
	s3TestSK     = "SK0example0secret0that0must0never0leave"
	s3TestBucket = "lz-bkt-state"
	s3TestHost   = "s3.gra.io.cloud.ovh.net"
)

var s3TestNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// s3Seen is one request as the fake S3 server received it.
type s3Seen struct {
	Method, Host, Path, RawQuery string
	RawPath                      string // the request URI as it arrived on the wire
	Header                       http.Header
	Body                         []byte
	SignedOK                     bool
}

// fakeS3 is an S3 path-style endpoint over TLS that every dial of its client reaches, whatever
// host the URL names. It checks each request's signature from what it received; secretOf gives
// the secret of an access key ("" refuses it). fault, when set, answers instead.
type fakeS3 struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	seen     []s3Seen
	objects  map[string][]byte
	secretOf func(ak string) string
	fault    func(rw http.ResponseWriter, r *http.Request) bool
	// get/put serve the object; nil: the in-memory map.
	get func(ak, bucket, key string) ([]byte, int)
	put func(ak, bucket, key string, data []byte) int
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()
	f := &fakeS3{t: t, objects: map[string][]byte{}, secretOf: func(ak string) string {
		if ak == s3TestAK {
			return s3TestSK
		}
		return ""
	}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// client dials the fake for every host and trusts its certificate (issued for example.com).
func (f *fakeS3) client() *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	addr := f.srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}}
}

func (f *fakeS3) requests() []s3Seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

// verify recomputes the request's signature from what arrived: the headers it names, the host,
// the path as received, the query and the body's hash.
func (f *fakeS3) verify(r *http.Request, body []byte) (string, bool) {
	authz := r.Header.Get("Authorization")
	rest, ok := strings.CutPrefix(authz, "AWS4-HMAC-SHA256 Credential=")
	if !ok {
		return "", false
	}
	cred, rest, _ := strings.Cut(rest, ", SignedHeaders=")
	signed, _, _ := strings.Cut(rest, ", Signature=")
	scope := strings.Split(cred, "/")
	if len(scope) != 5 || scope[2] != "gra" || scope[3] != "s3" || scope[4] != "aws4_request" {
		return "", false
	}
	sk := f.secretOf(scope[0])
	if sk == "" {
		return scope[0], false
	}
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil || at.Format("20060102") != scope[1] {
		return scope[0], false
	}
	if r.Header.Get("X-Amz-Content-Sha256") != sha256Hex(body) {
		return scope[0], false
	}
	h := http.Header{}
	for _, name := range strings.Split(signed, ";") {
		if name == "host" {
			h.Set("Host", r.Host)
			continue
		}
		h[http.CanonicalHeaderKey(name)] = r.Header.Values(name)
	}
	_, _, want := sigV4{accessKey: scope[0], secret: sk, region: "gra", service: "s3"}.sign(r.Method, r.URL.Path, r.URL.Query(), h, sha256Hex(body), at)
	return scope[0], want == authz
}

func (f *fakeS3) serve(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	ak, ok := f.verify(r, body)
	f.mu.Lock()
	f.seen = append(f.seen, s3Seen{Method: r.Method, Host: r.Host, Path: r.URL.Path, RawPath: r.RequestURI, RawQuery: r.URL.RawQuery,
		Header: r.Header.Clone(), Body: body, SignedOK: ok})
	f.mu.Unlock()
	if f.fault != nil && f.fault(rw, r) {
		return
	}
	if !ok {
		rw.WriteHeader(http.StatusForbidden)
		fmt.Fprint(rw, "<Error><Code>SignatureDoesNotMatch</Code></Error>")
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		var data []byte
		code := http.StatusOK
		if f.get != nil {
			data, code = f.get(ak, bucket, key)
		} else {
			f.mu.Lock()
			d, found := f.objects[bucket+"/"+key]
			f.mu.Unlock()
			if data = d; !found {
				code = http.StatusNotFound
			}
		}
		rw.WriteHeader(code)
		if code == http.StatusOK && r.Method == http.MethodGet {
			rw.Write(data)
		}
	case http.MethodPut:
		code := http.StatusOK
		if f.put != nil {
			code = f.put(ak, bucket, key, body)
		} else {
			f.mu.Lock()
			f.objects[bucket+"/"+key] = body
			f.mu.Unlock()
		}
		rw.WriteHeader(code)
	default:
		rw.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func s3TestKeys() map[string]string {
	return map[string]string{"AWS_ACCESS_KEY_ID": s3TestAK, "AWS_SECRET_ACCESS_KEY": s3TestSK}
}

func newTestS3Store(t *testing.T, f *fakeS3) *S3Store {
	t.Helper()
	s, err := NewS3Store(S3Options{Region: "gra", Keys: s3TestKeys(), HTTP: f.client(), Now: func() time.Time { return s3TestNow }})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	return s
}

// s3NoSecretIn fails when the secret appears in any request the fake received: URL, any header
// (Authorization included) or body.
func s3NoSecretIn(t *testing.T, f *fakeS3, secret string) {
	t.Helper()
	for _, r := range f.requests() {
		if strings.Contains(fmt.Sprintf("%s %s %s %s %v %s", r.Method, r.Host, r.RawPath, r.RawQuery, r.Header, r.Body), secret) {
			t.Errorf("%s %s carried the secret key", r.Method, r.Path)
		}
	}
}

// ---------------------------------------------------------------- endpoint and keys

// TestS3Endpoint: the endpoint is https://s3.<region>.io.cloud.ovh.net for an Object Storage
// region as the manifest writes it (spec.state.region, `^[a-z]+$`; kb guide
// storage-and-backup/object-storage/s3-getting-started-with-object-storage.mdx), the region's
// case folded; anything whose host would not be that pattern is refused (UNVERIFIED until T044).
func TestS3Endpoint(t *testing.T) {
	for region, want := range map[string]string{"gra": "https://s3.gra.io.cloud.ovh.net", "sbg": "https://s3.sbg.io.cloud.ovh.net", "GRA": "https://s3.gra.io.cloud.ovh.net"} {
		if got, err := ObjectStorageEndpoint(region); err != nil || got != want {
			t.Errorf("ObjectStorageEndpoint(%q) = %q, %v; want %q", region, got, err, want)
		}
	}
	for _, region := range []string{"", "gra11", "eu-west-par", "gra.evil.example", "gra/x", "gra#", "gra:443", "x@gra", "gra ", " gra", "grä", "gra\n", "rbx-archive"} {
		if got, err := ObjectStorageEndpoint(region); err == nil {
			t.Errorf("ObjectStorageEndpoint(%q) = %q, want a refusal", region, got)
		}
		if _, err := NewS3Store(S3Options{Region: region, Keys: s3TestKeys()}); err == nil {
			t.Errorf("NewS3Store with region %q: no refusal", region)
		}
	}
}

// TestS3StoreKeys: the store needs both of state.env's keys; a refusal names the variable, never
// a value.
// TestS3StoreClient (review r1): without an injected client the store gets a bounded timeout; with
// or without one, a redirect is never followed, and the caller's client is not modified.
func TestS3StoreClient(t *testing.T) {
	s, err := NewS3Store(S3Options{Region: "gra", Keys: s3TestKeys()})
	if err != nil {
		t.Fatal(err)
	}
	if s.http.Timeout <= 0 || s.http.Timeout > time.Minute || s.http.CheckRedirect == nil {
		t.Errorf("default client: timeout %v, redirect policy set %v", s.http.Timeout, s.http.CheckRedirect != nil)
	}
	mine := &http.Client{}
	if s, err = NewS3Store(S3Options{Region: "gra", Keys: s3TestKeys(), HTTP: mine}); err != nil {
		t.Fatal(err)
	}
	if s.http.CheckRedirect == nil || s.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse || mine.CheckRedirect != nil || s.http == mine {
		t.Error("an injected client follows redirects or was modified")
	}
}

func TestS3StoreKeys(t *testing.T) {
	for _, missing := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		keys := s3TestKeys()
		delete(keys, missing)
		_, err := NewS3Store(S3Options{Region: "gra", Keys: keys})
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("without %s: %v, want a refusal naming it", missing, err)
		}
		if err != nil && (strings.Contains(err.Error(), s3TestSK) || strings.Contains(err.Error(), s3TestAK)) {
			t.Errorf("without %s: the refusal carries a key: %v", missing, err)
		}
	}
	if _, err := NewS3Store(S3Options{Region: "gra", Keys: s3TestKeys()}); err != nil {
		t.Errorf("both keys: %v", err)
	}
}

// ---------------------------------------------------------------- round trip

// TestS3StoreRoundTrip: Put then Get reach https://s3.gra.io.cloud.ovh.net/<bucket>/<key>
// (path-style, kb s3-post-object-upload.mdx), signed for region gra and service s3 at the
// injected clock, with the body's SHA-256 in x-amz-content-sha256; the server verifies every
// signature from what it received; a missing object is fs.ErrNotExist; no request carries the
// secret, and the only credential material outside the signature is the access key id in
// Authorization. The URI on the wire is the canonical URI that was signed (each byte outside the
// unreserved set and '/' percent-encoded once), so a server that verifies against the URI as sent
// agrees with one that decodes and re-encodes it (review r1).
func TestS3StoreRoundTrip(t *testing.T) {
	f := newFakeS3(t)
	s := newTestS3Store(t, f)
	key := stacks.ArtifactKey("account-bootstrap")
	odd := "account/a b$c+d/outputs.json"
	odd2 := "account/50%/\u1234.json"
	if _, err := s.Get(s3TestBucket, key); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Get of a missing object: %v, want fs.ErrNotExist", err)
	}
	for _, k := range []string{key, odd, odd2} {
		data := []byte(`{"k":"` + k + `"}`)
		if err := s.Put(s3TestBucket, k, data); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
		got, err := s.Get(s3TestBucket, k)
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("Get %s = %q, %v; want %q", k, got, err, data)
		}
	}
	rs := f.requests()
	if len(rs) != 7 {
		t.Fatalf("%d requests, want 7 (get, then put and get of three keys)", len(rs))
	}
	for i, r := range rs {
		if !r.SignedOK {
			t.Errorf("request %d (%s %s): signature not verified", i, r.Method, r.Path)
		}
		if r.Host != s3TestHost {
			t.Errorf("request %d: host %q, want %s", i, r.Host, s3TestHost)
		}
		if !strings.HasPrefix(r.Path, "/"+s3TestBucket+"/") || r.RawQuery != "" {
			t.Errorf("request %d: path %q query %q, want /%s/<key> and no query", i, r.Path, r.RawQuery, s3TestBucket)
		}
		if r.Header.Get("X-Amz-Date") != "20261008T120000Z" {
			t.Errorf("request %d: x-amz-date %q, want the injected clock", i, r.Header.Get("X-Amz-Date"))
		}
		if a := r.Header.Get("Authorization"); !strings.HasPrefix(a, "AWS4-HMAC-SHA256 Credential="+s3TestAK+"/20261008/gra/s3/aws4_request, ") {
			t.Errorf("request %d: Authorization %q", i, a)
		}
		for name := range r.Header {
			if name != "Authorization" && strings.Contains(strings.Join(r.Header.Values(name), " "), s3TestAK) {
				t.Errorf("request %d: header %s carries the access key", i, name)
			}
		}
	}
	if rs[1].Method != http.MethodPut || rs[1].Path != "/"+s3TestBucket+"/"+key {
		t.Errorf("second request %s %s, want PUT /%s/%s", rs[1].Method, rs[1].Path, s3TestBucket, key)
	}
	for i, want := range map[int]string{
		1: "/" + s3TestBucket + "/" + key,
		3: "/" + s3TestBucket + "/account/a%20b%24c%2Bd/outputs.json",
		4: "/" + s3TestBucket + "/account/a%20b%24c%2Bd/outputs.json",
		5: "/" + s3TestBucket + "/account/50%25/%E1%88%B4.json",
		6: "/" + s3TestBucket + "/account/50%25/%E1%88%B4.json",
	} {
		if rs[i].RawPath != want {
			t.Errorf("request %d went out as %q, want the signed canonical URI %q", i, rs[i].RawPath, want)
		}
	}
	if rs[3].Path != "/"+s3TestBucket+"/"+odd || rs[5].Path != "/"+s3TestBucket+"/"+odd2 {
		t.Errorf("odd keys arrived as %q, %q", rs[3].Path, rs[5].Path)
	}
	s3NoSecretIn(t, f, s3TestSK)
}

// ---------------------------------------------------------------- method gate

// TestS3StoreMethods: GET, PUT and HEAD only; any other method (or a lower-case one) is refused
// before any request is sent, and the refusal names the method.
func TestS3StoreMethods(t *testing.T) {
	f := newFakeS3(t)
	s := newTestS3Store(t, f)
	for _, m := range []string{http.MethodPost, http.MethodDelete, http.MethodPatch, http.MethodOptions, http.MethodConnect, http.MethodTrace, "get", "put", "PROPFIND", ""} {
		if _, err := s.request(m, s3TestBucket, "k", nil); err == nil || (m != "" && !strings.Contains(err.Error(), m)) {
			t.Errorf("method %q: %v, want a refusal naming it", m, err)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("%d requests sent for refused methods", n)
	}
	for _, m := range []string{http.MethodPut, http.MethodHead, http.MethodGet} {
		if _, err := s.request(m, s3TestBucket, "k", nil); err != nil {
			t.Errorf("method %s: %v", m, err)
		}
	}
	if rs := f.requests(); len(rs) != 3 || rs[0].Method != http.MethodPut || rs[1].Method != http.MethodHead || rs[2].Method != http.MethodGet {
		t.Errorf("requests %v, want PUT, HEAD, GET", rs)
	}
}

// TestS3StoreNames: a bucket that is not an S3 bucket name or a key with an empty, `.` or `..`
// segment, a leading slash or a control character is refused before any request.
func TestS3StoreNames(t *testing.T) {
	f := newFakeS3(t)
	s := newTestS3Store(t, f)
	for _, b := range []string{"", "ab", "LZ-bkt", "lz_bkt", "-lz", "lz-", "lz/bkt", "lz bkt", strings.Repeat("a", 64)} {
		if _, err := s.Get(b, "k"); err == nil {
			t.Errorf("bucket %q: no refusal", b)
		}
	}
	for _, k := range []string{"", "/k", "k/", "a//b", "a/../b", "../b", "./b", "a/.", "a\x00b", "a\nb", "a\x7fb"} {
		if _, err := s.Get(s3TestBucket, k); err == nil {
			t.Errorf("key %q: no refusal", k)
		}
		if err := s.Put(s3TestBucket, k, []byte("x")); err == nil {
			t.Errorf("Put key %q: no refusal", k)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("%d requests sent for refused names", n)
	}
}

// ---------------------------------------------------------------- errors and caps

// TestS3StoreErrors: every non-200 status (404 on Get is fs.ErrNotExist), a redirect (never
// followed), an over-long or truncated answer and a reset connection are errors naming the
// operation, bucket and key; no error carries the server's text, the secret or the access key,
// also when the server echoes them; a Put larger than the cap is refused before any request.
func TestS3StoreErrors(t *testing.T) {
	echo := "SERVERTEXT " + s3TestSK + " " + s3TestAK
	faults := map[string]func(rw http.ResponseWriter, r *http.Request){}
	for _, code := range []int{400, 401, 403, 404, 409, 429, 500, 502, 503, 201, 204, 206} {
		faults[fmt.Sprint(code)] = func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(code)
			fmt.Fprint(rw, echo)
		}
	}
	for _, code := range []int{301, 302, 307, 308} {
		faults[fmt.Sprint(code)] = func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Location", "https://elsewhere.example/"+s3TestBucket+"/k")
			rw.WriteHeader(code)
			fmt.Fprint(rw, echo)
		}
	}
	faults["oversized"] = func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		rw.Write(bytes.Repeat([]byte("x"), maxObject+1))
	}
	faults["truncated"] = func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Length", "100")
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte(echo[:10]))
	}
	faults["reset"] = func(rw http.ResponseWriter, r *http.Request) {
		conn, _, err := rw.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}
	faults["chunked-unterminated"] = func(rw http.ResponseWriter, r *http.Request) {
		conn, buf, err := rw.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		buf.WriteString("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n")
		buf.Flush()
		conn.Close()
	}
	for name, fault := range faults {
		for _, op := range []string{"GET", "PUT"} {
			t.Run(name+"/"+op, func(t *testing.T) {
				f := newFakeS3(t)
				f.fault = func(rw http.ResponseWriter, r *http.Request) bool { fault(rw, r); return true }
				s := newTestS3Store(t, f)
				var err error
				if op == "GET" {
					var got []byte
					got, err = s.Get(s3TestBucket, "k")
					if err == nil {
						t.Fatalf("no error (got %d bytes)", len(got))
					}
					if errors.Is(err, fs.ErrNotExist) != (name == "404") {
						t.Errorf("fs.ErrNotExist %v for %s: %v", errors.Is(err, fs.ErrNotExist), name, err)
					}
				} else if err = s.Put(s3TestBucket, "k", []byte("{}")); err == nil {
					t.Fatal("no error")
				}
				msg := err.Error()
				if !strings.Contains(msg, op) || !strings.Contains(msg, s3TestBucket+"/k") {
					t.Errorf("error %q does not name %s %s/k", msg, op, s3TestBucket)
				}
				// Quiet errors (T083's pattern): no answer text, no key, no URL of a transport error.
				for _, bad := range []string{"SERVERTEXT", s3TestSK, s3TestAK, "hello", "elsewhere", "https://", s3TestHost} {
					if strings.Contains(msg, bad) {
						t.Errorf("error %q carries %q", msg, bad)
					}
				}
				if n := len(f.requests()); n != 1 {
					t.Errorf("%d requests, want 1 (no redirect followed, no retry)", n)
				}
			})
		}
	}
	f := newFakeS3(t)
	s := newTestS3Store(t, f)
	if err := s.Put(s3TestBucket, "k", bytes.Repeat([]byte("x"), maxObject+1)); err == nil {
		t.Error("Put above the cap: no refusal")
	}
	if err := s.Put(s3TestBucket, "k", bytes.Repeat([]byte("x"), maxObject)); err != nil {
		t.Errorf("Put at the cap: %v", err)
	}
	if got, err := s.Get(s3TestBucket, "k"); err != nil || len(got) != maxObject {
		t.Errorf("Get at the cap: %d bytes, %v", len(got), err)
	}
	if n := len(f.requests()); n != 2 {
		t.Errorf("%d requests, want 2 (the refused Put sent nothing)", n)
	}
}

// ---------------------------------------------------------------- the bootstrap phases over S3

// bssS3 serves the bootstrap harness's account bucket over the fake S3 endpoint: the secret of an
// access key is the fake cloud's, and objects go through bssStore (same files, same bucket
// binding: a key pair not bound to the bucket is AccessDenied).
func bssS3(t *testing.T, h *bssHarness) *fakeS3 {
	f := newFakeS3(t)
	f.secretOf = func(ak string) string {
		var c bssCloud
		if err := bssLoad(h.world.Cloud, &c); err != nil {
			return ""
		}
		for _, u := range c.Users {
			if u.AccessKey == ak {
				return u.Secret
			}
		}
		return ""
	}
	f.get = func(ak, bucket, key string) ([]byte, int) {
		data, err := bssStore{h: h, ak: ak, sk: f.secretOf(ak)}.Get(bucket, key)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, http.StatusNotFound
		case err != nil:
			return nil, http.StatusForbidden
		}
		return data, http.StatusOK
	}
	f.put = func(ak, bucket, key string, data []byte) int {
		if err := (bssStore{h: h, ak: ak, sk: f.secretOf(ak)}).Put(bucket, key, data); err != nil {
			return http.StatusForbidden
		}
		return http.StatusOK
	}
	return f
}

// TestBootstrapStateS3Store (T089): the state, publish and verify phases with the live S3 store
// against a fake S3 endpoint: the first run's publish reads the artefact (missing) and writes the
// bootstrap envelope with one signed PUT to https://s3.gra.io.cloud.ovh.net/<account bucket>/
// <artefact key> using state.env's keys; the second run's publish reads it back over S3, finds
// the same bytes and sends no PUT, so every phase reports unchanged. No request carries a secret.
// (account-governance consumes no artefact in this manifest, so verify reads nothing over S3.)
func TestBootstrapStateS3Store(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	f := bssS3(t, h)
	h.storeFor = func(keys map[string]string) (ObjectStore, error) {
		return NewS3Store(S3Options{Region: h.m.StateRegion, Keys: keys, HTTP: f.client(), Now: func() time.Time { return s3TestNow }})
	}
	rs, err := h.run(false)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	bsWantStatus(t, rs, PhasePublish, StatusRan)
	if r, _ := bsResult(rs, PhaseVerify); r.Status != StatusRan && r.Status != StatusUnchanged {
		t.Errorf("verify %s (%s), want it to pass", r.Status, r.Detail)
	}
	keys := bssStateEnv(t, h)
	envelope := bssEnvelope(t, h)
	if envelope == nil {
		t.Fatal("no envelope in the account bucket")
	}
	artefact := "/" + h.world.Bucket + "/" + stacks.ArtifactKey("account-bootstrap")
	var methods []string
	first := f.requests()
	for _, r := range first {
		if !r.SignedOK || r.Host != s3TestHost {
			t.Errorf("%s %s to %s: signed %v", r.Method, r.Path, r.Host, r.SignedOK)
		}
		if r.Path != artefact {
			t.Errorf("%s %s, want only the artefact %s", r.Method, r.Path, artefact)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Credential="+keys["AWS_ACCESS_KEY_ID"]+"/") {
			t.Errorf("%s %s not signed with state.env's access key", r.Method, r.Path)
		}
		methods = append(methods, r.Method)
		if r.Method == http.MethodPut && !bytes.Equal(r.Body, envelope) {
			t.Error("the PUT body is not the published envelope")
		}
	}
	if !slices.Equal(methods, []string{http.MethodGet, http.MethodPut}) {
		t.Errorf("first run S3 requests %v, want GET (missing) then one PUT", methods)
	}
	rs, err = h.run(false)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, p := range []string{PhaseState, PhasePublish, PhaseVerify} {
		bsWantStatus(t, rs, p, StatusUnchanged)
	}
	if second := f.requests()[len(first):]; len(second) != 1 || second[0].Method != http.MethodGet || second[0].Path != artefact || !second[0].SignedOK {
		t.Errorf("second run S3 requests %v, want one signed GET of the artefact", second)
	}
	s3NoSecretIn(t, f, keys["AWS_SECRET_ACCESS_KEY"])
	for sec := range h.secrets() {
		s3NoSecretIn(t, f, sec)
	}
	bssNoLeak(t, h, rs, err)
}
