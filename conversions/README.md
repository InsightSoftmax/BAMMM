# BAMMM Conversion Examples

Each subdirectory demonstrates a complete conversion through the SPLAT intermediate format.
Files in each directory:

| File | Description |
|---|---|
| `source.*` | The original job in its native scheduler format |
| `intermediate.yaml` | The SPLAT representation (what `bammm convert --from X` produces) |
| `target.*` | The job in the destination format (what `bammm convert --to Y` produces) |

Inline comments in each file explain what translates cleanly, what is approximated,
and what is permanently lost. Read the intermediate.yaml translation notes first.

---

## Conversions

### 01 — Slurm → SPLAT → Volcano
**Source:** `#SBATCH` script — 4-node PyTorch distributed training with GPU, InfiniBand constraint, mail notification, `--time-min` backfill hint  
**Target:** Volcano `vcjob` with pytorch plugin, gang scheduling, ConfigMap-mounted script  
**Key issues:** No container image in source (BAMMM uses placeholder); srun → torchrun; email notifications dropped; `--time-min` Slurm-only

### 02 — Volcano → SPLAT → Slurm
**Source:** Volcano `vcjob` with TensorFlow parameter-server architecture (chief + worker + ps roles)  
**Target:** Slurm script (flat model) + bonus het-job option in extension block  
**Key issues:** PS architecture has no HPC equivalent — flattened to rank-based role assignment; container → Singularity wrapper; Volcano lifecycle policies dropped; PVC mount paths need manual update

### 03 — HTCondor → SPLAT → PBS
**Source:** HTCondor submit file — genomic variant-calling parametric sweep (200 samples), GPU requirements, ClassAd matching, `periodic_hold`, `retry_request_memory` escalation  
**Target:** PBS array job script with Singularity and retry loop  
**Key issues:** ClassAd `requirements` expression (machine-matching logic) is **permanently lost** — stored in extension block but PBS cannot execute it; `periodic_hold`/`periodic_release`/`periodic_remove` dropped (replaced by walltime); memory escalation flattened to max value

### 04 — Flux → SPLAT → Kueue
**Source:** Flux JSON jobspec — LLM pretraining, hierarchical resource graph (socket topology), symbolic named dependency, embedded config file, `preemptible-after`  
**Target:** Kueue-annotated batch/v1 Job + ConfigMap + LocalQueue reference  
**Key issues:** Flux symbolic dependencies (`string: tokenized-pile-dataset`) have **no Kueue equivalent** — dropped entirely; socket NUMA topology lost (flat 32 CPU); preemptible-after dropped; Flux job IDs (F58 encoded) cannot be referenced by Kueue

### 05 — Armada → SPLAT → Slurm
**Source:** Armada job submission — two-pod gang job (driver + compute) with Ingress for metrics, headless Service, jobSetId grouping, multi-cluster transparent routing  
**Target:** Slurm het-job script (driver component + compute component)  
**Key issues:** Armada Ingress/Service has **no Slurm equivalent** — dropped; K8s DNS name for inter-pod communication replaced with Slurm hostname lookup; multi-cluster routing dropped (targets default cluster); jobSetId preserved only as `--comment`/`--wckey`

---

## Common Translation Patterns

| Pattern | Works well | Lossy | Impossible |
|---|---|---|---|
| Container → HPC | Singularity wrapper generated automatically | `module load` must be added manually | ClassAd requirements, K8s Services/Ingress |
| HPC → Container | Script embedded in ConfigMap | Slurm env vars ($SLURM_*) must be replaced | `module load`, burst buffer |
| Array jobs | All HPC schedulers ↔ each other | K8s schedulers (become N separate submissions) | HTCondor `queue N from` parametric variables |
| Gang scheduling | All K8s schedulers ↔ each other; Slurm (all-or-nothing nodes) | PBS/LSF (no native gang) | HTCondor (no gang support) |
| Dependencies | HPC schedulers ↔ each other | K8s schedulers (no native dependency) | Flux symbolic deps, Slurm `singleton` |
| GPU | All schedulers | Type/model hints are advisory only | Run.ai GPU fraction (rounds to 1 GPU) |
| Email notification | HPC schedulers ↔ each other | K8s schedulers (dropped unless sidecar) | — |

---

## Known Permanent Losses

These concepts cannot be represented in any generic format and will always require scheduler-specific handling:

1. **HTCondor `requirements` / `rank` expressions** — Turing-complete ClassAd matching against live machine properties. Stored in `extensions.htcondor` but executable only by HTCondor.

2. **Flux symbolic dependencies** (`string`/`fluid` schemes) — Named publish/subscribe data dependencies. No equivalent in any K8s scheduler or other HPC system.

3. **Flux NUMA/socket resource graph** — Hierarchical topology specification. Round-trips to Flux via `extensions.flux.resource_graph`; flattened for all other targets.

4. **Slurm het-jobs** — Heterogeneous multi-component allocations. Partially approximated as Volcano multi-task or PBS mixed-chunk select; only Slurm can execute them natively.

5. **Armada multi-cluster transparent routing** — Job lands on any cluster in a pool. Other schedulers require explicit cluster/partition selection.

6. **Run.ai GPU fraction** (`gpuFraction: 0.5`) — VRAM-level GPU partitioning via virtual GPU driver. All other schedulers get whole GPUs.

7. **LSF SLA scheduling class** — Guaranteed job start time windows. No equivalent outside LSF.
