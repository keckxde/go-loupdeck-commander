package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestParseTypedAppConfigAndProfiles verifies that the repository's config.yaml
// and referenced profile files parse, validate, and populate ApplicationConfig.Schema
// and each Profile.Schema.
func TestParseTypedAppConfigAndProfiles(t *testing.T) {
	appCfgPath := filepath.Join("..", "..", "config", "config.yaml")
	doc, err := ParseTypedAppConfigFile(appCfgPath)
	if err != nil {
		t.Fatalf("expected valid typed app config, got error: %v", err)
	}
	if doc.Application != "SmartHome Hub" {
		t.Fatalf("unexpected application name: %q", doc.Application)
	}
	if len(doc.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(doc.Profiles))
	}

	app := NewApplicationConfig()
	if !app.LoadFromFile(appCfgPath) {
		t.Fatalf("ApplicationConfig.LoadFromFile(%s) failed", appCfgPath)
	}
	if app.Schema == nil {
		t.Fatalf("expected app.Schema to be populated")
	}
	for _, prof := range app.Profiles {
		if prof.Schema == nil {
			t.Fatalf("expected profile %q Schema to be populated", prof.Name)
		}
	}
}

// TestTypedConfigRejectsInvalidDocuments verifies that strict YAML/JSON decoding
// and validation reject unknown fields, type mismatches, duplicate profile names,
// invalid hex colors, invalid filter enums, inverted min/max bounds, and empty control states.
func TestTypedConfigRejectsInvalidDocuments(t *testing.T) {
	appCases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "unknown top-level field",
			yaml: `
application: "Demo"
unknownAttribute: true
profiles:
  - name: "lights"
    file: "config/profile-lights.yaml"
`,
			wantErr: "field unknownAttribute not found",
		},
		{
			name: "wrong type for columns",
			yaml: `
application: "Demo"
columns: "four"
profiles:
  - name: "lights"
    file: "config/profile-lights.yaml"
`,
			wantErr: "cannot unmarshal !!str `four` into int",
		},
		{
			name: "duplicate profile name",
			yaml: `
application: "Demo"
profiles:
  - name: "lights"
    file: "config/profile-lights.yaml"
  - name: "lights"
    file: "config/profile-sockets.yaml"
`,
			wantErr: "duplicate profile name \"lights\"",
		},
	}

	for _, tc := range appCases {
		t.Run("App_"+tc.name, func(t *testing.T) {
			_, err := ParseTypedAppConfigBytes([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}

	profileCases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "unknown field in control spec",
			yaml: `
profile: "Test"
parameters:
  hostname: "localhost"
touch:
  center:
    "0":
      invalidProp: 123
      states:
        off:
          color: "#000000"
`,
			wantErr: "field invalidProp not found",
		},
		{
			name: "invalid hex color in state",
			yaml: `
profile: "Test"
parameters:
  hostname: "localhost"
touch:
  center:
    "0":
      states:
        off:
          color: "not-a-hex"
`,
			wantErr: "invalid hex color \"not-a-hex\"",
		},
		{
			name: "invalid filter enum",
			yaml: `
profile: "Test"
parameters:
  hostname: "localhost"
touch:
  center:
    "0":
      params:
        filter: "doubleclick"
      states:
        off:
          color: "#000000"
`,
			wantErr: "invalid event filter \"doubleclick\"",
		},
		{
			name: "min greater than max",
			yaml: `
profile: "Test"
parameters:
  hostname: "localhost"
touch:
  center:
    "0":
      params:
        min: 100
        max: 10
      states:
        off:
          color: "#000000"
`,
			wantErr: "min (100) cannot be greater than max (10)",
		},
		{
			name: "empty states in control",
			yaml: `
profile: "Test"
parameters:
  hostname: "localhost"
touch:
  center:
    "0":
      states: {}
`,
			wantErr: "must define at least one state in 'states'",
		},
	}

	for _, tc := range profileCases {
		t.Run("Profile_"+tc.name, func(t *testing.T) {
			_, err := ParseTypedProfileBytes([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}
