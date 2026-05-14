# helm-crd-wrapper

A generic CLI tool that wraps Kubernetes CRD YAML files (typically the output
of `controller-gen`) with Helm template directives so they can be shipped as
upgrade-aware chart templates.

The wrapper applies two independent, globally-configured concerns plus
template-delimiter escaping:

1. **Install gate** — wraps each CRD in `{{- if .Values.crds.install }} ...
   {{- end }}` so consumers can turn CRD installation on or off via
   `values.yaml`.
2. **`helm.sh/resource-policy: keep` annotation** — injected under
   `metadata.annotations` so `helm uninstall` does not cascade-delete every
   custom resource in the cluster. The injected block itself is wrapped in
   `{{- if .Values.crds.keep }}` so chart consumers can still flip it off.
3. **Go-template delimiter escaping** in CRD description text.
   `controller-gen` often emits literal `{{` / `}}` inside docstrings; Helm
   would otherwise try to interpret them and fail to render.

Each toggle is global across the directory of CRDs. There is no per-CRD
configuration — keep the tool's job narrow, make the chart's `values.yaml`
the single source of truth for gating.

## Install

```bash
go install github.com/stacklok/helm-crd-wrapper@latest
```

Or download a release binary from the
[releases page](https://github.com/stacklok/helm-crd-wrapper/releases).

## Usage

```text
helm-crd-wrapper \
  -source <dir>           # required: directory of raw CRD YAML files
  -target <dir>           # required: directory to write wrapped templates
  -install                # wrap each CRD in {{- if <install-value> }}
                          #          (default: true)
  -install-value <expr>   # Helm value path used by the install conditional
                          #          (default: ".Values.crds.install")
  -keep                   # inject helm.sh/resource-policy: keep
                          #          (default: true)
  -keep-value <expr>      # Helm value path used by the keep conditional
                          #          (default: ".Values.crds.keep")
  -escape                 # escape {{ }} in CRD content
                          #          (default: true)
  -templates-dir <dir>    # override embedded templates from disk
  -verbose                # extra logging
```

Exit code `0` on success. `1` on any wrapping error (missing file, invalid
YAML, source path escape, etc.). `2` when required flags are missing.

The typical invocation in CI is just:

```bash
helm-crd-wrapper -source ./crds -target ./templates
```

All three toggles default to true, so you only flip them when you want
something different (e.g. `-install=false` to ship unconditional CRDs).

## How the toggles flow into `values.yaml`

The wrapper makes **build-time** choices that emit Helm template scaffolding;
the chart consumer makes the **render-time** choice via `values.yaml`:

| CLI flag    | Build-time effect                            | Render-time control                                              |
| ----------- | -------------------------------------------- | ---------------------------------------------------------------- |
| `-install` (+ `-install-value`) | Wraps each CRD in `{{- if <install-value> }} ... {{- end }}` | The value at `<install-value>` (default `.Values.crds.install`) in `values.yaml` |
| `-keep` (+ `-keep-value`)       | Injects the keep-annotation block, itself wrapped in `{{- if <keep-value> }}` | The value at `<keep-value>` (default `.Values.crds.keep`) in `values.yaml` |
| `-escape`                       | Rewrites raw `{{`/`}}` in CRD descriptions to Helm-safe literals | n/a (escape is purely a build-time fix-up)                       |

A consumer chart therefore needs:

```yaml
# values.yaml
crds:
  install: true   # render CRDs at all (set false to skip CRD installation)
  keep: true      # add helm.sh/resource-policy: keep annotation
```

The wrapped output looks like:

```yaml
{{- if .Values.crds.install }}
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    {{- if .Values.crds.keep }}
    helm.sh/resource-policy: keep
    {{- end }}
    controller-gen.kubebuilder.io/version: v0.17.3
  name: widgets.example.stacklok.dev
spec:
  ...
{{- end }}
```

## Why no per-CRD configuration?

Both wrapping decisions are properties of the **chart**, not the **CRD**:

- Either every CRD survives `helm uninstall` or none do. Mixed `keep`
  behaviour would leak custom resources whose CRDs got deleted — the exact
  footgun the annotation exists to prevent.
- Either the chart manages CRD installation or it does not. Splitting CRDs
  into install/no-install groups inside a single chart is a smell that
  usually means there should be two charts.

So the tool stays narrow: one binary, two flags, no per-CRD overrides.

## Custom value paths

If `crds.install` / `crds.keep` clash with an existing values schema in your
chart, point the flags at any expression you like:

```bash
helm-crd-wrapper \
  -source ./crds \
  -target ./templates \
  -install-value .Values.installCRDs \
  -keep-value    .Values.preserveCRDs
```

Produces:

```yaml
{{- if .Values.installCRDs }}
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    {{- if .Values.preserveCRDs }}
    helm.sh/resource-policy: keep
    {{- end }}
  ...
{{- end }}
```

The flag accepts any Helm conditional expression — a single value, an `or`,
an `and`, anything that fits inside `{{- if ... }}`.

## Overriding the templates

For more involved customisation than swapping value paths, point
`-templates-dir` at a directory containing all three template files:

| File                  | Purpose                                                                            |
| --------------------- | ---------------------------------------------------------------------------------- |
| `header.tpl`          | Opening conditional. May contain the literal `__INSTALL_CONDITION__` placeholder, which is replaced with `-install-value`. |
| `footer.tpl`          | Closing line (default: `{{- end }}`).                                              |
| `keep-annotation.tpl` | Block inserted under `metadata.annotations:` when `-keep` is enabled. May contain the literal `__KEEP_CONDITION__` placeholder, which is replaced with `-keep-value`. |

Templates without the placeholders are used verbatim — useful if you want to
hardcode the annotation always on (no `crds.keep` value) by dropping the
`{{- if ... }}` wrapper from `keep-annotation.tpl` entirely.

## End-to-end examples

### `stacklok/toolhive`

```bash
helm-crd-wrapper \
  -source deploy/charts/operator-crds/files/crds \
  -target deploy/charts/operator-crds/templates
```

`values.yaml`:

```yaml
crds:
  install: true
  keep: true
```

### `stacklok/stacklok-llm-gateway`

Same invocation — the tool is intentionally a single shape:

```bash
helm-crd-wrapper \
  -source charts/operator-crds/files/crds \
  -target charts/operator-crds/templates
```

## Migration plan

Downstream repos adopt this binary roughly in this order. The migration
itself does not live in this repo — these are notes for the consumer PRs.

1. **`stacklok/toolhive`** — delete
   `deploy/charts/operator-crds/crd-helm-wrapper/`, add a `task crd-wrap`
   target that calls this binary, wire it into `task generate` after
   `controller-gen`. Collapse the multiple `crds.install.<group>` values in
   the chart's `values.yaml` down to a single `crds.install` boolean (and
   adjust any docs accordingly).
2. **`stacklok/stacklok-llm-gateway`** — add a `task crd-wrap` target
   that runs the same invocation, replace the hand-maintained
   `charts/operator-crds/templates/crds.yaml` with the generated per-CRD
   files.

## Local development

```bash
task build              # build the binary
task test               # run unit + golden + CLI integration tests
task test-update-golden # regenerate golden fixtures after intentional output changes
task lint               # golangci-lint
task helm-lint          # render output through `helm template` as a smoke test
task check              # build + test + lint + helm-lint
```

## Non-goals

- **CRD generation.** This tool wraps existing YAML; it does not invoke
  `controller-gen` or merge CRDs.
- **Helm chart scaffolding.** Consumers wire the output into their own
  charts.
- **Helm plugin shape.** The tool is a single static Go binary.
- **Per-CRD configuration.** See above — both wrapping decisions are
  chart-level concerns.

## License

Apache-2.0. See [LICENSE](./LICENSE).
