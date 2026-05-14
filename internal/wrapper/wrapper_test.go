// Copyright 2026 Stacklok, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package wrapper

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "regenerate golden files")

func loadTestTemplates(t *testing.T) map[string]string {
	t.Helper()
	tmpls, err := loadTemplates("")
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	return tmpls
}

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

// TestWrapContent_KeepInjectionExistingAnnotations confirms that when an
// `annotations:` block already exists, the keep annotation is injected into
// it without creating a duplicate block.
func TestWrapContent_KeepInjectionExistingAnnotations(t *testing.T) {
	t.Parallel()
	in := readFixture(t, "input/with_annotations.yaml")
	got, err := WrapContent(in, loadTestTemplates(t), Rule{Keep: true})
	if err != nil {
		t.Fatalf("WrapContent: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "helm.sh/resource-policy: keep") {
		t.Error("expected keep annotation in output")
	}
	if strings.Count(s, "annotations:") != 1 {
		t.Errorf("expected exactly one annotations: block, got %d", strings.Count(s, "annotations:"))
	}
	idxAnn := strings.Index(s, "annotations:")
	idxKeep := strings.Index(s, "helm.sh/resource-policy: keep")
	if idxAnn < 0 || idxKeep < idxAnn {
		t.Errorf("keep block not placed after annotations: idxAnn=%d idxKeep=%d", idxAnn, idxKeep)
	}
}

// TestWrapContent_KeepInjectionMissingAnnotations confirms a synthetic
// annotations: block is created above metadata.name when the CRD has none.
func TestWrapContent_KeepInjectionMissingAnnotations(t *testing.T) {
	t.Parallel()
	in := readFixture(t, "input/no_annotations.yaml")
	got, err := WrapContent(in, loadTestTemplates(t), Rule{Keep: true})
	if err != nil {
		t.Fatalf("WrapContent: %v", err)
	}
	s := string(got)
	if strings.Count(s, "annotations:") != 1 {
		t.Errorf("expected exactly one annotations: block, got %d", strings.Count(s, "annotations:"))
	}
	if !strings.Contains(s, "helm.sh/resource-policy: keep") {
		t.Error("expected keep annotation")
	}
	if strings.Index(s, "annotations:") > strings.Index(s, "name: widgets.example.stacklok.dev") {
		t.Error("annotations: block should appear before name: in metadata")
	}
}

// TestWrapContent_KeepDisabledLeavesContentAlone confirms that with Keep=false,
// no annotations are touched and no synthetic block is added.
func TestWrapContent_KeepDisabledLeavesContentAlone(t *testing.T) {
	t.Parallel()
	in := readFixture(t, "input/no_annotations.yaml")
	got, err := WrapContent(in, loadTestTemplates(t), Rule{})
	if err != nil {
		t.Fatalf("WrapContent: %v", err)
	}
	if strings.Contains(string(got), "annotations:") {
		t.Error("did not expect annotations: block to be added when Keep=false")
	}
	if strings.Contains(string(got), "helm.sh/resource-policy: keep") {
		t.Error("did not expect keep annotation when Keep=false")
	}
}

// TestWrapContent_EscapeToggle confirms escape on/off behaviour.
func TestWrapContent_EscapeToggle(t *testing.T) {
	t.Parallel()
	in := readFixture(t, "input/with_template_chars.yaml")
	tmpls := loadTestTemplates(t)

	on, err := WrapContent(in, tmpls, Rule{Escape: true})
	if err != nil {
		t.Fatalf("escape on: %v", err)
	}
	if !strings.Contains(string(on), `{{ "{{" }}`) {
		t.Error("escape=true should produce escaped open delimiters")
	}

	off, err := WrapContent(in, tmpls, Rule{Escape: false})
	if err != nil {
		t.Fatalf("escape off: %v", err)
	}
	if strings.Contains(string(off), `{{ "{{" }}`) {
		t.Error("escape=false should not escape delimiters")
	}
	if !strings.Contains(string(off), "{{ .steps.first.output }}") {
		t.Error("escape=false should leave raw delimiters in place")
	}
}

// TestWrapContent_InstallWrapsInConditional confirms Install=true emits the
// install gate header/footer.
func TestWrapContent_InstallWrapsInConditional(t *testing.T) {
	t.Parallel()
	in := readFixture(t, "input/with_annotations.yaml")
	got, err := WrapContent(in, loadTestTemplates(t), Rule{Install: true})
	if err != nil {
		t.Fatalf("WrapContent: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, "{{- if .Values.crds.install }}\n") {
		t.Errorf("unexpected header: %q", firstLine(s))
	}
	if !strings.HasSuffix(s, "{{- end }}\n") {
		t.Errorf("expected {{- end }} footer, got tail %q", s[max(0, len(s)-32):])
	}
}

// TestWrapContent_InstallDisabledSkipsHeader confirms Install=false produces
// no Helm wrapper at all.
func TestWrapContent_InstallDisabledSkipsHeader(t *testing.T) {
	t.Parallel()
	in := readFixture(t, "input/with_annotations.yaml")
	got, err := WrapContent(in, loadTestTemplates(t), Rule{})
	if err != nil {
		t.Fatalf("WrapContent: %v", err)
	}
	if strings.Contains(string(got), "{{- if") {
		t.Error("Install=false should not emit a {{- if header")
	}
	if strings.Contains(string(got), "{{- end }}") {
		t.Error("Install=false should not emit a {{- end footer")
	}
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// TestRun_RejectsNonCRDKind exercises the file-walking layer with a
// non-CRD document and confirms it fails fast.
func TestRun_RejectsNonCRDKind(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	tgt := filepath.Join(dir, "tgt")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bad.yaml"), []byte("kind: ConfigMap\nmetadata:\n  name: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{SourceDir: src, TargetDir: tgt, Stdout: discardWriter{}})
	if err == nil {
		t.Fatal("expected error on non-CRD kind")
	}
	if !strings.Contains(err.Error(), "expected CustomResourceDefinition") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_EmptySourceDirFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{SourceDir: src, TargetDir: filepath.Join(dir, "tgt"), Stdout: discardWriter{}})
	if err == nil {
		t.Fatal("expected error on empty source dir")
	}
	if !strings.Contains(err.Error(), "no YAML files") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRun_TemplatesDirOverride confirms -templates-dir replaces embedded
// templates wholesale (header / footer / keep-annotation).
func TestRun_TemplatesDirOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	tgt := filepath.Join(dir, "tgt")
	tdir := filepath.Join(dir, "templates")
	for _, d := range []string{src, tdir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tdir, "header.tpl"), []byte("HEADER\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tdir, "footer.tpl"), []byte("FOOTER\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tdir, "keep-annotation.tpl"), []byte("    KEEP-ANNOTATION\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	in := readFixture(t, "input/with_annotations.yaml")
	if err := os.WriteFile(filepath.Join(src, "with_annotations.yaml"), in, 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run(Options{
		SourceDir:    src,
		TargetDir:    tgt,
		TemplatesDir: tdir,
		Rule:         Rule{Install: true, Keep: true, Escape: false},
		Stdout:       discardWriter{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(tgt, "with_annotations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "HEADER\n") {
		t.Errorf("override header not used: %q", firstLine(s))
	}
	if !strings.HasSuffix(s, "FOOTER\n") {
		t.Errorf("override footer not used")
	}
	if !strings.Contains(s, "KEEP-ANNOTATION") {
		t.Errorf("override keep template not used")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
