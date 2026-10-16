package storageiam

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T029: the swift-reach check (T030) against a fake Keystone, a fake Swift and a fake S3 endpoint
// behind one TLS server that the client's dialer reaches for every host (routed by Host), and a
// fake OVHcloud API for Sweep. Whether a tenant S3 user's Keystone token reaches the other bucket
// through Swift is UNVERIFIED, as is whether Swift serves S3-API buckets; these fakes answer the
// worlds the live run (T025) may meet. Keystone follows the v3 password-auth shape (POST
// /v3/auth/tokens, X-Subject-Token, a catalog with an object-store endpoint per region), Swift
// the v1 account/container/object paths, S3 path-style requests on s3.<region>.io.cloud.ovh.net
// (live.S3Store). No test calls OVHcloud or reads ~/.config/ovh-lz/.
//
// What the tests fix for T030, beyond data-model.md: per user and route (list, read of the seed,
// write under lzprobe-swift-reach/) the requests are own control, other bucket, repeated own
// control, with one token; the record is observations.json with check "swift-reach", one
// observation per direction and route (step "a-list-b" … "b-write-a", credential "user-a" or
// "user-b", expected "denied", observed reach/denied/blocked/not-run, status, control_status,
// repeat_status, reason, as_expected) and findings; the root's users carry the descriptions
// swUsers names.

const (
	swRun      = "20261016T101500Z-1a2b"
	swRunLower = "20261016t101500z-1a2b"
	swOtherRun = "20261015t090000z-9f8e"
	swTenant   = "fedcba9876543210fedcba9876543210"
	swBucketA  = "lzprobe-swift-reach-" + swRunLower + "-a"
	swBucketB  = "lzprobe-swift-reach-" + swRunLower + "-b"
	swSeed     = "lzprobe-swift-reach/seed"
	swPrefix   = "lzprobe-swift-reach/"
	swUserA    = "user-Ab12Cd34"
	swUserB    = "user-Ef56Gh78"
	swPassA    = "Qz7vKx9mWp2rTn4h"
	swPassB    = "Jd5yHc8sLf3gBv6q"
	swCleanA   = "AKCLEANA0001"
	swCleanASK = "Xr4tYu8iOp2aSd6fZc1v"
	swCleanB   = "AKCLEANB0002"
	swCleanBSK = "Hn7mJk3lQw9eRt5yUb2x"
	swAdminID  = "EU.swiftsweepadmin"
	swAdminSK  = "Vp8cXz4bNm6aLs2dKf0g"
	swKeystone = "auth.cloud.ovh.net"
	swSwift    = "storage.gra.cloud.ovh.net"
	swSwiftSBG = "storage.sbg.cloud.ovh.net"
	swS3       = "s3.gra.io.cloud.ovh.net"
	swAuthURL  = "https://" + swKeystone + "/v3"
)

var (
	swSecrets = []string{swPassA, swPassB, swCleanASK, swCleanBSK, swAdminSK, swCleanA, swCleanB}
	swNow     = time.Date(2026, 10, 16, 10, 20, 0, 0, time.UTC)
	swSteps   = []string{"a-list-b", "a-read-b", "a-write-b", "b-list-a", "b-read-a", "b-write-a"}
)

// swUsers are the descriptions of the cloud-project users the root creates for one run: the two
// tenant users under test and the two bucket-scoped cleanup users.
func swUsers(lowerRun string) []string {
	p := "lzprobe-swift-reach-" + lowerRun
	return []string{p + "-a", p + "-b", p + "-a-cleanup", p + "-b-cleanup"}
}

func swEnv() map[string]string {
	return map[string]string{
		"run_id": swRun, "auth_url": swAuthURL, "region": "GRA", "tenant_id": swTenant, "seed_key": swSeed,
		"bucket_a": swBucketA, "bucket_b": swBucketB,
		"user_a_name": swUserA, "user_a_password": swPassA, "user_b_name": swUserB, "user_b_password": swPassB,
		"cleanup_a_key_id": swCleanA, "cleanup_a_secret": swCleanASK, "cleanup_b_key_id": swCleanB, "cleanup_b_secret": swCleanBSK,
	}
}

// swReq is one request a fake consumed: who sent it ("keystone:a", "a", "b", "cleanup-a",
// "cleanup-b", "admin", "?"), the host, method, path and query, and the Swift token it carried.
type swReq struct{ who, host, method, path, query, token string }

func (r swReq) String() string { return r.who + " " + r.method + " " + r.host + r.path }

// swReply is one scripted Swift answer: a status, whether a write is stored first, and whether the
// connection is then closed without an answer.
type swReply struct {
	status int
	store  bool
	hangup bool
}

// swWorld is the fakes. Knobs: loginRefuse refuses a user's token request (the body echoes the
// password); replies scripts "<user> <route> <bucket>" answers, consumed in order, before the
// defaults (own bucket: list 200, read 200 when the key exists, write 201 stored; other bucket
// and every Swift delete: 403); otherAllow answers the other bucket like the own one; killAfter
// makes a user's token invalid after its first other-bucket request; s3Keep acknowledges S3
// deletes without removing; apiKeep does the same for the API's deletes.
type swWorld struct {
	t       *testing.T
	srv     *httptest.Server
	api     *httptest.Server
	mu      sync.Mutex
	reqs    []swReq
	objects map[string]map[string]bool
	tokens  map[string]string // issued token → user
	dead    map[string]bool
	issued  int

	loginRefuse map[string]bool
	replies     map[string][]swReply
	otherAllow  bool
	killAfter   map[string]bool
	s3Keep      bool
	cross       map[string][]string // user → keys it stored in the other bucket

	buckets map[string][]string // region → bucket names (the API)
	users   []swAPIUser
	apiKeep bool

	// T030 knobs: seedRefuse refuses the cleanup keys' seed write; s3Refuse refuses every S3
	// request AccessDenied; goneKeys are cleanup keys whose user was deleted (403
	// InvalidAccessKeyId); bulkKeep acknowledges the API's bulk object deletes without removing.
	// A bucket the API deleted answers S3 404 NoSuchBucket.
	seedRefuse bool
	s3Refuse   bool
	goneKeys   map[string]bool
	bulkKeep   bool
	// catalogGRA, when set, is the URL the Keystone catalog gives for GRA's object store.
	catalogGRA string
	// roleEcho makes Keystone list the issued token and the password as role names; listWrong
	// answers an S3 listing 200 with an <Error> body (review r1).
	roleEcho  bool
	listWrong bool
}

type swAPIUser struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
	Username    string `json:"username"`
}

func newSwWorld(t *testing.T) *swWorld {
	t.Helper()
	w := &swWorld{t: t,
		objects: map[string]map[string]bool{swBucketA: {swSeed: true}, swBucketB: {swSeed: true}},
		tokens:  map[string]string{}, dead: map[string]bool{},
		loginRefuse: map[string]bool{}, replies: map[string][]swReply{}, killAfter: map[string]bool{}, cross: map[string][]string{},
		buckets: map[string][]string{"GRA": {}, "SBG": {}}, goneKeys: map[string]bool{},
	}
	w.srv = httptest.NewTLSServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.srv.Close)
	w.api = httptest.NewServer(http.HandlerFunc(w.serveAPI))
	t.Cleanup(w.api.Close)
	return w
}

func (w *swWorld) options() SwiftOptions {
	pool := x509.NewCertPool()
	pool.AddCert(w.srv.Certificate())
	addr := w.srv.Listener.Addr().String()
	return SwiftOptions{
		// No keep-alive: net/http resends an idempotent request once when a reused connection
		// closes without an answer, which would turn a scripted hangup into the default answer.
		HTTP: &http.Client{Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig:   &tls.Config{RootCAs: pool, ServerName: "example.com"},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}},
		Now:   func() time.Time { return swNow },
		API:   live.API{TokenURL: w.api.URL + "/token", BaseURL: w.api.URL + "/v1"},
		Admin: live.Credential{Endpoint: "ovh-eu", ClientID: swAdminID, ClientSecret: swAdminSK},
	}
}

func (w *swWorld) seen() []swReq {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.reqs)
}

func (w *swWorld) crossOf(user string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.cross[user])
}

func (w *swWorld) has(bucket, key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.objects[bucket][key]
}

