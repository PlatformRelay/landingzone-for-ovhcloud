package stacks

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Order and selection controls of 005 T040 (FR-009, FR-013; research R21; contracts/checks.md
// V006; ADR-0004, ADR-0007).
//
// The run order is compared with the pinned Terramate's `list --run-order`, run during the test on
// a copy of this repository's stacks/ tree and on fixture roots the reconciler (T036) created, and
// checked against every derived data and authority edge between rows. Selection is driven through
// Select with digests the tests make: CodeDigest of real trees (a scratch copy of this
// repository's stacks/, stages/, components/ and modules/, or generated fixture roots), the
// resolved-reference digests Adapt writes (T061), and records built from those as a last apply
// would have left them.
//
// Readings this file pins (evidence/T040.md): run order = topological levels over data and
// authority edges, ties by stack path (what the pinned Terramate prints); the code digest is the
// stack directory plus its local module closure; a consumer of a producer without an artefact
// stays selected and lists that producer in Blocked.

// selDigest is a stand-in sha256 for a named artefact or digest.
func selDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

const (
	selStateID   = "11111111111111111111111111111111"
	selDemoID    = "22222222222222222222222222222222"
	selOpsID     = "33333333333333333333333333333333"
	selChangedID = "44444444444444444444444444444444"
)

// selManifestData is the sandbox fixture with distinct state and environment refs (STATE,
// DEMO_DEV) and, with tenant set, a second tenant (environment dev, ref <TENANT>_DEV) with its
// tenant-state row and, with project, its project row.
func selManifestData(t *testing.T, tenant string, project bool) []byte {
	t.Helper()
	data := edit(t, manifestOf(t, "sandbox"), `"project": {"ref": "SANDBOX"}`, `"project": {"ref": "STATE"}`, 1)
	data = edit(t, data, `"project": {"mode": "reference", "ref": "SANDBOX"}`, `"project": {"mode": "reference", "ref": "DEMO_DEV"}`, 1)
	if tenant == "" {
		return data
	}
	ref := strings.ToUpper(tenant) + "_DEV"
	data = edit(t, data, "      }\n    ],\n    \"instances\": [",
		"      },\n      {\"name\": \""+tenant+"\", \"environments\": [{\"name\": \"dev\", \"project\": {\"mode\": \"reference\", \"ref\": \""+ref+"\"}, "+
			"\"regions\": [{\"name\": \"GRA11\", \"network\": {\"cidr\": \"10.30.0.0/24\", \"vlan_id\": 1}}], "+
			"\"budget_alert\": {\"enabled\": false}, \"quota_guard\": {\"enabled\": false}}]}\n    ],\n    \"instances\": [", 1)
	rows := `,
      {"id": "` + tenant + `-state", "stage": "tenant-state", "tenant": "` + tenant + `"}`
	if project {
		rows += `,
      {"id": "` + tenant + `-dev-project", "stage": "project", "tenant": "` + tenant + `", "environment": "dev"}`
	}
	runtimeRow := `{"id": "demo-dev-gra11-runtime", "stage": "runtime", "tenant": "demo", "environment": "dev", "region": "GRA11"}`
	return edit(t, data, runtimeRow, runtimeRow+rows, 1)
}

func selAccount(demo string) BoundAccount {
	return BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"STATE": selStateID, "DEMO_DEV": demo, "OPS_DEV": selOpsID}}
}

// selResolved is the resolved-reference digest of every row that takes one, as Adapt records it.
// Rows with a data edge (project-network, runtime) take none (data-model *Resolved-reference
// input*) and would need producer artefacts, so they are left out.
func selResolved(t *testing.T, m *Manifest, acct BoundAccount) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, in := range m.Instances {
		if slices.ContainsFunc(in.Edges, func(e Edge) bool { return e.Kind == EdgeData }) {
			continue
		}
		got, err := Adapt(AdaptOptions{Manifest: m, Consumer: in.ID, Account: acct, Dir: filepath.Join(t.TempDir(), "inputs")})
		if err != nil {
			t.Fatalf("adapt %s: %v", in.ID, err)
		}
		if got.Resolved == "" {
			t.Fatalf("adapt %s: no resolved-reference digest", in.ID)
		}
		out[in.ID] = got.Resolved
	}
	return out
}

