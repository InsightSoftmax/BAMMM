# User-defined translation rules

Adjust a conversion **without patching BAMMM**: supply a rules file that rewrites
the SPLAT intermediate representation between parse and emit.

```sh
bammm convert --from slurm --to pbs --rules my-rules.yaml job.sh
```

Rules operate on the SPLAT IR (after the source is parsed, before the target is
emitted), so one rule set works regardless of the source/target pair. See a
worked example in [`examples/rules/`](../examples/rules/).

## Why

When BAMMM meets something it doesn't anticipate — a site-specific resource, a
custom directive, a field a scheduler version added — it either preserves it
under `extensions.<scheduler>` (round-trips back, but doesn't translate to a
*different* scheduler) or drops it. Rules let you say "when converting X → Y,
turn *this* into *that*" yourself.

## Format

```yaml
apiVersion: bammm.io/rules/v1alpha1
rules:
  - when:
      from: slurm                       # optional: source format must equal this
      to: pbs                           # optional: target format must equal this
      has: spec.schedule.qos            # optional: path(s) must exist (string or list)
      equals:                           # optional: path == value
        spec.schedule.qos: debug
    # Actions, applied in this order when `when` matches:
    default:                            # set only if the path is absent
      spec.schedule.queue: normal
    set:                                # set (overwrite)
      spec.schedule.queue: debug-queue
    rename:                             # move a value from one path to another
      spec.schedule.account: spec.schedule.project
    remove:                             # delete path(s) (string or list)
      - spec.extensions.slurm.switches
    warn: routed debug QoS to debug-queue   # optional: printed to stderr when the rule fires
```

### Paths

Paths are **full SPLAT document paths**, exactly as they appear in
`bammm convert --to splat` output — e.g. `spec.schedule.queue`,
`metadata.name`, `spec.extensions.htcondor.requirements`. (Array indices are not
supported yet.)

### Matching

All conditions in a `when` must hold for the rule to fire. An empty `when: {}`
always matches. Multiple rules are evaluated in order; each matching rule's
actions apply cumulatively.

## Roadmap

- **CEL expressions** (`cel-go`) for `when` conditions and computed values, when
  the structural predicates above prove too limited.
- **Array-index paths** for list elements.
- Starlark or external pre/post hooks for transforms a declarative form can't
  express — an escalation, not a replacement for the rules file.
