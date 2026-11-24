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
		{name: "hybrid command", argv: []string{"loupedeck-commander", "hybrid"}, wantCmd: "hybrid"},
		{name: "both alias command", argv: []string{"loupedeck-commander", "both", "config/config.yaml"}, wantCmd: "hybrid", wantCfg: "config/config.yaml"},
		{name: "hybrid flag", argv: []string{"loupedeck-commander", "--hybrid"}, wantCmd: "hybrid"},
		{name: "both flag", argv: []string{"loupedeck-commander", "--both"}, wantCmd: "hybrid"},
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

func TestParseArgsHybridFlags(t *testing.T) {
	t.Run("hybrid defaults to live", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "hybrid"})
		if got.command != "hybrid" || !got.hybrid || !got.live || got.deviceOnly {
			t.Fatalf("unexpected parseArgs result for hybrid: %+v", got)
		}
	})

	t.Run("both alias defaults to live", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "both"})
		if got.command != "hybrid" || !got.hybrid || !got.live {
			t.Fatalf("unexpected parseArgs result for both: %+v", got)
		}
	})

	t.Run("--hybrid flag defaults to live", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "--hybrid"})
		if got.command != "hybrid" || !got.hybrid || !got.live {
			t.Fatalf("unexpected parseArgs result for --hybrid: %+v", got)
		}
	})

	t.Run("--both flag defaults to live", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "--both"})
		if got.command != "hybrid" || !got.hybrid || !got.live {
			t.Fatalf("unexpected parseArgs result for --both: %+v", got)
		}
	})

	t.Run("hybrid --device-only", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "hybrid", "--device-only"})
		if got.command != "hybrid" || !got.hybrid || !got.deviceOnly || !got.live {
			t.Fatalf("unexpected parseArgs result for hybrid --device-only: %+v", got)
		}
	})

	t.Run("hybrid --dry-run", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "hybrid", "--dry-run"})
		if got.command != "hybrid" || !got.hybrid || got.live {
			t.Fatalf("unexpected parseArgs result for hybrid --dry-run: %+v", got)
		}
	})
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

func TestParseArgsBindAndPort(t *testing.T) {
	t.Run("default bind and port", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "emulate"})
		if got.bind != "0.0.0.0" {
			t.Fatalf("default bind = %q, want %q", got.bind, "0.0.0.0")
		}
		if got.port != 3080 {
			t.Fatalf("default port = %d, want %d", got.port, 3080)
		}
	})

	t.Run("--bind=127.0.0.1", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "emulate", "--bind=127.0.0.1"})
		if got.bind != "127.0.0.1" {
			t.Fatalf("bind = %q, want %q", got.bind, "127.0.0.1")
		}
		if got.port != 3080 {
			t.Fatalf("port = %d, want %d", got.port, 3080)
		}
	})

	t.Run("--bind=192.168.1.50:4080", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "emulate", "--bind=192.168.1.50:4080"})
		if got.bind != "192.168.1.50" || got.port != 4080 {
			t.Fatalf("got bind=%q port=%d, want bind=%q port=%d", got.bind, got.port, "192.168.1.50", 4080)
		}
	})

	t.Run("--listen=127.0.0.1:8080", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "emulate", "--listen=127.0.0.1:8080"})
		if got.bind != "127.0.0.1" || got.port != 8080 {
			t.Fatalf("got bind=%q port=%d, want bind=%q port=%d", got.bind, got.port, "127.0.0.1", 8080)
		}
	})

	t.Run("-b short flag", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "emulate", "-b", "127.0.0.1"})
		if got.bind != "127.0.0.1" {
			t.Fatalf("bind = %q, want %q", got.bind, "127.0.0.1")
		}
	})

	t.Run("--port=127.0.0.1:3090", func(t *testing.T) {
		got := parseArgs([]string{"loupedeck-commander", "emulate", "--port=127.0.0.1:3090"})
		if got.bind != "127.0.0.1" || got.port != 3090 {
			t.Fatalf("got bind=%q port=%d, want bind=%q port=%d", got.bind, got.port, "127.0.0.1", 3090)
		}
	})
}
