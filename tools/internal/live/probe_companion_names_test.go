package live

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestCompanionRefusalPrintable (T016 review r2): the refusal names the published name, but a name
// with a line break or a terminal escape is shown quoted, so a root cannot forge lines in lz-live's
// output through it; a printable name is shown as it is (data-model: COMPANION_NAME_REFUSED <name>).
func TestCompanionRefusalPrintable(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"TF_VAR_x\nLZ-LIVE done", "FOO\r", "TF_VAR_x\x1b[2K", "BAR\x00", "TF_VAR_x y"} {
		err := admitCompanionName(name)
		if err == nil {
			t.Errorf("admitCompanionName(%q) = nil, want a refusal", name)
			continue
		}
		if strings.ContainsAny(err.Error(), "\n\r\x1b\x00 ") {
			t.Errorf("admitCompanionName(%q) = %q: the refusal carries the name's control characters unquoted", name, err.Error())
		}
		if want := "COMPANION_NAME_REFUSED " + strconv.Quote(name); err.Error() != want {
			t.Errorf("admitCompanionName(%q) = %q, want %q", name, err.Error(), want)
		}
	}
}

// TestCompanionMalformedNames (T016 review r1, FR-006): a published name that is no variable name
// at all is on no allow-list either. It is refused like any other unlisted name, with
// COMPANION_NAME_REFUSED <name>, before anything is written, bound or run; on `--cleanup` the
// refusal depends on the published values alone, so the root is destroyed, not kept.
func TestCompanionMalformedNames(t *testing.T) {
	for _, name := range []string{"TF_VAR_a-b", "TF_VAR_a b", "9TF_VAR_x", "TF_VAR_x=y"} {
		check := func(t *testing.T, err error, calls []tofuCall, binds []bindCall) {
			t.Helper()
			if ExitCode(err) == 0 {
				t.Errorf("the run exited 0: %q, a name on no allow-list, was published and not refused", name)
			}
			if comp := companionChildren(calls); len(comp) > 0 {
				t.Errorf("the companion ran %d tofu calls although the root published %q", len(comp), name)
			}
			if got := sequence(calls, "destroy"); !slices.Equal(got, []string{"p"}) {
				t.Errorf("destroys %v, want [p]: the root is destroyed after the refusal", got)
			}
			if len(binds) > 0 {
				t.Errorf("%d published identities were bound although the root published %q", len(binds), name)
			}
			if err != nil && strings.Contains(err.Error(), pairReservedValue) {
				t.Error("the refusal's error holds the published value")
			}
			namesRefusal(t, name, err)
		}
		t.Run("run/"+name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds []bindCall
			p, _ := w.pairingProbe(t, root, map[string]string{name: pairReservedValue}, &binds)
			err := p.Start(context.Background())
			check(t, err, w.calls(t, "tofu.log"), binds)
			checkNoPairSecret(t, w, root)
		})
		t.Run("cleanup/"+name, func(t *testing.T) {
			withUmask(t)
			root := tempPrivate(t)
			w := newRunWorld(t)
			var binds []bindCall
			p, _ := w.pairingProbe(t, root, map[string]string{name: pairReservedValue}, &binds)
			st := w.scenario.Stacks["p"]
			st.DestroyExit = 1
			w.scenario.Stacks["p"] = st
			w.save(t)
			if ExitCode(p.Start(context.Background())) == 0 {
				t.Fatal("a probe whose root destroy failed exited 0")
			}
			st.DestroyExit = 0
			w.scenario.Stacks["p"] = st
			w.save(t)
			before := len(w.calls(t, "tofu.log"))
			binds = binds[:0]
			err := p.Cleanup(context.Background())
			check(t, err, w.calls(t, "tofu.log")[before:], binds)
			checkNoPairSecret(t, w, root)
		})
	}
}
