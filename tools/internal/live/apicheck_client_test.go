package live

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// Spec 003 T032: the five operations the token-residual check sends (kb api/v1/me.json
// /me/api/oauth2/client, oauth2.clientRequest, oauth2.client, oauth2.clientSecret; kb
// api/v1/cloud.json GET /cloud/project/{serviceName}) and the token answer's expires_in.

const apiTestClientName = "lzprobe-token-residual-20261016T111500Z-3c4d"

func TestAPICheckerClientOps(t *testing.T) {
	t.Parallel()
	t.Run("operations", func(t *testing.T) {
		for _, tc := range []struct {
			call         APICall
			method, path string
			answer       string
		}{
			{APICall{Op: APICreateClient, Name: apiTestClientName}, "POST", "/v1/me/api/oauth2/client", `{"clientId":"EU.abc123","clientSecret":"created-secret-9Zq"}`},
			{APICall{Op: APIGetClient, ID: "EU.abc123"}, "GET", "/v1/me/api/oauth2/client/EU.abc123", `{"clientId":"EU.abc123","name":"n","identity":"urn:v1:eu:identity:credential:xx1111-ovh/oauth2-EU.abc123"}`},
			{APICall{Op: APIDeleteClient, ID: "EU.abc123"}, "DELETE", "/v1/me/api/oauth2/client/EU.abc123", `null`},
			{APICall{Op: APIGetProject, Project: apiTestProject}, "GET", "/v1/cloud/project/" + apiTestProject, `{"project_id":"x"}`},
		} {
			t.Run(string(tc.call.Op), func(t *testing.T) {
				f := newFakeCheckAPI(t)
				f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, tc.answer) }
				got, err := newTestAPIChecker(t, f).Do(context.Background(), tc.call)
				if err != nil || got.Status != http.StatusOK {
					t.Fatalf("Do = %+v, %v", got, err)
				}
				reqs := f.requests()
				if len(reqs) != 1 {
					t.Fatalf("%d requests, want 1", len(reqs))
				}
				r := reqs[0]
				if r.Method != tc.method || r.Path != tc.path || r.Auth != "Bearer "+apiTestToken {
					t.Errorf("sent %s %s (auth %q), want %s %s with the bearer token", r.Method, r.Path, r.Auth, tc.method, tc.path)
				}
				switch tc.call.Op {
				case APICreateClient:
					want := `{"callbackUrls":[],"description":"` + apiTestClientName + `","flow":"CLIENT_CREDENTIALS","name":"` + apiTestClientName + `"}`
					if string(r.Body) != want || r.ContentType != "application/json" {
						t.Errorf("body %s (%s), want %s as JSON", r.Body, r.ContentType, want)
					}
					if len(got.Clients) != 1 || got.Clients[0].ID != "EU.abc123" || got.Secret != "created-secret-9Zq" {
						t.Errorf("answer %+v, want the client id and its secret", got)
					}
				case APIGetClient:
					if len(got.Clients) != 1 || got.Clients[0] != (APIClient{ID: "EU.abc123", Name: "n", Identity: "urn:v1:eu:identity:credential:xx1111-ovh/oauth2-EU.abc123"}) {
						t.Errorf("answer %+v, want the client with its identity", got.Clients)
					}
				default:
					if len(r.Body) != 0 {
						t.Errorf("body %q on %s", r.Body, tc.call.Op)
					}
				}
			})
		}
	})

	t.Run("gate", func(t *testing.T) {
		for name, call := range map[string]APICall{
			"create without name":     {Op: APICreateClient},
			"create with spaced name": {Op: APICreateClient, Name: "lzprobe x"},
			"create with id":          {Op: APICreateClient, Name: apiTestClientName, ID: "EU.a"},
			"create with body":        {Op: APICreateClient, Name: apiTestClientName, Body: []byte("{}")},
			"list without name":       {Op: APIListClients},
			"list with project":       {Op: APIListClients, Name: apiTestClientName, Project: apiTestProject},
			"get without id":          {Op: APIGetClient},
			"get with a path id":      {Op: APIGetClient, ID: "../me"},
			"get with a dot id":       {Op: APIGetClient, ID: "."},
			"delete with a slash id":  {Op: APIDeleteClient, ID: "EU.a/x"},
			"delete with a name":      {Op: APIDeleteClient, ID: "EU.a", Name: "x"},
			"project without project": {Op: APIGetProject},
			"project not hex":         {Op: APIGetProject, Project: "../../me"},
			"project with name":       {Op: APIGetProject, Project: apiTestProject, Name: "b-1"},
		} {
			t.Run(name, func(t *testing.T) {
				f := newFakeCheckAPI(t)
				if _, err := newTestAPIChecker(t, f).Do(context.Background(), call); err == nil {
					t.Error("no refusal")
				}
				if n := len(f.requests()); n != 0 {
					t.Errorf("%d requests before the refusal, want 0", n)
				}
			})
		}
	})

	t.Run("list keeps the exact name", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		names := map[string]string{"EU.a1": apiTestClientName, "EU.b2": apiTestClientName + "-x", "EU.c3": strings.ToLower(apiTestClientName), "EU.d4": apiTestClientName}
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			id := strings.TrimPrefix(r.URL.Path, "/v1/me/api/oauth2/client")
			switch {
			case id == "":
				fmt.Fprint(rw, `["EU.a1","EU.b2","EU.c3","EU.gone","EU.d4"]`)
			case names[id[1:]] != "":
				fmt.Fprintf(rw, `{"clientId":%q,"name":%q,"identity":"SERVERTEXT"}`, id[1:], names[id[1:]])
			default:
				rw.WriteHeader(http.StatusNotFound)
			}
		}
		got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListClients, Name: apiTestClientName})
		if err != nil || got.Status != http.StatusOK {
			t.Fatalf("Do = %+v, %v", got, err)
		}
		var ids []string
		for _, c := range got.Clients {
			ids = append(ids, c.ID)
			if c.Identity != "" {
				t.Errorf("client %s keeps identity %q, which is not identity-shaped", c.ID, c.Identity)
			}
		}
		if !slices.Equal(ids, []string{"EU.a1", "EU.d4"}) {
			t.Errorf("kept %v, want the two of the exact name (a client deleted since the listing skipped)", ids)
		}
	})

	t.Run("answers of another shape", func(t *testing.T) {
		for name, tc := range map[string]struct {
			call   APICall
			answer func(rw http.ResponseWriter, r *http.Request)
		}{
			"listing not an array": {APICall{Op: APIListClients, Name: apiTestClientName}, func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, `{"SERVERTEXT":1}`) }},
			"listed id a path": {APICall{Op: APIListClients, Name: apiTestClientName}, func(rw http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/me/api/oauth2/client" {
					fmt.Fprint(rw, `["../../SERVERTEXT"]`)
					return
				}
				t.Errorf("a request for a listed id that is not a client id: %s", r.URL.Path)
			}},
			"client of another id": {APICall{Op: APIGetClient, ID: "EU.a1"}, func(rw http.ResponseWriter, r *http.Request) {
				fmt.Fprint(rw, `{"clientId":"SERVERTEXT","name":"n"}`)
			}},
			"created without a secret": {APICall{Op: APICreateClient, Name: apiTestClientName}, func(rw http.ResponseWriter, r *http.Request) {
				fmt.Fprint(rw, `{"clientId":"EU.abc123","note":"SERVERTEXT"}`)
			}},
			"created with an id not an id": {APICall{Op: APICreateClient, Name: apiTestClientName}, func(rw http.ResponseWriter, r *http.Request) {
				fmt.Fprint(rw, `{"clientId":"../SERVERTEXT","clientSecret":"`+apiTestSecret+`"}`)
			}},
		} {
			t.Run(name, func(t *testing.T) {
				f := newFakeCheckAPI(t)
				f.answer = tc.answer
				got, err := newTestAPIChecker(t, f).Do(context.Background(), tc.call)
				if err == nil {
					t.Fatalf("Do = %+v, want an error", got)
				}
				if got.Secret != "" {
					t.Errorf("a refused answer kept a secret")
				}
				apiNoLeak(t, err.Error())
			})
		}
	})

	t.Run("refusal echoing the token", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(rw, `{"message":"SERVERTEXT %s"}`, apiTestToken)
		}
		for _, call := range []APICall{{Op: APIGetProject, Project: apiTestProject}, {Op: APIDeleteClient, ID: "EU.a1"}, {Op: APIListClients, Name: apiTestClientName}, {Op: APICreateClient, Name: apiTestClientName}} {
			got, err := newTestAPIChecker(t, f).Do(context.Background(), call)
			if err != nil || got.Status != http.StatusForbidden || len(got.Clients) != 0 || got.Secret != "" {
				t.Errorf("%s: Do = %+v, %v; want the 403 as the answer and nothing else", call.Op, got, err)
			}
		}
	})
}