// written lists a bucket's keys under the prefix other than the seed.
func (w *swWorld) written(bucket string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for k := range w.objects[bucket] {
		if strings.HasPrefix(k, swPrefix) && k != swSeed {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func (w *swWorld) serve(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.mu.Lock()
	defer w.mu.Unlock()
	switch r.Host {
	case swKeystone:
		w.keystone(rw, r, body)
	case swSwift, swSwiftSBG:
		w.swift(rw, r, body)
	case swS3:
		w.s3(rw, r)
	default:
		w.reqs = append(w.reqs, swReq{"?", r.Host, r.Method, r.URL.Path, r.URL.RawQuery, ""})
		rw.WriteHeader(http.StatusNotFound)
	}
}

func (w *swWorld) keystone(rw http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		Auth struct {
			Identity struct {
				Password struct {
					User struct{ Name, Password string }
				}
			}
		}
	}
	_ = json.Unmarshal(body, &req)
	u := req.Auth.Identity.Password.User
	user := map[string]string{swUserA + " " + swPassA: "a", swUserB + " " + swPassB: "b"}[u.Name+" "+u.Password]
	who := "keystone:" + user
	if user == "" {
		who = "keystone:?"
	}
	w.reqs = append(w.reqs, swReq{who, r.Host, r.Method, r.URL.Path, r.URL.RawQuery, ""})
	if r.Method != http.MethodPost || r.URL.Path != "/v3/auth/tokens" || user == "" || w.loginRefuse[user] {
		// Keystone's 401 body; this fake echoes the request's user and password.
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(rw, `{"error":{"code":401,"title":"Unauthorized","message":"The request you have made requires authentication. user %s password %s"}}`, u.Name, u.Password)
		return
	}
	w.issued++
	tok := fmt.Sprintf("gAAAAAB%s%04dtok", user, w.issued)
	w.tokens[tok] = user
	rw.Header().Set("X-Subject-Token", tok)
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusCreated)
	ep := func(region, host string) map[string]any {
		u := "https://" + host + "/v1/AUTH_" + swTenant
		if region == "GRA" && w.catalogGRA != "" {
			u = w.catalogGRA
		}
		return map[string]any{"interface": "public", "region": region, "region_id": region, "url": u}
	}
	roles := []map[string]string{{"name": "objectstore_operator"}}
	if w.roleEcho {
		roles = append(roles, map[string]string{"name": u.Password}, map[string]string{"name": swCleanASK})
		for t := range w.tokens { // this login's token and any issued before it (the other user's)
			roles = append(roles, map[string]string{"name": t})
		}
	}
	json.NewEncoder(rw).Encode(map[string]any{"token": map[string]any{
		"expires_at": swNow.Add(24 * time.Hour).Format(time.RFC3339),
		"project":    map[string]any{"id": swTenant},
		"roles":      roles,
		"catalog": []map[string]any{
			{"type": "identity", "name": "keystone", "endpoints": []map[string]any{{"interface": "public", "region": "GRA", "url": swAuthURL}}},
			{"type": "object-store", "name": "swift", "endpoints": []map[string]any{ep("SBG", swSwiftSBG), ep("GRA", swSwift)}},
		},
	}})
}

// swRoute names a Swift request: list (GET container), read (GET object), write (PUT object),
// delete (DELETE object), or the method otherwise.
func swRoute(method, key string) string {
	switch {
	case method == http.MethodGet && key == "":
		return "list"
	case method == http.MethodGet:
		return "read"
	case method == http.MethodPut && key != "":
		return "write"
	case method == http.MethodDelete && key != "":
		return "delete"
	}
	return method
}

// swSplit splits a Swift path /v1/AUTH_<tenant>/<container>[/<object>].
func swSplit(path string) (bucket, key string, ok bool) {
	rest, found := strings.CutPrefix(path, "/v1/AUTH_"+swTenant+"/")
	if !found {
		return "", "", false
	}
	bucket, key, _ = strings.Cut(rest, "/")
	return bucket, key, bucket != ""
}

func (w *swWorld) swift(rw http.ResponseWriter, r *http.Request, body []byte) {
	tok := r.Header.Get("X-Auth-Token")
	user := w.tokens[tok]
	who := user
	if who == "" {
		who = "?"
	}
	w.reqs = append(w.reqs, swReq{who, r.Host, r.Method, r.URL.Path, r.URL.RawQuery, tok})
	bucket, key, ok := swSplit(r.URL.Path)
	if user == "" || w.dead[tok] {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !ok || r.Host != swSwift {
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	route := swRoute(r.Method, key)
	own := map[string]string{"a": swBucketA, "b": swBucketB}[user]
	other := map[string]string{"a": swBucketB, "b": swBucketA}[user]
	if bucket == other && w.killAfter[user] {
		defer func() { w.dead[tok] = true }()
	}
	rep := swReply{status: http.StatusForbidden}
	if bucket == own || bucket == other && w.otherAllow {
		switch route {
		case "list", "read":
			rep = swReply{status: http.StatusOK}
		case "write":
			rep = swReply{status: http.StatusCreated, store: true}
		}
	}
	k := user + " " + route + " " + bucket
	if q := w.replies[k]; len(q) > 0 {
		rep, w.replies[k] = q[0], q[1:]
	}
	if rep.store && w.objects[bucket] != nil {
		w.objects[bucket][key] = true
		if bucket == other {
			w.cross[user] = append(w.cross[user], key)
		}
	}
	if rep.hangup {
		if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
		return
	}
	if route == "read" && rep.status == http.StatusOK && !w.objects[bucket][key] {
		rep.status = http.StatusNotFound
	}
	rw.WriteHeader(rep.status)
	if rep.status != http.StatusOK {
		return
	}
	switch route {
	case "list":
		keys := slices.Sorted(maps.Keys(w.objects[bucket]))
		if r.URL.Query().Get("format") == "json" {
			var out []map[string]any
			for _, k := range keys {
				out = append(out, map[string]any{"name": k, "bytes": 4})
			}
			json.NewEncoder(rw).Encode(out)
			return
		}
		fmt.Fprint(rw, strings.Join(keys, "\n"))
	case "read":
		fmt.Fprint(rw, "seed")
	}
	_ = body
}

// s3 is the fake S3 endpoint: each cleanup key lists and deletes on its own bucket only.
func (w *swWorld) s3(rw http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	who := "?"
	for ak, name := range map[string]string{swCleanA: "cleanup-a", swCleanB: "cleanup-b"} {
		if strings.Contains(auth, "Credential="+ak+"/") {
			who = name
		}
	}
	w.reqs = append(w.reqs, swReq{who, r.Host, r.Method, r.URL.Path, r.URL.RawQuery, ""})
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	mine := map[string]string{"cleanup-a": swBucketA, "cleanup-b": swBucketB}[who]
	for ak, name := range map[string]string{swCleanA: "cleanup-a", swCleanB: "cleanup-b"} {
		if name == who && w.goneKeys[ak] {
			rw.WriteHeader(http.StatusForbidden)
			fmt.Fprint(rw, "<Error><Code>InvalidAccessKeyId</Code></Error>")
			return
		}
	}
	if bucket == "" || bucket != mine || w.s3Refuse {
		rw.WriteHeader(http.StatusForbidden)
		fmt.Fprint(rw, "<Error><Code>AccessDenied</Code></Error>")
		return
	}
	if w.objects[bucket] == nil {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, "<Error><Code>NoSuchBucket</Code></Error>")
		return
	}
	switch {
	case r.Method == http.MethodPut && key == swSeed && !w.seedRefuse:
		// The cleanup policy's one write: the seed (tests/live/probes/swift-reach/main.tf).
		w.objects[bucket][key] = true
		rw.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && key == "" && w.listWrong:
		fmt.Fprint(rw, "<Error><Code>InternalError</Code></Error>")
	case r.Method == http.MethodGet && key == "":
		prefix := r.URL.Query().Get("prefix")
		var keys []string
		for _, k := range slices.Sorted(maps.Keys(w.objects[bucket])) {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		var b strings.Builder
		if r.URL.Query().Has("versions") {
			fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>%s</Name><Prefix>%s</Prefix><IsTruncated>false</IsTruncated>`, bucket, prefix)
			for _, k := range keys {
				fmt.Fprintf(&b, `<Version><Key>%s</Key><VersionId>null</VersionId><IsLatest>true</IsLatest></Version>`, k)
			}
			b.WriteString(`</ListVersionsResult>`)
		} else {
			fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>%s</Name><Prefix>%s</Prefix><KeyCount>%d</KeyCount><IsTruncated>false</IsTruncated>`, bucket, prefix, len(keys))
			for _, k := range keys {
				fmt.Fprintf(&b, `<Contents><Key>%s</Key></Contents>`, k)
			}
			b.WriteString(`</ListBucketResult>`)
		}
		rw.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(rw, b.String())
	case r.Method == http.MethodDelete && key != "":
		if !w.s3Keep {
			delete(w.objects[bucket], key)
		}
		rw.WriteHeader(http.StatusNoContent)
	default:
		rw.WriteHeader(http.StatusForbidden)
		fmt.Fprint(rw, "<Error><Code>AccessDenied</Code></Error>")
	}
}

// serveAPI is the fake OVHcloud API for Sweep: regions, their bucket listings and deletes, the
// project's users and their deletes, for the sandbox admin only.
func (w *swWorld) serveAPI(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.mu.Lock()
	defer w.mu.Unlock()
	if r.URL.Path == "/token" {
		if strings.Contains(string(body), "client_id="+swAdminID) && strings.Contains(string(body), "client_secret="+swAdminSK) {
			fmt.Fprint(rw, `{"access_token":"tok-swadmin"}`)
			return
		}
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	who := "?"
	if r.Header.Get("Authorization") == "Bearer tok-swadmin" {
		who = "admin"
	}
	w.reqs = append(w.reqs, swReq{who, "api", r.Method, r.URL.Path, r.URL.RawQuery, ""})
	if who != "admin" {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	base := "/v1/cloud/project/" + swTenant
	rw.Header().Set("Content-Type", "application/json")
	rest, ok := strings.CutPrefix(r.URL.Path, base)
	if !ok {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"This service does not exist"}`)
		return
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	switch {
	case r.Method == http.MethodGet && rest == "/region":
		json.NewEncoder(rw).Encode(slices.Sorted(maps.Keys(w.buckets)))
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "region":
		json.NewEncoder(rw).Encode(map[string]any{"name": parts[1], "status": "UP",
			"services": []map[string]string{{"name": "storage-s3-standard", "status": "UP"}, {"name": "storage", "status": "UP"}}})
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "region" && parts[2] == "storage":
		out := []map[string]any{}
		for _, b := range w.buckets[parts[1]] {
			out = append(out, map[string]any{"name": b, "region": parts[1]})
		}
		json.NewEncoder(rw).Encode(out)
	case r.Method == http.MethodDelete && len(parts) == 4 && parts[0] == "region" && parts[2] == "storage":
		if !slices.Contains(w.buckets[parts[1]], parts[3]) {
			rw.WriteHeader(http.StatusNotFound)
			fmt.Fprint(rw, `{"message":"not found"}`)
			return
		}
		if !w.apiKeep {
			w.buckets[parts[1]] = slices.DeleteFunc(w.buckets[parts[1]], func(b string) bool { return b == parts[3] })
			delete(w.objects, parts[3])
		}
		fmt.Fprint(rw, `null`)
	case len(parts) == 5 && parts[0] == "region" && parts[2] == "storage" && !slices.Contains(w.buckets[parts[1]], parts[3]):
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"not found"}`)
	case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "region" && parts[2] == "storage" && parts[4] == "object":
		// T030: cloud.StorageObjectList[] (kb/api/v1/cloud.json), one entry per key.
		out := []map[string]any{}
		for _, k := range slices.Sorted(maps.Keys(w.objects[parts[3]])) {
			out = append(out, map[string]any{"key": k, "versionId": "null", "isLatest": true})
		}
		json.NewEncoder(rw).Encode(out)
	case r.Method == http.MethodPost && len(parts) == 5 && parts[0] == "region" && parts[2] == "storage" && parts[4] == "bulkDeleteObjects":
		// T030: cloud.StorageObjectDeleteBulk in, cloud.StorageObjectDeleteBulkResponse out.
		var in struct {
			Objects []struct {
				Key string `json:"key"`
			} `json:"objects"`
		}
		if json.Unmarshal(body, &in) != nil || len(in.Objects) == 0 {
			rw.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(rw, `{"message":"bad body"}`)
			return
		}
		deleted := []map[string]string{}
		for _, o := range in.Objects {
			if !w.bulkKeep {
				delete(w.objects[parts[3]], o.Key)
			}
			deleted = append(deleted, map[string]string{"key": o.Key})
		}
		json.NewEncoder(rw).Encode(map[string]any{"deleted": deleted, "errors": []any{}})
	case r.Method == http.MethodGet && rest == "/user":
		json.NewEncoder(rw).Encode(append([]swAPIUser{}, w.users...))
	case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "user":
		i := slices.IndexFunc(w.users, func(u swAPIUser) bool { return fmt.Sprint(u.ID) == parts[1] })
		if i < 0 {
			rw.WriteHeader(http.StatusNotFound)
			fmt.Fprint(rw, `{"message":"not found"}`)
			return
		}
		if !w.apiKeep {
			// A deleted cleanup user's S3 key stops working (T030: Undo after a Sweep).
			switch w.users[i].Description {
			case swUsers(swRunLower)[2]:
				w.goneKeys[swCleanA] = true
			case swUsers(swRunLower)[3]:
				w.goneKeys[swCleanB] = true
			}
			w.users = slices.Delete(w.users, i, i+1)
		}
		fmt.Fprint(rw, `null`)
	default:
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprint(rw, `{"message":"This call has not been granted"}`)
	}
}

// swObs and swRecord read observations.json as T030 writes it (the JSON names are the contract;
// fields T030 adds are ignored).
type swObs struct {
	Step       string `json:"step"`
	Credential string `json:"credential"`
	Expected   string `json:"expected"`
	Observed   string `json:"observed"`
	Status     int    `json:"status"`
	Control    int    `json:"control_status"`
	Repeat     int    `json:"repeat_status"`
	Reason     string `json:"reason"`
	AsExpected bool   `json:"as_expected"`
}

type swRecord struct {
	Check        string   `json:"check"`
	Observations []swObs  `json:"observations"`
	Findings     []string `json:"findings"`
}

// swLeaks returns the secrets (whole, or an 8-character fragment of a password) and the issued
// tokens that texts carry.
func (w *swWorld) swLeaks(texts ...string) []string {
	w.mu.Lock()
	bad := append(slices.Clone(swSecrets), "tok-swadmin")
	for tok := range w.tokens {
		bad = append(bad, tok)
	}
	w.mu.Unlock()
	for _, p := range []string{swPassA, swPassB} {
		bad = append(bad, p[:8], p[8:])
	}
	var out []string
	for _, s := range texts {
		for _, b := range bad {
			if strings.Contains(s, b) && !slices.Contains(out, b) {
				out = append(out, b)
			}
		}
	}
	return out
}

// swiftOf returns the Swift requests of one user, as "route bucket" with their tokens and keys.
func swiftOf(reqs []swReq, user string) (steps []string, toks []string, keys []string) {
	for _, r := range reqs {
		if r.who != user || (r.host != swSwift && r.host != swSwiftSBG) {
			continue
		}
		bucket, key, _ := swSplit(r.path)
		steps = append(steps, swRoute(r.method, key)+" "+bucket)
		toks = append(toks, r.token)
		keys = append(keys, key)
	}
	return steps, toks, keys
}

// swFull is one user's nine Swift requests: per route, own control, other bucket, repeated control.
func swFull(own, other string) []string {
	var out []string
	for _, route := range []string{"list", "read", "write"} {
		out = append(out, route+" "+own, route+" "+other, route+" "+own)
	}
	return out
}

// wantSequence checks a user's Swift requests begin with want (the full sequence when exact), the
// reads name the seed, the writes a key under the prefix other than the seed, and every request
// went to the catalog's GRA endpoint after the user's token request.
func wantSequence(t *testing.T, reqs []swReq, user string, want []string, exact bool) []string {
	t.Helper()
	got, toks, keys := swiftOf(reqs, user)
	if exact && !slices.Equal(got, want) || !exact && (len(got) < len(want) || !slices.Equal(got[:len(want)], want)) {
		t.Errorf("user %s's Swift requests = %q, want %q (exact %v)", user, got, want, exact)
		return toks
	}
	for i, s := range got {
		switch {
		case strings.HasPrefix(s, "read ") && keys[i] != swSeed:
			t.Errorf("user %s read %q, want the seed %s", user, keys[i], swSeed)
		case strings.HasPrefix(s, "write ") && (!strings.HasPrefix(keys[i], swPrefix) || keys[i] == swSeed):
			t.Errorf("user %s wrote %q, want a key under %s other than the seed", user, keys[i], swPrefix)
		}
	}
	login := slices.IndexFunc(reqs, func(r swReq) bool { return r.who == "keystone:"+user })
	first := slices.IndexFunc(reqs, func(r swReq) bool { return r.who == user })
	if login < 0 || login > first {
		t.Errorf("user %s: no token request before its first Swift request: %v", user, reqs)
	}
	for _, r := range reqs {
		if r.who == user && r.host != swSwift {
			t.Errorf("user %s sent %s, want the GRA object-store endpoint %s", user, r, swSwift)
		}
	}
	return toks
}

// swObserve runs Observe; the case checks the requests first, then reads the record with
// swParse, so a check that sends nothing fails on its first missing request.
func swObserve(t *testing.T, w *swWorld, c *live.ProbeCheck) (raw []byte, term string, err error) {
	t.Helper()
	var buf bytes.Buffer
	raw, err = c.Observe(context.Background(), swEnv(), &buf)
	if leaks := w.swLeaks(string(raw), buf.String(), fmt.Sprint(err)); len(leaks) > 0 {
		t.Errorf("record, terminal or error carry %q", leaks)
	}
	return raw, buf.String(), err
}

func swParse(t *testing.T, raw []byte, err error) swRecord {
	t.Helper()
	var rec swRecord
	if raw == nil {
		t.Fatalf("Observe returned no record (error %v)", err)
	}
	if jerr := json.Unmarshal(raw, &rec); jerr != nil {
		t.Fatalf("record %s: %v", raw, jerr)
	}
	if rec.Check != "swift-reach" {
		t.Errorf("record check = %q, want swift-reach", rec.Check)
	}
	return rec
}

// byStep indexes the record's observations; it fails unless they are exactly the six steps.
func byStep(t *testing.T, rec swRecord) map[string]swObs {
	t.Helper()
	m := map[string]swObs{}
	for _, ob := range rec.Observations {
		m[ob.Step] = ob
	}
	if got := slices.Sorted(maps.Keys(m)); len(rec.Observations) != len(swSteps) || !slices.Equal(got, slices.Sorted(slices.Values(swSteps))) {
		t.Fatalf("observations %+v, want one per step %v", rec.Observations, swSteps)
	}
	for _, s := range swSteps {
		ob := m[s]
		if ob.Credential != "user-"+s[:1] || ob.Expected != "denied" || ob.AsExpected != (ob.Observed == "denied") {
			t.Errorf("observation %+v: want credential user-%s, expected denied, as_expected only when denied", ob, s[:1])
		}
	}
	return m
}

func wantObs(t *testing.T, ob swObs, observed string, status, control, repeat int) {
	t.Helper()
	if ob.Observed != observed || status >= 0 && ob.Status != status || control >= 0 && ob.Control != control || repeat >= 0 && ob.Repeat != repeat {
		t.Errorf("observation %+v, want observed %s, status %d, control %d, repeat %d (-1: any)", ob, observed, status, control, repeat)
	}
	if observed != "denied" && observed != "reach" && ob.Reason == "" {
		t.Errorf("observation %s is %s without a reason", ob.Step, observed)
	}
}

// TestJudgeReach: the judge alone. Reach on any other-bucket 2xx; a denial needs a 2xx control,
// a 401/403 and a 2xx repeat; anything else is blocked.
func TestJudgeReach(t *testing.T) {
	a := func(s int) SwiftAnswer { return SwiftAnswer{Status: s} }
	gone := SwiftAnswer{Err: errors.New("connection reset")}
	for _, c := range []struct {
		name                   string
		control, other, repeat SwiftAnswer
		want                   string
		defect                 string
	}{
		{"other 200", a(200), a(200), a(200), "reach", "a 2xx on the other bucket not counted as reach"},
		{"other 201", a(201), a(201), a(201), "reach", "a 2xx other than 200 not counted as reach"},
		{"other 204 without control", a(403), a(204), a(0), "reach", "reach hidden by a failed control"},
		{"denied 403", a(200), a(403), a(200), "denied", "a judge that never records a denial"},
		{"denied 401", a(201), a(401), a(204), "denied", "a 401 with a working control and repeat not counted as a denial"},
		{"token lost", a(200), a(401), a(401), "blocked", "a token expiry or revocation counted as isolation"},
		{"repeat 403", a(200), a(403), a(403), "blocked", "a repeat refused counted as isolation"},
		{"repeat no answer", a(200), a(403), gone, "blocked", "a repeat without an answer counted as isolation"},
		{"repeat 500", a(200), a(403), a(500), "blocked", "a repeat outage counted as isolation"},
		{"control 403", a(403), a(403), a(403), "blocked", "a 403 counted as isolation without a positive control"},
		{"control 404", a(404), a(403), a(200), "blocked", "a missing own container counted as a control"},
		{"control 500", a(500), a(403), a(200), "blocked", "an outage counted as a control"},
		{"control no answer", gone, a(403), a(200), "blocked", "no control counted as a control"},
		{"other 404", a(200), a(404), a(200), "blocked", "a missing container counted as a denial"},
		{"other 500", a(200), a(500), a(200), "blocked", "an outage counted as a denial"},
		{"other 503", a(200), a(503), a(200), "blocked", "an outage counted as a denial"},
		{"other transport error", a(200), gone, a(200), "blocked", "a transport error counted as a denial"},
		{"other 3xx", a(200), a(301), a(200), "blocked", "a redirect counted as a denial"},
	} {
		if got := JudgeReach(c.control, c.other, c.repeat); got != c.want {
			t.Errorf("%s: JudgeReach = %q, want %q (exposes %s)", c.name, got, c.want, c.defect)
		}
	}
}

// TestSwiftObserve: both directions, three routes each, judged against the own bucket's
// same-route control and its repeat with the same token.
func TestSwiftObserve(t *testing.T) {
	t.Run("reach", func(t *testing.T) {
		// Exposes a check that does not try the other bucket, or counts any 2xx as expected; and a
		// read step with nothing to read (the root's seeds are in both buckets).
		w := newSwWorld(t)
		w.otherAllow = true
		raw, term, err := swObserve(t, w, SwiftReach(w.options()))
		reqs := w.seen()
		wantSequence(t, reqs, "a", swFull(swBucketA, swBucketB), true)
		wantSequence(t, reqs, "b", swFull(swBucketB, swBucketA), true)
		rec := swParse(t, raw, err)
		m := byStep(t, rec)
		for _, s := range swSteps {
			status := 200
			if strings.Contains(s, "write") {
				status = 201
			}
			wantObs(t, m[s], "reach", status, status, status)
			if !slices.Contains(rec.Findings, s) {
				t.Errorf("findings %q lack %s", rec.Findings, s)
			}
			if err == nil || !strings.Contains(err.Error(), s) {
				t.Errorf("Observe error %v does not name %s", err, s)
			}
			if !strings.Contains(term, s) {
				t.Errorf("terminal does not name %s:\n%s", s, term)
			}
		}
	})

	t.Run("denied", func(t *testing.T) {
		// Exposes a judge that never records a denial; and a repeat sent with another token.
		w := newSwWorld(t)
		w.replies["a list "+swBucketB] = []swReply{{status: 401}}
		raw, _, err := swObserve(t, w, SwiftReach(w.options()))
		reqs := w.seen()
		for _, u := range []string{"a", "b"} {
			own, other := swBucketA, swBucketB
			if u == "b" {
				own, other = other, own
			}
			toks := wantSequence(t, reqs, u, swFull(own, other), true)
			for i := 0; i+2 < len(toks); i += 3 {
				if toks[i] == "" || toks[i] != toks[i+1] || toks[i] != toks[i+2] {
					t.Errorf("user %s: control, other and repeat sent different tokens", u)
				}
			}
		}
		rec := swParse(t, raw, err)
		m := byStep(t, rec)
		wantObs(t, m["a-list-b"], "denied", 401, 200, 200)
		for _, s := range swSteps[1:] {
			c := 200
			if strings.Contains(s, "write") {
				c = 201
			}
			wantObs(t, m[s], "denied", 403, c, c)
		}
		if err != nil || len(rec.Findings) != 0 {
			t.Errorf("Observe = %v, findings %q; want no error, no finding when every step is denied", err, rec.Findings)
		}
	})

	t.Run("token-lost", func(t *testing.T) {
		// Exposes a token expiry or revocation counted as isolation (both users' tokens are lost
		// after their first other-bucket request).
		w := newSwWorld(t)
		w.killAfter["a"], w.killAfter["b"] = true, true
		w.replies["a list "+swBucketB] = []swReply{{status: 401}}
		w.replies["b list "+swBucketA] = []swReply{{status: 401}}
		raw, _, err := swObserve(t, w, SwiftReach(w.options()))
		wantSequence(t, w.seen(), "a", []string{"list " + swBucketA, "list " + swBucketB, "list " + swBucketA}, false)
		wantSequence(t, w.seen(), "b", []string{"list " + swBucketB, "list " + swBucketA, "list " + swBucketB}, false)
		rec := swParse(t, raw, err)
		m := byStep(t, rec)
		wantObs(t, m["a-list-b"], "blocked", 401, 200, 401)
		wantObs(t, m["b-list-a"], "blocked", 401, 200, 401)
		for _, s := range swSteps {
			if m[s].Observed == "denied" {
				t.Errorf("%s denied after the user's token was lost: %+v", s, m[s])
			}
		}
	})

	for _, st := range []int{403, 404, 500} {
		t.Run(fmt.Sprintf("control-%d", st), func(t *testing.T) {
			// Exposes a 403 counted as isolation without a positive control.
			// The other bucket's 403 is asked and recorded, as the given says.
			w := newSwWorld(t)
			w.replies["a list "+swBucketA] = []swReply{{status: st}, {status: st}}
			w.replies["b list "+swBucketB] = []swReply{{status: st}, {status: st}}
			raw, _, err := swObserve(t, w, SwiftReach(w.options()))
			wantSequence(t, w.seen(), "a", []string{"list " + swBucketA, "list " + swBucketB}, false)
			wantSequence(t, w.seen(), "b", []string{"list " + swBucketB, "list " + swBucketA}, false)
			m := byStep(t, swParse(t, raw, err))
			wantObs(t, m["a-list-b"], "blocked", 403, st, -1)
			wantObs(t, m["b-list-a"], "blocked", 403, st, -1)
		})
	}

	for _, c := range []struct {
		name   string
		reply  swReply
		status int
	}{{"other-404", swReply{status: 404}, 404}, {"other-503", swReply{status: 503}, 503}, {"other-transport", swReply{status: 403, hangup: true}, 0}} {
		t.Run(c.name, func(t *testing.T) {
			// Exposes an outage or a missing container counted as a denial.
			w := newSwWorld(t)
			w.replies["a list "+swBucketB] = []swReply{c.reply}
			w.replies["b list "+swBucketA] = []swReply{c.reply}
			raw, _, err := swObserve(t, w, SwiftReach(w.options()))
			wantSequence(t, w.seen(), "a", []string{"list " + swBucketA, "list " + swBucketB}, false)
			wantSequence(t, w.seen(), "b", []string{"list " + swBucketB, "list " + swBucketA}, false)
			m := byStep(t, swParse(t, raw, err))
			wantObs(t, m["a-list-b"], "blocked", c.status, 200, -1)
			wantObs(t, m["b-list-a"], "blocked", c.status, 200, -1)
		})
	}

	t.Run("login-b-fails", func(t *testing.T) {
		// Exposes one direction reported as C005.1 in full.
		w := newSwWorld(t)
		w.loginRefuse["b"] = true
		raw, _, err := swObserve(t, w, SwiftReach(w.options()))
		reqs := w.seen()
		wantSequence(t, reqs, "a", swFull(swBucketA, swBucketB), true)
		if !slices.ContainsFunc(reqs, func(r swReq) bool { return r.who == "keystone:b" }) {
			t.Errorf("no token request for user b: %v", reqs)
		}
		if got, _, _ := swiftOf(reqs, "b"); len(got) != 0 {
			t.Errorf("Swift requests as user b without a token: %q", got)
		}
		rec := swParse(t, raw, err)
		m := byStep(t, rec)
		for _, s := range swSteps[:3] {
			c := 200
			if strings.Contains(s, "write") {
				c = 201
			}
			wantObs(t, m[s], "denied", 403, c, c)
		}
		for _, s := range swSteps[3:] {
			wantObs(t, m[s], "blocked", 0, 0, 0)
		}
	})

	t.Run("login-both-fail", func(t *testing.T) {
		// Exposes a refused login reported as isolation.
		w := newSwWorld(t)
		w.loginRefuse["a"], w.loginRefuse["b"] = true, true
		raw, _, err := swObserve(t, w, SwiftReach(w.options()))
		reqs := w.seen()
		for _, u := range []string{"a", "b"} {
			if !slices.ContainsFunc(reqs, func(r swReq) bool { return r.who == "keystone:"+u }) {
				t.Errorf("no token request for user %s: %v", u, reqs)
			}
		}
		if i := slices.IndexFunc(reqs, func(r swReq) bool { return r.host == swSwift || r.host == swSwiftSBG }); i >= 0 {
			t.Errorf("a Swift request without a token: %s", reqs[i])
		}
		rec := swParse(t, raw, err)
		m := byStep(t, rec)
		for _, s := range swSteps {
			wantObs(t, m[s], "not-run", 0, 0, 0)
		}
		if err == nil {
			t.Errorf("Observe passed with both logins refused")
		}
	})
}

// undoReqs checks every Undo request went to S3 with the bucket's own cleanup key, and that
// each bucket was listed before its first delete and after its last.
func undoReqs(t *testing.T, reqs []swReq) (deleted map[string][]string) {
	t.Helper()
	deleted = map[string][]string{}
	type span struct{ firstList, firstDel, lastDel, lastList int }
	spans := map[string]*span{swBucketA: {-1, -1, -1, -1}, swBucketB: {-1, -1, -1, -1}}
	for i, r := range reqs {
		if r.host != swS3 {
			t.Errorf("undo sent %s: want S3 only, never the identity under test", r)
			continue
		}
		bucket, key, _ := strings.Cut(strings.TrimPrefix(r.path, "/"), "/")
		want := map[string]string{swBucketA: "cleanup-a", swBucketB: "cleanup-b"}[bucket]
		if want == "" || r.who != want {
			t.Errorf("undo sent %s: want the bucket's own cleanup key (%s)", r, want)
			continue
		}
		s := spans[bucket]
		switch {
		case r.method == http.MethodGet && key == "":
			if s.firstList < 0 {
				s.firstList = i
			}
			s.lastList = i
		case r.method == http.MethodDelete && key != "":
			if s.firstDel < 0 {
				s.firstDel = i
			}
			s.lastDel = i
			deleted[bucket] = append(deleted[bucket], key)
		default:
			t.Errorf("undo sent %s: want listings and object deletes only", r)
		}
	}
	for _, b := range []string{swBucketA, swBucketB} {
		s := spans[b]
		if s.firstList < 0 || s.firstDel >= 0 && (s.firstList > s.firstDel || s.lastList < s.lastDel) {
			t.Errorf("bucket %s: want a listing before the deletes and one after (%+v)", b, *s)
		}
	}
	for _, keys := range deleted {
		if slices.Contains(keys, swSeed) {
			t.Errorf("undo deleted the root's seed %s", swSeed)
		}
	}
	return deleted
}

// TestSwiftUndo: Undo lists and deletes what the check wrote under lzprobe-swift-reach/ in both
// buckets, with each bucket's cleanup credential over S3, never the seeds, and lists again.
func TestSwiftUndo(t *testing.T) {
	t.Run("cleanup-credentials", func(t *testing.T) {
		// Exposes cleanup that depends on the identity under test: a fresh process (no Observe)
		// whose Undo may not use the tenant users at all (undoReqs fails any Keystone or Swift
		// request), so their Swift access is irrelevant here.
		w := newSwWorld(t)
		mine := []string{swPrefix + "own-a-1", swPrefix + "own-a-2"}
		for _, k := range mine {
			w.objects[swBucketA][k] = true
		}
		w.objects[swBucketB][swPrefix+"own-b-1"] = true
		w.objects[swBucketA]["outside/keep"] = true
		var term bytes.Buffer
		err := SwiftReach(w.options()).Undo(context.Background(), swEnv(), &term)
		deleted := undoReqs(t, w.seen())
		if !slices.Equal(slices.Sorted(slices.Values(deleted[swBucketA])), mine) || !slices.Equal(deleted[swBucketB], []string{swPrefix + "own-b-1"}) {
			t.Errorf("undo deleted %v, want %v in A and %s in B", deleted, mine, swPrefix+"own-b-1")
		}
		if err != nil || len(w.written(swBucketA))+len(w.written(swBucketB)) != 0 {
			t.Errorf("Undo = %v, left %q and %q", err, w.written(swBucketA), w.written(swBucketB))
		}
		if !w.has(swBucketA, swSeed) || !w.has(swBucketB, swSeed) || !w.has(swBucketA, "outside/keep") {
			t.Errorf("undo deleted the seeds or an object outside %s", swPrefix)
		}
		if leaks := w.swLeaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
			t.Errorf("terminal or error carry %q", leaks)
		}
	})

	t.Run("tenant-delete-refused", func(t *testing.T) {
		// Exposes an object left behind because the tenant cannot delete what it wrote, so the
		// bucket's destroy fails.
		w := newSwWorld(t)
		w.replies["a write "+swBucketB] = []swReply{{status: 201, store: true}}
		w.replies["b write "+swBucketA] = []swReply{{status: 201, store: true}}
		c := SwiftReach(w.options())
		_, _ = c.Observe(context.Background(), swEnv(), io.Discard)
		strayB, strayA := w.crossOf("a"), w.crossOf("b")
		if len(strayB) == 0 || len(strayA) == 0 {
			t.Fatalf("precondition: the cross-bucket writes were not stored (a→B %q, b→A %q; requests %v)", strayB, strayA, w.seen())
		}
		before := len(w.seen())
		err := c.Undo(context.Background(), swEnv(), io.Discard)
		deleted := undoReqs(t, w.seen()[before:])
		for b, keys := range map[string][]string{swBucketB: strayB, swBucketA: strayA} {
			for _, k := range keys {
				if !slices.Contains(deleted[b], k) {
					t.Errorf("the cleanup key of %s did not delete %s", b, k)
				}
			}
		}
		if err != nil || len(w.written(swBucketA))+len(w.written(swBucketB)) != 0 {
			t.Errorf("Undo = %v, left %q in A and %q in B", err, w.written(swBucketA), w.written(swBucketB))
		}
	})

	t.Run("lost-write-answer", func(t *testing.T) {
		// Exposes cleanup by a remembered object name only.
		w := newSwWorld(t)
		w.replies["a write "+swBucketB] = []swReply{{status: 201, store: true, hangup: true}}
		w.replies["b write "+swBucketA] = []swReply{{status: 201, store: true, hangup: true}}
		c := SwiftReach(w.options())
		_, _ = c.Observe(context.Background(), swEnv(), io.Discard)
		if len(w.crossOf("a")) == 0 || len(w.crossOf("b")) == 0 {
			t.Fatalf("precondition: the cross-bucket writes were not stored (requests %v)", w.seen())
		}
		before := len(w.seen())
		err := c.Undo(context.Background(), swEnv(), io.Discard)
		undoReqs(t, w.seen()[before:])
		if err != nil || len(w.written(swBucketA))+len(w.written(swBucketB)) != 0 {
			t.Errorf("Undo = %v, left %q in A and %q in B", err, w.written(swBucketA), w.written(swBucketB))
		}
	})

	t.Run("still-listed", func(t *testing.T) {
		// Exposes a silent leftover: lz-live keeps the root when Undo fails.
		w := newSwWorld(t)
		w.s3Keep = true
		w.objects[swBucketA][swPrefix+"own-a-1"] = true
		err := SwiftReach(w.options()).Undo(context.Background(), swEnv(), io.Discard)
		deleted := undoReqs(t, w.seen())
		if !slices.Contains(deleted[swBucketA], swPrefix+"own-a-1") {
			t.Errorf("undo did not try to delete %s (deleted %v)", swPrefix+"own-a-1", deleted)
		}
		if err == nil || !strings.Contains(err.Error(), swPrefix+"own-a-1") {
			t.Errorf("Undo = %v, want an error naming what is left (%s)", err, swPrefix+"own-a-1")
		}
	})
}

// TestSwiftSweep: Sweep in a fresh process, with the run id and the project only, after creates
// whose answers were lost (no output, no state entry).
func TestSwiftSweep(t *testing.T) {
	// Sweep has the signature of ProbeCheck's Sweep field (data-model.md; the field lands in T038).
	var _ func(context.Context, string, string, io.Writer) error = SwiftOptions{}.sweep

	setup := func(t *testing.T) *swWorld {
		w := newSwWorld(t)
		foreignA := "lzprobe-swift-reach-" + swOtherRun + "-a"
		w.buckets["GRA"] = []string{swBucketA, foreignA, "lzprobe-swift-reach-" + swRunLower + "-c", "lzprobe-state-" + swRunLower}
		w.buckets["SBG"] = []string{swBucketB} // Sweep is not told the region: it must list them all.
		for i, d := range append(swUsers(swRunLower), swUsers(swOtherRun)[0], "lzprobe-swift-reach-"+swRunLower+"-c", "lzprobe-state-"+swRun) {
			w.users = append(w.users, swAPIUser{ID: 101 + i, Description: d, Username: fmt.Sprintf("user-%04x", 101+i)})
		}
		return w
	}
	ours := []string{
		"DELETE /v1/cloud/project/" + swTenant + "/region/GRA/storage/" + swBucketA,
		"DELETE /v1/cloud/project/" + swTenant + "/region/SBG/storage/" + swBucketB,
		"DELETE /v1/cloud/project/" + swTenant + "/user/101", "DELETE /v1/cloud/project/" + swTenant + "/user/102",
		"DELETE /v1/cloud/project/" + swTenant + "/user/103", "DELETE /v1/cloud/project/" + swTenant + "/user/104",
	}
	slices.Sort(ours)
	deletes := func(t *testing.T, reqs []swReq) ([]string, int) {
		var out []string
		last := -1
		for i, r := range reqs {
			if r.host != "api" || r.who != "admin" {
				t.Errorf("sweep sent %s: want the sandbox admin on the OVHcloud API only", r)
			}
			if r.method == http.MethodDelete {
				out, last = append(out, r.method+" "+r.path), i
			}
		}
		slices.Sort(out)
		return out, last
	}

	t.Run("lost-creates", func(t *testing.T) {
		// Exposes an orphan bucket or user nobody deletes.
		w := setup(t)
		var term bytes.Buffer
		err := w.options().sweep(context.Background(), swRun, swTenant, &term)
		reqs := w.seen()
		got, last := deletes(t, reqs)
		if !slices.Equal(got, ours) {
			t.Errorf("sweep deleted %q, want exactly %q", got, ours)
		}
		relisted := func(path string) bool {
			return last >= 0 && slices.ContainsFunc(reqs[last+1:], func(r swReq) bool { return r.method == http.MethodGet && r.path == path })
		}
		if !relisted("/v1/cloud/project/"+swTenant+"/region/GRA/storage") || !relisted("/v1/cloud/project/"+swTenant+"/region/SBG/storage") || !relisted("/v1/cloud/project/"+swTenant+"/user") {
			t.Errorf("sweep did not list buckets and users again after its deletes: %v", reqs)
		}
		// Found by listing, not deleted blind by a derived name or region.
		regions := slices.IndexFunc(reqs, func(r swReq) bool {
			return r.method == http.MethodGet && r.path == "/v1/cloud/project/"+swTenant+"/region"
		})
		storage := slices.IndexFunc(reqs, func(r swReq) bool { return r.method == http.MethodGet && strings.HasSuffix(r.path, "/storage") })
		if regions < 0 || storage >= 0 && regions > storage {
			t.Errorf("sweep listed storage before the project's regions: %v", reqs)
		}
		for _, k := range []struct{ list, del string }{
			{"/v1/cloud/project/" + swTenant + "/region/GRA/storage", "/v1/cloud/project/" + swTenant + "/region/GRA/storage/"},
			{"/v1/cloud/project/" + swTenant + "/region/SBG/storage", "/v1/cloud/project/" + swTenant + "/region/SBG/storage/"},
			{"/v1/cloud/project/" + swTenant + "/user", "/v1/cloud/project/" + swTenant + "/user/"},
		} {
			list := slices.IndexFunc(reqs, func(r swReq) bool { return r.method == http.MethodGet && r.path == k.list })
			del := slices.IndexFunc(reqs, func(r swReq) bool { return r.method == http.MethodDelete && strings.HasPrefix(r.path, k.del) })
			if list < 0 || del >= 0 && list > del {
				t.Errorf("sweep deleted under %s before listing it: %v", k.list, reqs)
			}
		}
		if err != nil {
			t.Errorf("sweep = %v, want nil", err)
		}
		if leaks := w.swLeaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 {
			t.Errorf("terminal or error carry %q", leaks)
		}
	})

	t.Run("partial", func(t *testing.T) {
		// Exposes a sweep that gives up when a kind is incomplete: only bucket a and user b were
		// created before the apply failed.
		w := newSwWorld(t)
		w.buckets["GRA"] = []string{swBucketA}
		w.users = []swAPIUser{{ID: 102, Description: swUsers(swRunLower)[1], Username: "user-0066"}}
		err := w.options().sweep(context.Background(), swRun, swTenant, io.Discard)
		want := []string{"DELETE /v1/cloud/project/" + swTenant + "/region/GRA/storage/" + swBucketA, "DELETE /v1/cloud/project/" + swTenant + "/user/102"}
		if got, _ := deletes(t, w.seen()); !slices.Equal(got, want) || err != nil {
			t.Errorf("sweep = %v, deleted %q, want nil and %q", err, got, want)
		}
	})

	t.Run("still-listed", func(t *testing.T) {
		// Exposes a sweep that trusts an acknowledged delete.
		w := setup(t)
		w.apiKeep = true
		err := w.options().sweep(context.Background(), swRun, swTenant, io.Discard)
		if got, _ := deletes(t, w.seen()); !slices.Equal(got, ours) {
			t.Errorf("sweep deleted %q, want %q", got, ours)
		}
		if err == nil || !strings.Contains(err.Error(), swBucketA) {
			t.Errorf("sweep = %v, want an error naming what is left (%s)", err, swBucketA)
		}
	})

	t.Run("bad-run-id", func(t *testing.T) {
		// Control (passes on the stub too): a sweep refuses a run id or project it cannot derive
		// exact names from, before any request.
		for _, c := range [][2]string{{"lzprobe", swTenant}, {strings.ToLower(swRun), swTenant}, {swRun, "../" + swTenant}} {
			w := newSwWorld(t)
			if err := w.options().sweep(context.Background(), c[0], c[1], io.Discard); err == nil || len(w.seen()) != 0 {
				t.Errorf("sweep(%q, %q) = %v after %v, want a refusal before any request", c[0], c[1], err, w.seen())
			}
		}
	})
}

// TestSwiftValidate: Validate refuses published variables the check cannot use, before any
// request, and the check publishes the data-model's output and public keys.
func TestSwiftValidate(t *testing.T) {
	c := SwiftReach(SwiftOptions{})
	if c.Output != "swift_check" {
		t.Errorf("Output = %q, want swift_check", c.Output)
	}
	if want := []string{"auth_url", "bucket_a", "bucket_b", "region", "run_id", "seed_key", "tenant_id", "user_a_name", "user_b_name"}; !slices.Equal(slices.Sorted(slices.Values(c.Public)), want) {
		t.Errorf("Public = %q, want %q", c.Public, want)
	}
	if err := c.Validate(swEnv()); err != nil {
		t.Errorf("Validate refused the run's own variables: %v", err)
	}
	edit := func(k, v string) map[string]string {
		e := swEnv()
		if v == "" {
			delete(e, k)
		} else {
			e[k] = v
		}
		return e
	}
	type badEnv struct {
		name string
		env  map[string]string
	}
	var missing []badEnv
	for _, k := range slices.Sorted(maps.Keys(swEnv())) {
		missing = append(missing, badEnv{"missing " + k, edit(k, "")})
	}
	for _, bad := range append(missing, []badEnv{
		{"run_id not a run id", edit("run_id", "lzprobe-swift-reach")},
		{"run_id lower case", edit("run_id", swRunLower)},
		{"bucket_a of another run", edit("bucket_a", "lzprobe-swift-reach-"+swOtherRun+"-a")},
		{"bucket_a upper case", edit("bucket_a", "lzprobe-swift-reach-"+swRun+"-a")},
		{"bucket_b the other's", edit("bucket_b", swBucketA)},
		{"bucket_b another root's", edit("bucket_b", "lzprobe-p25b-"+swRunLower)},
		{"seed_key elsewhere", edit("seed_key", "lzprobe-swift-reach/other")},
		{"seed_key outside the prefix", edit("seed_key", "seed")},
	}...) {
		w := newSwWorld(t)
		c := SwiftReach(w.options())
		err := c.Validate(bad.env)
		if err == nil {
			t.Errorf("%s: Validate accepted it (exposes a check that writes to a bucket the run does not own)", bad.name)
		} else if leaks := w.swLeaks(err.Error()); len(leaks) > 0 {
			t.Errorf("%s: refusal carries %q", bad.name, leaks)
		}
		if _, err := c.Observe(context.Background(), bad.env, io.Discard); err == nil || len(w.seen()) != 0 {
			t.Errorf("%s: Observe = %v after %v, want a refusal before any request", bad.name, err, w.seen())
		}
		if err := c.Undo(context.Background(), bad.env, io.Discard); err == nil || len(w.seen()) != 0 {
			t.Errorf("%s: Undo = %v after %v, want a refusal before any request", bad.name, err, w.seen())
		}
	}
}

// TestSwiftLeak: a Keystone error body that echoes the password reaches neither the record nor
// the terminal. The guard is shown first on a leaking mutant, then trusted on the check.
func TestSwiftLeak(t *testing.T) {
	w := newSwWorld(t)
	w.loginRefuse["a"], w.loginRefuse["b"] = true, true

	// The leaking mutant: a check that copies the server's text into its record and terminal.
	mutant := &live.ProbeCheck{Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, env["auth_url"]+"/auth/tokens",
			strings.NewReader(fmt.Sprintf(`{"auth":{"identity":{"methods":["password"],"password":{"user":{"name":%q,"password":%q}}}}}`, env["user_a_name"], env["user_a_password"])))
		resp, err := w.options().HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		text, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(term, "LZ-LIVE observe a login refused: %s\n", text)
		return json.Marshal(map[string]any{"check": "swift-reach", "reason": string(text)})
	}}
	var mterm bytes.Buffer
	mraw, _ := mutant.Observe(context.Background(), swEnv(), &mterm)
	if leaks := w.swLeaks(string(mraw)); !slices.Contains(leaks, swPassA) {
		t.Fatalf("the leak guard missed the mutant's record (found %q): it cannot be trusted", leaks)
	}
	if leaks := w.swLeaks(mterm.String()); !slices.Contains(leaks, swPassA) {
		t.Fatalf("the leak guard missed the mutant's terminal (found %q): it cannot be trusted", leaks)
	}

	w2 := newSwWorld(t)
	w2.loginRefuse["a"], w2.loginRefuse["b"] = true, true
	var term bytes.Buffer
	raw, err := SwiftReach(w2.options()).Observe(context.Background(), swEnv(), &term)
	reqs := w2.seen()
	for _, u := range []string{"a", "b"} {
		if !slices.ContainsFunc(reqs, func(r swReq) bool { return r.who == "keystone:"+u }) {
			t.Errorf("no token request for user %s, so no echoing answer was read: %v", u, reqs)
		}
	}
	if raw == nil {
		t.Errorf("Observe returned no record (error %v)", err)
	}
	if leaks := w2.swLeaks(string(raw), term.String(), fmt.Sprint(err)); len(leaks) > 0 {
		t.Errorf("record, terminal or error carry %q", leaks)
	}
}

