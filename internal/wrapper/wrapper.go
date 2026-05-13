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

// Package wrapper wraps Kubernetes CRD YAML files with Helm template
// directives: optional feature-flag gating, an optional
// helm.sh/resource-policy: keep annotation, and escaping of literal Go
// template delimiters that appear inside CRD descriptions.
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

// FeatureConditionPlaceholder is the substring inside header.tpl that
// is replaced with the rendered feature-flag expression.
const FeatureConditionPlaceholder = "__FEATURE_CONDITION__"

// DefaultValuesPrefix is the values key prefix used to construct feature-flag
// expressions when -values-prefix is not supplied. It matches the prefix used
// by stacklok/toolhive's operator chart.
const DefaultValuesPrefix = ".Values.crds.install"

// Options is the full input set required to wrap a directory of CRDs.
type Options struct {
	// SourceDir is the directory of raw CRD YAML files.
	SourceDir string
	// TargetDir is where wrapped templates are written.
	TargetDir string
	// Config is the optional per-CRD override config (nil-safe).
	Config *Config
	// Defaults are the CLI-flag defaults applied when a CRD has no override.
	Defaults Defaults
	// ValuesPrefix is prepended to each feature flag (e.g. `.Values.crds.install`).
	ValuesPrefix string
	// TemplatesDir, if non-empty, loads header/footer/keep-annotation templates
	// from disk instead of the embedded ones.
	TemplatesDir string
	// Verbose enables progress output to Stdout.
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
	if opts.ValuesPrefix == "" {
		opts.ValuesPrefix = DefaultValuesPrefix
	}
	if opts.Config == nil {
		opts.Config = &Config{}
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
	}

	rule, err := opts.Config.Resolve(crdName, opts.Defaults)
	if err != nil {
		return err
	}
	if opts.Verbose {
		fmt.Fprintf(out, "  Feature flags: %v\n", rule.FeatureFlags)
		fmt.Fprintf(out, "  Keep: %t  Escape: %t\n", rule.Keep, rule.Escape)
	}

	wrapped, err := WrapContent(content, tmpls, rule, opts.ValuesPrefix)
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

// WrapContent applies the resolved rule to a single CRD YAML document and
// returns the wrapped bytes. It is exposed for unit and golden-file tests.
func WrapContent(content []byte, tmpls map[string]string, rule ResolvedRule, valuesPrefix string) ([]byte, error) {
	var buf bytes.Buffer

	condition := BuildFeatureCondition(rule.FeatureFlags, valuesPrefix)
	useHeader := condition != ""
	if useHeader {
		header := strings.ReplaceAll(tmpls["header"], FeatureConditionPlaceholder, condition)
		buf.WriteString(header)
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	// Allow long lines: CRD descriptions can be very long.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	skipFirstLine := bytes.HasPrefix(content, []byte("---\n")) || bytes.HasPrefix(content, []byte("---\r\n"))
	annotationsBlockFound := false
	annotationsInjected := false
	keepInjected := false
	annotationsIndent := ""
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

		// No annotations: line in metadata — create one before the `name:`
		// field. CRDs always have metadata.name, so this is a reliable
		// insertion point. Captured indent on the metadata block is used to
		// indent the synthesised `annotations:` and `keep` block.
		if rule.Keep && !keepInjected && !annotationsBlockFound {
			if indent, ok := metadataNameLineIndent(raw); ok {
				annotationsIndent = indent
				buf.WriteString(annotationsIndent + "annotations:\n")
				buf.WriteString(tmpls["keep-annotation"])
				keepInjected = true
				annotationsInjected = true
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
	_ = annotationsInjected // for future diagnostic use

	if useHeader {
		buf.WriteString(tmpls["footer"])
	}
	return buf.Bytes(), nil
}

// metadataNameLineIndent returns the leading indent string of a `name:` line
// that sits directly underneath the `metadata:` block. Detection is based on
// the conventional two-space indent emitted by controller-gen. Only the
// metadata.name line qualifies — other `name:` lines (e.g. in spec.names) are
// indented deeper.
func metadataNameLineIndent(line string) (string, bool) {
	const conventional = "  " // two-space indent under metadata
	if !strings.HasPrefix(line, conventional+"name:") {
		return "", false
	}
	return conventional, true
}

// BuildFeatureCondition renders the Helm conditional expression for a set of
// feature flags using the given values prefix. An empty slice yields the empty
// string and signals the wrapper to skip the header/footer entirely.
func BuildFeatureCondition(flags []string, valuesPrefix string) string {
	if len(flags) == 0 {
		return ""
	}
	if valuesPrefix == "" {
		valuesPrefix = DefaultValuesPrefix
	}
	refs := make([]string, len(flags))
	for i, f := range flags {
		refs[i] = valuesPrefix + "." + f
	}
	if len(refs) == 1 {
		return refs[0]
	}
	return "or " + strings.Join(refs, " ")
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
