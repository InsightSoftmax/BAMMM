#!/usr/bin/env bash
# Tier 3 K8s dry-run: emit BAMMM's Kubernetes conversions and submit them to a
# real cluster's admission path with `kubectl apply --dry-run=server`. This
# exercises the operator webhooks (Kueue queue-label mutation, Volcano vcjob
# validation, JobSet structure) — beyond the static schema checks in Tier 2.
#
# Expects kubectl pointed at a cluster with the Volcano, Kueue, and JobSet
# operators installed and Ready, and scripts/dryrun/prereqs.yaml applied.
# Armada has no CRD, so its podSpecs are dry-run as bare Pods.
set -euo pipefail

cd "$(dirname "$0")/../.."

OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

echo "Building bammm..."
go build -o "$OUT/bammm" ./cmd/bammm
BAMMM="$OUT/bammm"

fail=0
dryrun() { # <label> <file>
  echo "── dry-run: $1"
  if ! kubectl apply --dry-run=server -f "$2"; then
    echo "   FAILED: $1"
    fail=1
  fi
}

# CRD workloads. Kueue output carries a commented LocalQueue reference block
# after the workload; strip it so we validate only the emitted workload.
strip_ref() { sed '/# Reference LocalQueue/,$d'; }

"$BAMMM" convert -f slurm  -t kueue   conversions/01-slurm-to-volcano/source.sh   | strip_ref > "$OUT/slurm_kueue.yaml"
"$BAMMM" convert -f slurm  -t volcano conversions/01-slurm-to-volcano/source.sh              > "$OUT/slurm_volcano.yaml"
"$BAMMM" convert -f volcano -t kueue  conversions/02-volcano-to-slurm/source.yaml | strip_ref > "$OUT/volcano_kueue.yaml"
"$BAMMM" convert -f volcano -t volcano conversions/02-volcano-to-slurm/source.yaml            > "$OUT/volcano_volcano.yaml"
"$BAMMM" convert -f armada -t kueue   conversions/05-armada-to-slurm/source.yaml  | strip_ref > "$OUT/armada_kueue.yaml"
"$BAMMM" convert -f armada -t volcano conversions/05-armada-to-slurm/source.yaml              > "$OUT/armada_volcano.yaml"

# Armada podSpecs → bare Pods (Armada is not a CRD).
"$BAMMM" convert -f armada -t armada conversions/05-armada-to-slurm/source.yaml \
  | uv run scripts/dryrun/armada-podspecs.py physics-team > "$OUT/armada_pods.yaml"

dryrun "slurm→kueue (Job)"        "$OUT/slurm_kueue.yaml"
dryrun "slurm→volcano (vcjob)"    "$OUT/slurm_volcano.yaml"
dryrun "volcano→kueue (JobSet)"   "$OUT/volcano_kueue.yaml"
dryrun "volcano→volcano (vcjob)"  "$OUT/volcano_volcano.yaml"
dryrun "armada→kueue (JobSet)"    "$OUT/armada_kueue.yaml"
dryrun "armada→volcano (vcjob)"   "$OUT/armada_volcano.yaml"
dryrun "armada podSpecs (Pods)"   "$OUT/armada_pods.yaml"

if [ "$fail" -ne 0 ]; then
  echo "Tier 3 K8s dry-run: FAILURES above."
  exit 1
fi
echo "Tier 3 K8s dry-run: all manifests accepted by the cluster."
