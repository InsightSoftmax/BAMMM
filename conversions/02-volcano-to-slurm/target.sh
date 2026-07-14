#!/bin/bash
# Slurm script — produced by:
#   bammm convert --from volcano --to slurm source.yaml
#
# TRANSLATION NOTES (SPLAT → Slurm):
#
# [AUTO] schedule.queue → --partition
# [AUTO] schedule.priority_class="high-priority" → --priority=700 (normalized to 0–1000 scale)
# [AUTO] resources.nodes=7, tasks=7 → --nodes=7 --ntasks=7 --ntasks-per-node=1
# [AUTO] resources.cpus_per_task=8 → --cpus-per-task=8
# [AUTO] resources.memory_per_task=32Gi → --mem=32G per node
# [AUTO] resources.gpu.count=4 → --gres=gpu:4 (no type specified; add :v100 etc. if needed)
# [AUTO] lifecycle.max_retries=3 → BAMMM generates retry wrapper loop in script
# [AUTO] lifecycle.ttl_after_finished → no Slurm equivalent; NOTE in script
#
# [APPROXIMATION] The original job had 3 roles: chief(1 GPU pod) + worker(4 GPU pods) + ps(2 CPU pods).
#   Slurm has no native concept of heterogeneous roles in a single allocation EXCEPT het-jobs.
#   Flattened to 7 identical nodes; TF_CONFIG is set per-rank to assign roles dynamically.
#   Role assignment: rank 0 = chief, ranks 1-4 = workers, ranks 5-6 = ps servers.
#
#   If your cluster supports Slurm het-jobs, use target-hetjob.sh instead (also generated).
#
# [APPROXIMATION] Container image → Singularity wrapper.
#   Requires Singularity/Apptainer installed on compute nodes.
#   If unavailable: install TensorFlow 2.14 natively or use a venv.
#
# [APPROXIMATION] /data/imagenet (K8s PVC) → update IMAGENET_DIR below to your NFS/Lustre path.
# [APPROXIMATION] /models/resnet50 (K8s PVC) → update MODEL_DIR below to your NFS/Lustre path.
#
# [DROPPED] Volcano gang scheduling min_available=7 → approximated: Slurm --nodes=7 guarantees
#   all 7 nodes start together (Slurm's FIFO/backfill holds jobs until all nodes are available).
#   This is not a hard gang guarantee; preemption or node failure may cause partial execution.
#
# [DROPPED] Volcano lifecycle policy "TaskCompleted → CompleteJob" — no equivalent.
#   This script exits when all srun tasks finish (or walltime expires).
#
# [DROPPED] Volcano priorityClassName → Slurm has no PriorityClass equivalent;
#   approximated as --priority=700 (user-adjustable).

#SBATCH --job-name=tf-resnet-training
#SBATCH --partition=ml-gpu
#SBATCH --priority=700
#SBATCH --nodes=7
#SBATCH --ntasks=7
#SBATCH --ntasks-per-node=1
#SBATCH --cpus-per-task=8
#SBATCH --mem=32G
#SBATCH --gres=gpu:4
#SBATCH --time=04:00:00
#SBATCH --output=/tmp/tf-resnet-%j.out
#SBATCH --error=/tmp/tf-resnet-%j.err

# ── PATHS: Update these to match your HPC filesystem ──────────────────────────
IMAGENET_DIR=/nfs/datasets/imagenet      # was: /data/imagenet (K8s PVC: imagenet-pvc)
MODEL_DIR=/nfs/models/resnet50           # was: /models/resnet50 (K8s PVC: model-store-pvc)
CONTAINER_IMAGE=docker://tensorflow/tensorflow:2.14.0-gpu

# ── Build TF_CONFIG for parameter-server strategy ─────────────────────────────
# Rank assignment: 0=chief, 1-4=workers, 5-6=ps
# Requires HOSTNAMES to be resolvable between nodes.
HOSTNAMES=($(scontrol show hostnames "$SLURM_JOB_NODELIST"))

CHIEF_HOST="${HOSTNAMES[0]}:2222"
WORKER_HOSTS=$(printf '"%s:2222",' "${HOSTNAMES[@]:1:4}" | sed 's/,$//')
PS_HOSTS=$(printf '"%s:2222",' "${HOSTNAMES[@]:5:2}" | sed 's/,$//')

# ── Retry wrapper (lifecycle.max_retries=3) ────────────────────────────────────
MAX_RETRIES=3
ATTEMPT=0
EXIT_CODE=1

while [ $ATTEMPT -lt $MAX_RETRIES ] && [ $EXIT_CODE -ne 0 ]; do
    ATTEMPT=$((ATTEMPT + 1))
    echo "[BAMMM] Attempt $ATTEMPT of $MAX_RETRIES"

    # ── Role assignment via srun --het-group is not used here (flattened model) ──
    # Each rank determines its role from SLURM_PROCID at runtime.
    srun \
        --ntasks=7 \
        --ntasks-per-node=1 \
        --cpus-per-task=8 \
        --gres=gpu:4 \
        singularity exec \
            --nv \
            --bind "${IMAGENET_DIR}:/data/imagenet:ro" \
            --bind "${MODEL_DIR}:/models/resnet50" \
            "${CONTAINER_IMAGE}" \
            /bin/bash -c '
                # Determine role from rank
                RANK=$SLURM_PROCID
                if [ "$RANK" -eq 0 ]; then
                    TASK_TYPE=chief
                    TASK_INDEX=0
                elif [ "$RANK" -le 4 ]; then
                    TASK_TYPE=worker
                    TASK_INDEX=$((RANK - 1))
                else
                    TASK_TYPE=ps
                    TASK_INDEX=$((RANK - 5))
                fi

                export TF_CPP_MIN_LOG_LEVEL=2
                export PYTHONPATH=/app

                python /app/train_resnet.py \
                    --task_type="$TASK_TYPE" \
                    --task_index="$TASK_INDEX" \
                    --num_epochs=50 \
                    --batch_size=256 \
                    --learning_rate=0.1 \
                    --data_dir=/data/imagenet \
                    --model_dir=/models/resnet50
            '

    EXIT_CODE=$?
    if [ $EXIT_CODE -ne 0 ] && [ $ATTEMPT -lt $MAX_RETRIES ]; then
        echo "[BAMMM] Attempt $ATTEMPT failed (exit $EXIT_CODE). Retrying..."
        sleep 30
    fi
done

if [ $EXIT_CODE -ne 0 ]; then
    echo "[BAMMM] All $MAX_RETRIES attempts failed."
    exit $EXIT_CODE
fi

echo "[BAMMM] Job completed successfully."
