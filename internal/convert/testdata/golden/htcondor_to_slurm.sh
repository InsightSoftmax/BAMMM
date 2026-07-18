#!/bin/bash
#SBATCH --job-name=genomics-sweep-2026-06
#SBATCH --account=bio.variant-calling
#SBATCH --cpus-per-task=8
#SBATCH --mem=32G
#SBATCH --gres=gpu:1
#SBATCH --output=/scratch/logs/gatk-%j-%a.out
#SBATCH --error=/scratch/logs/gatk-%j-%a.err
#SBATCH --mail-type=END
#SBATCH --mail-user=pipeline@genomics.org

srun /scripts/run-variant-calling.sh {sample_id} {bam_file} {output_dir}
