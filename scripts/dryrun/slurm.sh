#!/usr/bin/env bash
# Tier 3 Slurm dry-run: emit a SPLAT job to Slurm and validate the #SBATCH
# script with real `sbatch --test-only`, which parses the directives and reports
# when the job would start without submitting it.
#
# The job's queue is set to the test cluster's own partition (discovered via
# sinfo) so acceptance reflects our emitted syntax, not partition/account
# mismatches with the environment. Expects a working slurmctld on PATH.
set -euo pipefail

cd "$(dirname "$0")/../.."

OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

echo "Building bammm..."
go build -o "$OUT/bammm" ./cmd/bammm

partition="$(sinfo -h -o '%R' 2>/dev/null | head -1 || true)"
if [ -z "$partition" ]; then
  echo "no Slurm partition found (is slurmctld running?)" >&2
  exit 1
fi
echo "Using partition: $partition"

cat > "$OUT/job.splat.yaml" <<EOF
apiVersion: bammm.io/v1alpha1
kind: Job
metadata:
  name: dryrun-smoke
spec:
  schedule:
    queue: $partition
    walltime: "00:05:00"
  resources:
    nodes: 1
    tasks: 1
    cpusPerTask: 1
  execution:
    script: |
      #!/bin/bash
      echo "hello from bammm dry-run"
EOF

"$OUT/bammm" convert -f splat -t slurm "$OUT/job.splat.yaml" > "$OUT/job.sh"
echo "── emitted Slurm script ──"
cat "$OUT/job.sh"
echo "──────────────────────────"

echo "sbatch --test-only:"
sbatch --test-only "$OUT/job.sh"
echo "Tier 3 Slurm dry-run: script accepted by sbatch."
