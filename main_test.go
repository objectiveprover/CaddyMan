package main

import (
	"encoding/json"
	"testing"

	"caddyman/internal/config"
)

// The template built into the binary is the current config version.
func TestEmbeddedTemplateVersion(t *testing.T) {
	var c struct{ Version int }
	if err := json.Unmarshal(configTemplate, &c); err != nil {
		t.Fatal(err)
	}
	if c.Version != config.CurrentVersion {
		t.Errorf("embedded template version %d, CurrentVersion %d", c.Version, config.CurrentVersion)
	}
}
