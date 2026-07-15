#!/bin/bash
#SBATCH --job-name=mc-simulation-batch-2026-06
#SBATCH --partition=research-simulations
#SBATCH --priority=75
#SBATCH --comment=mc-simulation-batch-2026-06
#SBATCH --wckey=mc-simulation-batch-2026-06
# het component 0: driver
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --cpus-per-task=4
#SBATCH --mem=16G
#SBATCH --gres=gpu:1
#SBATCH hetjob
# het component 1: compute
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --cpus-per-task=32
#SBATCH --mem=128G
#SBATCH --gres=gpu:4

# ── Het-job execution (Slurm ≥ 20.11) ─────────────────────────────────────────
srun --het-group=0 singularity exec --nv docker://cern/mc-simulator:3.2.1-cuda /bin/bash -c 'echo "Driver pod starting on $(hostname)"
/opt/simulator/driver \
  --config /etc/sim/config.yaml \
  --workers "mc-sim-compute.physics-team.svc.cluster.local:8080" \
  --events 1000000 \
  --output s3://physics-results/higgs/run-$JOB_ID/
' : \
     --het-group=1 singularity exec --nv docker://cern/mc-simulator:3.2.1-cuda /bin/bash -c 'echo "Compute pod starting on $(hostname)"
/opt/simulator/worker \
  --port 8080 \
  --gpus 4 \
  --threads 32
'

# ── ALTERNATIVE: flatten to a single allocation (any Slurm version) ───────────
# If your cluster does not support het-jobs, replace the header above with a
# single allocation sized to the largest role and assign roles by $SLURM_PROCID:
#   #SBATCH --nodes=2
#   #SBATCH --cpus-per-task=32
#   #SBATCH --mem=128G
#   #SBATCH --gres=gpu:4
#   # then branch on $SLURM_PROCID to launch driver (rank 0) vs workers.
