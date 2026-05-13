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
	"testing"
)

// goldenCase captures one wrapping scenario. Each case lives as a pair of
// input/<file>.yaml + golden/<name>.golden.yaml under internal/testdata.
type goldenCase struct {
	name   string
	input  string // path under internal/testdata/
	rule   ResolvedRule
	prefix string
}

func TestGolden(t *testing.T) {
	t.Parallel()
	cases := []goldenCase{
		{
			name:  "keep-existing-annotations",
			input: "input/with_annotations.yaml",
			rule:  ResolvedRule{Keep: true, Escape: false},
		},
		{
			name:  "keep-missing-annotations",
			input: "input/no_annotations.yaml",
			rule:  ResolvedRule{Keep: true, Escape: false},
		},
		{
			name:  "escape-template-chars",
			input: "input/with_template_chars.yaml",
			rule:  ResolvedRule{Escape: true},
		},
		{
			name:  "single-feature-flag",
			input: "input/with_annotations.yaml",
			rule:  ResolvedRule{FeatureFlags: []string{"server"}, Keep: true, Escape: false},
		},
		{
			name:  "multi-feature-flag",
			input: "input/with_annotations.yaml",
			rule:  ResolvedRule{FeatureFlags: []string{"server", "virtualMcp"}, Keep: true, Escape: false},
		},
		{
			name:  "passthrough",
			input: "input/with_annotations.yaml",
			rule:  ResolvedRule{Escape: true},
		},
		{
			name:   "custom-prefix-and-flag",
			input:  "input/no_annotations.yaml",
			rule:   ResolvedRule{FeatureFlags: []string{"core"}, Keep: false, Escape: true},
			prefix: ".Values.features",
		},
	}

	tmpls, err := loadTemplates("")
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, err := os.ReadFile(filepath.Join("..", "testdata", tc.input))
			if err != nil {
				t.Fatalf("read input: %v", err)
			}
			prefix := tc.prefix
			if prefix == "" {
				prefix = DefaultValuesPrefix
			}
			got, err := WrapContent(in, tmpls, tc.rule, prefix)
			if err != nil {
				t.Fatalf("WrapContent: %v", err)
			}
			goldenPath := filepath.Join("..", "testdata", "golden", tc.name+".golden.yaml")
			if *updateGolden {
				if err := os.WriteFile(goldenPath, got, 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run `task test-update-golden` to create): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s",
					tc.name, string(want), string(got))
			}
		})
	}
}
