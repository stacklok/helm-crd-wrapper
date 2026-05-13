# helm-crd-wrapper

A generic CLI tool that wraps Kubernetes CRD YAML files (typically the output
of `controller-gen`) with Helm template directives so they can be shipped as
upgrade-aware chart templates.

The wrapper handles three independent concerns:

1. **`helm.sh/resource-policy: keep` annotation** so `helm uninstall` does not
   cascade-delete every custom resource in the cluster.
2. **Feature-flag conditionals** (`{{- if .Values... }} ... {{- end }}`) so a
   chart can install only the CRD groups a given consumer enables.
3. **Go-template delimiter escaping** in CRD description text. `controller-gen`
   often emits `{{` / `}}` inside field docstrings; Helm would otherwise try
   to interpret these and fail to render.

Each toggle can be turned on or off independently, and per-CRD overrides can
be supplied via an optional YAML config file. The behaviour replaces the
hardcoded feature-flag map that lived in `stacklok/toolhive` so consumer repos
can adopt the tool without forking.

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
  -config <file>          # optional: YAML config for per-CRD rules
  -keep                   # optional: inject helm.sh/resource-policy: keep
                          #          (default: false)
  -escape                 # optional: escape {{ }} in CRD content
                          #          (default: true)
  -values-prefix <string> # optional: values key prefix for feature flags
                          #          (default: ".Values.crds.install")
  -templates-dir <dir>    # optional: override embedded templates from disk
  -verbose                # optional: extra logging
```

Exit code `0` on success. `1` on any wrapping error (missing file, invalid
YAML, source path escape, strict-mode config miss, …). `2` when required
flags are missing.

## Config file format

The optional `-config` YAML file declares per-CRD feature-flag rules. The
**only** per-CRD field is `featureFlags`. Anything not listed falls back to
the CLI-flag defaults.

```yaml
# Top-level: when true, every CRD found in -source must have an entry below or
# the run fails. This is the regression gate that catches "forgot to add the
# new CRD to the config" bugs that motivated this tool.
strict: true

crds:
  mcpservers.toolhive.stacklok.dev:
    featureFlags: [server]

  # A single CRD can be gated behind multiple flags. The wrapper renders this
  # as `or .Values.crds.install.server .Values.crds.install.virtualMcp`.
  mcpexternalauthconfigs.toolhive.stacklok.dev:
    featureFlags: [server, virtualMcp]

  # No featureFlags → installed unconditionally (header/footer are omitted).
  aigateways.ai-gateway.stacklok.dev: {}
```

Keys under `crds:` are matched against the full CRD `metadata.name`
(plural.group). Unknown fields (including the legacy `keep:` per-CRD field)
are rejected so misconfigurations fail loudly.

### Why isn't `keep` a per-CRD option?

The `helm.sh/resource-policy: keep` annotation is a global, all-or-nothing
choice for a chart: either every CRD survives `helm uninstall` or none do.
Mixed behaviour would leak custom resources whose CRDs got deleted, which is
the exact footgun the annotation exists to prevent. So `-keep` lives only on
the CLI flag, and consumers can still flip it off at render time via
`.Values.crds.keep` (see below).

## End-to-end examples

### Example 1 — `stacklok/toolhive`

toolhive wants feature flags, the keep annotation, and a per-CRD list of which
flags each CRD belongs to. The config file replaces the hardcoded map that
used to live in the toolhive source tree.

`crds-config.yaml`:

```yaml
strict: true
crds:
  mcpservers.toolhive.stacklok.dev:                      { featureFlags: [server] }
  mcpremoteproxies.toolhive.stacklok.dev:                { featureFlags: [server] }
  mcptoolconfigs.toolhive.stacklok.dev:                  { featureFlags: [server] }
  mcpgroups.toolhive.stacklok.dev:                       { featureFlags: [server] }
  embeddingservers.toolhive.stacklok.dev:                { featureFlags: [server] }
  mcpregistries.toolhive.stacklok.dev:                   { featureFlags: [registry] }
  virtualmcpservers.toolhive.stacklok.dev:               { featureFlags: [virtualMcp] }
  virtualmcpcompositetooldefinitions.toolhive.stacklok.dev:
                                                         { featureFlags: [virtualMcp] }
  mcpoidcconfigs.toolhive.stacklok.dev:                  { featureFlags: [server] }
  mcptelemetryconfigs.toolhive.stacklok.dev:             { featureFlags: [server] }
  mcpexternalauthconfigs.toolhive.stacklok.dev:          { featureFlags: [server, virtualMcp] }
  mcpserverentries.toolhive.stacklok.dev:                { featureFlags: [server, virtualMcp] }
  mcpwebhookconfigs.toolhive.stacklok.dev:               { featureFlags: [server] }