// selArtifacts is a published artefact digest for every row.
func selArtifacts(m *Manifest) map[string]string {
	out := map[string]string{}
	for _, in := range m.Instances {
		out[in.ID] = selDigest("outputs.json of " + in.ID)
	}
	return out
}

// selCodes is a stand-in code digest for every row.
func selCodes(m *Manifest) map[string]string {
	out := map[string]string{}
	for _, in := range m.Instances {
		out[in.ID] = selDigest("code of " + in.ID)
	}
	return out
}

// selRecords is the record a last apply with these digests left for every row.
func selRecords(m *Manifest, code, resolved, artifacts map[string]string) map[string]Record {
	out := map[string]Record{}
	for _, in := range m.Instances {
		r := Record{CodeDigest: code[in.ID], Resolved: resolved[in.ID], Consumed: map[string]string{}}
		for _, e := range in.Edges {
			if e.Kind == EdgeData {
				r.Consumed[e.Producer] = artifacts[e.Producer]
			}
		}
		out[in.ID] = r
	}
	return out
}

func selIDs(sel []Selected) []string {
	out := make([]string, 0, len(sel))
	for _, s := range sel {
		out = append(out, s.ID)
	}
	return out
}

func selFind(sel []Selected, id string) (Selected, bool) {
	for _, s := range sel {
		if s.ID == id {
			return s, true
		}
	}
	return Selected{}, false
}

func runSelect(t *testing.T, opts SelectOptions) []Selected {
	t.Helper()
	sel, err := Select(opts)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	return sel
}

// wantSelected asserts the selected set exactly (as a set), each stack's reasons containing the
// given ones, and the selected stacks in run order.
func wantSelected(t *testing.T, m *Manifest, sel []Selected, want map[string][]Reason) {
	t.Helper()
	got := selIDs(sel)
	var wantIDs []string
	for id := range want {
		wantIDs = append(wantIDs, id)
	}
	if !sameSet(got, wantIDs) {
		t.Fatalf("selected %q, want %q", got, sortedCopy(wantIDs))
	}
	for id, reasons := range want {
		s, _ := selFind(sel, id)
		for _, r := range reasons {
			if !slices.Contains(s.Reasons, r) {
				t.Errorf("%s: reasons %+v, want one %+v", id, s.Reasons, r)
			}
		}
		if len(s.Reasons) == 0 {
			t.Errorf("%s: selected without a reason", id)
		}
	}
	order, err := Order(m)
	if err != nil {
		t.Fatal(err)
	}
	var inOrder []string
	for _, id := range order {
		if slices.Contains(got, id) {
			inOrder = append(inOrder, id)
		}
	}
	if !slices.Equal(got, inOrder) {
		t.Errorf("selected in order %q, want run order %q", got, inOrder)
	}
}

// edgesOrdered asserts every edge between rows has its producer earlier in order.
func edgesOrdered(t *testing.T, m *Manifest, order []string) {
	t.Helper()
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if len(pos) != len(m.Instances) || len(order) != len(m.Instances) {
		t.Fatalf("order %q is not each of the %d rows once", order, len(m.Instances))
	}
	for _, in := range m.Instances {
		for _, e := range in.Edges {
			p, ok := pos[e.Producer]
			if !ok {
				continue // an external producer is not a stack of this repository
			}
			if p >= pos[in.ID] {
				t.Errorf("%s edge %s → %s: producer at %d, consumer at %d", e.Kind, e.Producer, in.ID, p, pos[in.ID])
			}
		}
	}
}

// runOrder is the pinned Terramate's run order of the stacks under root, as instance ids.
func runOrder(t *testing.T, m *Manifest, root string) []string {
	t.Helper()
	byPath := map[string]string{}
	for _, in := range m.Instances {
		byPath[in.Path] = in.ID
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(terramate(t, root, "list", "--run-order")), "\n") {
		id, ok := byPath[strings.TrimSpace(line)]
		if !ok {
			t.Fatalf("terramate lists %q, no row of the manifest", line)
		}
		ids = append(ids, id)
	}
	return ids
}

func mustOrder(t *testing.T, m *Manifest) []string {
	t.Helper()
	order, err := Order(m)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	return order
}

