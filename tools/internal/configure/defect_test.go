package configure

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Controls of 004 T024 (b), FR-004: a refusal the catalogue cannot explain (a code without an
// entry, a refusal wrapped in another error) is a defect of the tool, one line naming the code and
// never the detail; an error that carries no refusal is not one, and stays the command's
// `configure: error:` line.

const defectSecret = "s004-SECRET-DETAIL"

func defectWant(code string) string {
	return "configure: defect: " + code + " could not be explained. " + messagesToolDefect +
		" Next: Report it with the answers file; meanwhile edit `stacks/deployments.yaml` by hand."
}

// TestDefectLine: an unknown configure code, an unknown generated code, a wrapped answers refusal,
// a wrapped manifest refusal and a code with a newline (quoted) each give the defect line naming
// the code, without the detail; controls: an I/O error, a foreign error, nil and a refusal the
// catalogue explains give no line.
func TestDefectLine(t *testing.T) {
	refusals := map[string]struct {
		err  error
		code string
	}{
		"unknown answers code":   {&AnswersError{Code: "NO_SUCH_CODE", Detail: defectSecret}, "NO_SUCH_CODE"},
		"unknown generated code": {&stacks.ManifestError{Code: "ANSWERS_SYNTAX", Detail: defectSecret}, "ANSWERS_SYNTAX"},
		"wrapped answers":        {fmt.Errorf("x: %w", &AnswersError{Code: CodeMissingField, Detail: defectSecret}), CodeMissingField},
		"wrapped manifest":       {fmt.Errorf("x: %w", &stacks.ManifestError{Code: stacks.CodeDuplicateVLAN, Detail: defectSecret}), stacks.CodeDuplicateVLAN},
		"code with a newline":    {&AnswersError{Code: "A\nB", Detail: defectSecret}, strconv.Quote("A\nB")},
	}
	for name, c := range refusals {
		if line, ok := DefectLine(c.err); !ok || line != defectWant(c.code) {
			t.Errorf("%s: got %q, %v; want %q", name, line, ok, defectWant(c.code))
		}
	}
	for name, err := range map[string]error{
		"io error":          fmt.Errorf("read answers file: %w", &fs.PathError{Op: "open", Path: "a.yaml", Err: fs.ErrPermission}),
		"foreign":           errors.New("UNKNOWN_FIELD: x"),
		"nil":               nil,
		"explained refusal": &AnswersError{Code: CodeMissingField, Detail: "org"},
	} {
		if line, ok := DefectLine(err); ok || line != "" {
			t.Errorf("control %s: got %q, %v; want no line", name, line, ok)
		}
	}
}
