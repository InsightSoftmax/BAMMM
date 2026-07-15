#!/bin/bash
#SBATCH --job-name=tf-resnet-training
#SBATCH --partition=ml-gpu
# het component 0: chief
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --cpus-per-task=8
#SBATCH --mem=32G
#SBATCH --gres=gpu:4
#SBATCH hetjob
# het component 1: worker
#SBATCH --nodes=1
#SBATCH --ntasks=4
#SBATCH --cpus-per-task=8
#SBATCH --mem=32G
#SBATCH --gres=gpu:4
#SBATCH hetjob
# het component 2: ps
#SBATCH --nodes=1
#SBATCH --ntasks=2
#SBATCH --cpus-per-task=4
#SBATCH --mem=16G

# ── Het-job execution (Slurm ≥ 20.11) ─────────────────────────────────────────
srun --het-group=0 singularity exec --nv docker://tensorflow/tensorflow:2.14.0-gpu /bin/bash -c 'python /app/train_resnet.py \
  --task_type=chief \
  --num_epochs=50 \
  --batch_size=256 \
  --learning_rate=0.1 \
  --data_dir=/data/imagenet \
  --model_dir=/models/resnet50
' : \
     --het-group=1 singularity exec --nv docker://tensorflow/tensorflow:2.14.0-gpu /bin/bash -c 'python /app/train_resnet.py \
  --task_type=worker \
  --num_epochs=50 \
  --batch_size=256 \
  --learning_rate=0.1 \
  --data_dir=/data/imagenet \
  --model_dir=/models/resnet50
' : \
     --het-group=2 singularity exec docker://tensorflow/tensorflow:2.14.0-gpu /bin/bash -c 'python /app/train_resnet.py \
  --task_type=ps \
  --num_epochs=50 \
  --batch_size=256 \
  --learning_rate=0.1 \
  --data_dir=/data/imagenet \
  --model_dir=/models/resnet50
'

# ── ALTERNATIVE: flatten to a single allocation (any Slurm version) ───────────
# If your cluster does not support het-jobs, replace the header above with a
# single allocation sized to the largest role and assign roles by $SLURM_PROCID:
#   #SBATCH --nodes=3
#   #SBATCH --cpus-per-task=8
#   #SBATCH --mem=32G
#   #SBATCH --gres=gpu:4
#   # then branch on $SLURM_PROCID to launch driver (rank 0) vs workers.
