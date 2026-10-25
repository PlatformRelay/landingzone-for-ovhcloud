package configure

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Controls of 004 T024 (a), FR-005 and FR-008: today's stacks/deployments.yaml is read like the
// answers file (a regular file, the last component no symlink, bounded size), and its refusal
// names the code and the path only.

// currentPadded is the sandbox manifest padded with a whole-line comment to exactly size bytes.
func currentPadded(t *testing.T, size int) []byte {
	t.Helper()
	base := answersReadFile(t, exportSandboxManifest)
	pad := size - len(base) - 2 // "#" and "\n"
	if pad < 0 {
		t.Fatalf("sandbox manifest exceeds %d bytes", size)
	}
	return append(base, []byte("#"+strings.Repeat("x", pad)+"\n")...)
}

// currentRoot is a temporary root whose stacks/deployments.yaml is made by place (nil: absent).
func currentRoot(t *testing.T, place func(path string) error) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "stacks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if place != nil {
		if err := place(filepath.Join(root, "stacks", "deployments.yaml")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func currentWriteFile(data []byte) func(string) error {
	return func(path string) error { return os.WriteFile(path, data, 0o644) }
}

// TestReadCurrentManifest: a symlink to a valid manifest, a directory, a FIFO and a file of 1 MiB
// plus one byte are each CURRENT_MANIFEST_INVALID naming the path alone, with no bytes; controls:
// an absent file is nil and no error (every row `+`), a regular file of exactly 1 MiB is read whole
// (rejection; exposes a followed symlink, an unbounded read, or a blocked open).
func TestReadCurrentManifest(t *testing.T) {
	target := filepath.Join(t.TempDir(), "deployments.yaml")
	if err := os.WriteFile(target, answersReadFile(t, exportSandboxManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	fits, over := currentPadded(t, MaxCurrentManifestBytes), currentPadded(t, MaxCurrentManifestBytes+1)
	if MaxCurrentManifestBytes != 1<<20 || len(over) != 1<<20+1 {
		t.Fatalf("bound %d, padding %d bytes; want 1 MiB", MaxCurrentManifestBytes, len(over))
	}
	refused := map[string]func(string) error{
		"symlink":   func(p string) error { return os.Symlink(target, p) },
		"directory": func(p string) error { return os.Mkdir(p, 0o755) },
		"fifo":      func(p string) error { return syscall.Mkfifo(p, 0o600) },
		"oversized": currentWriteFile(over),
	}
	for name, place := range refused {
		t.Run(name, func(t *testing.T) {
			data, err := ReadCurrentManifest(currentRoot(t, place))
			e, ok := err.(*AnswersError)
			if !ok || e.Code != CodeCurrentManifestInvalid || e.Detail != "stacks/deployments.yaml" || data != nil {
				t.Errorf("%d bytes, %T %v; want CURRENT_MANIFEST_INVALID at stacks/deployments.yaml and no bytes", len(data), err, err)
			}
		})
	}
	if data, err := ReadCurrentManifest(currentRoot(t, nil)); data != nil || err != nil {
		t.Errorf("control: absent: %d bytes, %v; want nil, nil", len(data), err)
	}
	if data, err := ReadCurrentManifest(currentRoot(t, currentWriteFile(fits))); err != nil || string(data) != string(fits) {
		t.Errorf("control: exactly 1 MiB: %d bytes, %v; want the file", len(data), err)
	}
}

// TestReadCurrentManifestUncoded: a read error other than absence or a refusal (a NUL in the root
// path: EINVAL) is a plain error naming the file, never a refusal of its content.
func TestReadCurrentManifestUncoded(t *testing.T) {
	_, err := ReadCurrentManifest("bad\x00root")
	var ae *AnswersError
	// the prefix, not only the path: the open error itself already holds the joined path
	if err == nil || errors.As(err, &ae) || !strings.HasPrefix(err.Error(), "read stacks/deployments.yaml: ") {
		t.Errorf("got %T %v; want a plain error naming stacks/deployments.yaml", err, err)
	}
}
