#!/bin/bash
#PBS -N genomics-sweep-2026-06
#PBS -q ${PBS_GPU_QUEUE}
#PBS -A bio.variant-calling
#PBS -J 0-199
#PBS -l select=1:ncpus=8:mem=128gb:ngpus=1:scratch=500gb:ib=true
#PBS -l walltime=48:00:00
#PBS -o /scratch/logs/gatk-%J-%I.out
#PBS -e /scratch/logs/gatk-%J-%I.err
#PBS -m e
#PBS -M pipeline@genomics.org
#PBS -W stagein=hg38.fa@login01:/reference/hg38.fa,hg38.fa.fai@login01:/reference/hg38.fa.fai,known-sites.vcf.gz@login01:/reference/known-sites.vcf.gz,run-variant-calling.sh@login01:/scripts/run-variant-calling.sh

PBS_GPU_QUEUE=gpu-standard        # MANUAL: set to your GPU queue
PBS_MANIFEST_FILE=/scratch/sample-manifest.txt  # must be accessible from compute nodes


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