```

Invocation:

```bash
helm-crd-wrapper \
  -source deploy/charts/operator-crds/files/crds \
  -target deploy/charts/operator-crds/templates \
  -config deploy/charts/operator-crds/crds-config.yaml \
  -keep \
  -values-prefix .Values.crds.install
```

The wrapped chart consumes the values:

```yaml
crds:
  keep: true          # consumed by the keep-annotation template
  install:
    server: true      # consumed by the per-CRD feature-flag conditionals
    registry: true
    virtualMcp: true
```

### Example 2 — `stacklok/stacklok-llm-gateway`

llm-gateway wants the keep annotation on every CRD and no feature flags. No
config file is required.

```bash
helm-crd-wrapper \
  -source charts/operator-crds/files/crds \
  -target charts/operator-crds/templates \
  -keep
```

The wrapped chart consumes the single value:

```yaml
crds:
  keep: true
```

## How `keep` flows from `values.yaml`

The `-keep` flag is a **build-time** decision: it controls whether the
`keep-annotation.tpl` block is injected into the wrapped CRD templates at all.

The injected block itself is wrapped in `{{- if .Values.crds.keep }}`, so the
chart consumer makes the final **render-time** decision in their
`values.yaml`:

```yaml
# values.yaml
crds:
  keep: true   # render helm.sh/resource-policy: keep on every CRD
```

After `helm template` runs, the CRD looks like:

```yaml
metadata:
  annotations:
    helm.sh/resource-policy: keep      # present when crds.keep=true
    controller-gen.kubebuilder.io/version: v0.17.3
  name: mcpservers.toolhive.stacklok.dev
```

With `crds.keep: false` the annotation is omitted entirely and Helm will
cascade-delete the CRDs (and every custom resource they back) on `helm
uninstall`. Charts that ship `keep` should default `crds.keep: true` in their
`values.yaml` to make that the safe default.

If you'd rather hardcode the annotation always-on without giving consumers an
opt-out, override `keep-annotation.tpl` via `-templates-dir` and drop the
`{{- if .Values.crds.keep }}` wrapper. See the next section.

## Overriding the templates

The embedded templates live under
[`internal/wrapper/templates`](./internal/wrapper/templates/). To replace any
of them, point `-templates-dir` at a directory containing the three files:

| File                  | Purpose                                                                            |
| --------------------- | ---------------------------------------------------------------------------------- |
| `header.tpl`          | Opening line, with the literal placeholder `__FEATURE_CONDITION__`.                |
| `footer.tpl`          | Closing line.                                                                      |
| `keep-annotation.tpl` | Block inserted under `metadata.annotations:` when `-keep` (or per-CRD) is enabled. |

The default `keep-annotation.tpl` itself uses `{{- if .Values.crds.keep }}` so
consumers can still flip the annotation off at render time. Override the file
to change that.

## Migration plan

Downstream repos adopt this binary roughly in this order. The migration
itself does not live in this repo — these are notes for the consumer PRs.

1. **`stacklok/toolhive`** — delete
   `deploy/charts/operator-crds/crd-helm-wrapper/`, add a `task crd-wrap`
   target that calls this binary with the config file in Example 1, wire it
   into `task generate` after `controller-gen`. Compare the produced output
   against the previous wrapper's output byte-for-byte before merging.

2. **`stacklok/stacklok-llm-gateway`** — add a `task crd-wrap` target
   that runs Example 2, replace the hand-maintained
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

## License

Apache-2.0. See [LICENSE](./LICENSE).
