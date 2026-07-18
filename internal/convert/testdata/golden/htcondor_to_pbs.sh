#!/bin/bash
#PBS -N genomics-sweep-2026-06
#PBS -A bio.variant-calling
#PBS -P genomics-pipeline
#PBS -l select=1:ncpus=8:mem=32gb:ngpus=1:scratch=200gb
#PBS -o /scratch/logs/gatk-%J-%I.out
#PBS -e /scratch/logs/gatk-%J-%I.err
#PBS -m e
#PBS -M pipeline@genomics.org

/scripts/run-variant-calling.sh {sample_id} {bam_file} {output_dir}
