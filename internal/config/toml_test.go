package config

import (
	"strings"
	"testing"
)

func TestBoundedTOML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, code string
	}{
		{"empty", "", ""},
		{"full syntax", "[engine]\nruntime='crun'\nconmon_path=[\"/usr/bin/conmon\", {append=true}]\n" +
			"[extra]\nquoted=\"\"\"a\\ntext\"\"\"\ndate=2026-09-13\n", ""},
		{"duplicate", "key=1\nkey=2", ErrConfigMalformed.Error()},
		{"duplicate table", "[engine]\na=1\n[engine]\nb=2", ErrConfigMalformed.Error()},
		{"malformed", "password=\"secret-value", ErrConfigMalformed.Error()},
		{"bytes", strings.Repeat(" ", MaxTOMLBytes+1), ErrConfigLimit.Error()},
		{"array depth", "a=" + strings.Repeat("[", MaxTOMLDepth+1) + "1" + strings.Repeat("]", MaxTOMLDepth+1), ErrConfigLimit.Error()},
		{"key depth", strings.Repeat("a.", MaxTOMLDepth) + "a=1", ErrConfigLimit.Error()},
		{"table depth", "[" + strings.Repeat("a.", MaxTOMLDepth) + "a]", ErrConfigLimit.Error()},
		{"combined depth", "[" + strings.Repeat("a.", MaxTOMLDepth-2) + "a]\nb.c=1", ErrConfigLimit.Error()},
		{"nodes", "a=[" + strings.Repeat("1,", MaxTOMLNodes) + "]", ErrConfigLimit.Error()},
		{"strings not nesting", "a='" + strings.Repeat("[", MaxTOMLDepth+1) + "'", ""},
		{"inline deep key", "a={" + strings.Repeat("a.", MaxTOMLDepth) + "a=1}", ErrConfigLimit.Error()},
		{"huge nesting", "a=" + strings.Repeat("[", 20000) + strings.Repeat("]", 20000), ErrConfigMalformed.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value, err := ParseTOML([]byte(tt.input))
			if tt.code == "" {
				if err != nil || value == nil {
					t.Fatalf("valid document: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.code || value != nil {
				t.Fatalf("error=%v value=%v; want fixed %s", err, value, tt.code)
			}
		})
	}
}

func TestTOML10Compatibility(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		valid bool
	}{
		{`a={b=1,}`, false}, {"a={\nb=1}", false}, {"a={b=1 # comment\n}", false},
		{`a="\x41"`, false}, {`"\x41"=1`, false}, {`a="\e"`, false},
		{`a=12:30`, false}, {`a=2026-09-13T12:30Z`, false}, {`a=2026-09-13T12:30+01:00`, false},
		{`a={b=[{c=1,}]}`, false}, {`a={b=1, c=2}`, true}, {`a={}`, true},
		{"a={b=\"\"\"multi\nline\"\"\", c=[\n1,\n2,\n]}", true},
		{`a="\\x41"`, true}, {`a='\e\x41'`, true}, {`a=12:30:00`, true}, {`a=2026-09-13T12:30:00+01:00`, true},
	} {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseTOML([]byte(test.input))
			if test.valid && err != nil || !test.valid && (err == nil || err.Error() != "config_toml_version_unsupported") {
				t.Fatalf("error=%v; supported TOML 1.0=%v", err, test.valid)
			}
		})
	}
}
