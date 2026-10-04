// json2junit renders a `tofu test -json` stream as JUnit XML. It is a view of
// the report observation, not a separate oracle: the verdict, counts and
// diagnostics all come from report.Evaluate, and a failing observation exits 1.
//
// Capture the stream first, then pass the exit status of that tofu process;
// in a pipeline `$?` would be the status of an earlier command.
//
//	tofu test -json > stream.jsonl; status=$?
//	json2junit -exit "$status" < stream.jsonl > report.xml
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/PlatformRelay/ovh-landing-zone-accelerator/tools/internal/report"
)

const maxStream = 64 << 20

type suites struct {
	XMLName xml.Name `xml:"testsuites"`
	Suites  []suite  `xml:"testsuite"`
}

type suite struct {
	Name       string     `xml:"name,attr"`
	Tests      int        `xml:"tests,attr"`
	Failures   int        `xml:"failures,attr"`
	Errors     int        `xml:"errors,attr"`
	Skipped    int        `xml:"skipped,attr"`
	Properties []property `xml:"properties>property"`
	Cases      []testcase `xml:"testcase"`
}

type property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type testcase struct {
	Class     string   `xml:"classname,attr"`
	Name      string   `xml:"name,attr"`
	Failure   *message `xml:"failure"`
	Error     *message `xml:"error"`
	Skipped   *message `xml:"skipped"`
	SystemOut string   `xml:"system-out,omitempty"`
}

type message struct {
	Message string `xml:"message,attr,omitempty"`
	Text    string `xml:",chardata"`
}

func render(o report.Observation) suite {
	s := suite{Name: "tofu test", Properties: []property{
		{"tool", o.Tool}, {"tool_version", o.ToolVersion}, {"exit_code", fmt.Sprint(o.ExitCode)}, {"status", string(o.Status)},
	}}
	for _, r := range o.Runs {
		c := testcase{Class: r.File, Name: r.Name}
		switch r.Status {
		case "pass":
		case "skip":
			c.Skipped, s.Skipped = &message{Message: "skipped by tofu"}, s.Skipped+1
		case "error":
			c.Error, s.Errors = &message{Message: "run errored"}, s.Errors+1
		default:
			c.Failure, s.Failures = &message{Message: "run " + r.Status}, s.Failures+1
		}
		s.Cases = append(s.Cases, c)
	}
	// The observation itself is a test case, so faults tofu reports as success
	// (zero tests, failed cleanup) still fail the JUnit view.
	// Every diagnostic is rendered, warnings on passing runs included, with
	// the run it followed; failed cleanup resources are listed verbatim.
	var text strings.Builder
	for _, d := range o.Diagnostics {
		fmt.Fprintf(&text, "%s: %s", d.Severity, d.Summary)
		if d.Run != "" {
			fmt.Fprintf(&text, " (%s/%s)", d.File, d.Run)
		}
		fmt.Fprintf(&text, "\n%s\n", d.Detail)
	}
	for _, resource := range o.Cleanup {
		fmt.Fprintf(&text, "cleanup failed: %s\n", resource)
	}
	verdict := testcase{Class: "report", Name: "observation", SystemOut: text.String()}
	if o.Status != report.Pass {
		verdict.Failure = &message{Message: strings.Join(o.Reasons, " "), Text: text.String()}
		s.Failures++
	}
	s.Cases = append(s.Cases, verdict)
	s.Tests = len(s.Cases)
	return s
}

func run(in io.Reader, out io.Writer, exitCode int) int {
	stream, err := io.ReadAll(io.LimitReader(in, maxStream+1))
	if err != nil || len(stream) > maxStream {
		fmt.Fprintln(os.Stderr, "INPUT_LIMIT: stream unreadable or larger than 64 MiB")
		return 2
	}
	o := report.Evaluate(stream, exitCode)
	data, err := xml.MarshalIndent(suites{Suites: []suite{render(o)}}, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err := fmt.Fprintf(out, "%s%s\n", xml.Header, data); err != nil {
		return 2
	}
	if o.Status != report.Pass {
		return 1
	}
	return 0
}

func main() {
	exit := flag.Int("exit", -1, "exit status of the tofu test process (required)")
	flag.Parse()
	if *exit < 0 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "USAGE: json2junit -exit <tofu exit status> < stream.jsonl")
		os.Exit(2)
	}
	os.Exit(run(os.Stdin, os.Stdout, *exit))
}
