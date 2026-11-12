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
		wantCfg string
	}{
		{name: "default run", argv: []string{"loupedeck-commander"}, wantCmd: "run"},
		{name: "positional config implies run", argv: []string{"loupedeck-commander", "config/config.yaml"}, wantCmd: "run", wantCfg: "config/config.yaml"},
		{name: "flag config implies run", argv: []string{"loupedeck-commander", "--config=config/custom.yaml"}, wantCmd: "run", wantCfg: "config/custom.yaml"},
		{name: "version subcommand", argv: []string{"loupedeck-commander", "version"}, wantCmd: "version"},
		{name: "version long flag", argv: []string{"loupedeck-commander", "--version"}, wantCmd: "version"},
		{name: "version short flag", argv: []string{"loupedeck-commander", "-v"}, wantCmd: "version"},
		{name: "help flag", argv: []string{"loupedeck-commander", "--help"}, wantCmd: "help"},
		{name: "validate command", argv: []string{"loupedeck-commander", "validate"}, wantCmd: "validate"},
		{name: "validate positional config", argv: []string{"loupedeck-commander", "validate", "config/config.yaml"}, wantCmd: "validate", wantCfg: "config/config.yaml"},
		{name: "emulate command", argv: []string{"loupedeck-commander", "emulate"}, wantCmd: "emulate"},
		{name: "panel command (live device-only)", argv: []string{"loupedeck-commander", "panel", "config/config.yaml"}, wantCmd: "panel", wantCfg: "config/config.yaml"},
		{name: "loupedeck-emulator shortcut", argv: []string{"loupedeck-emulator"}, wantCmd: "emulate"},
		{name: "loupedeck-emulator panel mode", argv: []string{"loupedeck-emulator", "panel"}, wantCmd: "panel"},
		{name: "loupedeck-emulator positional config", argv: []string{"loupedeck-emulator", "config/config.yaml"}, wantCmd: "emulate", wantCfg: "config/config.yaml"},
		{name: "loupedeck-emulator path shortcut", argv: []string{"/usr/bin/loupedeck-emulator", "--port=4000"}, wantCmd: "emulate"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseArgs(tc.argv)
			if got.command != tc.wantCmd {
				t.Fatalf("parseArgs(%v).command = %q, want %q", tc.argv, got.command, tc.wantCmd)
			}
			if tc.wantCfg != "" && got.config != tc.wantCfg {
				t.Fatalf("parseArgs(%v).config = %q, want %q", tc.argv, got.config, tc.wantCfg)
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

