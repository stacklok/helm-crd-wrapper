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

// Package wrapper wraps Kubernetes CRD YAML files with two independent Helm
// template concerns: an `{{- if .Values.crds.install }}` install gate, and a
// `helm.sh/resource-policy: keep` annotation. Literal Go template delimiters
// in CRD descriptions are escaped so Helm does not try to interpret them.
package wrapper

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed templates/header.tpl templates/footer.tpl templates/keep-annotation.tpl
var embeddedTemplates embed.FS

// templateNames is the canonical list of template files the wrapper expects.
// Both embedded and on-disk overrides must provide all three.
var templateNames = []string{"header", "footer", "keep-annotation"}

// Rule controls the wrapping decisions applied to a single CRD.
type Rule struct {
	// Install wraps the CRD in `{{- if .Values.crds.install }} ... {{- end }}`
	// using the header/footer templates.
	Install bool
	// Keep injects the keep-annotation template under metadata.annotations.
	Keep bool
	// Escape applies template-delimiter escaping to every line of CRD content.
	Escape bool
}

// Options is the full input set required to wrap a directory of CRDs.
type Options struct {
	// SourceDir is the directory of raw CRD YAML files.
	SourceDir string
	// TargetDir is where wrapped templates are written.
	TargetDir string
	// Rule controls the wrapping decisions; the same rule is applied to every
	// CRD found in SourceDir.
	Rule Rule
	// TemplatesDir, if non-empty, loads header/footer/keep-annotation templates
	// from disk instead of the embedded ones.
	TemplatesDir string
	// Verbose enables progress output.
	Verbose bool
	// Stdout is the writer for progress messages (defaults to os.Stdout).
	Stdout io.Writer
}

// Run is the top-level entry point: it loads templates, walks the source
// directory, and writes a wrapped copy of every CRD into the target directory.
func Run(opts Options) error {
	if opts.SourceDir == "" {
		return errors.New("source directory is required")
	}
	if opts.TargetDir == "" {
		return errors.New("target directory is required")
	}
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}

	tmpls, err := loadTemplates(opts.TemplatesDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(opts.TargetDir, 0o750); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	cleanSource := filepath.Clean(opts.SourceDir)
	files, err := filepath.Glob(filepath.Join(cleanSource, "*.yaml"))
	if err != nil {
		return fmt.Errorf("glob source files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no YAML files found in %s", opts.SourceDir)
	}

	fmt.Fprintf(out, "Found %d CRD files to process\n", len(files))

	for _, file := range files {
		if err := wrapFile(file, cleanSource, opts.TargetDir, tmpls, opts, out); err != nil {
			return fmt.Errorf("wrap %s: %w", file, err)
		}
	}

	fmt.Fprintln(out, "CRD wrapping completed successfully")
	return nil
}

func wrapFile(sourcePath, sourceDir, targetDir string, tmpls map[string]string, opts Options, out io.Writer) error {
	filename := filepath.Base(sourcePath)
	fmt.Fprintf(out, "Processing: %s\n", filename)

	cleanPath := filepath.Clean(sourcePath)
	if !strings.HasPrefix(cleanPath, sourceDir+string(filepath.Separator)) && cleanPath != sourceDir {
		return fmt.Errorf("source path escapes source directory: %s", sourcePath)
	}

	content, err := os.ReadFile(cleanPath) //nolint:gosec // path is sanitized above
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	crdName, err := extractCRDName(content)
	if err != nil {
		return fmt.Errorf("extract CRD name: %w", err)
	}
	if opts.Verbose {
		fmt.Fprintf(out, "  CRD name: %s\n", crdName)
		fmt.Fprintf(out, "  Install: %t  Keep: %t  Escape: %t\n", opts.Rule.Install, opts.Rule.Keep, opts.Rule.Escape)
	}

	wrapped, err := WrapContent(content, tmpls, opts.Rule)
	if err != nil {
		return fmt.Errorf("wrap content: %w", err)
	}

	targetPath := filepath.Join(targetDir, filename)
	if err := os.WriteFile(targetPath, wrapped, 0o600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	fmt.Fprintf(out, "  Created: %s\n", targetPath)
	return nil
}

// WrapContent applies the rule to a single CRD YAML document and returns the
// wrapped bytes. It is exposed for unit and golden-file tests.
func WrapContent(content []byte, tmpls map[string]string, rule Rule) ([]byte, error) {
	var buf bytes.Buffer

	if rule.Install {
		buf.WriteString(tmpls["header"])
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	// CRD descriptions can be very long; raise the scanner's max token size.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	skipFirstLine := bytes.HasPrefix(content, []byte("---\n")) || bytes.HasPrefix(content, []byte("---\r\n"))
	annotationsBlockFound := false
	keepInjected := false
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()

		if lineNum == 1 && skipFirstLine && strings.TrimSpace(raw) == "---" {
			continue
		}

		trimmed := strings.TrimSpace(raw)

		// Inject keep into an existing annotations block.
		if rule.Keep && !keepInjected && trimmed == "annotations:" {
			buf.WriteString(raw + "\n")
			buf.WriteString(tmpls["keep-annotation"])
			annotationsBlockFound = true
			keepInjected = true
			continue
		}

		// No annotations: line under metadata — create one before the
		// metadata.name line. CRDs always have metadata.name, so this is a
		// reliable insertion point.
		if rule.Keep && !keepInjected && !annotationsBlockFound {
			if indent, ok := metadataNameLineIndent(raw); ok {
				buf.WriteString(indent + "annotations:\n")
				buf.WriteString(tmpls["keep-annotation"])
				keepInjected = true
				// fall through to write the `name:` line itself
			}
		}

		if rule.Escape {
			buf.WriteString(EscapeTemplateDelimiters(raw) + "\n")
		} else {
			buf.WriteString(raw + "\n")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan content: %w", err)
	}

	if rule.Keep && !keepInjected {
		return nil, fmt.Errorf("could not locate insertion point for keep annotation: no metadata.name field found")
	}

	if rule.Install {
		buf.WriteString(tmpls["footer"])
	}
	return buf.Bytes(), nil
}

// metadataNameLineIndent returns the leading indent string of a `name:` line
// that sits directly underneath the `metadata:` block. Detection is based on
// the conventional two-space indent emitted by controller-gen. Other `name:`
// lines (e.g. spec.names.singular) are indented deeper and do not match.
func metadataNameLineIndent(line string) (string, bool) {
	const conventional = "  "
	if !strings.HasPrefix(line, conventional+"name:") {
		return "", false
	}
	return conventional, true
}

func loadTemplates(dir string) (map[string]string, error) {
	tmpls := make(map[string]string, len(templateNames))
	for _, name := range templateNames {
		var (
			data []byte
			err  error
		)
		if dir != "" {
			data, err = os.ReadFile(filepath.Join(dir, name+".tpl")) //nolint:gosec // path is operator-supplied
		} else {
			data, err = embeddedTemplates.ReadFile("templates/" + name + ".tpl")
		}
		if err != nil {
			return nil, fmt.Errorf("load %s template: %w", name, err)
		}
		tmpls[name] = string(data)
	}
	return tmpls, nil
}

func extractCRDName(content []byte) (string, error) {
	var crd struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(content, &crd); err != nil {
		return "", fmt.Errorf("parse YAML: %w", err)
	}
	if crd.Kind != "CustomResourceDefinition" {
		return "", fmt.Errorf("expected CustomResourceDefinition, got %q", crd.Kind)
	}
	if crd.Metadata.Name == "" {
		return "", errors.New("CRD metadata.name is empty")
	}
	return crd.Metadata.Name, nil
}
