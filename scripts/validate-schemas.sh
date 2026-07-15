#!/usr/bin/env bash
# Tier 2 execution validation: emit BAMMM's Kubernetes-target conversions and
# validate them against real API/CRD schemas with kubeconform.
#
# Built-in resources (batch/v1 Job, v1 ConfigMap) use kubeconform's default
# schema location (fetched). CRDs (Volcano Job, Kueue LocalQueue) use the JSON
# schemas vendored under testdata/schemas/ — see testdata/schemas/README.md.
#
# Only Kubernetes targets are checked here: Slurm is a shell script and Armada's
# JobSubmitRequest is a gRPC projection, not a K8s manifest.
set -euo pipefail

cd "$(dirname "$0")/.."

KUBECONFORM="go run github.com/yannh/kubeconform/cmd/kubeconform@v0.8.0"
SCHEMA_DIR="testdata/schemas"
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

# from:to:source-file — explicit list of conversions that produce valid K8s
# manifests. Kueue targets only single-role jobs (batch/v1 Job has one pod
# template), so multi-role sources (volcano/armada) are emitted to Volcano only.
CONVERSIONS=(
  "slurm:kueue:conversions/01-slurm-to-volcano/source.sh"
  "slurm:volcano:conversions/01-slurm-to-volcano/source.sh"
  "volcano:volcano:conversions/02-volcano-to-slurm/source.yaml"
  "armada:volcano:conversions/05-armada-to-slurm/source.yaml"
)

echo "Building bammm..."
go build -o "$OUT/bammm" ./cmd/bammm

echo "Emitting Kubernetes manifests..."
for entry in "${CONVERSIONS[@]}"; do
  from="${entry%%:*}"
  rest="${entry#*:}"
  to="${rest%%:*}"
  src="${rest#*:}"
  "$OUT/bammm" convert --from "$from" --to "$to" "$src" > "$OUT/${from}_to_${to}.yaml"
done

echo "Validating with kubeconform..."
# shellcheck disable=SC2086
$KUBECONFORM \
  -strict -summary -verbose \
  -schema-location default \
  -schema-location "${SCHEMA_DIR}/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json" \
  "$OUT"/*.yaml
