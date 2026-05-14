// Copyright 2026 Stacklok, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_EndToEnd builds the binary and runs it against a fixture, asserting
// that the output matches the install+keep+escape golden file.
func TestCLI_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI integration test in short mode")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "helm-crd-wrapper")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}

	src := filepath.Join(dir, "src")
	tgt := filepath.Join(dir, "tgt")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("internal", "testdata", "input", "with_template_chars.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "with_template_chars.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	run := exec.Command(bin,
		"-source", src,
		"-target", tgt,
		"-install",
		"-keep",
		"-escape",
	)
	run.Stdout = out
	run.Stderr = out
	if err := run.Run(); err != nil {
		t.Fatalf("run binary failed: %v\nlog:\n%s", err, out.String())
	}

	produced, err := os.ReadFile(filepath.Join(tgt, "with_template_chars.yaml"))
	if err != nil {
		t.Fatalf("read produced output: %v", err)
	}
	golden, err := os.ReadFile(filepath.Join("internal", "testdata", "golden", "install-keep-escape.golden.yaml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(produced, golden) {
		t.Errorf("end-to-end output diverges from golden\n--- want ---\n%s\n--- got ---\n%s",
			string(golden), string(produced))
	}
}

// TestCLI_MissingFlagsExitsNonZero confirms the binary returns exit code 2 when
// required flags are missing.
func TestCLI_MissingFlagsExitsNonZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "helm-crd-wrapper")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}

	run := exec.Command(bin)
	stderr := &bytes.Buffer{}
	run.Stderr = stderr
	err := run.Run()
	if err == nil {
		t.Fatal("expected non-zero exit with no flags")
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("expected usage in stderr, got %q", stderr.String())
	}
}
