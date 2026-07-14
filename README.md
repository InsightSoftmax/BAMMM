# BAMMM: Batch Automatic Magic Multiplexing Mechanism

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://github.com/finos/bammm/actions/workflows/ci.yml/badge.svg)](https://github.com/finos/bammm/actions/workflows/ci.yml)
[![Status](https://img.shields.io/badge/Status-Early%20Development-orange.svg)]()

---

Stuck trying to migrate to a new batch scheduler, but you can't convince ol' Neckbeard who's been writing `#SBATCH` scripts since before the Linux kernel hit 1.0? **Rejoice.** BAMMM rewrites those ancient batch job manifests into whatever crusty scheduler you actually want to run them on.

Got a HPC cluster full of Slurm scripts and a new K8s platform breathing down your neck? BAMMM converts them. Kubernetes shop that just inherited a PBS-era bioinformatics pipeline? BAMMM handles it. Tired of maintaining the same distributed training job in three different formats for three different teams? You know where this is going.

BAMMM converts between **10 batch scheduler formats** via a common interchange format called **SPLAT** (Scheduler-Portable Language for Abstracting Tasks). No scheduler lock-in. No rewriting jobs by hand. No explaining to your users why their 400-line `#BSUB` script needs to become a Kueue YAML.

---

## How it works

Every scheduler format goes in as a **Parser** and comes out as an **Emitter**. In between sits SPLAT — a single YAML representation that's a superset of everything all 10 schedulers can express:

```
slurm script   ──┐                        ┌── slurm script
volcano yaml   ──┤                        ├── volcano yaml
kueue yaml     ──┼──▶  SPLAT (IR) ──▶ ───┼── kueue yaml
htcondor .sub  ──┤                        ├── pbs script
flux json      ──┘                        └── armada yaml ...
```

```sh
# Convert a Slurm script to Volcano
bammm convert --from slurm --to volcano job.sh

# Or pipe it
cat job.sub | bammm convert --from htcondor --to pbs

# Inspect the intermediate representation
bammm convert --from slurm --to splat job.sh
```

Wherever the translation is lossy, BAMMM tells you exactly what got dropped, what got approximated, and what's stuck in the `extensions.*` block waiting to be round-tripped back.

## Supported schedulers

| Scheduler | Type | Status |
|---|---|---|
| **Slurm** | HPC | Parser ✅ · Emitter 🚧 |
| **Volcano** | K8s | 🚧 |
| **Kueue** | K8s | 🚧 |
| **Armada** | K8s | 🚧 |
| **YuniKorn** | K8s | 🚧 |
| **HTCondor** | HPC | 🚧 |
| **OpenPBS** | HPC | 🚧 |
| **Flux** | HPC | 🚧 |
| **LSF** | HPC | Community 🚧 |
| **Run.ai** | K8s | Partner 🚧 |

## Installation

```sh
# Homebrew (coming soon)
brew install finos/tap/bammm

# From source (requires Go 1.24+)
go install github.com/finos/bammm/cmd/bammm@latest

# Or just grab a release binary from GitHub Releases
```

## SPLAT format quick start

SPLAT is the format you write if you want a job that runs *anywhere*. Specify both a container image and a shell script — K8s emitters use the container, HPC emitters use the script.

```yaml
apiVersion: bammm.io/v1alpha1
kind: Job
metadata:
  name: my-training-job
spec:
  schedule:
    queue: gpu-batch
    walltime: "08:00:00"
    account: my-project
  resources:
    nodes: 4
    tasksPerNode: 8
    cpusPerTask: 6
    memoryPerTask: "48Gi"
    gpu:
      count: 2
      type: a100
  execution:
    container:
      image: "nvcr.io/nvidia/pytorch:24.01-py3"
      command: ["torchrun", "train.py"]
    script: |
      #!/bin/bash
      module load cuda/12.1
      srun python train.py
```

See [SPEC.md](SPEC.md) for the complete field reference.
See [`conversions/`](conversions/) for five annotated worked examples with full translation notes.
See [`examples/`](examples/) for standalone SPLAT job examples.

## Translation honesty

BAMMM tells you upfront when a conversion is lossy. Some things genuinely cannot cross the HPC/K8s divide:

- **HTCondor ClassAd `requirements`** — Turing-complete machine-matching expressions. Stored in `extensions.htcondor`, not executable anywhere else.
- **Flux symbolic named dependencies** — publish/subscribe data dependencies. Dropped with a warning.
- **Slurm het-jobs** — only Slurm can execute them natively; approximated as multi-task elsewhere.
- **Run.ai GPU fractions** — virtual GPU partitioning rounds to whole GPUs on every other scheduler.

The full list is in [SPEC.md § Known Lossy Translations](SPEC.md#known-lossy-translations) and [conversions/README.md](conversions/README.md).

## Development

```sh
make build    # → bin/bammm
make test     # go test -race ./...
make lint     # golangci-lint
make check    # lint + test
```

Requires Go 1.24+. For the Python corpus scraper: `uv` (not pip).

See [CLAUDE.md](CLAUDE.md) for architecture decisions, the build plan, and notes for contributors.

## Collaboration

Active collaboration between:

- **AWS**
- **Nitka Consulting**
- **Insight Softmax Consulting**
- **Run.ai / NVIDIA** (Run.ai validation)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). LSF and PBS Pro contributors especially welcome — those require vendor licenses for local testing.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
