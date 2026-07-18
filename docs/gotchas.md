# Gotchas & sharp edges

The non-obvious things about BAMMM that bite people. If a conversion "loses" a
field or a spec doesn't do what you expect, start here.

## SPLAT field names are camelCase

SPLAT is parsed as YAML-over-JSON, so field names follow the **camelCase** json
tags: `cpusPerTask`, `memoryPerTask`, `workingDir`, `maxRetries`, `nodeSelector`,
`clientId`, … — **not** `cpus_per_task` / snake_case.

The parser is lenient: an unknown field (including a snake_case misspelling) is
**silently ignored**, not rejected. So a typo doesn't error — it just quietly
drops that field from the job. If a conversion is missing something you set,
check the casing first.

Guard rails:

- Validate a SPLAT file against `schema/splat.schema.json` (strict — unknown
  fields fail) before trusting it.
- `bammm convert --from splat --to splat <file>` round-trips through the IR;
  anything you wrote that comes back missing was dropped.

## Translation is lossy on purpose

No two schedulers express the same things. BAMMM's contract is *honesty*, not
magic:

- Concepts with no equivalent on the target are **dropped with a warning** or
  **approximated** (see [SPEC.md § Known Lossy Translations](../SPEC.md#known-lossy-translations)).
- Anything BAMMM can't map to a first-class field is preserved verbatim under
  `extensions.<scheduler>` so it round-trips back to the *same* scheduler — but
  it does **not** cross to a different one.
- Run any conversion with `--report` to see how much of your corpus lands in
  `extensions.*` (a proxy for translation loss).

## Priority: direction and range

Schedulers disagree on both the numeric range and the *direction* of priority
(some treat a lower number as higher priority, nice-style). BAMMM normalizes
every native priority onto one canonical band — **higher always means higher
priority** — and denormalizes on the way out. See `internal/splat/priority.go`.

- The canonical band is `0–1000` by default. Widen it with
  `bammm convert --priority-range MIN:MAX` (e.g. `0:100000`) to reduce rounding
  loss when round-tripping schedulers with large native ranges.
- K8s schedulers prefer a named `priorityClass` over a number; that takes
  precedence where both exist.

## Container and script can coexist

A SPLAT job may set **both** `execution.container` (an OCI image) and
`execution.script` (a shell script). This is intentional: K8s emitters use the
container, HPC emitters use the script. Set both if you want one spec that runs
everywhere; set only the one you have otherwise.

## Multi-role jobs become a JobSet on K8s

A job with `spec.tasks` (multiple roles) can't fit one `batch/v1` Job, so the
Kueue and YuniKorn emitters produce a **JobSet** (`jobset.x-k8s.io`). That means
the target cluster needs the JobSet controller installed. Supporting objects
some emitters print (a Kueue `LocalQueue`, a script `ConfigMap`) are marked as
required manual prerequisites — read the comments in the output.

## Install channels

- **Homebrew** ships as a **cask** and is effectively macOS-oriented. On Linux,
  use the `.deb`/`.rpm`/`.apk` from the release, the release tarball, or
  `go install`.
- The **container image** is on GHCR: `ghcr.io/insightsoftmax/bammm`.

## Corpus specs aren't in the repo

`testdata/corpus/**` holds third-party job specs scraped from GitHub and is
**gitignored** for licensing reasons — only the `manifest.json` provenance files
are committed. Regenerate the actual specs with `make corpus SCHED=<name>` (needs
a `GITHUB_TOKEN`). See the scraper header for the query-quoting and rate-limit
gotchas that make GitHub code search cooperate.
