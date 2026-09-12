// Package main parses CLI flags and delegates evaluation to internal/app.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/version"
)

func main() {
	if code := run(os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("capagent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts app.Options
	var showVersion, showHelp, asJSON bool
	flags.BoolVar(&showVersion, "version", false, "Print version information")
	flags.BoolVar(&showVersion, "v", false, "Print version information")
	flags.BoolVar(&showHelp, "help", false, "Print help")
	flags.BoolVar(&showHelp, "h", false, "Print help")
	flags.StringVar(&opts.Fixture, "fixture", "", "Replay DIR/fixture.json using offline services")
	flags.StringVar(&opts.Runtime, "runtime", "", "Discover local runtime (podman)")
	flags.StringVar(&opts.Context, "context", "", "Evaluation identity (current)")
	flags.StringVar(&opts.PodmanPath, "podman-path", "", "Select an absolute Podman executable path")
	flags.BoolVar(&asJSON, "json", false, "Print JSON (the default output format)")
	flags.BoolVar(&opts.Pretty, "pretty", false, "Indent JSON output")
	flags.BoolVar(&opts.Debug, "debug", false, "Print diagnostic codes on stderr")
	flags.BoolVar(&opts.Active, "active", false, "Unavailable in this release")
	flags.Usage = func() { flags.PrintDefaults() }
	if err := flags.Parse(args); err != nil {
		return app.ExitUsage
	}
	if flags.NArg() != 0 {
		if _, err := fmt.Fprintln(stderr, "capagent: unexpected command; use --help"); err != nil {
			return app.ExitExecution
		}
		return app.ExitUsage
	}
	if showHelp {
		flags.Usage()
		return app.ExitSatisfied
	}
	if showVersion {
		if _, err := fmt.Fprintln(stdout, version.Info()); err != nil {
			return app.ExitExecution
		}
		return app.ExitSatisfied
	}
	return app.Execute(context.Background(), opts, stdout, stderr)
}
