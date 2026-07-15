#!/bin/bash
# Slurm het-job script — produced by:
#   bammm convert --from armada --to slurm source.yaml
#
# This is a Slurm HETEROGENEOUS JOB (requires Slurm ≥ 20.11).
# Two components run simultaneously in one allocation:
#   Component 0 (het-group 0): driver — 1 node, 4 CPU, 16Gi, 1 GPU
#   Component 1 (het-group 1): compute — 1 node, 32 CPU, 128Gi, 4 GPU
#
# TRANSLATION NOTES (SPLAT → Slurm):
#
# [AUTO] gang.min_available=2 → het-job guarantees both components start together
# [AUTO] tasks[driver].resources → component 0 resources
# [AUTO] tasks[compute].resources → component 1 resources
# [AUTO] schedule.priority=675 → --priority=675
# [AUTO] placement.tolerations[simulation-node] → --constraint=simulation-node
#
# [DROPPED] Armada ingress (port 9090 / Prometheus):
#   No Slurm equivalent. If you need metrics exposure, run a port-forward tunnel
#   from the login node or use a VPN to access the node directly.
#   Node address will be in $SLURM_JOB_NODELIST component 0.
#
# [DROPPED] Armada headless service:
#   Replaced with direct hostname lookup from $SLURM_JOB_NODELIST.
#   Driver addresses the compute node by hostname instead of K8s DNS.
#
# [DROPPED] jobSetId grouping:
#   Preserved as --comment and --wckey for manual tracking in sacct.
#
# [DROPPED] Armada multi-cluster routing:
#   Targeting current/default Slurm cluster. Update --cluster if needed.
#
# [APPROXIMATION] Container image → Singularity. Requires Singularity/Apptainer.
# [APPROXIMATION] K8s ConfigMap "mc-sim-config" → /etc/sim/config.yaml must exist
#   on the compute nodes via shared filesystem or manual pre-placement.
#   If not available, the driver will fail. See MANUAL STEPS below.
# [APPROXIMATION] S3 output → aws s3 cp in post-job step. Requires AWS credentials
#   available on the compute node (IAM role, or AWS_ACCESS_KEY_ID/SECRET env vars).
#
# REQUIRED MANUAL STEPS:
#   1. Ensure Singularity/Apptainer is available on simulation nodes
#   2. Place /etc/sim/config.yaml on compute nodes (from K8s ConfigMap "mc-sim-config")
#      OR add: --bind /your/local/config.yaml:/etc/sim/config.yaml to singularity calls
#   3. Configure AWS credentials for S3 output (aws configure or instance profile)
#   4. If het-jobs are not enabled on your cluster, use target-flat.sh (single-node fallback)
#      generated alongside this file

# ── Het-job component 0: driver ────────────────────────────────────────────────
#SBATCH --job-name=mc-simulation-batch-2026-06
#SBATCH --partition=research-simulations
#SBATCH --priority=675
#SBATCH --comment=mc-simulation-batch-2026-06
#SBATCH --wckey=mc-simulation-batch-2026-06
#SBATCH --nodes=1
#SBATCH --ntasks-per-node=1
#SBATCH --cpus-per-task=4
#SBATCH --mem=16G
#SBATCH --gres=gpu:1
#SBATCH --constraint=simulation-node
#SBATCH --time=04:00:00
#SBATCH --output=/tmp/mc-sim-driver-%j.out
#SBATCH --error=/tmp/mc-sim-driver-%j.err
# ── Het-job component 1: compute ───────────────────────────────────────────────
#SBATCH hetjob
#SBATCH --nodes=1
#SBATCH --ntasks-per-node=1
#SBATCH --cpus-per-task=32
#SBATCH --mem=128G
#SBATCH --gres=gpu:4
#SBATCH --constraint=simulation-node
#SBATCH --time=04:00:00
#SBATCH --output=/tmp/mc-sim-compute-%j.out
#SBATCH --error=/tmp/mc-sim-compute-%j.err

# ── Get hostnames for both het-job components ─────────────────────────────────
DRIVER_HOST=$(scontrol show hostnames "${SLURM_JOB_NODELIST_HET_GROUP_0}" | head -n1)
COMPUTE_HOST=$(scontrol show hostnames "${SLURM_JOB_NODELIST_HET_GROUP_1}" | head -n1)

echo "Het-job ID: ${SLURM_JOB_ID}"
echo "Driver node: ${DRIVER_HOST}"
echo "Compute node: ${COMPUTE_HOST}"
echo "JobSet: mc-simulation-batch-2026-06 (from Armada jobSetId)"

# ── Start compute worker (component 1) in background ─────────────────────────
srun --het-group=1 \
    singularity exec \
        --nv \
        docker://cern/mc-simulator:3.2.1-cuda \
        /opt/simulator/worker \
            --port 8080 \
            --gpus 4 \
            --threads 32 &
COMPUTE_PID=$!

# Give the compute worker time to start up before the driver connects
sleep 10

# ── Start driver (component 0) ────────────────────────────────────────────────
srun --het-group=0 \
    singularity exec \
        --nv \
        --bind /etc/sim:/etc/sim:ro \
        docker://cern/mc-simulator:3.2.1-cuda \
        /opt/simulator/driver \
            --config /etc/sim/config.yaml \
            --workers "${COMPUTE_HOST}:8080" \
            --events 1000000 \
            --output "/tmp/mc-output-${SLURM_JOB_ID}/"
DRIVER_EXIT=$?

# ── Wait for compute worker to finish ────────────────────────────────────────
wait $COMPUTE_PID
COMPUTE_EXIT=$?

# ── Stage output to S3 ────────────────────────────────────────────────────────
# NOTE: Armada job wrote directly to s3://physics-results/higgs/run-$JOB_ID/
# Slurm job writes to /tmp first and copies here. Requires awscli.
if command -v aws &>/dev/null; then
    aws s3 cp \
        "/tmp/mc-output-${SLURM_JOB_ID}/" \
        "s3://physics-results/higgs/run-${SLURM_JOB_ID}/" \
        --recursive
    S3_EXIT=$?
    [ $S3_EXIT -ne 0 ] && echo "WARN: S3 upload failed (exit $S3_EXIT)"
else
    echo "WARN: awscli not found; output left in /tmp/mc-output-${SLURM_JOB_ID}/"
fi

# ── Report ────────────────────────────────────────────────────────────────────
echo "Driver exit: ${DRIVER_EXIT}"
echo "Compute exit: ${COMPUTE_EXIT}"

# Fail if driver failed (compute is a service; its exit code is less meaningful)
exit $DRIVER_EXIT
