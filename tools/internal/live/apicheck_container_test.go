package live

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Spec 003 T041: the Swift container operations (kb api/v1/cloud.json
// /cloud/project/{serviceName}/storage, "Create/Get/Delete SWIFT container").

// TestAPICheckerContainerOps: each container operation goes out with its method, path, query and
// body; the create's body is cloud.ProjectStorageCreation with archive false; the delete is
// recursive with the id as one escaped segment.
func TestAPICheckerContainerOps(t *testing.T) {
	t.Parallel()
	base := "/v1/cloud/project/" + apiTestProject + "/storage"
	cases := []struct {
		call                      APICall
		method, path, query, body string
	}{
		{APICall{Op: APICreateContainer, Project: apiTestProject, Region: "GRA", Name: "lzprobe-swift-reach-x-swift-a"}, "POST", base, "",
			`{"archive":false,"containerName":"lzprobe-swift-reach-x-swift-a","region":"GRA"}`},
		{APICall{Op: APIListContainers, Project: apiTestProject}, "GET", base, "", ""},
		{APICall{Op: APIDeleteContainer, Project: apiTestProject, ID: "R1JB/MTpj=="}, "DELETE", base + "/R1JB%2FMTpj==", "recursive=true", ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.call.Op), func(t *testing.T) {
			f := newFakeCheckAPI(t)
			f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, "[]") }
			got, err := newTestAPIChecker(t, f).Do(context.Background(), tc.call)
			reqs := f.requests()
			if err != nil || got.Status != http.StatusOK || len(reqs) != 1 {
				t.Fatalf("Do = %+v, %v after %d requests; want 200 after one", got, err, len(reqs))
			}
			r := reqs[0]
			if r.Method != tc.method || r.Path != tc.path || r.Query != tc.query || string(r.Body) != tc.body || r.Auth != "Bearer "+apiTestToken {
				t.Errorf("sent %s %s?%s %q, want %s %s?%s %q with the bearer token", r.Method, r.Path, r.Query, r.Body, tc.method, tc.path, tc.query, tc.body)
			}
		})
	}
}

// TestAPICheckerContainerGate: a container operation with a part it does not take or of the wrong
// shape is refused before any request; a container id that could leave its path segment is one.
func TestAPICheckerContainerGate(t *testing.T) {
	t.Parallel()
	refused := map[string]APICall{
		"create without region":    {Op: APICreateContainer, Project: apiTestProject, Name: "lzprobe-c"},
		"create with an id":        {Op: APICreateContainer, Project: apiTestProject, Region: "GRA", Name: "lzprobe-c", ID: "x"},
		"create name not a name":   {Op: APICreateContainer, Project: apiTestProject, Region: "GRA", Name: "../x"},
		"list with a name":         {Op: APIListContainers, Project: apiTestProject, Name: "lzprobe-c"},
		"list project not an id":   {Op: APIListContainers, Project: "../p"},
		"delete without id":        {Op: APIDeleteContainer, Project: apiTestProject},
		"delete id dot-dot":        {Op: APIDeleteContainer, Project: apiTestProject, ID: ".."},
		"delete id dot":            {Op: APIDeleteContainer, Project: apiTestProject, ID: "."},
		"delete id with a query":   {Op: APIDeleteContainer, Project: apiTestProject, ID: "a?recursive=false"},
		"delete id with a space":   {Op: APIDeleteContainer, Project: apiTestProject, ID: "a b"},
		"delete id with a percent": {Op: APIDeleteContainer, Project: apiTestProject, ID: "a%2e"},
	}
	for name, call := range refused {
		t.Run(name, func(t *testing.T) {
			f := newFakeCheckAPI(t)
			if _, err := newTestAPIChecker(t, f).Do(context.Background(), call); err == nil || len(f.requests()) != 0 {
				t.Errorf("Do(%+v) = %v after %d requests, want a refusal before any", call, err, len(f.requests()))
			}
		})
	}
}

