# BAMMM — Project Guide for Claude

## What this project is

BAMMM (Batch Automatic Magic Multiplexing Mechanism) converts batch job specifications
between scheduler formats via a common interchange format called **SPLAT**
(Scheduler-Portable Language for Abstracting Tasks).

```
slurm script  ──┐
volcano yaml  ──┤                    ┌── slurm script
kueue yaml    ──┼──▶  SPLAT (IR) ──▶┼── volcano yaml
htcondor .sub ──┤                    └── kueue yaml ...
flux json     ──┘
```

The full format specification is in `SPEC.md`. Worked conversion examples (with
translation notes) are in `conversions/`.

## Language and architecture

**Go.** Single static binary — runs on HPC clusters where Python is unavailable.
K8s-ecosystem schedulers (Volcano, Kueue, Armada, YuniKorn) have Go API types we
can import directly rather than re-implementing.

**Core abstraction** — one `Parser` and one `Emitter` per scheduler:

```go
type Parser interface {
    Parse([]byte) (*splat.Job, error)
}

type Emitter interface {
    Emit(*splat.Job) ([]byte, error)
}
```

SPLAT types live in `internal/splat/`. Parser/emitter packages live under
`internal/parser/<scheduler>/` and `internal/emitter/<scheduler>/`.

## Supported schedulers

| Scheduler | Type | Local CI? | How |
|---|---|---|---|
| Slurm | HPC | Yes | Slinky / Slurm Operator on kind |
| Volcano | K8s | Yes | kind + Volcano operator |
| Kueue | K8s | Yes | kind + Kueue |
| Armada | K8s | Yes | kind + Armada quickstart |
| YuniKorn | K8s | Yes | kind + YuniKorn plugin |
| HTCondor | HPC | Yes | Docker single-node |
| Flux | HPC | Yes | Docker (fluxrm/flux-sched) |
| OpenPBS | HPC | Yes | Docker |
| LSF | HPC | Community | IBM license — validate via contributors |
| PBS Pro | HPC | Community | Altair license — validate via contributors |
| Run.ai | K8s | Partner | NVIDIA/Run.ai platform |

## Build plan

1. **Corpus** — scrape GitHub for real job specs (Python + DocETL pipeline, separate from Go code)
2. **PZ semantic join** — find cross-scheduler equivalent pairs for conversion test ground truth
3. **Parsers** — TDD against corpus fixtures
4. **Emitters** — TDD; round-trip tests + cross-scheduler tests
5. **Execution validation** — tiered: syntax → schema/CRD → scheduler dry-run → actual run

Test fixture data lives in `testdata/corpus/` (gitignored if large; use Git LFS or
a separate store). Human-reviewed conversion pairs live in `testdata/pairs/`.

## Key decisions made

- **SPLAT** is the format name (not CBIF). `apiVersion: bammm.io/v1alpha1`, `kind: Job`.
- Three-tier field system: Tier 1 (universal), Tier 2 (common), Tier 3 (`extensions.*` per-scheduler passthrough for round-trip fidelity).
- Both `execution.container` (OCI image) and `execution.script` (HPC bare-metal) can coexist in one spec; emitter picks the right one.
- OpenPBS first for the PBS family; PBS Pro/LSF via community contributors.
- The corpus scraper is Python (DocETL); the converter is Go. They are separate tools.

## Known hard translation limits

See `SPEC.md#known-lossy-translations` and `conversions/README.md` for the full list.
Short version:
- HTCondor ClassAd `requirements`/`rank` expressions — stored in `extensions.htcondor`, not translatable
- Flux symbolic named dependencies — stored in `extensions.flux`, not translatable
- Flux NUMA/socket resource graph — flattened to total CPU count for non-Flux targets
- Slurm het-jobs — only Slurm can execute; approximated elsewhere
- Run.ai GPU fractions — rounded to whole GPU on other schedulers

## Collaborations

- AWS
- Nitka Consulting
- Insight Softmax Consulting
- Run.ai (NVIDIA) contacts — for Run.ai validation
- Potential LSF contacts via community

## Repo layout

```
SPEC.md                    # SPLAT format specification
CLAUDE.md                  # this file
examples/                  # example SPLAT jobs
conversions/               # hand-crafted source → intermediate → target examples
cmd/bammm/                 # CLI entrypoint
internal/
  splat/                   # SPLAT IR types
  parser/                  # Parser interface + per-scheduler parsers
  emitter/                 # Emitter interface + per-scheduler emitters
  convert/                 # orchestration (parser → splat → emitter)
testdata/
  corpus/                  # raw scraped job specs (per scheduler)
  pairs/                   # human-reviewed cross-scheduler equivalent pairs
  fixtures/                # expected parse/emit outputs
scripts/
  corpus/                  # Python/DocETL corpus scraper (separate from Go code)
```
