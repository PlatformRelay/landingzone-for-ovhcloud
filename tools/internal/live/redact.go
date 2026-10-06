package live

import (
	"io"
	"sort"
	"strings"
	"sync"
)

// Redaction G2, stream part (FR-010, SC-005, research R12): every child stream and every file the
// run core writes passes through a Redactor holding the run's secrets (the authority's credential
// values and the probe passphrase).

// redacted replaces a secret.
const redacted = "[REDACTED]"

// Redactor replaces every occurrence of a secret; empty secrets are ignored.
type Redactor struct{ secrets []string } // longest first, so a secret holding another goes whole

// NewRedactor returns a Redactor for secrets.
func NewRedactor(secrets ...string) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		if s != "" {
			r.secrets = append(r.secrets, s)
		}
	}
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
	return r
}

// Redact returns s with every secret replaced.
func (r *Redactor) Redact(s string) string {
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, redacted)
	}
	return s
}

// held is the length of the longest suffix of s that is a proper prefix of a secret: what a
// writer must hold back until the next write tells whether the secret follows.
func (r *Redactor) held(s string) int {
	n := 0
	for _, sec := range r.secrets {
		for k := min(len(sec)-1, len(s)); k > n; k-- {
			if strings.HasSuffix(s, sec[:k]) {
				n = k
				break
			}
		}
	}
	return n
}

// redactWriter is safe for concurrent writers (a child's stderr copier and the apply stream
// consumer share the terminal).
type redactWriter struct {
	mu  sync.Mutex
	r   *Redactor
	w   io.Writer
	buf string
}

// Writer returns a writer that redacts what is written to it before it reaches w, also a secret
// split across two writes; Close flushes what it still holds back.
func (r *Redactor) Writer(w io.Writer) io.WriteCloser { return &redactWriter{r: r, w: w} }

func (rw *redactWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	out := rw.r.Redact(rw.buf + string(p))
	h := rw.r.held(out)
	rw.buf = out[len(out)-h:]
	if _, err := io.WriteString(rw.w, out[:len(out)-h]); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (rw *redactWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	_, err := io.WriteString(rw.w, rw.r.Redact(rw.buf))
	rw.buf = ""
	return err
}