// TestAPICheckerExpiry: the token answer's expires_in is given only as a JSON integer.
func TestAPICheckerExpiry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		field string
		want  TokenExpiry
	}{
		{`,"expires_in":3599`, TokenExpiry{3599, true}},
		{`,"expires_in":-5`, TokenExpiry{-5, true}},
		{`,"expires_in":0`, TokenExpiry{0, true}},
		{``, TokenExpiry{}},
		{`,"expires_in":null`, TokenExpiry{}},
		{`,"expires_in":"3599"`, TokenExpiry{}},
		{`,"expires_in":3599.5`, TokenExpiry{}},
		{`,"expires_in":99999999999999999999`, TokenExpiry{}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(rw, `{"access_token":%q%s}`, apiTestToken, tc.field)
		}))
		c, got, err := NewAPICheckerExpiry(context.Background(), API{TokenURL: srv.URL + "/token", BaseURL: srv.URL + "/v1"}, Credential{ClientID: apiTestClient, ClientSecret: apiTestSecret})
		srv.Close()
		if err != nil || c == nil || got != tc.want {
			t.Errorf("answer%s: expiry %+v (err %v), want %+v", tc.field, got, err, tc.want)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusUnauthorized) }))
	defer srv.Close()
	if _, _, err := NewAPICheckerExpiry(context.Background(), API{TokenURL: srv.URL + "/token"}, Credential{ClientID: apiTestClient, ClientSecret: apiTestSecret}); err == nil {
		t.Error("a refused token request gave no error")
	} else {
		apiNoLeak(t, err.Error())
	}
}
