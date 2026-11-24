package interfaces

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
)

// shellSanitizeRegex matches shell metacharacters and line breaks stripped from
// interpolated parameter values when shell parameter sanitization is enabled.
var shellSanitizeRegex = regexp.MustCompile("[`$;|&><\\r\\n]")

// SHELLif implements a local shell command execution backend built on BaseIf.
type SHELLif struct {
	BaseIf
}

// NewSHELLif creates and returns a new SHELLif instance.
func NewSHELLif() *SHELLif {
	return &SHELLif{}
}

// sanitizeShellValue strips dangerous shell metacharacters and newlines
// (`$ ; | & > < \r \n) from value to mitigate command injection during
// template parameter substitution.
func sanitizeShellValue(value string) string {
	return shellSanitizeRegex.ReplaceAllString(value, "")
}

// Check validates that options contains all mandatory BaseIf parameters. It
// returns 0 on success or a negative error code from BaseIf.Check.
func (s *SHELLif) Check(options map[string]interface{}) int {
	res := s.BaseIf.Check(options)
	if res < 0 {
		s.LogError("SHELLif: mandatory parameter missing\n")
		return res
	}
	return 0
}

// Stop logs shutdown of the shell interface and returns nil.
func (s *SHELLif) Stop() error {
	s.LogInfo("SHELLif: Stopped\n")
	return nil
}

// Call formats cmd using options and executes the resulting command via
// `sh -c`, unless dry-run mode is active. By default, substituted parameter
// values are sanitized with sanitizeShellValue; setting
// options["sanitizeShellParams"] to false disables this sanitization.
//
// In dry-run mode or on successful execution, Call emits an "ok"
// ActivityEntry and returns a map[string]string containing "stdout" and
// "stderr" along with a nil error. On validation failure it returns (false, nil),
// and on execution failure it emits an "error" ActivityEntry and returns
// (false, err).
func (s *SHELLif) Call(cmd string, options map[string]interface{}) (interface{}, error) {
	res := s.Check(options)
	dryRun := s.GetDryRun()
	if res < 0 {
		s.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "shell",
			Status:    "error",
			Simulated: dryRun,
			Raw:       cmd,
			Formatted: "",
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("Check failed with code %d", res),
		})
		return false, nil
	}

	shouldSanitize := true
	if v, ok := options["sanitizeShellParams"].(bool); ok && !v {
		shouldSanitize = false
	}
	var transform func(string, string) string
	if shouldSanitize {
		transform = func(val string, _ string) string {
			return sanitizeShellValue(val)
		}
	}

	formattedCmd, ok := s.BaseCall(cmd, options, transform)
	if !ok {
		return false, nil
	}

	if dryRun {
		s.LogInfo(fmt.Sprintf("SHELLif [Simulated]: %s\n", formattedCmd))
		s.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "shell",
			Status:    "ok",
			Simulated: true,
			Raw:       cmd,
			Formatted: formattedCmd,
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("[Simulated] %s", formattedCmd),
		})
		return map[string]string{
			"stdout": fmt.Sprintf("[Simulated] %s\n", formattedCmd),
			"stderr": "",
		}, nil
	}

	out, err := exec.Command("sh", "-c", formattedCmd).CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		msg := err.Error()
		if outStr != "" {
			msg = outStr
		}
		s.LogError(fmt.Sprintf("SHELLif: Command execution error (%s): %s\n", formattedCmd, msg))
		s.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "shell",
			Status:    "error",
			Simulated: false,
			Raw:       cmd,
			Formatted: formattedCmd,
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   msg,
		})
		return false, err
	}

	msg := formattedCmd
	if outStr != "" {
		msg = outStr
	}
	s.EmitActivity(ActivityEntry{
		Timestamp: time.Now().UnixMilli(),
		Interface: "shell",
		Status:    "ok",
		Simulated: false,
		Raw:       cmd,
		Formatted: formattedCmd,
		ButtonID:  options["id"],
		State:     config.ToString(options["state"]),
		Event:     config.ToString(options["event"]),
		Message:   msg,
	})
	return map[string]string{
		"stdout": string(out),
		"stderr": "",
	}, nil
}
