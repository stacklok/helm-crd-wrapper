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

import "strings"

// EscapeTemplateDelimiters escapes literal {{ and }} found inside a single
// line of CRD YAML so that Helm does not interpret CRD description text as
// template directives. Helm understands the rendered form `{{ "{{" }}` as a
// literal `{{`.
//
// Lines that are intentional Helm directives (those starting with `{{-` or
// that already reference `.Values`) are passed through untouched so the
// wrapper's own template scaffolding survives the pass.
func EscapeTemplateDelimiters(line string) string {
	if !strings.Contains(line, "{{") {
		return line
	}

	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "{{-") ||
		(strings.HasPrefix(trimmed, "{{") && strings.Contains(trimmed, ".Values")) {
		return line
	}

	const (
		openSentinel  = "\x00OPEN\x00"
		closeSentinel = "\x00CLOSE\x00"
	)
	line = strings.ReplaceAll(line, "{{", openSentinel)
	line = strings.ReplaceAll(line, "}}", closeSentinel)
	line = strings.ReplaceAll(line, openSentinel, `{{ "{{" }}`)
	line = strings.ReplaceAll(line, closeSentinel, `{{ "}}" }}`)
	return line
}
