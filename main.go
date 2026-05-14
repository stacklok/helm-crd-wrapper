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

// Command helm-crd-wrapper wraps Kubernetes CRD YAML files with a Helm
// install gate ({{- if .Values.crds.install }}) and an optional
// helm.sh/resource-policy: keep annotation, plus template-delimiter
// escaping for CRD descriptions.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/stacklok/helm-crd-wrapper/internal/wrapper"
)

// build-time injected by goreleaser ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		sourceDir    = flag.String("source", "", "Source directory containing raw CRD YAML files (required)")
		targetDir    = flag.String("target", "", "Target directory for wrapped Helm templates (required)")
		install      = flag.Bool("install", true, "Wrap each CRD in {{- if .Values.crds.install }} ... {{- end }}")
		keep         = flag.Bool("keep", true, "Inject helm.sh/resource-policy: keep annotation")
		escape       = flag.Bool("escape", true, "Escape literal {{ and }} in CRD content")
		templatesDir = flag.String("templates-dir", "", "Override embedded templates from disk")
		verbose      = flag.Bool("verbose", false, "Enable verbose output")
		showVersion  = flag.Bool("version", false, "Print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: helm-crd-wrapper -source <dir> -target <dir> [flags]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("helm-crd-wrapper %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	if *sourceDir == "" || *targetDir == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := wrapper.Run(wrapper.Options{
		SourceDir:    *sourceDir,
		TargetDir:    *targetDir,
		TemplatesDir: *templatesDir,
		Verbose:      *verbose,
		Rule: wrapper.Rule{
			Install: *install,
			Keep:    *keep,
			Escape:  *escape,
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
