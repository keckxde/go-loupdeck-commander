package main

import (
	"strings"
	"testing"
)

func TestParseArgsVersionAndCommands(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		wantCmd string
	}{
		{name: "default run", argv: []string{"loupedeck-commander"}, wantCmd: "run"},
		{name: "version subcommand", argv: []string{"loupedeck-commander", "version"}, wantCmd: "version"},
		{name: "version long flag", argv: []string{"loupedeck-commander", "--version"}, wantCmd: "version"},
		{name: "version short flag", argv: []string{"loupedeck-commander", "-v"}, wantCmd: "version"},
		{name: "help flag", argv: []string{"loupedeck-commander", "--help"}, wantCmd: "help"},
		{name: "validate command", argv: []string{"loupedeck-commander", "validate"}, wantCmd: "validate"},
		{name: "emulate command", argv: []string{"loupedeck-commander", "emulate"}, wantCmd: "emulate"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseArgs(tc.argv)
			if got.command != tc.wantCmd {
				t.Fatalf("parseArgs(%v).command = %q, want %q", tc.argv, got.command, tc.wantCmd)
			}
		})
	}
}

func TestFormatVersion(t *testing.T) {
	out := formatVersion()
	if !strings.Contains(out, Version) {
		t.Fatalf("expected formatVersion() %q to contain Version %q", out, Version)
	}
	if !strings.Contains(out, Commit) {
		t.Fatalf("expected formatVersion() %q to contain Commit %q", out, Commit)
	}
}

