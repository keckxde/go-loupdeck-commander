package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/commander"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	"gitlab.com/keckxde/loupedeck-commander-go/pkg/emulator"
)

// Build-time version variables injected via -ldflags:
//
//	-X main.Version=<git-tag> -X main.Commit=<sha> -X main.BuildTime=<utc-iso8601>
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func formatVersion() string {
	return fmt.Sprintf(
		"loupedeck-commander %s (commit: %s, built: %s, go: %s, %s/%s)",
		Version,
		Commit,
		BuildTime,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}

var cfgFileRegex = regexp.MustCompile(`(?i)^config.*\.(ya?ml|json5?)$`)

func resolveDefaultConfig(baseDir, host string) string {
	candidates := []string{
		"config/config.yaml",
		"config/config.json",
		fmt.Sprintf("config/config-%s.yaml", host),
		fmt.Sprintf("config/config-%s.json", host),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(baseDir, c)); err == nil {
			return c
		}
	}
	cfgDir := filepath.Join(baseDir, "config")
	if entries, err := os.ReadDir(cfgDir); err == nil {
		var matches []string
		for _, e := range entries {
			if !e.IsDir() && cfgFileRegex.MatchString(e.Name()) {
				matches = append(matches, e.Name())
			}
		}
		sort.Strings(matches)
		if len(matches) > 0 {
			return "config/" + matches[0]
		}
	}
	return "config/config.yaml"
}

type cliArgs struct {
	command string
	config  string
	port    int
	live    bool
	fix     bool
	model   string
}

func parseArgs(argv []string) cliArgs {
	host, _ := os.Hostname()
	explicitConfig := false
	args := cliArgs{
		command: "run",
		config:  "config/config.yaml",
		port:    3080,
		live:    false,
		fix:     false,
		model:   "live",
	}

	for i := 1; i < len(argv); i++ {
		token := argv[i]
		if !strings.HasPrefix(token, "-") {
			lower := strings.ToLower(token)
			switch lower {
			case "emulate", "emulator", "validate", "help", "version", "run":
				args.command = lower
			default:
				args.config = token
				explicitConfig = true
			}
			continue
		}

		trimmed := strings.TrimLeft(token, "-")
		parts := strings.SplitN(trimmed, "=", 2)
		rawKey := parts[0]
		val := ""
		hasEq := len(parts) == 2
		if hasEq {
			val = parts[1]
		}

		nextVal := func(fallback string) string {
			if hasEq && val != "" {
				return val
			}
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				i++
				return argv[i]
			}
			return fallback
		}

		switch rawKey {
		case "config", "c":
			args.config = nextVal(args.config)
			explicitConfig = true
		case "host":
			host = nextVal(host)
			if !explicitConfig {
				args.config = fmt.Sprintf("config/config-%s.yaml", host)
				explicitConfig = true
			}
		case "port", "p":
			pStr := nextVal("3080")
			if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
				args.port = p
			}
		case "live":
			args.live = !hasEq || val == "" || strings.EqualFold(val, "true")
		case "fix":
			args.fix = !hasEq || val == "" || strings.EqualFold(val, "true")
		case "model":
			args.model = strings.ToLower(nextVal("live"))
		case "validate":
			args.command = "validate"
		case "emulate", "emulator":
			args.command = "emulate"
		case "version", "v":
			args.command = "version"
		case "help", "h":
			args.command = "help"
		}
	}

	if !explicitConfig {
		cwd, _ := os.Getwd()
		args.config = resolveDefaultConfig(cwd, host)
	}
	return args
}

func printHelp() {
	fmt.Printf(`
Loupedeck Commander (Go) %s — Hardware Daemon, Emulator & Configuration Tool

Usage:
  loupedeck-commander [command] [options]

Commands:
  run         Start the Loupedeck Commander hardware daemon (default)
  emulate     Start the interactive Web Emulator & Configuration Studio
  validate    Validate configuration & profile files for errors and deprecations
  version     Print version, git tag/commit, and build metadata
  help        Show this help message

Options:
  --config=<file>   Application config file (default: config/config.yaml or config/config-<hostname>.yaml)
  --host=<hostname> Resolve config/config-<hostname>.yaml
  --port=<number>   HTTP port for the Web Emulator (default: 3080)
  --live            Execute real Shell/HTTP/OPC UA calls in emulator mode
  --model=<model>   Emulated device model: live | ct | live-s | razer | razer-x (default: live)
  --fix             Automatically fix deprecated button attributes & typos during 'validate'
  --version, -v     Print version information and exit
`, Version)
}

