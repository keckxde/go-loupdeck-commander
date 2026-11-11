package interfaces

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
)

type HTTPif struct {
	BaseIf
	Client *http.Client
}

func NewHTTPif() *HTTPif {
	return &HTTPif{
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (h *HTTPif) Check(options map[string]interface{}) int {
	res := h.BaseIf.Check(options)
	if res < 0 {
		h.LogError("HTTPif: mandatory parameter missing\n")
		return res
	}
	if _, ok := options["hostname"]; !ok {
		h.LogError("HTTPif: mandatory parameter hostname missing\n")
		return -21
	}
	return 0
}

func (h *HTTPif) Stop() error {
	h.LogInfo("HTTPif: Stopped\n")
	return nil
}

func (h *HTTPif) Call(url1 string, options map[string]interface{}) (bool, error) {
	res := h.Check(options)
	dryRun := h.GetDryRun()
	if res < 0 {
		h.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "http",
			Status:    "error",
			Simulated: dryRun,
			Raw:       url1,
			Formatted: "",
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("Check failed with code %d", res),
		})
		return false, nil
	}

	urlFormatted, ok := h.BaseCall(url1, options, nil)
	if !ok {
		return false, nil
	}

	parsedURL, err := url.Parse(urlFormatted)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		errMsg := "Invalid URL"
		if err != nil {
			errMsg = err.Error()
		}
		h.LogError(fmt.Sprintf("HTTPif: error with URL: %s\n", errMsg))
		h.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "http",
			Status:    "error",
			Simulated: dryRun,
			Raw:       url1,
			Formatted: urlFormatted,
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("URL Error: %s", errMsg),
		})
		return false, err
	}

	if dryRun {
		h.LogInfo(fmt.Sprintf("HTTPif [Simulated]: GET %s\n", parsedURL.String()))
		h.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "http",
			Status:    "ok",
			Simulated: true,
			Raw:       url1,
			Formatted: parsedURL.String(),
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("[Simulated GET] %s", parsedURL.String()),
		})
		return true, nil
	}

	req, err := http.NewRequest(http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return false, err
	}
	user := config.ToString(options["username"])
	pass := config.ToString(options["password"])
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		h.LogError(fmt.Sprintf("HTTPif: GET error for %s: %v\n", parsedURL.String(), err))
		h.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "http",
			Status:    "error",
			Simulated: false,
			Raw:       url1,
			Formatted: parsedURL.String(),
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("HTTP GET failed for %s", parsedURL.String()),
		})
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		h.EmitActivity(ActivityEntry{
			Timestamp: time.Now().UnixMilli(),
			Interface: "http",
			Status:    "error",
			Simulated: false,
			Raw:       url1,
			Formatted: parsedURL.String(),
			ButtonID:  options["id"],
			State:     config.ToString(options["state"]),
			Event:     config.ToString(options["event"]),
			Message:   fmt.Sprintf("HTTP GET failed for %s", parsedURL.String()),
		})
		return false, nil
	}

	h.EmitActivity(ActivityEntry{
		Timestamp: time.Now().UnixMilli(),
		Interface: "http",
		Status:    "ok",
		Simulated: false,
		Raw:       url1,
		Formatted: parsedURL.String(),
		ButtonID:  options["id"],
		State:     config.ToString(options["state"]),
		Event:     config.ToString(options["event"]),
		Message:   fmt.Sprintf("GET %s", parsedURL.String()),
	})
	return true, nil
}
