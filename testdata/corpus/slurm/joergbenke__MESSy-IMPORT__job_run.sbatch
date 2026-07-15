#!/bin/sh

# Copyright (c) 2024 The YAC Authors
#
# SPDX-License-Identifier: CC0-1.0

#SBATCH --account=ka1298
#SBATCH --output=messy_import_%j.out 
#SBATCH --error=messy_import_%j.err 
#SBATCH --partition=compute
#SBATCH --time=00:05:00
#SBATCH --nodes=1
#SBATCH hetjob
#SBATCH --partition=compute
#SBATCH --time=00:05:00
#SBATCH --nodes=1 

. ./activate_levante_env
export LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/sw/spack-levante/netcdf-fortran-4.5.3-jlxcfz/lib

CONFIG_FILE=input/coupling.yaml

srun -n 1 ./toy_atm.x -c $CONFIG_FILE : \
     -n 1 ./toy_ocn.x -c $CONFIG_FILE
