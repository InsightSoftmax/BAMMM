#!/usr/bin/env bash
# Set up a minimal single-node Slurm on the current host for `sbatch
# --test-only`: slurmctld + slurmd + munge, no accounting (slurmdbd). Intended
# for the Tier 3 Slurm dry-run on a GitHub ubuntu runner.
set -euo pipefail

sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y slurm-wlm munge

# Munge auth: create a key and start the daemon.
if [ ! -s /etc/munge/munge.key ]; then
  sudo bash -c 'dd if=/dev/urandom of=/etc/munge/munge.key bs=1024 count=1 2>/dev/null'
fi
sudo chown munge:munge /etc/munge/munge.key
sudo chmod 400 /etc/munge/munge.key
sudo systemctl restart munge

cpus="$(nproc)"
sudo mkdir -p /var/spool/slurmctld /var/spool/slurmd /var/log/slurm
sudo chown -R slurm: /var/spool/slurmctld /var/log/slurm

sudo tee /etc/slurm/slurm.conf >/dev/null <<CONF
ClusterName=bammm
SlurmctldHost=localhost
AuthType=auth/munge
SlurmUser=slurm
StateSaveLocation=/var/spool/slurmctld
SlurmdSpoolDir=/var/spool/slurmd
SlurmctldPidFile=/run/slurmctld.pid
SlurmdPidFile=/run/slurmd.pid
ProctrackType=proctrack/linuxproc
ReturnToService=2
SchedulerType=sched/backfill
SelectType=select/cons_tres
SelectTypeParameters=CR_Core
NodeName=localhost CPUs=${cpus} RealMemory=500 State=UNKNOWN
PartitionName=debug Nodes=ALL Default=YES MaxTime=INFINITE State=UP
CONF

sudo systemctl restart slurmctld
sudo systemctl restart slurmd

# Give the node a moment to register, then show state.
for _ in $(seq 1 10); do
  if sinfo -h >/dev/null 2>&1; then break; fi
  sleep 1
done
sinfo
