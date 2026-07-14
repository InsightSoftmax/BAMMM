#!/bin/bash
#SBATCH --job-name=bert-finetune
#SBATCH --partition=gpu-hpc
#SBATCH --account=nlp-research
#SBATCH --qos=gpu-qos
#SBATCH --nodes=4
#SBATCH --ntasks-per-node=8
#SBATCH --cpus-per-task=6
#SBATCH --mem-per-cpu=8G
#SBATCH --gres=gpu:a100:2
#SBATCH --time=08:00:00
#SBATCH --time-min=02:00:00
#SBATCH --constraint=infiniband&avx512
#SBATCH --output=/scratch/logs/bert-%j.out
#SBATCH --error=/scratch/logs/bert-%j.err
#SBATCH --mail-type=END,FAIL
#SBATCH --mail-user=researcher@university.edu
#SBATCH --signal=USR1@120

module load cuda/12.1 python/3.11 openmpi/4.1.5

export OMP_NUM_THREADS=6
export NCCL_DEBUG=INFO
export NCCL_IB_DISABLE=0
export MASTER_ADDR=$(scontrol show hostnames "$SLURM_JOB_NODELIST" | head -n 1)
export MASTER_PORT=29500

echo "Job ID: $SLURM_JOB_ID"
echo "Nodes: $SLURM_JOB_NODELIST"
echo "Master: $MASTER_ADDR"

srun python -m torch.distributed.run \
    --nproc_per_node=8 \
    --nnodes=$SLURM_NNODES \
    --node_rank=$SLURM_NODEID \
    --master_addr=$MASTER_ADDR \
    --master_port=$MASTER_PORT \
    /workspace/train.py \
        --model bert-large-uncased \
        --dataset /scratch/data/squad \
        --output-dir /scratch/checkpoints/bert-$SLURM_JOB_ID \
        --epochs 5 \
        --batch-size 32
