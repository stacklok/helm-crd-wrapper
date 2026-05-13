// Copyright 2026 Stacklok, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package wrapper

import "testing"

func TestEscapeTemplateDelimiters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text passes through",
			in:   "  description: hello world",
			want: "  description: hello world",
		},
		{
			name: "double curly is escaped",
			in:   "  description: use {{ .Values.x }} please",
			want: `  description: use {{ "{{" }} .Values.x {{ "}}" }} please`,
		},
		{
			name: "intentional Helm directive {{- if preserved",
			in:   "{{- if .Values.crds.install.server }}",
			want: "{{- if .Values.crds.install.server }}",
		},
		{
			name: "intentional Helm directive referencing .Values preserved",
			in:   "{{ .Values.crds.keep }}",
			want: "{{ .Values.crds.keep }}",
		},
		{
			name: "no opening braces left untouched even with closing",
			in:   "description: trailing }} alone",
			want: "description: trailing }} alone",
		},
		{
			name: "multiple occurrences on one line",
			in:   "txt {{ a }} and {{ b }}",
			want: `txt {{ "{{" }} a {{ "}}" }} and {{ "{{" }} b {{ "}}" }}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EscapeTemplateDelimiters(tt.in)
			if got != tt.want {
				t.Errorf("EscapeTemplateDelimiters(%q)\n  got:  %q\n  want: %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildFeatureCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		flags  []string
		prefix string
		want   string
	}{
		{name: "no flags returns empty", flags: nil, prefix: DefaultValuesPrefix, want: ""},
		{
			name:   "single flag with default prefix",
			flags:  []string{"server"},
			prefix: DefaultValuesPrefix,
			want:   ".Values.crds.install.server",
		},
		{
			name:   "multiple flags OR-joined",
			flags:  []string{"server", "virtualMcp"},
			prefix: DefaultValuesPrefix,
			want:   "or .Values.crds.install.server .Values.crds.install.virtualMcp",
		},
		{
			name:   "custom prefix is honoured",
			flags:  []string{"core"},
			prefix: ".Values.features",
			want:   ".Values.features.core",
		},
		{
			name:   "empty prefix falls back to default",
			flags:  []string{"server"},
			prefix: "",
			want:   ".Values.crds.install.server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BuildFeatureCondition(tt.flags, tt.prefix)
			if got != tt.want {
				t.Errorf("BuildFeatureCondition(%v, %q) = %q, want %q", tt.flags, tt.prefix, got, tt.want)
			}
		})
	}
}
