package live

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// Redaction G2, stream part (FR-010, SC-005): a seeded secret never reaches an output, also when
// a child writes it in pieces; everything else passes through unchanged.

const (
	seedSecret = "seeded-SECRET-7f3a9c41d2e8b605"
	seedOther  = "second-secret-value-0420"
)

func TestRedactString(t *testing.T) {
	r := NewRedactor(seedSecret, seedOther, "")
	for name, in := range map[string]string{
		"alone":     seedSecret,
		"embedded":  "token=" + seedSecret + "; next",
		"twice":     seedSecret + " and " + seedSecret,
		"two":       seedOther + "|" + seedSecret,
		"adjacent":  seedSecret + seedSecret,
		"multiline": "line one\nclient_secret = \"" + seedSecret + "\"\nline three",
	} {
		t.Run(name, func(t *testing.T) {
			out := r.Redact(in)
			if strings.Contains(out, seedSecret) || strings.Contains(out, seedOther) {
				t.Errorf("secret survived: %q", out)
			}
			// What is not a secret survives.
			for _, keep := range []string{"token=", "; next", " and ", "|", "line one\n", "line three"} {
				if strings.Contains(in, keep) && !strings.Contains(out, keep) {
					t.Errorf("non-secret text %q dropped: %q", keep, out)
				}
			}
		})
	}
	// An empty secret matches nothing; text without a secret is unchanged byte for byte.
	plain := "OpenTofu 1.13.0: terraform_data.first: Creation complete after 0s [id=815a]\n"
	if got := r.Redact(plain); got != plain {
		t.Errorf("text without a secret changed: %q -> %q", plain, got)
	}
}

// TestRedactWriterSplitWrites: a child writes the secret across several writes (pipes deliver
// arbitrary chunks); the redacting writer must not let any part of it through.
func TestRedactWriterSplitWrites(t *testing.T) {
	in := "start " + seedSecret + " middle " + seedOther + " end\n"
	for _, chunk := range []int{1, 3, 7, len(seedSecret) - 1, len(in)} {
		t.Run("chunk="+strconv.Itoa(chunk), func(t *testing.T) {
			var buf bytes.Buffer
			w := NewRedactor(seedSecret, seedOther).Writer(&buf)
			for i := 0; i < len(in); i += chunk {
				end := min(i+chunk, len(in))
				n, err := w.Write([]byte(in[i:end]))
				if err != nil || n != end-i {
					t.Fatalf("write: n=%d err=%v", n, err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			if strings.Contains(out, seedSecret) || strings.Contains(out, seedOther) {
				t.Errorf("secret survived a split write: %q", out)
			}
			// No prefix of the secret long enough to identify it leaks either.
			if strings.Contains(out, seedSecret[:len(seedSecret)/2]) {
				t.Errorf("half of the secret leaked: %q", out)
			}
			for _, keep := range []string{"start ", " middle ", " end\n"} {
				if !strings.Contains(out, keep) {
					t.Errorf("non-secret text %q dropped: %q", keep, out)
				}
			}
		})
	}
}

// TestRedactWriterFlushesOnClose: text held back while it could still be the start of a secret
// reaches the output on Close.
func TestRedactWriterFlushesOnClose(t *testing.T) {
	var buf bytes.Buffer
	w := NewRedactor(seedSecret).Writer(&buf)
	tail := "trailing " + seedSecret[:5]
	if _, err := w.Write([]byte(tail)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.String() != tail {
		t.Errorf("Close flushed %q, want %q", buf.String(), tail)
	}
}
