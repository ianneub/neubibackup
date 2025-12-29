package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfigTemplate_IsValidYAML(t *testing.T) {
	var parsed map[string]interface{}
	err := yaml.Unmarshal([]byte(DefaultConfigTemplate), &parsed)
	if err != nil {
		t.Fatalf("DefaultConfigTemplate is not valid YAML: %v", err)
	}
}

func TestDefaultConfigTemplate_HasRequiredSections(t *testing.T) {
	requiredSections := []string{
		"version:",
		"schedule:",
		"repository:",
		"backup:",
		"healthchecks:",
		"pushover:",
		"tailscale:",
	}

	for _, section := range requiredSections {
		if !strings.Contains(DefaultConfigTemplate, section) {
			t.Errorf("DefaultConfigTemplate missing required section: %s", section)
		}
	}
}

func TestDefaultConfigTemplate_HasExamples(t *testing.T) {
	examples := []string{
		"rest:https://",    // REST server example
		"time:",            // Schedule time
		"paths:",           // Backup paths
		"excludes:",        // Excludes
		"ping_url:",        // Healthchecks URL
		"user_key:",        // Pushover user key
		"auth_key:",        // Tailscale auth key
	}

	for _, example := range examples {
		if !strings.Contains(DefaultConfigTemplate, example) {
			t.Errorf("DefaultConfigTemplate missing example/field: %s", example)
		}
	}
}

func TestDefaultConfigTemplate_HasHelpfulComments(t *testing.T) {
	comments := []string{
		"# NeubiBackup Configuration",
		"# Documentation:",
		"# 24-hour format",
		"# macOS",
		"# Windows",
	}

	for _, comment := range comments {
		if !strings.Contains(DefaultConfigTemplate, comment) {
			t.Errorf("DefaultConfigTemplate missing helpful comment: %s", comment)
		}
	}
}

func TestDefaultConfigTemplate_CanBeLoaded(t *testing.T) {
	// Verify the template can be loaded as a Config struct
	var cfg Config
	err := yaml.Unmarshal([]byte(DefaultConfigTemplate), &cfg)
	if err != nil {
		t.Fatalf("DefaultConfigTemplate cannot be loaded as Config: %v", err)
	}

	// Verify basic structure
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if cfg.Schedule.Time == "" {
		t.Error("Schedule.Time should not be empty")
	}
}

func TestDefaultConfigTemplate_DefaultValues(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(DefaultConfigTemplate), &cfg); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Check healthchecks defaults
	if cfg.Healthchecks.Enabled != false {
		t.Error("Healthchecks should be disabled by default")
	}
	if cfg.Healthchecks.SendLogsOnFailure != true {
		t.Error("Healthchecks.SendLogsOnFailure should be true by default")
	}

	// Check pushover defaults
	if cfg.Pushover.Enabled != false {
		t.Error("Pushover should be disabled by default")
	}
	if cfg.Pushover.OnSuccess != false {
		t.Error("Pushover.OnSuccess should be false by default")
	}
	if cfg.Pushover.OnFailure != true {
		t.Error("Pushover.OnFailure should be true by default")
	}

	// Check tailscale defaults
	if cfg.Tailscale.Enabled != false {
		t.Error("Tailscale should be disabled by default")
	}
	if cfg.Tailscale.Hostname != "neubibackup" {
		t.Errorf("Tailscale.Hostname = %q, want 'neubibackup'", cfg.Tailscale.Hostname)
	}
}

func TestDefaultConfigTemplate_HasCommonExcludes(t *testing.T) {
	commonExcludes := []string{
		".DS_Store",
		"node_modules",
		".git",
		"__pycache__",
	}

	for _, exclude := range commonExcludes {
		if !strings.Contains(DefaultConfigTemplate, exclude) {
			t.Errorf("DefaultConfigTemplate missing common exclude: %s", exclude)
		}
	}
}

func TestDefaultConfigTemplate_ResticArgsSection(t *testing.T) {
	if !strings.Contains(DefaultConfigTemplate, "restic_args:") {
		t.Error("DefaultConfigTemplate missing restic_args section")
	}
	if !strings.Contains(DefaultConfigTemplate, "--verbose") {
		t.Error("DefaultConfigTemplate should include --verbose example")
	}
	if !strings.Contains(DefaultConfigTemplate, "--one-file-system") {
		t.Error("DefaultConfigTemplate should mention --one-file-system")
	}
}
