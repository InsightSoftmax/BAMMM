# BAMMM: Batch Automatic Magic Multiplexing Mechanism

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CI](https://github.com/InsightSoftmax/BAMMM/actions/workflows/ci.yml/badge.svg)](https://github.com/InsightSoftmax/BAMMM/actions/workflows/ci.yml)
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

# Convert a whole directory in bulk (recurses, mirrors the tree into out/)
bammm convert --from slurm --to kueue --input-dir jobs/ --output-dir out/

# Or a glob of files (bulk runs require --output-dir)
bammm convert --from slurm --to kueue jobs/*.sh --output-dir out/
```

Bulk runs name each output after its source with the target's extension,
continue past files that fail to convert, print a summary, and exit non-zero
if any failed. Use `--pattern '*.sbatch'` to filter which files in `--input-dir`
are converted, and `--recursive=false` to stay in the top directory.

Add `--report` to any run to print a coverage report — how many sources use
each SPLAT field, and how much falls into `extensions.*` passthrough (a signal
of potential translation loss):

```sh
bammm convert --from slurm --to splat --input-dir corpus/slurm --output-dir out/ --report
```

Priority is normalized onto a canonical 0–1000 band (higher = higher priority)
so it survives conversion between schedulers that disagree on range and
direction. Widen the band with `--priority-range MIN:MAX` (e.g. `0:100000`) to
reduce rounding loss when round-tripping schedulers with large native ranges:

```sh
bammm convert --from pbs --to slurm --priority-range 0:100000 job.pbs
```

### Validating specs

`bammm validate` checks that specs parse (and, with `--to`, convert) without
emitting anything — handy for CI gates and for checking a whole corpus:

```sh
bammm validate --from slurm job.sh                 # does it parse?
bammm validate --from slurm --to kueue job.sh       # does it parse AND convert?
bammm validate --from slurm --input-dir corpus/slurm  # bulk; exits non-zero if any fail
```

Wherever the translation is lossy, BAMMM tells you exactly what got dropped, what got approximated, and what's stuck in the `extensions.*` block waiting to be round-tripped back.

## Supported schedulers

✅ = available · 🚧 = planned

| Scheduler | Type | Parser (`--from`) | Emitter (`--to`) |
|---|---|---|---|
| **Slurm** | HPC | ✅ | ✅ |
| **Volcano** | K8s | ✅ | ✅ |
| **Kueue** | K8s | ✅ | ✅ |
| **Armada** | K8s | ✅ | ✅ |
| **OpenPBS** | HPC | ✅ | ✅ |
| **HTCondor** | HPC | ✅ | ✅ |
| **Flux** | HPC | 🚧 | 🚧 |
| **YuniKorn** | K8s | ✅ | ✅ |
| **Run.ai** | K8s | ✅ | ✅ |
| **LSF** | HPC | 🚧 | 🚧 |

Plus `splat` as both `--from` and `--to` for validation / round-tripping. Run `bammm formats` for the live list.

## Installation

**From source** (requires Go 1.25+):

```sh
go install github.com/InsightSoftmax/BAMMM/cmd/bammm@latest
```

**Release binary — Linux** (`amd64`; use `bammm_linux_arm64.tar.gz` on ARM):

```sh
curl -sSL https://github.com/InsightSoftmax/BAMMM/releases/latest/download/bammm_linux_amd64.tar.gz \
  | tar -xz bammm
sudo install bammm /usr/local/bin/bammm
```

**Release binary — macOS** (Apple silicon; use `bammm_darwin_amd64.tar.gz` on Intel):

```sh
curl -sSL https://github.com/InsightSoftmax/BAMMM/releases/latest/download/bammm_darwin_arm64.tar.gz \
  | tar -xz bammm
sudo install bammm /usr/local/bin/bammm
```

**Linux packages** (`.deb` / `.rpm` / `.apk` are attached to each [release](https://github.com/InsightSoftmax/BAMMM/releases/latest)):

```sh
# Debian/Ubuntu
curl -sSLO https://github.com/InsightSoftmax/BAMMM/releases/latest/download/bammm_linux_amd64.deb
sudo dpkg -i bammm_linux_amd64.deb
# RHEL/Fedora: sudo rpm -i bammm_linux_amd64.rpm
```

**Homebrew** (macOS / Linux):

```sh
brew install InsightSoftmax/tap/bammm
```

**Container image** (GHCR, multi-arch):

```sh
docker run --rm -i ghcr.io/insightsoftmax/bammm:latest convert -f slurm -t kueue < job.sh
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

The full list is in [SPEC.md § Known Lossy Translations](SPEC.md#known-lossy-translations) and [conversions/README.md](conversions/README.md). The sharp edges that bite people — the camelCase field rule, priority direction, `extensions.*` passthrough — are collected in [docs/gotchas.md](docs/gotchas.md).

Need a translation BAMMM doesn't do out of the box? A user-defined
[declarative rules file](docs/translation-rules.md) is designed (not yet built)
to let you adjust mappings without patching source.

## Development

```sh
make build             # → bin/bammm
make test              # go test -race ./...
make lint              # golangci-lint
make check             # lint + test
make validate-schemas  # kubeconform Tier 2 schema validation
make schema            # regenerate schema/splat.schema.json from the Go types
make dryrun-k8s        # Tier 3: kubectl --dry-run=server (needs a cluster + operators)
make dryrun-slurm      # Tier 3: sbatch --test-only (needs slurmctld)
```

Execution validation is tiered: Tier 1 syntax (`bammm validate`), Tier 2
schema/CRD (`kubeconform`, in CI), Tier 3 real scheduler admission — kind +
Volcano/Kueue/JobSet operators and a Slurm controller, run by the separate
[`dryrun`](.github/workflows/dryrun.yml) workflow (PRs touching emitters,
nightly, or on demand).

Requires Go 1.25+.

**Adding a scheduler?** See [docs/adding-a-scheduler.md](docs/adding-a-scheduler.md)
for the full parser/emitter contribution guide.

### Corpus

Real-world job specs for testing are scraped from GitHub into
`testdata/corpus/<scheduler>/` (gitignored; only manifests are tracked). The
scraper is config-driven — one entry per scheduler — and runs under `uv`:

```sh
export GITHUB_TOKEN=ghp_...
uv run scripts/corpus/fetch_corpus.py --list       # supported schedulers
make corpus SCHED=slurm                             # scrape a corpus
make corpus SCHED=pairs                             # hunt repos with ≥2 formats
```

The `pairs` mode is different: instead of scraping one scheduler by file, it
finds repos that contain specs for two or more schedulers side by side —
candidate cross-scheduler *equivalent* pairs for conversion ground truth.

See [CLAUDE.md](CLAUDE.md) for architecture decisions, the build plan, and notes for contributors.

## Acknowledgements

BAMMM started as an idea kicked around with Boris Litvin (AWS), until Marlow
Warnicke (NVIDIA) nerd-sniped me into actually turning it into a real tool. It's
built and maintained under Insight Softmax Consulting and GR-OSS.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). LSF and PBS Pro contributors especially welcome — those require vendor licenses for local testing.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
