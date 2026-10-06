package live

import "io"

// Redaction G2, stream part (FR-010, SC-005, research R12): every child stream and every file the
// run core writes passes through a Redactor holding the run's secrets (the authority's credential
// values and the probe passphrase).
//
// STUB (T054): the tests pin the behaviour; T055 implements it.

// Redactor replaces every occurrence of a secret; empty secrets are ignored.
type Redactor struct{}

// NewRedactor returns a Redactor for secrets.
func NewRedactor(secrets ...string) *Redactor { return &Redactor{} }

// Redact returns s with every secret replaced.
func (r *Redactor) Redact(s string) string { return s }

// Writer returns a writer that redacts what is written to it before it reaches w, also a secret
// split across two writes; Close flushes what it still holds back.
func (r *Redactor) Writer(w io.Writer) io.WriteCloser { return nopCloser{w} }

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
