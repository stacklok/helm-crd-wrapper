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
			in:   "{{- if .Values.crds.install }}",
			want: "{{- if .Values.crds.install }}",
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
