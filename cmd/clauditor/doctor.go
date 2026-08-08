package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mjraval/clauditor/internal/collect"
	"github.com/mjraval/clauditor/internal/config"
	"github.com/mjraval/clauditor/internal/doctor"
)

// cmdDoctor runs SPEC §12's environment checks and prints a PASS/WARN/FAIL
// table. It returns a non-nil error iff any check FAILed — main.go's run()
// already exits 1 on any returned error, so doctor never calls os.Exit
// itself (same convention as every other cmd/clauditor/*.go file).
func cmdDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	load := commonFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// config.Load's error becomes its own Check (SPEC §12 item "config
	// parses") rather than aborting before the table prints; the rest of
	// the checks still run against config.Default() so a malformed config
	// doesn't hide every other signal.
	cfg, cfgErr := load()
	configCheck := doctor.Check{Name: "config parses", Status: doctor.PASS, Detail: "ok"}
	if cfgErr != nil {
		configCheck.Status = doctor.FAIL
		configCheck.Detail = cfgErr.Error()
		cfg = config.Default()
	}

	checks := make([]doctor.Check, 0, 1)
	checks = append(checks, configCheck)
	checks = append(checks, doctor.RunAll(ctx, cfg, collect.NewRunner())...)

	printDoctorTable(os.Stdout, checks)

	if doctor.AnyFail(checks) {
		return fmt.Errorf("doctor: one or more checks failed")
	}
	return nil
}

func printDoctorTable(w io.Writer, checks []doctor.Check) {
	fmt.Fprintln(w, "clauditor doctor")
	fmt.Fprintln(w)
	pass, warn, fail, skip := 0, 0, 0, 0
	for _, c := range checks {
		fmt.Fprintf(w, "%-4s  %-32s %s\n", c.Status, c.Name, c.Detail)
		switch c.Status {
		case doctor.PASS:
			pass++
		case doctor.WARN:
			warn++
		case doctor.FAIL:
			fail++
		case doctor.SKIP:
			skip++
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d checks: %d PASS, %d WARN, %d FAIL, %d SKIP\n", len(checks), pass, warn, fail, skip)
}
