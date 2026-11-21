package live

import (
	"context"
	"slices"
	"testing"
	"time"
)

// Spec 007 FR-016 (T040): the leftover check excludes a listed bucket as retained only when a
// retained state holds it by the id the inventory uses, <project>/<region>/<name> (FR-013), so a
// bucket of the retained bucket's name in another project or region is judged like any other. A
// child (S3 credential, S3 policy, subnet) is retained exactly when its parent is, whatever its
// own id. The fixtures are the idWorld fake listings of leftovers_ids_test.go; the retained ids
// are given as the chain's state read hands them to the check (chain_test.go covers that read).

// scopeBucket is the prefix-matching bucket name every listing of case (1) uses.
const scopeBucket = "lz2-demo-bkt-state"

// scopeRetained is case (1)'s retained bucket: scopeBucket of the run's project in GRA.
var scopeRetained = map[string][]string{tStorage: {idProject + "/GRA/" + scopeBucket}}

// retainedCheck runs the leftover check over the world's projects with the retained ids,
// failing on a listing error.
func (w *idWorld) retainedCheck(retained map[string][]string, projects ...string) Report {
	w.t.Helper()
	c := LeftoverCheck{Lister: &fakeLister{responses: w.resp}, Prefix: "lz2-", RunID: idRun, Backoff: []time.Duration{}, Retained: retained}
	for _, p := range projects {
		c.Projects = append(c.Projects, Project{ID: p, URN: idURN(p)})
	}
	rep := c.Check(context.Background(), nil)
	if len(rep.Errors) > 0 {
		w.t.Fatalf("listing errors: %v", rep.Errors)
	}
	return rep
}

// scopeOne lists scopeBucket once, in project and region, with case (1)'s retained bucket, and
// returns the report's leftover keys.
func scopeOne(t *testing.T, project, region string) []string {
	t.Helper()
	w := newIDWorld(t, idProject, idOther)
	w.bucket(project, region, scopeBucket)
	rep := w.retainedCheck(scopeRetained, idProject, idOther)
	if got := len(rep.Listed[tStorage]); got != 1 {
		t.Fatalf("listed %d buckets, want 1 (fixture)", got)
	}
	return leftoverKeys(rep.Leftovers)
}

func wantKeys(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: leftovers %v, want exactly %v (FR-016)", what, got, want)
	}
}

// Case (1), one report: the three buckets of one name listed at once; only the retained one's
// project and region (A/GRA) is excluded, so two leftovers of that id remain.
func TestRetainedScopeBucketOneReport(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject, idOther)
	w.bucket(idProject, "GRA", scopeBucket)
	w.bucket(idOther, "GRA", scopeBucket)
	w.bucket(idProject, "SBG", scopeBucket)
	rep := w.retainedCheck(scopeRetained, idProject, idOther)
	if got := len(rep.Listed[tStorage]); got != 3 {
		t.Fatalf("listed %d buckets, want 3 (fixture)", got)
	}
	bucket := tStorage + " " + scopeBucket
	wantKeys(t, "A/GRA, B/GRA and A/SBG listed, A/GRA retained", leftoverKeys(rep.Leftovers), []string{bucket, bucket})
}

// Case (1), A/GRA alone: the retained bucket itself is excluded.
func TestRetainedScopeBucketSameProjectRegion(t *testing.T) {
	t.Parallel()
	wantKeys(t, "A/GRA listed, A/GRA retained", scopeOne(t, idProject, "GRA"), nil)
}

// Case (1), B/GRA alone: another project's bucket of the retained name is a leftover.
func TestRetainedScopeBucketOtherProject(t *testing.T) {
	t.Parallel()
	wantKeys(t, "B/GRA listed, A/GRA retained", scopeOne(t, idOther, "GRA"), []string{tStorage + " " + scopeBucket})
}

// Case (1), A/SBG alone: another region's bucket of the retained name is a leftover.
func TestRetainedScopeBucketOtherRegion(t *testing.T) {
	t.Parallel()
	wantKeys(t, "A/SBG listed, A/GRA retained", scopeOne(t, idProject, "SBG"), []string{tStorage + " " + scopeBucket})
}

// Case (2), first half: a retained user excludes its S3 credential and its S3 policy (holds
// before FR-016).
func TestRetainedScopeUserExcludesChildren(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject)
	w.user(idProject, "4841", "lz2-retained-s3", true, "c0000000000000000000000000004841")
	rep := w.retainedCheck(map[string][]string{tUser: {"4841"}}, idProject)
	wantKeys(t, "retained user 4841 with a credential and a policy", leftoverKeys(rep.Leftovers), nil)
}

// Case (2), second half: an S3 policy whose own composed id <project>/<user-id> is retained but
// whose user is not is still a leftover, with its user and credential (a child follows its
// parent; holds before FR-016).
func TestRetainedScopePolicyFollowsUser(t *testing.T) {
	t.Parallel()
	w := newIDWorld(t, idProject)
	w.user(idProject, "4842", "lz2-other-s3", true, "c0000000000000000000000000004842")
	rep := w.retainedCheck(map[string][]string{tS3Pol: {idProject + "/4842"}}, idProject)
	wantKeys(t, "S3 policy "+idProject+"/4842 retained, user 4842 not", leftoverKeys(rep.Leftovers),
		[]string{tUser + " 4842", tCred + " c0000000000000000000000000004842", tS3Pol + " 4842"})
}
