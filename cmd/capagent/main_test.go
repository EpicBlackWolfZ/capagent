package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
)

func TestCLIFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0}, {"version", []string{"-v"}, 0},
		{"no fixture", nil, app.ExitUsage}, {"unknown command", []string{"inspect"}, app.ExitUsage},
		{"unknown flag", []string{"--invalid"}, app.ExitUsage}, {"active", []string{"--active"}, app.ExitUsage},
		{"fixture", []string{"--fixture", "../../testdata/fixtures/v1/supported", "--json", "--pretty"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.want {
				t.Fatal(code, stderr.String())
			}
			if tt.want != 0 && (stdout.Len() != 0 || stderr.Len() == 0) {
				t.Fatal("invalid error streams")
			}
		})
	}
}

func TestMainHelp(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"capagent", "--help"}
	main()
}
