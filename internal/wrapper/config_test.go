// Copyright 2026 Stacklok, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return p
}

func TestLoadConfig_EmptyPathReturnsZeroValue(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil zero config")
	}
	if cfg.Strict || len(cfg.CRDs) != 0 {
		t.Errorf("expected zero-value config, got %+v", cfg)
	}
}

func TestLoadConfig_ParsesValidYAML(t *testing.T) {
	t.Parallel()
	body := `
strict: true
crds:
  mcpservers.toolhive.stacklok.dev:
    featureFlags: [server]
  mcpexternalauthconfigs.toolhive.stacklok.dev:
    featureFlags: [server, virtualMcp]
`
	p := writeTempFile(t, "config.yaml", body)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.Strict {
		t.Error("expected strict=true")
	}
	if len(cfg.CRDs) != 2 {
		t.Fatalf("expected 2 CRD entries, got %d", len(cfg.CRDs))
	}
	e1 := cfg.CRDs["mcpservers.toolhive.stacklok.dev"]
	if len(e1.FeatureFlags) != 1 || e1.FeatureFlags[0] != "server" {
		t.Errorf("mcpservers featureFlags = %v", e1.FeatureFlags)
	}
	e2 := cfg.CRDs["mcpexternalauthconfigs.toolhive.stacklok.dev"]
	if len(e2.FeatureFlags) != 2 {
		t.Errorf("mcpexternalauthconfigs featureFlags = %v", e2.FeatureFlags)
	}
}

// TestLoadConfig_PerCRDKeepIsRejected confirms we reject the old shape so users
// migrating from earlier drafts get a clear error rather than a silent no-op.
func TestLoadConfig_PerCRDKeepIsRejected(t *testing.T) {
	t.Parallel()
	body := `
crds:
  foo.example.com:
    featureFlags: [x]
    keep: true
`
	p := writeTempFile(t, "config.yaml", body)
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error for legacy per-CRD keep field")
	}
}

func TestLoadConfig_InvalidYAMLFailsClearly(t *testing.T) {
	t.Parallel()
	p := writeTempFile(t, "config.yaml", "crds: [not valid mapping")
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error parsing invalid YAML")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Errorf("error %q should mention 'parse config'", err)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := LoadConfig("/no/such/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Errorf("error %q should mention 'read config'", err)
	}
}

func TestLoadConfig_UnknownFieldsAreRejected(t *testing.T) {
	t.Parallel()
	body := `
crds:
  foo.example.com:
    notARealField: yes
`
	p := writeTempFile(t, "config.yaml", body)
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestConfigResolve_StrictMissingCRDFails(t *testing.T) {
	t.Parallel()
	cfg := &Config{Strict: true, CRDs: map[string]CRDConfig{}}
	_, err := cfg.Resolve("foo.example.com", Defaults{Keep: true, Escape: true})
	if err == nil {
		t.Fatal("expected strict-mode error")
	}
	if !strings.Contains(err.Error(), "strict mode") {
		t.Errorf("error should mention strict mode, got %q", err)
	}
}

func TestConfigResolve_NonStrictMissingCRDUsesDefaults(t *testing.T) {
	t.Parallel()
	cfg := &Config{CRDs: map[string]CRDConfig{}}
	rule, err := cfg.Resolve("foo.example.com", Defaults{
		FeatureFlags: []string{"core"},
		Keep:         true,
		Escape:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rule.Keep || !rule.Escape || len(rule.FeatureFlags) != 1 || rule.FeatureFlags[0] != "core" {
		t.Errorf("unexpected rule: %+v", rule)
	}
}

func TestConfigResolve_PerCRDFeatureFlagsOverrideDefaults(t *testing.T) {
	t.Parallel()
	cfg := &Config{CRDs: map[string]CRDConfig{
		"foo.example.com": {FeatureFlags: []string{"a", "b"}},
	}}
	rule, err := cfg.Resolve("foo.example.com", Defaults{
		FeatureFlags: []string{"default"},
		Keep:         true,
		Escape:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rule.FeatureFlags) != 2 || rule.FeatureFlags[0] != "a" || rule.FeatureFlags[1] != "b" {
		t.Errorf("featureFlags not overridden: %v", rule.FeatureFlags)
	}
	if !rule.Keep {
		t.Error("keep must inherit from CLI defaults regardless of per-CRD config")
	}
	if !rule.Escape {
		t.Error("escape should still inherit from defaults")
	}
}

// TestConfigResolve_KeepIsGlobalOnly confirms that the CLI's -keep default is
// applied to every CRD; the config file has no say in the matter.
func TestConfigResolve_KeepIsGlobalOnly(t *testing.T) {
	t.Parallel()
	cfg := &Config{CRDs: map[string]CRDConfig{
		"foo.example.com": {FeatureFlags: []string{"x"}},
	}}
	rule, err := cfg.Resolve("foo.example.com", Defaults{Keep: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rule.Keep {
		t.Error("missing keep override should leave default keep=true intact")
	}
}