// The run order of the real six stacks, and of the fixtures, is the pinned Terramate's
// `list --run-order` and orders every data and authority edge between rows producer first.
func TestSelectionOrder(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		m := decoded(t, readFile(t, filepath.Join(repositoryRoot, ManifestPath)))
		got := mustOrder(t, m)
		want := []string{"account-bootstrap", "account-governance", "demo-state", "demo-dev-project", "demo-dev-gra11-network", "demo-dev-gra11-runtime"}
		if !slices.Equal(got, want) {
			t.Errorf("order %q, want %q", got, want)
		}
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "terramate.tm.hcl"), readFile(t, filepath.Join(repositoryRoot, "terramate.tm.hcl")))
		copyTree(t, filepath.Join(repositoryRoot, "stacks"), filepath.Join(root, "stacks"))
		if tm := runOrder(t, m, root); !slices.Equal(got, tm) {
			t.Errorf("order %q, terramate list --run-order %q", got, tm)
		}
		edgesOrdered(t, m, got)
	})
	for _, fixture := range []string{"sandbox", "growth", "tenant-only"} {
		t.Run(fixture, func(t *testing.T) {
			data := manifestOf(t, fixture)
			m := decoded(t, data)
			root := generationRoot(t, data)
			if _, err := Reconcile(ReconcileOptions{Root: root, Terramate: pinnedTerramate(t)}); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			got := mustOrder(t, m)
			if tm := runOrder(t, m, root); !slices.Equal(got, tm) {
				t.Errorf("order %q, terramate list --run-order %q", got, tm)
			}
			edgesOrdered(t, m, got)
		})
	}
	// An authority edge alone orders: project only runs under account-governance's principal.
	m := decoded(t, manifestOf(t, "sandbox"))
	order := mustOrder(t, m)
	if slices.Index(order, "account-governance") > slices.Index(order, "demo-dev-project") {
		t.Errorf("order %q: account-governance (authority producer) after demo-dev-project", order)
	}
}

// The order is the same over repeated runs and whatever the row order of the manifest.
func TestSelectionOrderDeterministic(t *testing.T) {
	m := decoded(t, manifestOf(t, "growth"))
	want := mustOrder(t, m)
	for i := 0; i < 20; i++ {
		if got := mustOrder(t, m); !slices.Equal(got, want) {
			t.Fatalf("run %d: order %q, want %q", i, got, want)
		}
	}
	for seed := int64(1); seed <= 10; seed++ {
		shuffled := *m
		shuffled.Instances = slices.Clone(m.Instances)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled.Instances), func(i, j int) {
			shuffled.Instances[i], shuffled.Instances[j] = shuffled.Instances[j], shuffled.Instances[i]
		})
		if got := mustOrder(t, &shuffled); !slices.Equal(got, want) {
			t.Errorf("rows shuffled (seed %d): order %q, want %q", seed, got, want)
		}
	}
}

// chainManifest is a hand-built graph with a three-stack data chain a → b → c, a stack d under
// a's authority only, and an unrelated stack e: the stage table has no data chain of three. The
// path of e sorts before a's while its id sorts after, so a tie broken by id is seen.
func chainManifest() *Manifest {
	row := func(id string, edges ...Edge) Instance {
		path := "stacks/chain/" + id
		if id == "e" {
			path = "stacks/chain/0-e"
		}
		return Instance{ID: id, Stage: "runtime", Path: path, Edges: edges}
	}
	return &Manifest{Name: "chain", Instances: []Instance{
		row("a"),
		row("b", Edge{Producer: "a", Kind: EdgeData}),
		row("c", Edge{Producer: "b", Kind: EdgeData}),
		row("d", Edge{Producer: "a", Kind: EdgeAuthority}),
		row("e"),
	}}
}

