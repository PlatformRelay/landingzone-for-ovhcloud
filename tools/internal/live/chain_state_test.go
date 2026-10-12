package live

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestChainStateObjectCheck (T102, T044 gap 2): after each stack's apply, before the next one, the
// chain reads that stack's state object (the manifest's StateBucket/StateKey) with the stack's own
// S3 keys and fails the stage unless it holds an encryption envelope. A plaintext state, a
// missing object (404) and a refused read (403) each fail the stack whose object it is: nothing
// after it is applied, the destroy-on-exit runs, the error names the key, and nothing of the body
// reaches the error, the terminal, the process streams or the run directory.
func TestChainStateObjectCheck(t *testing.T) {
	const marker = "PLAINTEXT-STATE-MARKER-7f3a"
	plaintext := `{"version":4,"serial":3,"resources":[{"type":"ovh_cloud_project_user","instances":[{"attributes":{"password":"` + marker + `"}}]}]}`
	const failing = "demo-dev-gra11-network"
	cases := []struct {
		name   string
		answer *chainStateAnswer
		reason string // in the error
	}{
		{name: "envelope"},
		{name: "plaintext", answer: &chainStateAnswer{status: http.StatusOK, body: plaintext}, reason: "is not encrypted: plaintext state"},
		{name: "missing", answer: &chainStateAnswer{status: http.StatusNotFound}, reason: "is missing after the apply (404"},
		{name: "refused", answer: &chainStateAnswer{status: http.StatusForbidden}, reason: "the read was refused (403"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newChainHarness(t)
			c := chainExec{}
			applied := laneOrder
			if tc.answer != nil {
				c.stateObjects = map[string]chainStateAnswer{failing: *tc.answer}
				applied = laneOrder[:slices.Index(laneOrder, failing)+1]
			}
			r := h.chain(c)
			var forward []string
			for _, call := range r.tofu() {
				if call.Cmd == "apply" && !call.Destroy {
					forward = append(forward, call.Stack)
				}
			}
			if !slices.Equal(forward, applied) {
				t.Errorf("applied %v, want %v", forward, applied)
			}
			// One read per applied stack, of its own state key with its own S3 keys, between its
			// apply and the next stack's.
			var reads, want []string
			for _, call := range r.calls {
				if call.Cmd == "s3get" {
					reads = append(reads, call.Stack+" "+call.Key)
				}
			}
			for _, id := range applied {
				in := h.row(id)
				want = append(want, in.StateBucket+"/"+in.StateKey+" "+laneCreds[laneAuthority[id]]["AWS_ACCESS_KEY_ID"])
			}
			if !slices.Equal(reads, want) {
				t.Errorf("state-object reads %v, want %v", reads, want)
			}
			for i, id := range applied {
				in := h.row(id)
				ap := r.index(false, func(call laneCall) bool { return call.Cmd == "apply" && !call.Destroy && call.Stack == id })
				rd := r.index(false, func(call laneCall) bool { return call.Cmd == "s3get" && call.Stack == in.StateBucket+"/"+in.StateKey })
				if ap < 0 || rd < ap {
					t.Errorf("%s: state object read at %d, apply at %d: want the read after the apply", id, rd, ap)
				}
				if i+1 < len(applied) {
					if next := r.index(false, func(call laneCall) bool { return call.Cmd == "apply" && !call.Destroy && call.Stack == applied[i+1] }); next < rd {
						t.Errorf("%s: state object read at %d after the next apply at %d", id, rd, next)
					}
				}
			}
			if tc.answer == nil {
				laneExit(t, r.laneRun, 0)
				return
			}
			laneExit(t, r.laneRun, 1)
			in := h.row(failing)
			if r.err == nil || !strings.Contains(r.err.Error(), in.StateBucket+"/"+in.StateKey) || !strings.Contains(r.err.Error(), tc.reason) {
				t.Errorf("err %v, want it to name %s/%s and say %q", r.err, in.StateBucket, in.StateKey, tc.reason)
			}
			if term := h.term.String(); !strings.Contains(term, "LZ-LIVE chain "+failing+" fail") {
				t.Errorf("terminal does not report the stage failed:\n%s", term)
			}
			chainDestroyedAfter(t, h, r, []string{failing}, "fail")
			var leaked []string
			if strings.Contains(fmt.Sprint(r.err), marker) {
				leaked = append(leaked, "error")
			}
			if strings.Contains(h.term.String(), marker) {
				leaked = append(leaked, "terminal")
			}
			if strings.Contains(r.process, marker) {
				leaked = append(leaked, "process streams")
			}
			_ = filepath.WalkDir(r.runDir, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					if raw, rerr := os.ReadFile(p); rerr == nil && strings.Contains(string(raw), marker) {
						leaked = append(leaked, p)
					}
				}
				return nil
			})
			if len(leaked) > 0 {
				t.Errorf("the state object's body reached %v", leaked)
			}
		})
	}
}
