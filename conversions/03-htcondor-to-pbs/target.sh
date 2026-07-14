#!/bin/bash
# PBS script — produced by:
#   bammm convert --from htcondor --to pbs source.sub
#
# TRANSLATION NOTES (SPLAT → PBS):
#
# [AUTO] resources.cpus_per_task=8 → -l select=1:ncpus=8
# [AUTO] resources.memory_per_task=128Gi → -l select=1:mem=128gb
#        NOTE: Using max escalation value; original HTCondor request was 32Gi.
# [AUTO] resources.disk_per_task=500Gi → -l select=...:scratch=500gb
# [AUTO] resources.gpu.count=1 → -l select=...:ngpus=1
# [AUTO] array.indices="0-199" → -J 0-199
# [AUTO] schedule.walltime=PT48H → -l walltime=48:00:00
# [AUTO] schedule.account → -A bio.variant-calling
# [AUTO] notifications.email → -M pipeline@genomics.org + -m e
# [AUTO] output paths with {job_id}/{array_index} → %J/%I PBS substitution
# [AUTO] file_staging.inputs → -W stagein= directives
# [AUTO] file_staging.outputs → -W stageout= directives
#
# [DROPPED] requirements ClassAd expression:
#   - HasInfiniband: mapped to -l select=...:ib=true (if PBS site supports this resource)
#     If not, REMOVE ":ib=true" from the select statement.
#   - HasGATK4 || HasSingularity: no PBS equivalent. Ensure Singularity is available.
#   - OpSysAndVer: no PBS equivalent. Job will run on whatever node PBS assigns.
# [DROPPED] rank expression: PBS has no machine-preference ranking. First-available wins.
# [DROPPED] periodic_hold (48h runtime): replaced by -l walltime=48:00:00 (hard kill at 48h).
# [DROPPED] periodic_release: no PBS equivalent.
# [DROPPED] periodic_remove (7-day idle): no PBS equivalent. Jobs don't idle in PBS queues
#   for 7 days in practice; the queue or walltime handles this.
# [DROPPED] retry_request_memory escalation: PBS has no dynamic resource escalation.
#   Using max memory (128Gi) for all attempts.
# [DROPPED] rank (machine preference): no PBS equivalent.
#
# REQUIRED MANUAL STEPS:
#   1. Set PBS_GPU_QUEUE below to your cluster's GPU queue name.
#   2. Verify ":ib=true" is a valid PBS resource on your cluster; remove if not.
#   3. Verify Singularity/Apptainer is installed on GPU nodes.
#   4. Update PBS_MANIFEST_FILE to the path of your sample manifest.
#   5. Create /scratch/logs/ if it doesn't exist.

PBS_GPU_QUEUE=gpu-standard        # MANUAL: set to your GPU queue
PBS_MANIFEST_FILE=/scratch/sample-manifest.txt  # must be accessible from compute nodes

#PBS -N genomics-sweep-2026-06
#PBS -q ${PBS_GPU_QUEUE}
#PBS -A bio.variant-calling
#PBS -J 0-199
#PBS -l select=1:ncpus=8:mem=128gb:ngpus=1:scratch=500gb:ib=true
#PBS -l walltime=48:00:00
#PBS -o /scratch/logs/gatk-%J-%I.out
#PBS -e /scratch/logs/gatk-%J-%I.err
#PBS -j n
#PBS -m e
#PBS -M pipeline@genomics.org
#PBS -W stagein=hg38.fa@login01:/reference/hg38.fa,hg38.fa.fai@login01:/reference/hg38.fa.fai,known-sites.vcf.gz@login01:/reference/known-sites.vcf.gz,run-variant-calling.sh@login01:/scripts/run-variant-calling.sh

# ── Read parameters for this array element ────────────────────────────────────
# PBS_ARRAY_INDEX is 0-based (OpenPBS/Torque) or 1-based (PBS Pro); adjust LINE if needed.
LINE=$((PBS_ARRAY_INDEX + 1))   # +1 because awk lines are 1-based; remove if PBS Pro 1-based
read -r SAMPLE_ID BAM_FILE OUTPUT_DIR <<< "$(awk "NR==${LINE}" "${PBS_MANIFEST_FILE}")"

if [ -z "$SAMPLE_ID" ]; then
    echo "ERROR: No sample at index ${PBS_ARRAY_INDEX} in ${PBS_MANIFEST_FILE}"
    exit 1
fi

echo "PBS_ARRAY_INDEX: ${PBS_ARRAY_INDEX}"
echo "SAMPLE_ID: ${SAMPLE_ID}"
echo "BAM_FILE: ${BAM_FILE}"
echo "OUTPUT_DIR: ${OUTPUT_DIR}"

# ── Retry loop (lifecycle.max_retries=3) ──────────────────────────────────────
MAX_RETRIES=3
ATTEMPT=0
EXIT_CODE=1

while [ $ATTEMPT -lt $MAX_RETRIES ] && [ $EXIT_CODE -ne 0 ]; do
    ATTEMPT=$((ATTEMPT + 1))
    echo "[BAMMM] Attempt $ATTEMPT of $MAX_RETRIES"

    singularity exec \
        --nv \
        --bind /reference:/reference:ro \
        --bind "${OUTPUT_DIR}:${OUTPUT_DIR}" \
        docker://broadinstitute/gatk:4.5.0.0 \
        /scripts/run-variant-calling.sh "${SAMPLE_ID}" "${BAM_FILE}" "${OUTPUT_DIR}"

    EXIT_CODE=$?

    if [ $EXIT_CODE -ne 0 ] && [ $ATTEMPT -lt $MAX_RETRIES ]; then
        echo "[BAMMM] Attempt $ATTEMPT failed (exit $EXIT_CODE). Retrying in 60s..."
        sleep 60
    fi
done

if [ $EXIT_CODE -ne 0 ]; then
    echo "[BAMMM] All $MAX_RETRIES attempts failed for sample ${SAMPLE_ID}."
    exit $EXIT_CODE
fi

# ── Stage output files back ───────────────────────────────────────────────────
# PBS stagein/stageout handles reference files; output VCFs staged manually
# because output_dir is parameterized per array element.
mkdir -p "/results/${SAMPLE_ID}/"
cp "${OUTPUT_DIR}/${SAMPLE_ID}.vcf.gz"     "/results/${SAMPLE_ID}/${SAMPLE_ID}.vcf.gz"
cp "${OUTPUT_DIR}/${SAMPLE_ID}.vcf.gz.tbi" "/results/${SAMPLE_ID}/${SAMPLE_ID}.vcf.gz.tbi"

echo "[BAMMM] Completed sample ${SAMPLE_ID}"