// Selection over the chain: what a code, digest, record or artefact change selects.
func TestSelectionTransitive(t *testing.T) {
	m := chainManifest()
	code, artifacts := selCodes(m), selArtifacts(m)
	resolved := map[string]string{"a": selDigest("resolved a")}
	records := selRecords(m, code, resolved, artifacts)
	base := func() SelectOptions {
		return SelectOptions{Manifest: m, Code: maps.Clone(code), Resolved: maps.Clone(resolved), Artifacts: maps.Clone(artifacts), Records: maps.Clone(records)}
	}
	if order := mustOrder(t, m); !slices.Equal(order, []string{"e", "a", "b", "d", "c"}) {
		t.Errorf("chain order %q, want levels, ties by path: [e a b d c]", order)
	}
	t.Run("unchanged", func(t *testing.T) {
		wantSelected(t, m, runSelect(t, base()), map[string][]Reason{})
	})
	t.Run("upstream", func(t *testing.T) {
		opts := base()
		opts.Code["a"] = selDigest("new code of a")
		wantSelected(t, m, runSelect(t, opts), map[string][]Reason{
			"a": {{Kind: ReasonCode}}, "b": {{Kind: ReasonConsumer, From: "a"}}, "c": {{Kind: ReasonConsumer, From: "b"}}})
	})
	t.Run("intermediate", func(t *testing.T) {
		opts := base()
		opts.Code["b"] = selDigest("new code of b")
		wantSelected(t, m, runSelect(t, opts), map[string][]Reason{
			"b": {{Kind: ReasonCode}}, "c": {{Kind: ReasonConsumer, From: "b"}}})
	})
	t.Run("leaf", func(t *testing.T) {
		opts := base()
		opts.Code["c"] = selDigest("new code of c")
		wantSelected(t, m, runSelect(t, opts), map[string][]Reason{"c": {{Kind: ReasonCode}}})
	})
	t.Run("producer-digest", func(t *testing.T) {
		opts := base()
		opts.Artifacts["a"] = selDigest("republished outputs.json of a")
		wantSelected(t, m, runSelect(t, opts), map[string][]Reason{
			"b": {{Kind: ReasonInput, From: "a"}}, "c": {{Kind: ReasonConsumer, From: "b"}}})
	})
	t.Run("resolved", func(t *testing.T) {
		opts := base()
		opts.Resolved["a"] = selDigest("new resolved a")
		wantSelected(t, m, runSelect(t, opts), map[string][]Reason{
			"a": {{Kind: ReasonResolved}}, "b": {{Kind: ReasonConsumer, From: "a"}}, "c": {{Kind: ReasonConsumer, From: "b"}}})
	})
	t.Run("authority", func(t *testing.T) {
		// d runs under a's authority only: a's code change orders d after a but never selects it,
		// nor does a's republished artefact.
		opts := base()
		opts.Code["a"] = selDigest("new code of a")
		opts.Artifacts["a"] = selDigest("republished outputs.json of a")
		if _, ok := selFind(runSelect(t, opts), "d"); ok {
			t.Errorf("d (authority consumer of a) selected")
		}
	})
	t.Run("no-record", func(t *testing.T) {
		opts := base()
		delete(opts.Records, "b")
		wantSelected(t, m, runSelect(t, opts), map[string][]Reason{
			"b": {{Kind: ReasonNoRecord}}, "c": {{Kind: ReasonConsumer, From: "b"}}})
	})
	t.Run("no-record-leaf", func(t *testing.T) {
		opts := base()
		delete(opts.Records, "e")
		wantSelected(t, m, runSelect(t, opts), map[string][]Reason{"e": {{Kind: ReasonNoRecord}}})
	})
	t.Run("blocked", func(t *testing.T) {
		// a has published nothing: b is selected and blocked on a; c, whose producer b has
		// published, is selected as b's consumer and not blocked.
		opts := base()
		delete(opts.Artifacts, "a")
		sel := runSelect(t, opts)
		wantSelected(t, m, sel, map[string][]Reason{"b": {{Kind: ReasonInput, From: "a"}}, "c": {{Kind: ReasonConsumer, From: "b"}}})
		if b, _ := selFind(sel, "b"); !slices.Equal(b.Blocked, []string{"a"}) {
			t.Errorf("b blocked on %q, want [a]", b.Blocked)
		}
		if c, _ := selFind(sel, "c"); len(c.Blocked) != 0 {
			t.Errorf("c blocked on %q, want none", c.Blocked)
		}
	})
	t.Run("blocked-first-run", func(t *testing.T) {
		// Nothing applied yet: every stack is selected, and each data consumer is blocked.
		opts := base()
		opts.Records, opts.Artifacts = map[string]Record{}, map[string]string{}
		sel := runSelect(t, opts)
		wantSelected(t, m, sel, map[string][]Reason{"a": {{Kind: ReasonNoRecord}}, "b": {{Kind: ReasonNoRecord}},
			"c": {{Kind: ReasonNoRecord}}, "d": {{Kind: ReasonNoRecord}}, "e": {{Kind: ReasonNoRecord}}})
		for id, want := range map[string][]string{"a": nil, "b": {"a"}, "c": {"b"}, "d": nil, "e": nil} {
			if s, _ := selFind(sel, id); !slices.Equal(s.Blocked, want) {
				t.Errorf("%s blocked on %q, want %q", id, s.Blocked, want)
			}
		}
	})
	t.Run("missing-code", func(t *testing.T) {
		opts := base()
		delete(opts.Code, "c")
		if _, err := Select(opts); err == nil {
			t.Errorf("a row without a current code digest was selected over")
		}
	})
}

