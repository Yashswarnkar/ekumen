// Command ekumen-import turns the hand-written records under a project's
// .ekumen/ folder into events (T-import). With --dry-run it only reports.
//
//	ekumen-import [--root .ekumen] [--dry-run] [--json]
//
// Exit status: 0 on success, 1 when verification fails or on error, 2 on
// bad usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/legacyimport"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("ekumen-import", flag.ContinueOnError)
	fl.SetOutput(stderr)
	root := fl.String("root", ".ekumen", "the project's .ekumen/ folder")
	dry := fl.Bool("dry-run", false, "report what would be written; write nothing")
	asJSON := fl.Bool("json", false, "print the report as JSON")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "ekumen-import: unexpected arguments %v\n", fl.Args())
		return 2
	}

	plan, err := legacyimport.Prepare(*root)
	if err != nil {
		fmt.Fprintf(stderr, "ekumen-import: %v\n", err)
		return 1
	}
	out := struct {
		DryRun  bool                  `json:"dry_run"`
		OK      bool                  `json:"ok"`
		Plan    *legacyimport.Plan    `json:"plan"`
		Written *legacyimport.Written `json:"written,omitempty"`
	}{DryRun: *dry, OK: plan.OK(), Plan: plan}

	code := 0
	if !plan.OK() {
		code = 1
	} else if !*dry {
		w, err := legacyimport.Apply(plan)
		out.Written = w
		if err != nil {
			fmt.Fprintf(stderr, "ekumen-import: %v\n", err)
			code = 1
		}
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "ekumen-import: %v\n", err)
			return 1
		}
		return code
	}
	var sb strings.Builder
	if *dry {
		sb.WriteString("dry run: nothing written\n")
	}
	plan.Report(&sb)
	if w := out.Written; w != nil {
		fmt.Fprintf(&sb, "wrote %d events, %d brief files; removed %d old briefs\n", len(w.Events), len(w.Files), len(w.Removed))
		for _, l := range append(append(w.Files, w.Events...), w.Removed...) {
			fmt.Fprintf(&sb, "  %s\n", l)
		}
	}
	if !plan.OK() {
		sb.WriteString("FAILED: verification mismatches or conflicts; nothing written\n")
	}
	fmt.Fprint(stdout, sb.String())
	return code
}
