package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/configure"
)

// Controls of 004 T024 (a) and (b): the command reads today's stacks/deployments.yaml like the
// answers file and never prints a value from it; a refusal the catalogue cannot explain fails as
// a tool defect, apart from the `configure: error:` line of an I/O error.

const s004Secret = "s004-AKIAEXAMPLESECRETVALUE"

// s004CurrentRefusal is the one line of a refused current manifest, the path alone as detail.
const s004CurrentRefusal = "configure: CURRENT_MANIFEST_INVALID: stacks/deployments.yaml. "

// TestCurrentManifestRefusedWithoutValues: for check and export, a current manifest whose
// apiVersion holds a secret-looking value, and a symlink to a valid manifest, each print the one
// CURRENT_MANIFEST_INVALID line naming the path alone, exit 1, the value nowhere in the output and
// nothing changed under the root; control: the same manifest as a regular file gives `diff: none`
// (exposes the decoder's error echoed, or a followed symlink).
func TestCurrentManifestRefusedWithoutValues(t *testing.T) {
	valid := cfgRead(t, filepath.Join(repoRoot, "stacks/deployments.yaml"))
	elsewhere := filepath.Join(t.TempDir(), "deployments.yaml")
	cfgWrite(t, elsewhere, valid, 0o644)
	cases := map[string]func(t *testing.T, path string){
		"secret value": func(t *testing.T, path string) {
			cfgWrite(t, path, bytes.Replace(valid, []byte("lz.platformrelay.dev/v1alpha1"), []byte(s004Secret), 1), 0o644)
		},
		"symlink": func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, path); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, place := range cases {
		for _, verb := range []string{"check", "export"} {
			t.Run(name+"/"+verb, func(t *testing.T) { s004CurrentRow(t, place, verb) })
		}
	}
	root := cfgRoot(t)
	if code, lines := cfgRun(t, osStaging, "-root", root, "check", sandboxAnswers); code != 0 || lines[0] != "diff: none" {
		t.Errorf("control: regular manifest: exit %d, %q", code, lines)
	}
}

func s004CurrentRow(t *testing.T, place func(t *testing.T, path string), verb string) {
	root := cfgRoot(t)
	place(t, filepath.Join(root, "stacks/deployments.yaml"))
	before := cfgInventory(t, root)
	code, lines := cfgRun(t, osStaging, "-root", root, verb, sandboxAnswers)
	if code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], s004CurrentRefusal) || strings.Contains(strings.Join(lines, "\n"), s004Secret) {
		t.Errorf("exit %d, %q; want exit 1 and one line starting %q, no value from the file", code, lines, s004CurrentRefusal)
	}
	if changed := cfgChanged(before, cfgInventory(t, root)); len(changed) > 0 {
		t.Errorf("changed %v after a refusal", changed)
	}
}

// TestRefuseToolDefect: a refusal code without a catalogue entry and a wrapped refusal each print
// the one defect line naming the code, never the detail and never as `configure: error:`, exit 1;
// controls: an I/O error prints `configure: error: <error>`, and a refusal the catalogue explains
// prints its catalogue line (exposes a catalogue defect printed raw as an I/O error).
func TestRefuseToolDefect(t *testing.T) {
	ioErr := &fs.PathError{Op: "open", Path: "a.yaml", Err: fs.ErrPermission}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unknown code", &configure.AnswersError{Code: "NO_SUCH_CODE", Detail: s004Secret}, "configure: defect: NO_SUCH_CODE could not be explained. "},
		{"wrapped refusal", fmt.Errorf("x: %w", &configure.AnswersError{Code: configure.CodeMissingField, Detail: s004Secret}), "configure: defect: MISSING_FIELD could not be explained. "},
		{"io error", ioErr, "configure: error: " + ioErr.Error()},
		{"explained refusal", &configure.AnswersError{Code: configure.CodeMissingField, Detail: "org"}, "configure: MISSING_FIELD: org. "},
		{"foreign error", errors.New("boom"), "configure: error: boom"},
	}
	for _, c := range cases {
		var out bytes.Buffer
		code := refuse(&out, c.err)
		line := strings.TrimSuffix(out.String(), "\n")
		if code != 1 || strings.Contains(line, "\n") || !strings.HasPrefix(line, c.want) || strings.Contains(line, s004Secret) {
			t.Errorf("%s: exit %d, %q; want exit 1 and one line starting %q", c.name, code, line, c.want)
		}
	}
}
