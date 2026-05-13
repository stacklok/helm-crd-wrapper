// Copyright 2026 Stacklok, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wrapper

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config models the optional -config YAML file that declares per-CRD rules.
//
// When Strict is true, any CRD present in the source directory that is not
// listed under CRDs causes an error. When false (the default), unlisted CRDs
// fall back to the CLI-flag defaults.
type Config struct {
	Strict bool                 `yaml:"strict"`
	CRDs   map[string]CRDConfig `yaml:"crds"`
}

// CRDConfig is the per-CRD override block. FeatureFlags is the list of
// `<prefix>.<flag>` keys that gate installation: a single entry renders as
// `<prefix>.<flag>`, multiple entries render as `or <prefix>.<a> <prefix>.<b>`.
//
// Note: the `keep` annotation toggle is intentionally NOT per-CRD. Whether or
// not the helm.sh/resource-policy: keep annotation is injected is a global
// decision controlled by the -keep CLI flag, and chart consumers can still
// turn it off at render time via .Values.crds.keep.
type CRDConfig struct {
	FeatureFlags []string `yaml:"featureFlags"`
}

// LoadConfig reads and parses a config file. An empty path returns a zero-value
// Config so the caller can use the same code path whether or not -config was
// supplied.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return &Config{}, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

// ResolvedRule is the per-CRD plan the wrapper executes after merging defaults
// with config overrides.
type ResolvedRule struct {
	FeatureFlags []string
	Keep         bool
	Escape       bool
}

// Defaults captures the CLI-flag defaults used when a CRD has no per-CRD
// override.
type Defaults struct {
	FeatureFlags []string
	Keep         bool
	Escape       bool
}

// Resolve produces the effective rule for a CRD given the loaded config and
// the CLI-flag defaults. crdName is the full `metadata.name` of the CRD (e.g.
// `mcpservers.toolhive.stacklok.dev`). Resolve enforces strict mode: when
// Config.Strict is true and the CRD has no entry, Resolve returns an error.
func (c *Config) Resolve(crdName string, defaults Defaults) (ResolvedRule, error) {
	rule := ResolvedRule{
		FeatureFlags: defaults.FeatureFlags,
		Keep:         defaults.Keep,
		Escape:       defaults.Escape,
	}

	entry, ok := c.CRDs[crdName]
	if !ok {
		if c.Strict {
			return ResolvedRule{}, fmt.Errorf("CRD %q has no entry in config (strict mode)", crdName)
		}
		return rule, nil
	}

	if entry.FeatureFlags != nil {
		rule.FeatureFlags = entry.FeatureFlags
	}
	return rule, nil
}