// TestAPICheckerContainerListing: a listing keeps every container's id and name; an answer that is
// not an array of objects with both is an error, never an empty listing; a refusal is the answer.
func TestAPICheckerContainerListing(t *testing.T) {
	t.Parallel()
	list := APICall{Op: APIListContainers, Project: apiTestProject}
	run := func(t *testing.T, status int, body string) (APIAnswer, error) {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(status); fmt.Fprint(rw, body) }
		return newTestAPIChecker(t, f).Do(context.Background(), list)
	}
	a, err := run(t, 200, `[{"id":"Y29udGFpbmVy0001","name":"lzprobe-c","region":"GRA","archive":false,"storedObjects":1},{"id":"Y2","name":"backups"}]`)
	if want := []APIContainer{{"Y29udGFpbmVy0001", "lzprobe-c"}, {"Y2", "backups"}}; err != nil || !slices.Equal(a.Containers, want) {
		t.Errorf("listing = %+v, %v; want %+v", a.Containers, err, want)
	}
	for name, body := range map[string]string{
		"null": `null`, "object": `{}`, "null entry": `[null]`, "no id": `[{"name":"lzprobe-c"}]`,
		"no name": `[{"id":"Y2"}]`, "numeric id": `[{"id":7,"name":"lzprobe-c"}]`, "SERVERTEXT": `SERVERTEXT`,
	} {
		t.Run(name, func(t *testing.T) {
			if a, err := run(t, 200, body); err == nil {
				t.Errorf("listing %s = %+v, want an error", body, a)
			} else {
				apiNoLeak(t, err.Error())
			}
		})
	}
	if a, err := run(t, 403, `{"message":"missing IAM permissions: publicCloudProject:apiovh:storage/get"}`); err != nil || a.Status != 403 ||
		!slices.Equal(a.Actions, []string{"publicCloudProject:apiovh:storage/get"}) {
		t.Errorf("refused listing = %+v, %v; want 403 with the action", a, err)
	}
}

// TestAPICheckerContainerFailures: a refused create or delete keeps its status and the actions it
// names; a delete without an answer (hangup, oversized answer) is an error that names the route,
// never the listed container id (review r1).
func TestAPICheckerContainerFailures(t *testing.T) {
	t.Parallel()
	const id = "SECRETISHID0001"
	del := APICall{Op: APIDeleteContainer, Project: apiTestProject, ID: id}
	create := APICall{Op: APICreateContainer, Project: apiTestProject, Region: "GRA", Name: "lzprobe-c"}
	for _, call := range []APICall{create, del} {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusForbidden)
			fmt.Fprint(rw, `{"message":"missing IAM permissions: publicCloudProject:apiovh:storage/create"}`)
		}
		a, err := newTestAPIChecker(t, f).Do(context.Background(), call)
		if err != nil || a.Status != 403 || !slices.Equal(a.Actions, []string{"publicCloudProject:apiovh:storage/create"}) {
			t.Errorf("%s refused = %+v, %v; want 403 with the action", call.Op, a, err)
		}
	}
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"hangup": func(rw http.ResponseWriter, r *http.Request) {
			if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		},
		"oversized": func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprint(rw, strings.Repeat("x", maxBody+2))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeCheckAPI(t)
			f.answer = answer
			_, err := newTestAPIChecker(t, f).Do(context.Background(), del)
			if err == nil || strings.Contains(err.Error(), id) {
				t.Errorf("delete without an answer = %v, want an error without the container id", err)
			}
		})
	}
}

// TestLeftoversContainerShape: a Swift container listed without a name, or with an empty one,
// cannot be classified (the kind has no inventory entry), so the check fails rather than pass.
func TestLeftoversContainerShape(t *testing.T) {
	t.Parallel()
	w, _ := loadSynthetic(t)
	for name, body := range map[string]string{
		"no name":    `[{"id":"R1JBMTE6eA","region":"GRA11"}]`,
		"null name":  `[{"id":"R1JBMTE6eA","name":null}]`,
		"empty name": `[{"id":"R1JBMTE6eA","name":""}]`,
		"no id":      `[{"name":"lz-swift"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			s := syntheticSeed{Replace: map[string][]json.RawMessage{"/cloud/project/p1/storage": {json.RawMessage(body)}}}
			c, _, inv := worldCheck(t, w, s)
			if rep := c.Check(context.Background(), inv); rep.Outcome != "fail" || len(rep.Errors) == 0 {
				t.Errorf("listing %s: outcome %q, errors %v; want fail with an error", body, rep.Outcome, rep.Errors)
			}
		})
	}
}