// codeRoot is a scratch copy of this repository's generated stacks and the stages, components
// and modules they call.
func codeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"stacks", "stages", "components", "modules"} {
		copyTree(t, filepath.Join(repositoryRoot, dir), filepath.Join(root, dir))
	}
	return root
}

func codeDigests(t *testing.T, root string, m *Manifest) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, in := range m.Instances {
		d, err := CodeDigest(root, in)
		if err != nil {
			t.Fatalf("code digest of %s: %v", in.ID, err)
		}
		if len(d) != 64 || strings.Trim(d, "0123456789abcdef") != "" {
			t.Fatalf("code digest of %s = %q, want sha256 hex", in.ID, d)
		}
		out[in.ID] = d
	}
	return out
}

// appendTo appends a comment line to the file at rel under root.
func appendTo(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	data := readFile(t, path)
	writeFile(t, path, append(data, []byte("\n# changed by T040\n")...))
}

// The code digest covers a stack's directory and the stages, components and modules it calls,
// and nothing else.
func TestSelectionCodeDigest(t *testing.T) {
	m := decoded(t, readFile(t, filepath.Join(repositoryRoot, ManifestPath)))
	root := codeRoot(t)
	base := codeDigests(t, root, m)
	if again := codeDigests(t, root, m); !maps.Equal(base, again) {
		t.Fatalf("code digests differ between two reads of one tree: %v, %v", base, again)
	}
	seen := map[string]string{}
	for id, d := range base {
		if other, ok := seen[d]; ok {
			t.Errorf("%s and %s have one code digest", id, other)
		}
		seen[d] = id
	}
	all := []string{"account-bootstrap", "account-governance", "demo-state", "demo-dev-project", "demo-dev-gra11-network", "demo-dev-gra11-runtime"}
	for _, c := range []struct {
		name, file string
		changed    []string
	}{
		{"stack-file", "stacks/tenants/demo/dev/gra11/project-network/_lz_main.tf", []string{"demo-dev-gra11-network"}},
		{"stack-test", "stacks/tenants/demo/dev/gra11/runtime/tests/_lz_offline.tftest.hcl", []string{"demo-dev-gra11-runtime"}},
		{"stage-project", "stages/project/main.tf", []string{"demo-dev-project"}},
		{"component-project-factory", "components/project-factory/main.tf", []string{"demo-dev-project"}},
		{"module-cloud-project", "modules/cloud-project/main.tf", []string{"demo-dev-project"}},
		{"module-private-network", "modules/private-network/main.tf", []string{"demo-dev-gra11-network"}},
		{"module-object-storage", "modules/object-storage/main.tf", []string{"demo-dev-gra11-runtime"}},
		{"component-state-backend", "components/state-backend/main.tf", []string{"account-bootstrap", "demo-state"}},
		{"module-iam-policy", "modules/iam-policy/main.tf", []string{"account-governance"}},
		{"module-naming", "modules/naming/main.tf", all},
		{"unreached", "stages/t040-unreached/main.tf", nil},
		{"working-dir", "stacks/tenants/demo/dev/project/.terraform/terraform.tfstate", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := codeRoot(t)
			if _, err := os.Stat(filepath.Join(root, c.file)); err == nil {
				appendTo(t, root, c.file)
			} else {
				writeFile(t, filepath.Join(root, c.file), []byte("# added by T040\n"))
			}
			got := codeDigests(t, root, m)
			var changed []string
			for _, id := range all {
				if got[id] != base[id] {
					changed = append(changed, id)
				}
			}
			if !sameSet(changed, c.changed) {
				t.Errorf("editing %s changed the code digest of %q, want %q", c.file, changed, c.changed)
			}
		})
	}
}

