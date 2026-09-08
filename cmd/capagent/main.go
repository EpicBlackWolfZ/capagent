// Package main provides the command-line interface entrypoint for capagent.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/EpicBlackWolfZ/capagent/internal/version"
)

func main() {
	var (
		showVersion bool
		showHelp    bool
	)

	flag.BoolVar(&showVersion, "version", false, "Print version information and exit")
	flag.BoolVar(&showVersion, "v", false, "Print version information (shorthand)")
	flag.BoolVar(&showHelp, "help", false, "Print help information and exit")
	flag.BoolVar(&showHelp, "h", false, "Print help information (shorthand)")

	flag.Usage = func() {
		out := flag.CommandLine.Output()
		_, _ = fmt.Fprintf(out, "capagent - Portable Linux Container Compatibility Engine\n\n")
		_, _ = fmt.Fprintf(out, "Usage:\n")
		_, _ = fmt.Fprintf(out, "  capagent [flags] [command]\n\n")
		_, _ = fmt.Fprintf(out, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if showHelp {
		flag.Usage()
		return
	}

	if showVersion {
		fmt.Println(version.Info())
		return
	}

	version.PrintStartupBanner(os.Stdout)
}
