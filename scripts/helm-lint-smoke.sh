#!/usr/bin/env bash
# helm-lint-smoke.sh runs helm-crd-wrapper against the testdata fixtures and
# renders the resulting templates through `helm template` to confirm they
# parse as valid Helm. This is the regression gate for the
# escapeTemplateDelimiters bug class.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v helm >/dev/null 2>&1; then
  echo "helm is required for the smoke test" >&2
  exit 1
fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

chart_dir="$workdir/chart"
mkdir -p "$chart_dir/templates"

cat > "$chart_dir/Chart.yaml" <<'EOF'
apiVersion: v2
name: helm-crd-wrapper-smoke
description: Smoke-test chart for wrapped CRDs
type: application
version: 0.0.0
appVersion: "0.0.0"
EOF

cat > "$chart_dir/values.yaml" <<'EOF'
crds:
  keep: true
  install:
    server: true
    virtualMcp: true
features:
  core: true
EOF

go build -o "$workdir/helm-crd-wrapper" .

"$workdir/helm-crd-wrapper" \
  -source internal/testdata/input \
  -target "$chart_dir/templates" \
  -keep \
  -escape \
  -values-prefix .Values.crds.install \
  -verbose

# helm template should render without error. Output is discarded; failures
# (parse errors, unescaped delimiters, etc.) surface as non-zero exit.
helm template smoke-release "$chart_dir" >/dev/null
echo "helm template rendered successfully"