// A code change in the real tree selects through CodeDigest and Select: a producer's stage change
// selects it and its data consumers; a change only an intermediate or leaf stack reaches selects
// it and not its producers; a change only an authority producer reaches selects nothing else.
func TestSelectionCodeChange(t *testing.T) {
	m := decoded(t, readFile(t, filepath.Join(repositoryRoot, ManifestPath)))
	base := codeDigests(t, codeRoot(t), m)
	artifacts := selArtifacts(m)
	resolved := map[string]string{"account-bootstrap": selDigest("r1"), "demo-state": selDigest("r1"), "account-governance": selDigest("r2"), "demo-dev-project": selDigest("r3")}
	records := selRecords(m, base, resolved, artifacts)
	for _, c := range []struct {
		name, file string
		want       map[string][]Reason
	}{
		{"stage-project", "stages/project/main.tf", map[string][]Reason{
			"demo-dev-project":       {{Kind: ReasonCode}},
			"demo-dev-gra11-network": {{Kind: ReasonConsumer, From: "demo-dev-project"}},
			"demo-dev-gra11-runtime": {{Kind: ReasonConsumer, From: "demo-dev-project"}}}},
		{"module-private-network", "modules/private-network/main.tf", map[string][]Reason{
			"demo-dev-gra11-network": {{Kind: ReasonCode}}}},
		{"stage-account-governance", "stages/account-governance/main.tf", map[string][]Reason{
			"account-governance": {{Kind: ReasonCode}}}},
		{"component-state-backend", "components/state-backend/main.tf", map[string][]Reason{
			"account-bootstrap": {{Kind: ReasonCode}}, "demo-state": {{Kind: ReasonCode}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := codeRoot(t)
			appendTo(t, root, c.file)
			sel := runSelect(t, SelectOptions{Manifest: m, Code: codeDigests(t, root, m), Resolved: resolved, Artifacts: artifacts, Records: records})
			wantSelected(t, m, sel, c.want)
		})
	}
}

// A change in one tenant's stacks never selects another tenant's.
func TestSelectionUnrelatedTenant(t *testing.T) {
	m := decoded(t, manifestOf(t, "growth"))
	code, artifacts := selCodes(m), selArtifacts(m)
	records := selRecords(m, code, map[string]string{}, artifacts)
	for _, c := range []struct {
		name, prefix string
		change       func(o *SelectOptions)
		want         []string
	}{
		{"code", "demo-dev-", func(o *SelectOptions) { o.Code["demo-dev-project"] = selDigest("new") },
			[]string{"demo-dev-project", "demo-dev-gra11-network", "demo-dev-sbg5-network", "demo-dev-gra11-runtime-blue", "demo-dev-gra11-runtime-green", "demo-dev-sbg5-runtime"}},
		{"artefact", "shop-prod-", func(o *SelectOptions) { o.Artifacts["shop-prod-project"] = selDigest("new") },
			[]string{"shop-prod-gra11-network", "shop-prod-sbg5-network", "shop-prod-gra11-runtime", "shop-prod-sbg5-runtime"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := SelectOptions{Manifest: m, Code: maps.Clone(code), Resolved: map[string]string{}, Artifacts: maps.Clone(artifacts), Records: records}
			c.change(&opts)
			got := selIDs(runSelect(t, opts))
			if !sameSet(got, c.want) {
				t.Errorf("selected %q, want %q", got, c.want)
			}
			for _, id := range got {
				if !strings.HasPrefix(id, c.prefix) {
					t.Errorf("%s selected by a change under %s", id, c.prefix)
				}
			}
		})
	}
}

// A new tenant row changes account-governance's generated tenants map, so its code digest, and
// adds a tenant-state with no record: those two are selected, the existing tenant's stacks are
// not. Both trees are generated with the pinned Terramate.
func TestSelectionNewTenant(t *testing.T) {
	oldData, newData := selManifestData(t, "", false), selManifestData(t, "ops", false)
	oldM, newM := decoded(t, oldData), decoded(t, newData)
	oldRoot, newRoot := planRoot(t, oldData, false), planRoot(t, newData, false)
	acct := selAccount(selDemoID)
	oldCode, oldResolved, artifacts := codeDigests(t, oldRoot, oldM), selResolved(t, oldM, acct), selArtifacts(oldM)
	records := selRecords(oldM, oldCode, oldResolved, artifacts)
	if tenants := readFile(t, filepath.Join(newRoot, "stacks/account/account-governance/_lz_tenants.auto.tfvars.json")); !strings.Contains(string(tenants), `"ops"`) {
		t.Fatalf("generated tenants map %s lacks the new tenant", tenants)
	}
	sel := runSelect(t, SelectOptions{Manifest: newM, Code: codeDigests(t, newRoot, newM), Resolved: selResolved(t, newM, acct), Artifacts: artifacts, Records: records})
	wantSelected(t, newM, sel, map[string][]Reason{
		"account-governance": {{Kind: ReasonCode}},
		"ops-state":          {{Kind: ReasonNoRecord}}})
	// Unchanged manifest and account: nothing is selected.
	wantSelected(t, oldM, runSelect(t, SelectOptions{Manifest: oldM, Code: codeDigests(t, oldRoot, oldM), Resolved: selResolved(t, oldM, acct), Artifacts: artifacts, Records: records}), map[string][]Reason{})
}

// A changed LZ_PROJECT_ID_<REF> changes the resolved-reference input Adapt writes for
// account-governance and that environment's project, so their recorded digests select them (and
// the project's data consumers); the other tenant's project and the state stacks are not.
func TestSelectionResolvedReference(t *testing.T) {
	m := decoded(t, selManifestData(t, "ops", true))
	code, artifacts := selCodes(m), selArtifacts(m)
	records := selRecords(m, code, selResolved(t, m, selAccount(selDemoID)), artifacts)
	wantSelected(t, m, runSelect(t, SelectOptions{Manifest: m, Code: code, Resolved: selResolved(t, m, selAccount(selDemoID)), Artifacts: artifacts, Records: records}),
		map[string][]Reason{})
	sel := runSelect(t, SelectOptions{Manifest: m, Code: code, Resolved: selResolved(t, m, selAccount(selChangedID)), Artifacts: artifacts, Records: records})
	wantSelected(t, m, sel, map[string][]Reason{
		"account-governance":     {{Kind: ReasonResolved}},
		"demo-dev-project":       {{Kind: ReasonResolved}},
		"demo-dev-gra11-network": {{Kind: ReasonConsumer, From: "demo-dev-project"}},
		"demo-dev-gra11-runtime": {{Kind: ReasonConsumer, From: "demo-dev-project"}}})
}

// A symbolic link in a stack's closure is refused, not skipped: a skipped link would let the file
// it points to change without changing the digest (005 T041).
func TestSelectionCodeDigestRefusesSymlink(t *testing.T) {
	root := t.TempDir()
	stack := filepath.Join(root, "stacks", "x")
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.tf"), []byte("# outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stack, "main.tf"), []byte("# stack\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Instance{ID: "x", Path: "stacks/x"}
	if d, err := CodeDigest(root, in); err != nil || d == "" {
		t.Fatalf("CodeDigest without a link = %q, %v", d, err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.tf"), filepath.Join(stack, "linked.tf")); err != nil {
		t.Fatal(err)
	}
	if d, err := CodeDigest(root, in); err == nil {
		t.Errorf("CodeDigest with a symbolic link = %q, want a refusal", d)
	}
	// A module reached through a linked ancestor directory (generated fixture roots link stages/)
	// is digested by its content: a change behind the link changes the digest (review r2).
	if err := os.Remove(filepath.Join(stack, "linked.tf")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "x", "main.tf"), []byte("# outside module\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stack, "main.tf"), []byte("module \"x\" {\n  source = \"../../modules/x\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := CodeDigest(root, in)
	if err != nil {
		t.Fatalf("CodeDigest through a linked directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "x", "main.tf"), []byte("# outside module v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after, err := CodeDigest(root, in); err != nil || after == before {
		t.Errorf("a change behind a linked directory: digest %q (%v), was %q; want a new digest", after, err, before)
	}
}

// Module sources are read as HCL (005 T041, review r1): a one-line module block is followed, a
// commented-out source is not, and a source that is not a literal string or a JSON configuration
// file is refused rather than left out of the digest.
func TestSelectionCodeDigestModuleSources(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := Instance{ID: "x", Path: "stacks/x"}
	digest := func() string {
		t.Helper()
		d, err := CodeDigest(root, in)
		if err != nil {
			t.Fatalf("CodeDigest: %v", err)
		}
		return d
	}
	write("modules/child/main.tf", "# child v1\n")
	write("modules/commented/main.tf", "# commented v1\n")
	write("stacks/x/main.tf", "module \"child\" { source = \"../../modules/child\" }\n/*\nmodule \"old\" {\n  source = \"../../modules/commented\"\n}\n*/\n")
	before := digest()
	write("modules/child/main.tf", "# child v2\n")
	if digest() == before {
		t.Error("a change in a module called from a one-line block left the digest unchanged")
	}
	before = digest()
	write("modules/commented/main.tf", "# commented v2\n")
	if digest() != before {
		t.Error("a change in a module named only in a comment changed the digest")
	}
	for name, body := range map[string]string{
		"computed source": "variable \"v\" {}\nmodule \"c\" {\n  source = \"../../modules/${var.v}\"\n}\n",
		"no source":       "module \"c\" {\n}\n",
		"unparsable":      "module \"c\" {\n",
	} {
		write("stacks/x/main.tf", body)
		if d, err := CodeDigest(root, in); err == nil {
			t.Errorf("%s: CodeDigest = %q, want a refusal", name, d)
		}
	}
	write("stacks/x/main.tf", "# plain\n")
	write("stacks/x/extra.tf.json", `{"module": {"c": {"source": "../../modules/child"}}}`)
	if d, err := CodeDigest(root, in); err == nil {
		t.Errorf("JSON configuration: CodeDigest = %q, want a refusal", d)
	}
}

// Records are read from <dir>/<id>.json for the manifest's rows (005 T041): a missing directory
// is a first run (no record), a row without a file has none, and a malformed record or one with a
// field the record does not have is refused, never read as missing.
func TestSelectionReadRecords(t *testing.T) {
	m := decoded(t, manifestOf(t, "sandbox"))
	dir := t.TempDir()
	if got, err := ReadRecords(filepath.Join(dir, "absent"), m); err != nil || len(got) != 0 {
		t.Fatalf("ReadRecords(missing dir) = %v, %v; want no records", got, err)
	}
	write := func(id, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("account-bootstrap", `{"applied_at": "2026-10-07T00:00:00Z", "source_revision": "abc", "code_digest": "c1", "consumed": {}}`)
	write("not-a-row", `{"code_digest": "c2", "consumed": {}}`)
	got, err := ReadRecords(dir, m)
	want := map[string]Record{"account-bootstrap": {AppliedAt: "2026-10-07T00:00:00Z", SourceRevision: "abc", CodeDigest: "c1", Consumed: map[string]string{}}}
	if err != nil || len(got) != 1 || got["account-bootstrap"].CodeDigest != "c1" || got["account-bootstrap"].SourceRevision != want["account-bootstrap"].SourceRevision {
		t.Errorf("ReadRecords = %+v, %v; want %+v", got, err, want)
	}
	for name, body := range map[string]string{
		"malformed":     `{"code_digest": `,
		"unknown-field": `{"code_digest": "c1", "consumed": {}, "extra": 1}`,
		"trailing-data": `{"code_digest": "c1", "consumed": {}} garbage`,
		"two-objects":   `{"code_digest": "c1", "consumed": {}} {"code_digest": "c2"}`,
	} {
		write("account-bootstrap", body)
		if got, err := ReadRecords(dir, m); err == nil {
			t.Errorf("%s record read as %+v, want a refusal", name, got)
		}
	}
}
