package interfaces

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
)

var shellSanitizeRegex = regexp.MustCompile("[`$;|&><\\r\\n]")

type SHELLif struct {
	BaseIf
}

func NewSHELLif() *SHELLif {
	return &SHELLif{}
}

func sanitizeShellValue(value string) string {
	return shellSanitizeRegex.ReplaceAllString(value, "")
}

func (s *SHELLif) Check(options map[string]interface{}) int {
	res := s.BaseIf.Check(options)
	if res < 0 {
		s.LogError("SHELLif: mandatory parameter missing\n")
		return res
	}
	return 0
}

func (s *SHELLif) Stop() error {
	s.LogInfo("SHELLif: Stopped\n")
	return nil
}

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
