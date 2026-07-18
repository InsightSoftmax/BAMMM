# Adding a scheduler to BAMMM

This guide walks through adding support for a new batch scheduler. Each scheduler
is two independent halves — a **Parser** (native format → SPLAT) and an
**Emitter** (SPLAT → native format) — and you can contribute either or both. If
you only care about migrating *off* a scheduler, a Parser is enough; to migrate
*onto* it, you need an Emitter.

Everything flows through **SPLAT**, the intermediate representation in
`internal/splat` (see [SPEC.md](../SPEC.md)). You never translate scheduler A
directly to scheduler B — you translate A → SPLAT → B, so N schedulers need 2N
parsers/emitters instead of N² direct converters.

## The two interfaces

```go
// internal/parser/parser.go
type Parser interface {
    Parse([]byte) (*splat.Job, error)
}

// internal/emitter/emitter.go
type Emitter interface {
    Emit(*splat.Job) ([]byte, error)
}
```

Both are registered at init time into a global registry, keyed by the format
name used on the CLI (`--from`/`--to`).

## Repository layout

```
internal/
  splat/                 # the IR — read SPEC.md; add fields here only if truly universal
  parser/
    <scheduler>/         # your Parser
    all/all.go           # blank-imports every parser (add yours here)
  emitter/
    <scheduler>/         # your Emitter
    all/all.go           # blank-imports every emitter (add yours here)
  <scheduler>/types.go   # optional: native wire structs (for YAML/JSON formats)
  k8senc/                # shared helpers for the Kubernetes family
  jobset/                # shared JobSet encode/decode (multi-role K8s jobs)
convert/                 # orchestration (parser → splat → emitter) + golden tests
cmd/bammm/               # CLI
scripts/corpus/          # corpus scraper (add a config entry to test against real specs)
```

## Step by step

### 1. Write the Parser

Create `internal/parser/<scheduler>/parser.go`:

```go
package myscheduler

import (
    "github.com/InsightSoftmax/BAMMM/internal/parser"
    "github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() { parser.Register("myscheduler", parserImpl{}) }

type parserImpl struct{}

func (parserImpl) Parse(data []byte) (*splat.Job, error) { return Parse(data) }

// Parse converts a native MyScheduler spec into a SPLAT job.
func Parse(data []byte) (*splat.Job, error) {
    job := &splat.Job{APIVersion: splat.APIVersion, Kind: splat.Kind}
    job.Metadata.Annotations = map[string]string{"bammm.io/source-format": "myscheduler"}
    // ... populate job.Spec from the native format ...
    return job, nil
}
```

Guidelines:

- **Map to first-class SPLAT fields** wherever a concept exists (queue, walltime,
  cpus, gpus, dependencies, …). Consult SPEC.md; don't invent fields.
- **Field names are camelCase.** SPLAT is parsed via json tags
  (`cpusPerTask`, not `cpus_per_task`).
- **Preserve everything else** in `job.Spec.Extensions.<Scheduler>` (a
  `map[string]interface{}`) so it round-trips back even though it can't be
  translated. This is how BAMMM stays honest about lossiness.
- **Priority** goes through `splat.PriorityScale` — add a scale var in
  `internal/splat/priority.go` describing your scheduler's native range and
  direction, and call `.Normalize()` on parse. (Lower-is-better schemes set
  `Invert: true`.)
- **Kubernetes-family** schedulers should reuse `internal/k8senc`
  (`ResourcesFromContainer`, `EnvMap`, `Tolerations`, `VolumesFromPod`, …) and,
  for multi-role jobs, `internal/jobset`.

### 2. Write the Emitter

Create `internal/emitter/<scheduler>/emitter.go` — the mirror image:

```go
package myscheduler

import (
    "github.com/InsightSoftmax/BAMMM/internal/emitter"
    "github.com/InsightSoftmax/BAMMM/internal/splat"
)

func init() { emitter.Register("myscheduler", emitterImpl{}) }

type emitterImpl struct{}

func (emitterImpl) Emit(job *splat.Job) ([]byte, error) { return Emit(job) }

func Emit(job *splat.Job) ([]byte, error) {
    // ... render the native format from job.Spec ...
    // Re-emit job.Spec.Extensions.<Scheduler> so a round-trip is lossless.
    // Call PriorityScale.Denormalize() to map priority back to native range.
    return out, nil
}
```

Both `execution.container` (OCI image) and `execution.script` (HPC script) can be
present; emit whichever your scheduler runs.

### 3. Register it

Add a blank import to **both** `internal/parser/all/all.go` and
`internal/emitter/all/all.go`:

```go
_ "github.com/InsightSoftmax/BAMMM/internal/parser/myscheduler"
```

Then drop it from the "Planned" list in `cmd/bammm/main.go` (the `formats`
command) and mark it ✅ in the README table.

### 4. Test it

- **Unit tests** next to the code: `parser_test.go` asserts native → SPLAT for
  representative inputs; `emitter_test.go` asserts SPLAT → native. Add a
  **round-trip** test (native → SPLAT → native, or SPLAT → native → SPLAT) to
  catch drift.
- **Corpus** (recommended): add a `Scheduler` entry to `scripts/corpus/fetch_corpus.py`
  with GitHub code-search queries + an accept predicate, then
  `make corpus SCHED=myscheduler` to pull real-world specs and measure your
  parser's coverage against them. (Corpus files are gitignored; only the
  provenance manifest is committed.)
- **Golden tests**: `internal/convert` renders cross-scheduler conversions into
  `internal/convert/testdata/golden`. After adding a scheduler, regenerate with
  `go test ./internal/convert -update` and review the diff.

### 5. If you added SPLAT fields

Only add to `internal/splat` when a concept is genuinely cross-scheduler (Tier 1
universal or Tier 2 common). If you do:

- update the field reference in [SPEC.md](../SPEC.md), and
- regenerate the JSON Schema with `make schema` (a CI drift test enforces this).

Scheduler-specific things do **not** belong in the IR — use
`extensions.<scheduler>` instead.

## Validation tiers

Conversions are validated at increasing fidelity; wire your scheduler into
whichever apply:

1. **Tier 1 — syntax:** `bammm validate` (parse / parse+convert). Free.
2. **Tier 2 — schema:** K8s emitters are validated with `kubeconform` against
   vendored CRD schemas (`make validate-schemas`).
3. **Tier 3 — admission:** the `dryrun` workflow submits emitted jobs to a real
   scheduler (kind + operators, or a controller) with dry-run/test-only flags.

## Checklist

- [ ] `internal/parser/<scheduler>/parser.go` with `init()` registration
- [ ] `internal/emitter/<scheduler>/emitter.go` with `init()` registration
- [ ] Blank imports added to both `all/all.go` files
- [ ] Unit tests + a round-trip test
- [ ] Priority scale added to `internal/splat/priority.go` (if applicable)
- [ ] Unknown fields preserved in `extensions.<scheduler>`
- [ ] Corpus config entry + coverage checked against real specs
- [ ] Golden tests regenerated (`go test ./internal/convert -update`)
- [ ] Removed from the "Planned" list; README table updated
- [ ] `make check` passes
```