func runValidate(args cliArgs) int {
	configPath := args.config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		alt := strings.Replace(configPath, ".yaml", ".json", 1)
		if _, err2 := os.Stat(alt); err2 == nil {
			configPath = alt
		}
	}

	fmt.Printf("🔍 Validating configuration: %s\n\n", configPath)
	cwd, _ := os.Getwd()

	if args.fix {
		parsedApp, _ := config.ParseConfigFileSafe(filepath.Join(cwd, configPath))
		if parsedApp != nil {
			if profs, ok := parsedApp["profiles"].([]interface{}); ok {
				for _, entryRaw := range profs {
					entry, ok := entryRaw.(map[string]interface{})
					if !ok || entry == nil {
						continue
					}
					fileRel := config.ToString(entry["file"])
					if fileRel == "" {
						continue
					}
					profPath := filepath.Join(cwd, fileRel)
					profParsed, _ := config.ParseConfigFileSafe(profPath)
					if profParsed == nil {
						continue
					}
					fixRes := config.AutoFixProfileData(profParsed)
					if len(fixRes.FixesApplied) > 0 {
						if strings.HasSuffix(profPath, ".json") || strings.HasSuffix(profPath, ".json5") {
							_ = config.WriteJSONFile(profPath, fixRes.FixedProfileData)
						} else {
							_ = config.WriteYAMLFile(profPath, fixRes.FixedProfileData)
						}
						fmt.Printf("✨ Applied %d auto-fix(es) in %s:\n", len(fixRes.FixesApplied), fileRel)
						for _, fix := range fixRes.FixesApplied {
							fmt.Printf("   - %s\n", fix)
						}
						fmt.Println()
					}
				}
			}
		}
	}

	report := config.ValidateAppConfigFile(configPath, cwd)
	if len(report.Issues) == 0 {
		fmt.Println("✅ All configuration and profile files are valid with zero warnings!")
		return 0
	}

	for _, issue := range report.Issues {
		tag := "ℹ️  [INFO]   "
		if issue.Severity == "error" {
			tag = "❌ [ERROR]  "
		} else if issue.Severity == "warning" {
			tag = "⚠️  [WARNING]"
		}
		loc := issue.File
		if issue.Path != "" {
			loc = fmt.Sprintf("%s -> %s", issue.File, issue.Path)
		}
		fmt.Printf("%s %s\n", tag, loc)
		fmt.Printf("   %s\n", issue.Message)
		if issue.Suggestion != "" {
			fmt.Printf("   💡 Suggestion: %s\n", issue.Suggestion)
		}
		fmt.Println()
	}

	fmt.Println("──────────────────────────────────────────────────────────")
	fmt.Printf(
		"Summary: %d error(s), %d warning(s), %d info note(s) across %d profile(s).\n",
		report.Summary.ErrorCount,
		report.Summary.WarningCount,
		report.Summary.InfoCount,
		report.Summary.ProfileCount,
	)
	if report.Valid {
		fmt.Println("Result:  ✅ VALID (no blocking errors)")
		return 0
	}
	fmt.Println("Result:  ❌ INVALID (please fix errors above)")
	return 1
}

func waitForSignal(onStop func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	sig := <-sigCh
	fmt.Printf("\nReceived %v => closing Loupedeck connection and stopping...\n", sig)
	if onStop != nil {
		onStop()
	}
}

func main() {
	args := parseArgs(os.Args)

	switch args.command {
	case "version":
		fmt.Println(formatVersion())
		return
	case "help":
		printHelp()
		return
	case "validate":
		os.Exit(runValidate(args))
	case "emulate", "emulator":
		fmt.Println(formatVersion())
		dryRun := !args.live
		srv, err := emulator.StartEmulatorServer(emulator.ServerOptions{
			Config: args.config,
			Port:   args.port,
			DryRun: &dryRun,
			Model:  args.model,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start emulator server: %v\n", err)
			os.Exit(1)
		}
		waitForSignal(func() {
			_ = srv.Close()
		})
	default:
		fmt.Println(formatVersion())
		handler, err := commander.NewBaseLoupeDeckHandler(args.config, commander.HandlerOptions{
			ExitOnStop: false,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize Loupedeck Commander: %v\n", err)
			os.Exit(1)
		}
		if err := handler.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting Loupedeck Commander: %v\n", err)
			os.Exit(1)
		}
		waitForSignal(func() {
			_ = handler.Stop()
		})
	}
}