// TestSwiftFakes: the fakes answer as the cases above say they do, so the cases' red on the stub
// is the stub's, not the fixture's (a fixture control: it passes on the stub by design).
func TestSwiftFakes(t *testing.T) {
	ctx := context.Background()
	w := newSwWorld(t)
	w.killAfter["b"] = true
	client := w.options().HTTP
	do := func(method, url, tok string, body string) (int, http.Header, string, error) {
		req, _ := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Auth-Token", tok)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(b), nil
	}
	login := func(name, pass string) (int, string, string) {
		st, h, body, err := do(http.MethodPost, swAuthURL+"/auth/tokens", "", fmt.Sprintf(`{"auth":{"identity":{"methods":["password"],"password":{"user":{"name":%q,"domain":{"id":"default"},"password":%q}}},"scope":{"project":{"id":%q}}}}`, name, pass, swTenant))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		return st, h.Get("X-Subject-Token"), body
	}
	st, tokA, body := login(swUserA, swPassA)
	if st != 201 || tokA == "" || !strings.Contains(body, `"url":"https://`+swSwift+`/v1/AUTH_`+swTenant+`"`) {
		t.Fatalf("login a = %d %q %s, want 201, a token and the GRA object-store endpoint", st, tokA, body)
	}
	if _, tokA2, _ := login(swUserA, swPassA); tokA2 == tokA {
		t.Errorf("a second login issued the same token")
	}
	w.loginRefuse["a"] = true
	if st, _, body := login(swUserA, swPassA); st != 401 || !strings.Contains(body, swPassA) {
		t.Errorf("refused login = %d %s, want 401 echoing the password", st, body)
	}
	sw := "https://" + swSwift + "/v1/AUTH_" + swTenant + "/"
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, swBucketA + "?format=json", 200}, {http.MethodGet, swBucketA + "/" + swSeed, 200},
		{http.MethodPut, swBucketA + "/" + swPrefix + "w1", 201}, {http.MethodGet, swBucketB, 403},
		{http.MethodGet, swBucketB + "/" + swSeed, 403}, {http.MethodPut, swBucketB + "/" + swPrefix + "w1", 403},
		{http.MethodDelete, swBucketA + "/" + swPrefix + "w1", 403},
	} {
		if st, _, _, err := do(c.method, sw+c.path, tokA, ""); err != nil || st != c.want {
			t.Errorf("%s %s = %d %v, want %d", c.method, c.path, st, err, c.want)
		}
	}
	if _, _, list, _ := do(http.MethodGet, sw+swBucketA+"?format=json", tokA, ""); !strings.Contains(list, swSeed) || !strings.Contains(list, swPrefix+"w1") {
		t.Errorf("own listing %s lacks the seed or the write", list)
	}
	if st, _, _, _ := do(http.MethodGet, sw+swBucketA, "forged", ""); st != 401 {
		t.Errorf("unknown token = %d, want 401", st)
	}
	_, tokB, _ := login(swUserB, swPassB)
	if st, _, _, _ := do(http.MethodGet, sw+swBucketB, tokB, ""); st != 200 {
		t.Errorf("b's control before the other bucket = %d, want 200", st)
	}
	_, _, _, _ = do(http.MethodGet, sw+swBucketA, tokB, "")
	if st, _, _, _ := do(http.MethodGet, sw+swBucketB, tokB, ""); st != 401 {
		t.Errorf("b's repeat after killAfter = %d, want 401", st)
	}
	w.otherAllow = true
	w.replies["a write "+swBucketB] = []swReply{{status: 201, store: true, hangup: true}}
	if st, _, _, _ := do(http.MethodGet, sw+swBucketB+"/"+swSeed, tokA, ""); st != 200 {
		t.Errorf("other seed with otherAllow = %d, want 200", st)
	}
	w.replies["a list "+swBucketB] = []swReply{{status: 403, hangup: true}}
	n := len(w.seen())
	if _, _, _, err := do(http.MethodGet, sw+swBucketB, tokA, ""); err == nil || len(w.seen()) != n+1 {
		t.Errorf("hangup list: err %v after %d requests, want a transport error after exactly one (no resend)", err, len(w.seen())-n)
	}
	if _, _, _, err := do(http.MethodPut, sw+swBucketB+"/"+swPrefix+"lost", tokA, ""); err == nil || !w.has(swBucketB, swPrefix+"lost") {
		t.Errorf("hangup write: err %v, stored %v; want a transport error after the store", err, w.has(swBucketB, swPrefix+"lost"))
	}

	// S3, through the package's own S3 checker: each cleanup key on its own bucket only.
	s3 := func(ak, sk string) *live.S3Checker {
		c, err := live.NewS3Checker(live.S3Options{Region: "GRA", HTTP: client, Now: func() time.Time { return swNow },
			Keys: map[string]string{"AWS_ACCESS_KEY_ID": ak, "AWS_SECRET_ACCESS_KEY": sk}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ca, cb := s3(swCleanA, swCleanASK), s3(swCleanB, swCleanBSK)
	if a, err := ca.Do(ctx, live.S3Call{Op: live.S3ListObjectVersions, Bucket: swBucketA}); err != nil || a.Status != 200 || !strings.Contains(string(a.Body), "<Key>"+swPrefix+"w1</Key>") {
		t.Errorf("cleanup-a lists A = %+v %v, want 200 with %s", a, err, swPrefix+"w1")
	}
	if a, err := ca.Do(ctx, live.S3Call{Op: live.S3ListObjectVersions, Bucket: swBucketB}); err != nil || a.Status != 403 {
		t.Errorf("cleanup-a lists B = %+v %v, want 403", a, err)
	}
	if a, err := cb.Do(ctx, live.S3Call{Op: live.S3DeleteObject, Bucket: swBucketB, Key: swPrefix + "lost"}); err != nil || a.Status != 204 || w.has(swBucketB, swPrefix+"lost") {
		t.Errorf("cleanup-b deletes = %+v %v, still there %v", a, err, w.has(swBucketB, swPrefix+"lost"))
	}

	// The API, with the sandbox admin's token.
	w.mu.Lock()
	w.buckets["GRA"] = []string{swBucketA}
	w.users = []swAPIUser{{ID: 7, Description: swUsers(swRunLower)[0]}}
	w.mu.Unlock()
	tr, err := http.PostForm(w.api.URL+"/token", map[string][]string{"grant_type": {"client_credentials"}, "client_id": {swAdminID}, "client_secret": {swAdminSK}})
	if err != nil {
		t.Fatal(err)
	}
	tb, _ := io.ReadAll(tr.Body)
	tr.Body.Close()
	if !strings.Contains(string(tb), "tok-swadmin") {
		t.Fatalf("admin token answer %s", tb)
	}
	api := func(method, path string) (int, string) {
		req, _ := http.NewRequestWithContext(ctx, method, w.api.URL+"/v1/cloud/project/"+swTenant+path, nil)
		req.Header.Set("Authorization", "Bearer tok-swadmin")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for _, c := range []struct{ method, path, want string }{
		{http.MethodGet, "/region", `["GRA","SBG"]`}, {http.MethodGet, "/region/GRA/storage", swBucketA},
		{http.MethodGet, "/user", swUsers(swRunLower)[0]}, {http.MethodDelete, "/region/GRA/storage/" + swBucketA, "null"},
		{http.MethodDelete, "/user/7", "null"}, {http.MethodGet, "/region/GRA/storage", "[]"}, {http.MethodGet, "/user", "[]"},
	} {
		if st, b := api(c.method, c.path); st != 200 || !strings.Contains(b, c.want) {
			t.Errorf("%s %s = %d %s, want 200 with %s", c.method, c.path, st, b, c.want)
		}
	}
}

// T030: the cases the implementation adds beyond T029's, from the task's scope and the
// coordinator's decisions of 2026-10-16 (idempotent Undo, Sweep wired, a bucket emptied before
// its delete, a foreign-host auth_url refused).

// TestSwiftSeed: the provider has no object resource (provider docs, resources/: no object
// upload), so the check's first step writes each bucket's seed with that bucket's cleanup key,
// before any Swift request; a refused seed write leaves the read steps without a control.
func TestSwiftSeed(t *testing.T) {
	t.Run("written-first", func(t *testing.T) {
		// Exposes a read step with nothing to read when the root could not seed.
		w := newSwWorld(t)
		delete(w.objects[swBucketA], swSeed)
		delete(w.objects[swBucketB], swSeed)
		raw, _, err := swObserve(t, w, SwiftReach(w.options()))
		reqs := w.seen()
		first := slices.IndexFunc(reqs, func(r swReq) bool { return r.host == swSwift || r.host == swSwiftSBG })
		for b, who := range map[string]string{swBucketA: "cleanup-a", swBucketB: "cleanup-b"} {
			put := slices.IndexFunc(reqs, func(r swReq) bool {
				return r.host == swS3 && r.who == who && r.method == http.MethodPut && r.path == "/"+b+"/"+swSeed
			})
			if put < 0 || first >= 0 && put > first {
				t.Errorf("no seed write to %s with %s before the first Swift request: %v", b, who, reqs)
			}
		}
		if !w.has(swBucketA, swSeed) || !w.has(swBucketB, swSeed) {
			t.Errorf("seeds not written")
		}
		m := byStep(t, swParse(t, raw, err))
		wantObs(t, m["a-read-b"], "denied", 403, 200, 200)
		wantObs(t, m["b-read-a"], "denied", 403, 200, 200)
		if err != nil {
			t.Errorf("Observe = %v, want nil when every step is denied", err)
		}
	})

	t.Run("write-refused", func(t *testing.T) {
		// Exposes a missing seed read as a denial: the own read answers 404, so the read steps
		// are blocked, the other routes still observed, and Observe fails (C005.1 not shown).
		w := newSwWorld(t)
		w.seedRefuse = true
		delete(w.objects[swBucketA], swSeed)
		delete(w.objects[swBucketB], swSeed)
		raw, term, err := swObserve(t, w, SwiftReach(w.options()))
		m := byStep(t, swParse(t, raw, err))
		wantObs(t, m["a-read-b"], "blocked", 403, 404, 404)
		wantObs(t, m["b-read-a"], "blocked", 403, 404, 404)
		wantObs(t, m["a-list-b"], "denied", 403, 200, 200)
		wantObs(t, m["b-write-a"], "denied", 403, 201, 201)
		if err == nil || !strings.Contains(err.Error(), "a-read-b") {
			t.Errorf("Observe = %v, want an error naming the blocked steps", err)
		}
		if !strings.Contains(term, "seed") {
			t.Errorf("terminal does not say the seed write failed:\n%s", term)
		}
	})
}

// TestSwiftUndoIdempotent (coordinator decision 2026-10-16): Undo converges. A second Undo after a
// successful one passes; so does an Undo after a Sweep removed the bucket or the cleanup user (404
// NoSuchBucket, 403 InvalidAccessKeyId); any other refusal still fails it.
func TestSwiftUndoIdempotent(t *testing.T) {
	t.Run("second-undo", func(t *testing.T) {
		w := newSwWorld(t)
		w.replies["a write "+swBucketB] = []swReply{{status: 201, store: true}}
		c := SwiftReach(w.options())
		_, _ = c.Observe(context.Background(), swEnv(), io.Discard)
		if err := c.Undo(context.Background(), swEnv(), io.Discard); err != nil {
			t.Fatalf("first Undo = %v", err)
		}
		before := len(w.seen())
		if err := c.Undo(context.Background(), swEnv(), io.Discard); err != nil {
			t.Errorf("second Undo = %v, want nil", err)
		}
		if d := undoReqs(t, w.seen()[before:]); len(d) != 0 {
			t.Errorf("second Undo deleted %v, want nothing", d)
		}
	})

	t.Run("after-sweep", func(t *testing.T) {
		// A fresh process: Sweep deleted both buckets and all four users, then --cleanup reruns Undo.
		w := newSwWorld(t)
		w.buckets["GRA"] = []string{swBucketA, swBucketB}
		for i, d := range swUsers(swRunLower) {
			w.users = append(w.users, swAPIUser{ID: 201 + i, Description: d})
		}
		c := SwiftReach(w.options())
		if err := c.Sweep(context.Background(), swRun, swTenant, io.Discard); err != nil {
			t.Fatalf("Sweep = %v", err)
		}
		var term bytes.Buffer
		if err := c.Undo(context.Background(), swEnv(), &term); err != nil {
			t.Errorf("Undo after a Sweep = %v, want nil (already removed is undone)", err)
		}
		if !strings.Contains(term.String(), "already") {
			t.Errorf("terminal does not say what was already removed:\n%s", term.String())
		}
	})

	for _, c := range []struct {
		name  string
		setup func(w *swWorld)
		ok    bool
	}{
		{"bucket-gone", func(w *swWorld) { delete(w.objects, swBucketA); delete(w.objects, swBucketB) }, true},
		{"credential-gone", func(w *swWorld) { w.goneKeys[swCleanA], w.goneKeys[swCleanB] = true, true }, true},
		{"access-denied", func(w *swWorld) { w.s3Refuse = true }, false},
		// Review r1: a 200 whose body is not a ListVersionsResult is no empty listing.
		{"wrong-listing", func(w *swWorld) { w.listWrong = true }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Exposes a non-convergent Undo, or (access-denied) one that tolerates any refusal.
			w := newSwWorld(t)
			c.setup(w)
			err := SwiftReach(w.options()).Undo(context.Background(), swEnv(), io.Discard)
			if (err == nil) != c.ok {
				t.Errorf("Undo = %v, want passing %v", err, c.ok)
			}
			if err != nil && !strings.Contains(err.Error(), swBucketA) {
				t.Errorf("Undo error %v does not name the bucket", err)
			}
		})
	}
}

// TestSwiftSweepEmpties (coordinator decision 2026-10-16): Sweep empties a bucket before its
// delete; a bucket it cannot empty is not deleted and fails the sweep naming it, while the other
// resources are still swept. Through the check's Sweep field (Sweep: o.sweep).
func TestSwiftSweepEmpties(t *testing.T) {
	foreign := "lzprobe-swift-reach-" + swOtherRun + "-a"
	setup := func(t *testing.T) *swWorld {
		w := newSwWorld(t)
		w.buckets["GRA"] = []string{swBucketA, foreign}
		w.buckets["SBG"] = []string{swBucketB}
		w.objects[swBucketA][swPrefix+"write-a-control"] = true
		w.objects[swBucketB][swPrefix+"write-a-other"] = true
		w.objects[foreign] = map[string]bool{"keep": true}
		for i, d := range swUsers(swRunLower) {
			w.users = append(w.users, swAPIUser{ID: 301 + i, Description: d})
		}
		return w
	}
	region := map[string]string{swBucketA: "GRA", swBucketB: "SBG"}

	t.Run("emptied", func(t *testing.T) {
		// Exposes a sweep that deletes a bucket still holding objects.
		w := setup(t)
		c := SwiftReach(w.options())
		if c.Sweep == nil {
			t.Fatal("the check has no Sweep")
		}
		err := c.Sweep(context.Background(), swRun, swTenant, io.Discard)
		reqs := w.seen()
		for _, b := range []string{swBucketA, swBucketB} {
			base := "/v1/cloud/project/" + swTenant + "/region/" + region[b] + "/storage/" + b
			bulk := slices.IndexFunc(reqs, func(r swReq) bool { return r.method == http.MethodPost && r.path == base+"/bulkDeleteObjects" })
			del := slices.IndexFunc(reqs, func(r swReq) bool { return r.method == http.MethodDelete && r.path == base })
			list := slices.IndexFunc(reqs, func(r swReq) bool { return r.method == http.MethodGet && r.path == base+"/object" })
			if list < 0 || bulk < list || del < bulk {
				t.Errorf("bucket %s: want its objects listed, then deleted, then the bucket (list %d, bulk %d, delete %d)", b, list, bulk, del)
			}
		}
		if slices.ContainsFunc(reqs, func(r swReq) bool { return strings.Contains(r.path, foreign) }) || !w.has(foreign, "keep") {
			t.Errorf("sweep touched the other run's bucket: %v", reqs)
		}
		if err != nil {
			t.Errorf("Sweep = %v, want nil", err)
		}
	})

	t.Run("not-emptied", func(t *testing.T) {
		// Exposes a sweep that deletes a bucket whose objects stay, or gives up on the users.
		w := setup(t)
		w.bulkKeep = true
		err := SwiftReach(w.options()).Sweep(context.Background(), swRun, swTenant, io.Discard)
		reqs := w.seen()
		for _, b := range []string{swBucketA, swBucketB} {
			if slices.ContainsFunc(reqs, func(r swReq) bool { return r.method == http.MethodDelete && strings.HasSuffix(r.path, "/storage/"+b) }) {
				t.Errorf("sweep deleted %s while it still held objects", b)
			}
			if err == nil || !strings.Contains(err.Error(), b) {
				t.Errorf("Sweep = %v, want an error naming %s", err, b)
			}
		}
		w.mu.Lock()
		left := len(w.users)
		w.mu.Unlock()
		if left != 0 {
			t.Errorf("%d users left: the sweep gave up on the users after a bucket", left)
		}
	})
}

// TestSwiftCatalog: the token goes only to the project's Swift account on an OVHcloud storage
// host (storage.<region>.cloud.ovh.net/v1/AUTH_<tenant>, pcs-curl-commands-memo.mdx:16); a
// catalog naming another endpoint for the region leaves the user without a session.
func TestSwiftCatalog(t *testing.T) {
	for _, u := range []string{
		"https://storage.gra.example.net/v1/AUTH_" + swTenant,
		"http://" + swSwift + "/v1/AUTH_" + swTenant,
		"https://" + swSwift + "/v1/AUTH_" + strings.Repeat("0", 32),
		"https://" + swSwift + ":8443/v1/AUTH_" + swTenant,
	} {
		w := newSwWorld(t)
		w.catalogGRA = u
		raw, _, err := swObserve(t, w, SwiftReach(w.options()))
		if reqs := w.seen(); slices.ContainsFunc(reqs, func(r swReq) bool { return r.token != "" || r.who == "?" }) {
			t.Errorf("%s: a request went to an endpoint the catalog should not be trusted for: %v", u, reqs)
		}
		m := byStep(t, swParse(t, raw, err))
		for _, s := range swSteps {
			if m[s].Observed != "not-run" || !strings.Contains(m[s].Reason, "catalog") {
				t.Errorf("%s: %+v, want not-run naming the catalog", u, m[s])
			}
		}
	}
}

// TestSwiftUndoKeyNames (review r2): Undo's error names a leftover key only when it is shaped like
// the check's own keys and holds no secret; a listed key carrying a password is not echoed.
func TestSwiftUndoKeyNames(t *testing.T) {
	w := newSwWorld(t)
	w.s3Keep = true
	for _, k := range []string{swPrefix + "own-a-1", swPrefix + swPassA, swPrefix + strings.ToLower(swPassB)} {
		w.objects[swBucketA][k] = true
	}
	err := SwiftReach(w.options()).Undo(context.Background(), swEnv(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), swPrefix+"own-a-1") || !strings.Contains(err.Error(), "another shape") {
		t.Errorf("Undo = %v, want an error naming %s and the others by a placeholder", err, swPrefix+"own-a-1")
	}
	if leaks := w.swLeaks(fmt.Sprint(err), strings.ToLower(fmt.Sprint(err))); len(leaks) > 0 {
		t.Errorf("Undo's error carries %q", leaks)
	}
	if err != nil && strings.Contains(err.Error(), strings.ToLower(swPassB)) {
		t.Errorf("Undo's error carries the lower-cased password")
	}
}

// TestSwiftRoleEcho (review r1): a successful Keystone answer listing the token or the password
// as a role name puts neither in the record or the terminal (the token is not in the run's
// redactor); the honest role is still recorded.
func TestSwiftRoleEcho(t *testing.T) {
	w := newSwWorld(t)
	w.roleEcho = true
	// swObserve fails the test when the record, terminal or error carries a token or a secret.
	raw, term, err := swObserve(t, w, SwiftReach(w.options()))
	var rec struct {
		Roles map[string][]string `json:"roles"`
	}
	_ = json.Unmarshal(raw, &rec)
	for _, u := range []string{"user-a", "user-b"} {
		if !slices.Equal(rec.Roles[u], []string{"objectstore_operator"}) {
			t.Errorf("roles of %s = %q, want only objectstore_operator (the echoed token, password and key dropped)", u, rec.Roles[u])
		}
	}
	if !strings.Contains(term, "objectstore_operator") {
		t.Errorf("the role objectstore_operator is not printed:\n%s", term)
	}
	if err != nil {
		t.Errorf("Observe = %v, want nil (every step denied)", err)
	}
}

// TestSwiftValidateAuthURL (deferred from T029 to T030): Validate refuses an auth_url other than
// OVHcloud's Keystone (docs.ovhcloud.com storage-and-backup/object-storage/
// pcs-curl-commands-memo.mdx:15), before any request: the passwords go to it.
func TestSwiftValidateAuthURL(t *testing.T) {
	for _, u := range []string{
		"https://auth.example.net/v3",
		"http://auth.cloud.ovh.net/v3",
		"https://auth.cloud.ovh.net.example.net/v3",
		"https://user@auth.cloud.ovh.net/v3",
		"https://auth.cloud.ovh.net:8443/v3",
		"https://auth.cloud.ovh.net/v2.0",
		"https://auth.cloud.ovh.net/v3?x=1",
	} {
		w := newSwWorld(t)
		env := swEnv()
		env["auth_url"] = u
		c := SwiftReach(w.options())
		if err := c.Validate(env); err == nil {
			t.Errorf("%s: Validate accepted it", u)
		}
		if _, err := c.Observe(context.Background(), env, io.Discard); err == nil || len(w.seen()) != 0 {
			t.Errorf("%s: Observe = %v after %v, want a refusal before any request", u, err, w.seen())
		}
	}
}
