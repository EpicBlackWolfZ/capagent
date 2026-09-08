package main

import (
	"flag"
	"os"
	"testing"
)

func TestMainFlags(t *testing.T) {
	// Backup CommandLine arguments and flag state
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	t.Run("version flag parses successfully", func(t *testing.T) {
		flag.CommandLine = flag.NewFlagSet("capagent", flag.ContinueOnError)
		os.Args = []string{"capagent", "--version"}
		main()
	})

	t.Run("help flag parses successfully", func(t *testing.T) {
		flag.CommandLine = flag.NewFlagSet("capagent", flag.ContinueOnError)
		os.Args = []string{"capagent", "--help"}
		main()
	})

	t.Run("default execution renders banner", func(t *testing.T) {
		flag.CommandLine = flag.NewFlagSet("capagent", flag.ContinueOnError)
		os.Args = []string{"capagent"}
		main()
	})
}
