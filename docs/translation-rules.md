# User-defined translation rules (design — planned)

> **Status: proposed, not yet implemented.** This document is the design for a
> feature that lets users adjust translations *without patching BAMMM's source*.
> It exists so the feature is specified before it is built; nothing here works
> today.

## Problem

BAMMM maps native fields to SPLAT and back. When it meets something it doesn't
anticipate — a site-specific resource, a custom directive, a field a given
scheduler version added — the current options are:

1. it lands in `extensions.<scheduler>` (preserved for round-trip, but **not**
   translated to a *different* scheduler), or
2. it's dropped.

Neither lets a user say "when converting X → Y, turn *this* into *that*" without
editing Go and rebuilding.

## Approach: a declarative rules file

A user supplies a rules file and points BAMMM at it:

```sh
bammm convert --from slurm --to pbs --rules my-rules.yaml job.sh
```

Rules operate on the **SPLAT IR** (after parse, before emit), so one rule set
works regardless of the source/target pair. Each rule is a `match` → `action`.

### Sketch

```yaml
apiVersion: bammm.io/rules/v1alpha1
rules:
  # Promote a preserved Slurm extension into a first-class field for the target.
  - when:
      from: slurm
      has: extensions.slurm.switches          # dotted path into the SPLAT IR
    set:
      placement.constraint: "network=switch"  # target-agnostic SPLAT field

  # Rename / remap a value.
  - when:
      has: schedule.qos
      equals: { schedule.qos: debug }
    set:
      schedule.queue: debug-queue

  # Drop with an explicit warning instead of silent loss.
  - when:
      to: pbs
      has: extensions.htcondor.requirements
    drop:
      warn: "HTCondor requirements expression cannot be expressed in PBS"
```

Action verbs (initial set): `set`, `rename`, `drop` (with `warn`), `default`
(set only if absent).

### Match expressions

Start with simple structural predicates (`has`, `equals`, `from`, `to`). If that
proves too limited, add an embedded **CEL** (`cel-go`) expression for conditions
and computed values — sandboxed, no code execution, e.g.:

```yaml
  - when:
      expr: 'has(spec.resources.gpu) && spec.resources.gpu.count > 4'
    set:
      schedule.queue: gpu-large
```

Starlark or external pre/post hooks are a possible later escalation for
transforms that a declarative form can't express, but the declarative rules file
is the primary interface.

## Open questions

- Precedence when multiple rules match (first-wins vs. all-apply).
- Whether rules may write into `extensions.*` (target-specific escape hatch).
- How rule-driven changes surface in `--report` and the lossiness accounting.
- Packaging: site-wide default rules vs. per-invocation `--rules`.

## When built

This feature must ship with: a `SPEC` section (or its own `SPEC-rules.md`), CLI
help for `--rules`, worked examples under `examples/rules/`, and a note in
[gotchas.md](gotchas.md) about rule precedence. Update this document from
"proposed" to the real reference at that point.
