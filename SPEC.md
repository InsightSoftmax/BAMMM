# BAMMM Jobspec: SPLAT Format Specification

**SPLAT** — Scheduler-Portable Language for Abstracting Tasks  
**Version:** v1alpha1  
**apiVersion:** `bammm.io/v1alpha1`

---

## Overview

SPLAT is the common interchange format at the heart of BAMMM. It is a YAML document that acts as a
superset of all major batch scheduler job specs. A job expressed in SPLAT can be translated to and
from any of the supported schedulers with high fidelity.

The format bridges two fundamentally different worlds:

| Paradigm | Schedulers | Execution model |
|---|---|---|
| **Container-native** | Armada, Volcano, Kueue, YuniKorn, Run.ai | OCI images, Kubernetes pods |
| **HPC bare-metal** | Slurm, PBS/OpenPBS, LSF, HTCondor, Flux | Executables/scripts on allocated nodes |

A SPLAT document supports both execution models simultaneously, so a job can be authored once and
translated to either family.

---

## Field Classification

Fields are grouped into three tiers:

| Tier | Description | Coverage |
|---|---|---|
| **Tier 1: Universal Core** | Fields that every scheduler understands | ~10 schedulers |
| **Tier 2: Common Extended** | Fields that most schedulers support with some variation | ~6–9 schedulers |
| **Tier 3: Extensions** | Scheduler-specific passthrough for round-trip fidelity | 1–2 schedulers |

Translators MUST handle Tier 1. Translators SHOULD handle Tier 2 where the target scheduler
supports the concept. Tier 3 fields are scheduler-namespaced and are preserved when converting
from a native format so they can be round-tripped back.

---

## Document Structure

```
apiVersion: bammm.io/v1alpha1
kind: Job

metadata:
  ...

spec:
  schedule:     # queue routing, priority, timing
  resources:    # per-task CPU/memory/GPU/disk
  execution:    # container image OR script/executable
  tasks:        # multi-role jobs (overrides execution/resources if present)
  gang:         # gang scheduling parameters
  array:        # parametric / array jobs
  dependencies: # inter-job ordering
  lifecycle:    # retry, requeue, TTL
  placement:    # node selection, topology, exclusivity
  output:       # stdout/stderr paths
  notifications:# email alerts
  fileStaging: # data movement before/after job
  volumes:      # K8s PVC/ConfigMap/hostPath mounts
  workloadType: # training | interactive | inference | batch
  extensions:   # scheduler-specific passthrough blocks
```

---

## Tier 1: Universal Core Fields

These fields translate to every supported scheduler.

### `metadata`

```yaml
metadata:
  name: string                 # job display name; used in scheduler UI and logs
  labels:                      # key-value labels (K8s labels, Slurm --comment equivalent)
    key: value
  annotations:                 # opaque key-value annotations (not interpreted by BAMMM)
    key: value
  clientId: string            # idempotency key; safe to resubmit on network failure
                               # Armada: client_id; others: deduplicated by BAMMM
```

### `spec.schedule`

```yaml
spec:
  schedule:
    queue: string              # target queue / partition name
                               # Slurm: --partition; PBS: -q; LSF: -q; Flux: --queue
                               # Armada/Volcano/Kueue/YuniKorn: queue or localQueue name
    priority: integer          # canonical 0–1000 scale by default, HIGHER = HIGHER PRIORITY (500 = normal)
                               # Each scheduler's native priority is normalized into this band on
                               # parse and denormalized back to its native range on emit, so cross-
                               # scheduler conversions stay in-range and keep the right direction.
                               # The band is configurable: `bammm convert --priority-range MIN:MAX`
                               # (e.g. 0:100000) widens it to reduce round-trip rounding loss for
                               # schedulers with large native ranges.
                               # Native ranges: Slurm 0–1000, PBS -1024..1023, HTCondor -1000..1000,
                               # Armada 0–1000. nice/fair-share style values (lower = higher) invert.
                               # Slurm: --priority; PBS: -p; HTCondor: priority; LSF: -sp; Flux: --urgency
                               # Armada/Volcano: priorityClassName is preferred; use priority_class
    priorityClass: string     # K8s PriorityClass name (K8s-based schedulers only)
                               # Takes precedence over `priority` for K8s schedulers
    account: string            # billing account / allocation
                               # Slurm: --account; PBS: -A; LSF: -P; Flux: --bank
    project: string            # sub-account project grouping
                               # Flux: --project; LSF: -G; HTCondor: +ProjectName
    walltime: duration         # maximum runtime; ISO 8601 (PT2H30M) or HH:MM:SS or seconds
                               # Slurm: --time; PBS: -l walltime; LSF: -W; Flux: --time-limit
    qos: string                # quality-of-service class (Slurm: --qos; others: ignored)
```

### `spec.resources`

The canonical per-task resource model. For HPC schedulers, "task" maps to an MPI rank / slot.
For K8s schedulers, "task" maps to a container / pod.

```yaml
spec:
  resources:
    cpusPerTask: integer     # CPUs per task
                               # Slurm: --cpus-per-task; K8s: resources.requests.cpu
                               # PBS: ncpus per chunk; LSF: -n / ptile-derived
    memoryPerTask: quantity  # memory per task; accepts "4Gi", "4G", "4096M", "4096"(MB)
                               # Canonical unit: Gi/Mi; HPC formats converted on output
    tasks: integer             # total number of tasks / MPI ranks / pods
                               # Slurm: --ntasks; PBS: select×mpiprocs; K8s: replicas sum
    nodes: integer             # node count (HPC convenience; K8s: derived from tasks)
                               # Slurm: --nodes; PBS: select count; LSF: -n / span-derived
    tasksPerNode: integer    # Slurm: --ntasks-per-node; PBS: mpiprocs per chunk
    gpu:
      count: number            # GPUs per task; float for fractions (e.g., 0.5)
                               # Slurm: --gpus-per-task; K8s: nvidia.com/gpu request
                               # Run.ai: gpuFraction when < 1
      type: string             # GPU model hint; advisory only
                               # Slurm: --gres=gpu:<type>:N; K8s: nodeSelector
      memory: quantity         # GPU VRAM request (Run.ai: gpuMemory; K8s: advisory)
      fraction: float          # 0 < x ≤ 1; Run.ai fractional GPU; else rounds up to 1
      migProfile: string      # NVIDIA MIG partition e.g. "1g.10gb"
                               # Run.ai: migProfile; K8s: nvidia.com/mig-<profile>
    diskPerTask: quantity    # local scratch disk per task
                               # Slurm: (no direct; hint for tmpfs); K8s: ephemeral-storage
                               # HTCondor: request_disk; PBS: scratch per chunk
    genericResources:         # arbitrary consumable resources
      <name>: integer          # Slurm: --gres=<name>:<count>; HTCondor: request_<name>
                               # PBS: custom chunk resource; LSF: rusage[<name>=N]
    limits:                    # K8s resource limits (optional; defaults to = requests)
      cpusPerTask: integer
      memoryPerTask: quantity
```

### `spec.execution`

```yaml
spec:
  execution:
    # ── Container mode (K8s-based schedulers) ────────────────────────────
    container:
      image: string            # OCI image reference (required for K8s schedulers)
      imagePullPolicy: Always | IfNotPresent | Never
      imagePullSecrets: [string]
      command: [string]        # entrypoint override (K8s: command; Docker: ENTRYPOINT)
      args: [string]           # arguments (K8s: args; Docker: CMD)

    # ── Script / executable mode (HPC schedulers) ─────────────────────────
    script: |                  # inline job script (mutually exclusive with executable)
      #!/bin/bash              # Slurm: sbatch script body; PBS/LSF: bsub script body
      ./my-program             # Flux: embedded as attributes.system.files.script
    executable: string         # path to binary (HTCondor: executable; Flux: command[0])
    arguments: string          # argument string (HTCondor: arguments; split for others)
    shell: string              # interpreter for script (PBS: -S; default: /bin/bash)

    # ── Common to both ────────────────────────────────────────────────────
    workingDir: string        # working directory
                               # Slurm: --chdir; PBS: derived; Flux: attributes.system.cwd
                               # K8s: container.workingDir

    environment:
      vars:                    # environment variables
        KEY: value             # Slurm: export KEY=val; K8s: env[]; Flux: attributes.system.environment
      secrets:                 # from K8s Secrets (K8s schedulers only)
        - name: ENV_VAR_NAME
          secretName: my-secret
          secretKey: secret-field
      configMaps:             # from K8s ConfigMaps (K8s schedulers only)
        - name: ENV_VAR_NAME
          configMapName: my-cm
          configMapKey: cm-field
      inheritFromSubmitter: boolean
                               # PBS: -V; HTCondor: getenv=True; LSF: -env all
                               # K8s: not applicable (always isolated)

    stdin: string              # stdin file path (HPC schedulers; K8s: N/A)
```

### `spec.output`

```yaml
spec:
  output:
    stdout: string             # path template for stdout
                               # Supported variables: {job_id}, {array_index}, {job_name}
                               # Slurm: %j→{job_id}, %a→{array_index}
                               # PBS: %J→{job_id}, %I→{array_index}
                               # LSF: %J→{job_id}, %I→{array_index}
                               # HTCondor: $(Cluster)→{job_id}, $(Process)→{array_index}
    stderr: string             # path template for stderr
    mergeStderr: boolean      # combine stdout and stderr (Slurm: no -e; PBS: -j oe)
    openMode: truncate | append   # Slurm: --open-mode; default: truncate
```

---

## Tier 2: Common Extended Fields

### `spec.schedule` (extended)

```yaml
spec:
  schedule:
    # (extends Tier 1 schedule)
    partition: string          # alias for queue; preferred terminology for Slurm users
    hold: boolean              # submit in held state
                               # Slurm: --hold; PBS: -h; LSF: -H; HTCondor: hold=True
    reservation: string        # named advance reservation
                               # Slurm: --reservation; LSF: -U; PBS: -l advres=<name>
    beginAfter: datetime      # defer start until this ISO 8601 datetime
                               # Slurm: --begin; PBS: -a; LSF: -b; HTCondor: +DeferralTime
    deadline: datetime         # hard kill-by time
                               # Slurm: --deadline; LSF: -t
    walltimeMin: duration     # minimum acceptable runtime (backfill hint)
                               # Slurm: --time-min; others: ignored (advisory)
    signalBeforeEnd: string  # "SIGNAL@SECONDS" — send signal before walltime expires
                               # Slurm: --signal; Flux: via --signal option; others: N/A
```

### `spec.gang`

```yaml
spec:
  gang:
    minAvailable: integer     # minimum tasks that must start simultaneously
                               # Volcano: spec.minAvailable; Armada: PodGroup annotation
                               # YuniKorn: sum of task group minMember
                               # Slurm: implied by --nodes (all nodes or none)
                               # Kueue: minCount sum across podSets
    style: soft | hard         # soft: fall back to normal scheduling on timeout
                               # hard: reject job if gang cannot form within timeout
                               # YuniKorn: gangSchedulingStyle annotation
                               # Run.ai: podGroupPolicy (hard=all)
    timeout: duration          # max wait for gang formation before soft fallback / rejection
                               # YuniKorn: placeholderTimeoutInSeconds
```

### `spec.array`

```yaml
spec:
  array:
    indices: string            # index range specification
                               # Formats: "0-99", "1-50:2" (step), "0-999%50" (throttle)
                               #          "1,3,7-12" (explicit), "0-99%10" (max concurrent)
                               # Slurm: --array; PBS: -J; LSF: -J "name[...]"; HTCondor: queue N
                               # Flux: --cc (copies); HTCondor: queue N from list
    maxConcurrent: integer    # max simultaneously running array elements
                               # Slurm: % suffix; PBS: % suffix; LSF: % suffix
    # Environment variable injected per element (normalized name):
    # BAMMM_ARRAY_INDEX → Slurm: SLURM_ARRAY_TASK_ID
    #                   → PBS:   PBS_ARRAY_INDEX (0-based Torque, 1-based PBS Pro)
    #                   → LSF:   LSB_JOBINDEX
    #                   → HTCondor: $(Process)
    #                   → Flux:  FLUX_JOB_CC
```

### `spec.dependencies`

```yaml
spec:
  dependencies:
    - scheme: afterok          # start only after successful completion (exit 0)
                               # Slurm: afterok; PBS: afterok; LSF: done(); Flux: afterok
      value: string            # job name or job ID
                               # NOTE: by name requires scheduler support (LSF: native;
                               #       Slurm: by ID only; Flux: by Flux job ID or name)

    - scheme: afternotok       # start only after failure
                               # Slurm: afternotok; PBS: afternotok; LSF: exit(); Flux: afternotok

    - scheme: afterany         # start after any outcome
                               # Slurm: afterany; PBS: afterany; LSF: ended(); Flux: afterany

    - scheme: after            # start after job begins execution
                               # Slurm: after; PBS: after; LSF: started(); Flux: after

    - scheme: singleton        # start after all jobs with same name+account finish
                               # Slurm: singleton (native); others: BAMMM resolves at submit time

    - scheme: begin_time       # start not before this time (ISO 8601 datetime or Unix timestamp)
                               # Slurm: --begin; PBS: -a; Flux: begin-time dependency

    - count: integer           # wait for N elements of an array to complete (PBS: on:N semantics)
                               # PBS: depend=on:N:jobid; others: approximated as afterok of last
```

### `spec.lifecycle`

```yaml
spec:
  lifecycle:
    maxRetries: integer       # retry count on failure
                               # Volcano: spec.maxRetry; Run.ai: backoffLimit
                               # K8s: .spec.backoffLimit; Slurm: (requeue + scontrol requeue)
                               # HTCondor: max_retries; Flux: --requeue-count
    requeueOnFailure: boolean  # auto-requeue on transient failures
                               # Slurm: --requeue; PBS: -r y; LSF: -r; HTCondor: via periodic_release
    successExitCodes: [integer]  # non-zero exit codes treated as success
                               # HTCondor: success_exit_code; Slurm: (script-level only)
    ttlAfterFinished: duration   # delete/clean up job record after completion
                               # Volcano: ttlSecondsAfterFinished; K8s: ttlSecondsAfterFinished
```

### `spec.placement`

```yaml
spec:
  placement:
    # ── K8s-native placement ───────────────────────────────────────────────
    nodeSelector:             # require nodes with these labels
      key: value               # K8s: nodeSelector; YuniKorn: inherits from PodSpec
                               # Slurm: --constraint (BAMMM maps to feature flags)
    tolerations: [object]      # K8s tolerations (pass-through; K8s schedulers only)
    affinity: object           # K8s NodeAffinity/PodAffinity (pass-through; K8s only)
    topologySpread: [object]  # K8s topologySpreadConstraints (pass-through; K8s only)

    # ── HPC placement ─────────────────────────────────────────────────────
    topology: scatter | pack | free
                               # scatter: one task per node (PBS: place=scatter; Slurm: --spread-job)
                               # pack:    all tasks on one node (PBS: place=pack; Slurm: --nodes=1)
                               # free:    scheduler decides (default)
    groupBy: string           # group tasks within same network/rack domain
                               # PBS: place=group=<resource>; Slurm: --switches (approximation)
    constraint: string         # hardware feature constraint string
                               # Slurm: --constraint (e.g., "avx512&infiniband")
                               # Flux: attributes.system.constraints.properties[] (BAMMM splits on &)
                               # PBS: select[...] condition; LSF: select[...] expression
    prefer: string             # soft placement preference (Slurm: --prefer; advisory for others)
    exclusive: boolean         # exclusive node access; no other jobs share the node
                               # Slurm: --exclusive; PBS: place=excl; LSF: -x
                               # K8s: approximated via taints; not natively supported

    # ── Ordered node pool preference (Run.ai, Kueue flavors) ──────────────
    nodePools: [string]       # ordered preference list of node pools / resource flavors
                               # Run.ai: nodePools; Kueue: resourceFlavor preference (advisory)
```

### `spec.notifications`

```yaml
spec:
  notifications:
    email: string              # recipient address
                               # Slurm: --mail-user; PBS: -M; LSF: -u; HTCondor: notify_user
    events:                    # events that trigger notification
      - begin                  # Slurm: BEGIN; PBS: b; LSF: -B
      - end                    # Slurm: END; PBS: e; LSF: -N
      - fail                   # Slurm: FAIL; PBS: a; LSF: (end email covers this)
      - requeue                # Slurm: REQUEUE
      - time_limit_50          # Slurm: TIME_LIMIT_50 (50% of walltime consumed)
      - time_limit_80          # Slurm: TIME_LIMIT_80
      - time_limit_90          # Slurm: TIME_LIMIT_90
    # NOTE: K8s-based schedulers (Armada, Volcano, Kueue, YuniKorn, Run.ai) do not have
    # native email notification. BAMMM can optionally generate a notification sidecar.
```

### `spec.file_staging`

```yaml
spec:
  fileStaging:
    inputs:                    # copy files TO the compute node before job starts
      - src: string            # source: local path, s3://..., gs://..., host:/path
        dst: string            # destination path on compute node
                               # PBS: stagein= ; LSF: -f "src > dst"
                               # HTCondor: transfer_input_files; Flux: (use embedded_files)
    outputs:                   # copy files FROM the compute node after job completes
      - src: string            # source path on compute node
        dst: string            # destination: path, s3://..., gs://...
                               # PBS: stageout= ; LSF: -f "src < dst"; HTCondor: transfer_output_files
    embeddedFiles:            # inline files in the jobspec (Flux-native; others: sidecar copy)
      - name: string           # filename relative to working_dir
        content: string        # file content
        encoding: utf-8 | base64
        permissions: string    # octal string, e.g., "0755"
    transferPolicy: always | if_needed | never
                               # HTCondor: should_transfer_files; others: always assumed
    checkpointFiles: [string] # files to preserve across retries / checkpoint cycles
                               # HTCondor: transfer_checkpoint_files; others: user-managed
```

### `spec.volumes` (K8s schedulers)

```yaml
spec:
  volumes:
    - name: string
      mountPath: string
      readOnly: boolean
      # Exactly one source:
      pvc: string              # PVC claim name
      configMap: string       # ConfigMap name
      secret: string           # Secret name
      hostPath: string        # host directory (requires hostPath permission)
      emptyDir: boolean       # ephemeral tmpfs scratch
      nfs:
        server: string
        path: string
```

### `spec.tasks` (multi-role jobs)

When `tasks` is present, it overrides `spec.resources` and `spec.execution`. Each task defines
a named role with independent resources and execution config.

```yaml
spec:
  tasks:
    - name: string             # role name (master, worker, ps, evaluator, etc.)
      replicas: integer        # pod/slot count for this role
      minReplicas: integer    # minimum for gang (YuniKorn: taskGroup.minMember)
      resources: <resources>   # per-task resources for this role (overrides top-level)
      execution: <execution>   # execution config for this role (overrides top-level)
      lifecycle:
        maxRetries: integer
        policies:              # Volcano-style event/action FSM (Volcano only; see extensions)
          - event: string
            action: string
            timeout: duration
      placement: <placement>   # per-role placement constraints (YuniKorn: per task group)
      dependsOn:              # intra-job task ordering (Volcano: DependsOn)
        - name: string         # task name that must complete before this task starts
```

### `spec.workload_type` and `spec.distributed_framework`

```yaml
spec:
  workloadType: training | interactive | inference | batch
                               # Run.ai: TrainingWorkload vs InteractiveWorkload vs InferenceWorkload
                               # interactive = long-running, lower priority, non-preemptible by default
                               # training = finite, preemptible by default
                               # inference = latency-sensitive serving
                               # batch = generic (default)
  distributedFramework: pytorch | tensorflow | mpi | horovod | xgboost | none
                               # Run.ai: distributedFramework; Volcano: plugins (pytorch, tensorflow, mpi)
                               # Slurm: --mpi= option; PBS: mpiprocs chunk resource
```

---

## Tier 3: Extensions (Scheduler-Specific Passthrough)

These blocks are populated when converting FROM a native format and are used when converting
back TO that format. They preserve information that has no generic equivalent.

When a field exists in both the generic spec and an extension block, the **extension block takes
precedence** for its target scheduler. This enables scheduler-specific tuning on top of a
generic base spec.

```yaml
spec:
  extensions:

    # ── Slurm ──────────────────────────────────────────────────────────────
    slurm:
      partition: string                  # explicit partition (overrides spec.schedule.queue)
      mpi: pmix_v4 | openmpi | pmi2 | none
      burst_buffer: string               # --bb spec string
      burst_buffer_file: string          # --bbf path
      time_min: duration                 # --time-min backfill hint
      wckey: string                      # workload characterization key
      profile: [task, energy, filesystem, network]
      switches: integer                  # max leaf switches
      switches_max_wait: duration
      kill_on_bad_exit: boolean          # --kill-on-bad-exit
      spread_job: boolean                # --spread-job
      no_kill: boolean                   # --no-kill on node failure
      overcommit: boolean                # --overcommit
      sockets_per_node: integer
      cores_per_socket: integer
      threads_per_core: integer
      ntasks_per_socket: integer
      ntasks_per_core: integer
      ntasks_per_gpu: integer
      cpus_per_gpu: integer
      mem_per_gpu: quantity
      gpu_bind: closest | none | single | map_gpu | mask_gpu
      gres_flags: string
      network: string                    # Cray network binding
      het_components:                    # heterogeneous job components
        - resources: <resources>
          schedule: <schedule>
          execution: <execution>
          placement: <placement>

    # ── PBS / OpenPBS ─────────────────────────────────────────────────────
    pbs:
      place: string                      # full place expression e.g. "scatter:excl:group=rack"
      stagein: [string]                  # "localfile@host:remotefile"
      stageout: [string]
      aoe: string                        # Alternate Operating Environment (diskless boot image)
      checkpoint_interval: integer       # minutes of CPU time between checkpoints
      mpiprocs_per_chunk: integer        # mpiprocs= per select chunk
      ompthreads_per_chunk: integer      # ompthreads= per select chunk
      mixed_chunks: string               # raw select= string for heterogeneous chunks
                                         # e.g. "1:ncpus=16:mem=64gb+4:ncpus=4:ngpus=1"

    # ── LSF ────────────────────────────────────────────────────────────────
    lsf:
      app_profile: string                # -app: admin-defined resource template
      service_class: string              # -sla: SLA scheduling class
      license_project: string            # -Lp: License Scheduler project
      rusage_string: string              # raw -R "rusage[...]" expression (takes precedence)
      compute_unit:                      # cu[] topology grouping
        type: rack | chassis | enclosure | switch
        policy: pack | balance | any
        max_units: integer
        exclusive: boolean
      same_resource: [string]            # same[resource:resource] homogeneity constraint
      order_expression: string           # order[!cpupeak:!ut] host sorting
      autoresizable: boolean             # -ar: allow dynamic slot add/remove
      frequency: string                  # target CPU frequency
      pre_exec: string                   # -E: pre-execution command on exec host
      post_exec: string                  # -Ep: post-execution command
      cpu_time_limit: duration           # -c: per-process CPU time limit
      stack_limit: quantity              # -S: stack size limit
      thread_limit: integer              # -T: concurrent thread limit
      run_window: string                 # time-of-day scheduling window
      gpu_options: string                # raw -gpu "..." string

    # ── HTCondor ───────────────────────────────────────────────────────────
    htcondor:
      universe: vanilla | docker | container | parallel | java | vm | grid | local | scheduler
      requirements: string               # ClassAd expression — OPAQUE, non-portable
                                         # e.g. "Machine =?= \"gpu-01\" && HasSingularity"
      rank: string                       # ClassAd rank expression — OPAQUE, non-portable
      periodic_hold: string              # ClassAd expression evaluated by schedd daemon
      periodic_hold_reason: string
      periodic_hold_subcode: integer
      periodic_release: string           # ClassAd expression
      periodic_remove: string            # ClassAd expression
      periodic_vacate: string            # ClassAd expression
      on_exit_hold: string               # ClassAd expression evaluated at exit
      on_exit_remove: string             # ClassAd expression evaluated at exit
      checkpointExitCode: integer      # exit with this code = checkpoint saved, requeue
      concurrency_limits: [string]       # named token pool limits e.g. ["cms.higgs:5"]
      max_materialize: integer           # late materialization cap
      max_idle: integer                  # idle job cap for factory jobs
      retry_request_memory: [quantity]   # escalating memory on each retry
      retry_request_disk: [quantity]     # escalating disk on each retry
      project_name: string               # +ProjectName ClassAd attribute
      accounting_group: string           # +AccountingGroup ClassAd attribute
      batch_name: string                 # group label in condor_q
      log: string                        # HTCondor event log path
      keep_claim_idle: integer           # seconds to hold machine between jobs
      stream_output: boolean
      stream_error: boolean
      encrypt_input_files: [string]
      encrypt_output_files: [string]
      transfer_output_remaps: map<string, string>
      output_destination: string         # plugin-based output (s3://...)
      grid_resource: string              # universe=grid: "batch pbs login.host.edu"
      machine_count: integer             # universe=parallel: slot count
      dagman: object                     # DAGMan workflow spec (opaque; see DAGMan docs)

    # ── Flux / Fluxion ─────────────────────────────────────────────────────
    flux:
      bank: string                       # accounting bank (--bank; overrides spec.schedule.account)
      urgency: integer                   # 0–31 scheduling urgency (--urgency)
      resource_graph: object             # raw RFC 14 resources[] block (takes precedence over
                                         # spec.resources; for topology-precise specs)
      symbolic_dependencies:             # RFC 26 named publish/subscribe dependencies
        - scheme: string                 # "string" or "fluid"
          value: string                  # named data artifact
          type: in | out | inout         # consume / produce / both
          scope: user | global           # visibility scope
      embeddedFiles:                    # RFC 37 files inline in jobspec
        <name>:
          encoding: utf-8 | base64
          data: string
          size: integer
      preemptible_after: float           # seconds of runtime before job becomes preemptible
      count_expression:                  # RFC 45 algebraic resource count
        min: integer
        max: integer
        operator: "+" | "*" | "^"
        operand: integer
      constraints_spec: object           # raw RFC 31 constraints object
      shell_options: object              # attributes.system.shell.options
      user_attributes: object            # attributes.user free-form namespace

    # ── Armada ─────────────────────────────────────────────────────────────
    armada:
      job_set_id: string                 # logical job group (collective cancel/monitor)
      scheduler_name: string             # named scheduler plugin on target cluster
      ingress:                           # expose job ports via K8s Ingress
        - ports: [integer]
          tls_enabled: boolean
          cert_name: string
          use_cluster_ip: boolean
          annotations: map<string, string>
      services:                          # expose job ports via K8s Service
        - type: NodePort | Headless
          ports: [integer]
          name: string
      namespace: string                  # K8s namespace on target cluster

    # ── Volcano ────────────────────────────────────────────────────────────
    volcano:
      min_success: integer               # min tasks that must succeed (partial completion)
      max_retry: integer                 # job-level retry count
      ttl_seconds: integer               # cleanup TTL after completion
      running_estimate: duration         # estimated runtime hint for scheduling
      plugins:                           # mutation plugins applied at admission
        - gang                           # gang scheduling (usually automatic)
        - svc                            # headless Service for DNS discovery
        - ssh                            # inject SSH keys + hostfile for mpirun
        - env                            # inject VC_TASK_INDEX, VC_TASK_REPLICAS etc.
        - pytorch                        # PyTorch distributed training env setup
        - tensorflow                     # TF distributed training env setup
        - mpi                            # MPI hostfile + env
        - paddle | mxnet | ray           # framework-specific env injection
      policies:                          # job-level lifecycle FSM
        - event: PodFailed | PodEvicted | PodPending | PodRunning | TaskCompleted |
                 TaskFailed | Unknown | OutOfSync | CommandIssued | JobUpdated | "*"
          action: AbortJob | TerminateJob | CompleteJob | ResumeJob | RestartJob |
                  RestartTask | RestartPod | RestartPartition | SyncJob | EnqueueJob
          timeout: duration              # delay before action fires
          events: [string]              # alternative to event: trigger on multiple events
          exit_code: integer             # trigger on specific exit code
      network_topology: object           # NetworkTopologySpec

    # ── Kueue ──────────────────────────────────────────────────────────────
    kueue:
      local_queue: string                # LocalQueue name (namespace-scoped)
                                         # takes precedence over spec.schedule.queue
      workload_priority_class: string    # WorkloadPriorityClass name
      max_execution_time: integer        # seconds; Workload.spec.maximumExecutionTimeSeconds
      min_count: integer                 # elastic admission: minimum pod count per podSet
      reclaim_pods:                      # release quota for completed pods mid-run
        - pod_set_name: string
          count: integer

    # ── YuniKorn (Apache YuniKorn) ─────────────────────────────────────────
    yunikorn:
      app_id: string                     # applicationId label/annotation
      disable_state_aware: boolean       # opt out of state-aware app sorting
      gang_scheduling_style: Soft | Hard
      gang_timeout: integer              # seconds; placeholderTimeoutInSeconds

    # ── Run.ai ──────────────────────────────────────────────────────────────
    runai:
      project: string                    # Run.ai project (maps to K8s namespace)
      department: string                 # organizational grouping above project
      interactive: boolean               # use InteractiveWorkload CRD
      large_shm: boolean                 # mount /dev/shm as large tmpfs for PyTorch DDP
      minReplicas: integer              # elastic distributed: minimum worker count
      max_replicas: integer              # elastic distributed: maximum worker count
      nodePools: [string]               # ordered node pool preference list
      migProfile: string                # NVIDIA MIG profile e.g. "1g.10gb"
      pod_group_policy: all | none       # gang scheduling policy
      run_as_user: boolean               # run as container's default user
      service_account: string            # Kubernetes service account
```

---

## Template Variables

BAMMM normalizes output path and environment variable templates. Use these in `output.stdout`,
`output.stderr`, and `execution.environment.vars`:

| BAMMM variable | Slurm | PBS | LSF | HTCondor | Flux | Notes |
|---|---|---|---|---|---|---|
| `{job_id}` | `%j` | `%J` | `%J` | `$(Cluster)` | `{{id}}` | Scheduler-assigned job ID |
| `{array_job_id}` | `%A` | `%J` | `%J` | `$(Cluster)` | — | Parent array job ID |
| `{array_index}` | `%a` | `%I` | `%I` | `$(Process)` | `$FLUX_JOB_CC` | Array element index |
| `{job_name}` | `%x` | — | — | — | — | Job name |
| `{node_hostname}` | `%N` | — | — | — | — | First allocated node |
| `{task_index}` | — | — | — | — | — | Intra-job task index (Volcano: `VC_TASK_INDEX`) |

---

## Resource Unit Conventions

BAMMM accepts units from all scheduler families and normalizes to Kubernetes units on output for
K8s schedulers, and to plain integers (MB / GB) for HPC schedulers.

| BAMMM input | Canonical (K8s out) | HPC out |
|---|---|---|
| `"4Gi"` | `4Gi` | `4096M` (LSF/Slurm) / `4gb` (PBS) |
| `"4G"` | `4Gi` (≈3.72, warn) or exact `4000Mi` | `4000M` |
| `"4096M"` | `4Gi` | `4096M` |
| `"4096"` (bare) | interpreted as MB → `4Gi` | `4096M` |
| `"0.5"` cpu | `500m` | `1` (round up) |
| `"500m"` cpu | `500m` | `1` (round up, warn if < 1000m) |

**Duration normalization:** ISO 8601 (`PT2H30M`) is canonical. All of the following are accepted:
`2:30:00`, `150m`, `9000s`, `9000` (seconds), `2-12:00:00` (Slurm D-HH:MM:SS).

---

## Scheduler Translation Matrix

This table maps the key BAMMM fields to their native equivalents. "~" = approximated; "✗" = no support.

| BAMMM field | Armada | Volcano | Kueue | Slurm | HTCondor | LSF | PBS | YuniKorn | Run.ai | Flux |
|---|---|---|---|---|---|---|---|---|---|---|
| `schedule.queue` | queue | spec.queue | LocalQueue name | --partition | — | -q | -q | queue annotation | project | --queue |
| `schedule.priority` | priority (float) | priorityClassName | WorkloadPriorityClass | --priority | priority | -sp | -p | (queue-derived) | priorityClass | --urgency |
| `schedule.account` | — | — | — | --account | +AccountingGroup | -P | -A | — | — | --bank |
| `schedule.walltime` | K8s activeDeadline | K8s activeDeadline | maximumExecutionTime | --time | allowed_job_duration | -W | -l walltime | — | — | --time-limit |
| `resources.cpus_per_task` | resources.requests.cpu | resources.requests.cpu | resources.requests.cpu | --cpus-per-task | request_cpus | -n/ptile-derived | ncpus per chunk | resources.requests.cpu | cpuCoreRequest | cores per slot |
| `resources.memory_per_task` | resources.requests.memory | resources.requests.memory | resources.requests.memory | --mem-per-cpu × cpus | request_memory | rusage[mem=] | mem per chunk | resources.requests.memory | cpuMemoryRequest | memory per slot |
| `resources.gpu.count` | nvidia.com/gpu | nvidia.com/gpu | nvidia.com/gpu | --gpus-per-task | request_gpus | rusage[ngpus_excl_p=] | ngpus per chunk | nvidia.com/gpu | gpu / gpuFraction | gpu per slot |
| `resources.disk_per_task` | ephemeral-storage | ephemeral-storage | ephemeral-storage | (tmpfs mount) | request_disk | rusage[scratch=] | scratch per chunk | ephemeral-storage | — | — |
| `execution.container.image` | pod_specs[].containers[].image | template.spec.containers[].image | pod template image | ✗ (singularity hint) | docker_image | ✗ | ✗ | pod template image | image | ✗ |
| `execution.script` | ✗ (use init container) | ✗ (use init container) | ✗ | sbatch script body | executable+arguments | bsub script body | qsub script body | ✗ | ✗ | command in jobspec |
| `array.indices` | ✗ (N separate submissions) | ✗ (replicas) | batch/v1 array | --array | queue N | -J "name[...]" | -J range | ✗ | completions | --cc |
| `dependencies[afterok]` | ✗ (external orchestrator) | ✗ | ✗ | afterok:jobid | DAGMan PARENT/CHILD | done(jobid) | afterok:jobid | ✗ | ✗ | afterok:jobid |
| `gang.min_available` | PodGroup annotation | spec.minAvailable | Workload minCount | implied by --nodes | ✗ | ✗ | ✗ | taskGroup.minMember | podGroupPolicy=all | (all nodes) |
| `placement.exclusive` | nodeSelector/taint | toleration | ResourceFlavor taint | --exclusive | requirements | -x | place=excl | — | — | exclusive=true |
| `notifications.email` | ✗ | ✗ | ✗ | --mail-user | notify_user | -u | -M | ✗ | ✗ | ✗ |
| `file_staging.inputs` | initContainer | initContainer | initContainer | (manual) | transfer_input_files | -f src > dst | stagein= | initContainer | pvc/s3 | embedded_files |
| `lifecycle.max_retries` | — | spec.maxRetry | backoffLimit | (script-level) | max_retries | -Q | — | — | backoffLimit | --requeue-count |

---

## Known Lossy Translations

Some concepts cannot be faithfully represented in the generic format. These are documented here
so tooling can warn users.

### Fundamental losses (cannot be approximated)

| Concept | Scheduler | Issue |
|---|---|---|
| ClassAd `requirements` / `rank` expressions | HTCondor | Turing-complete machine-matching expressions. Stored in `extensions.htcondor.requirements` as opaque strings; cannot be translated to any other scheduler. |
| Flux hierarchical resource graph | Flux | The RFC 14 `resources` tree captures physical topology (socket → core → GPU locality). Translation to flat "N cpus + M gpus" loses NUMA relationships. Round-trip via `extensions.flux.resource_graph` preserves the original. |
| LSF License Scheduling (`-Lp`) | LSF | FlexLM token scheduling is unique to LSF. `extensions.lsf.license_project` preserves the value; other schedulers will ignore it. |
| Flux symbolic named dependencies | Flux | `string`/`fluid` scheme dependencies by named data artifacts cannot be expressed as `afterok:jobid`. Stored in `extensions.flux.symbolic_dependencies`. |
| HTCondor `periodic_*` policies | HTCondor | Daemon-evaluated time-triggered ClassAd expressions. No equivalent in any other scheduler. Stored opaquely. |
| PBS AOE (diskless boot image) | PBS | Assigning a boot image per-job is unique to PBS Pro. Stored in `extensions.pbs.aoe`. |
| Armada multi-cluster pool routing | Armada | Jobs are placed on any cluster in a pool; the submitter doesn't control which physical cluster. No concept in single-cluster schedulers. |
| LSF `cu[]` compute unit topology | LSF | Rack/chassis grouping with pack/balance policy. `extensions.lsf.compute_unit` preserves it; approximated as `placement.constraint` for Slurm. |
| Run.ai GPU fraction enforcement | Run.ai | VRAM-level partitioning via Run.ai's virtual GPU driver. On other schedulers, `gpu.fraction < 1` is rounded up to 1 (whole GPU) with a warning. |

### Approximated translations (information may be lost)

| Concept | From | To | Approximation |
|---|---|---|---|
| `gang.min_available` | Any | HTCondor, LSF, PBS | These schedulers don't support gang scheduling natively. BAMMM issues a warning; the job runs without gang guarantees. |
| `dependencies[afterok]` | Slurm/PBS/Flux | Armada, Volcano, Kueue, YuniKorn, Run.ai | These K8s schedulers have no dependency mechanism. BAMMM generates a warning; orchestration must be handled externally (Argo, Airflow). |
| `notifications.email` | Slurm/PBS/LSF/HTCondor | K8s schedulers | K8s schedulers have no email notification. BAMMM can optionally generate a notification Job/CronJob as a sidecar; disabled by default. |
| `placement.exclusive` | Slurm/PBS/LSF | K8s schedulers | Expressed as a node taint + toleration; enforced only if the cluster admin has set up the taint. |
| `execution.container.image` | K8s schedulers | Slurm/PBS/LSF/HTCondor | Container image name is preserved as a comment/annotation in the HPC script. Execution requires Singularity/Apptainer on the target cluster; BAMMM generates `singularity exec docker://<image>` wrapper. |
| `execution.script` | HPC schedulers | K8s schedulers | Script is embedded in a ConfigMap and mounted into the container. A default base image (`bash:latest`) is used unless `execution.container.image` is also specified. |
| `array.indices` | Slurm/PBS/LSF | Armada/Volcano/Kueue/YuniKorn/Run.ai | Translated as N separate job submissions with `BAMMM_ARRAY_INDEX` env var set. No scheduler-side throttle. |
| `dependencies[singleton]` | Slurm | PBS/LSF/HTCondor/Flux/K8s | BAMMM resolves the name to a job ID at submit time (requires BAMMM to have scheduler access). |
| `resources.gpu.type` | Any | Most | Used as a `nodeSelector` hint; not all schedulers support GPU model filtering natively. |
| Volcano event/action policies | Volcano | Others | Other schedulers support only `max_retries`. Conditional completion logic (`if TaskCompleted → CompleteJob`) cannot be expressed generically. |
| `spec.workloadType: interactive` | Run.ai | Others | Interactive scheduling semantics (non-preemptible, long-running) are approximated as high-priority + `--exclusive` where applicable. |
| Slurm heterogeneous jobs | Slurm | Others | `extensions.slurm.het_components` preserves the spec. Only Slurm can execute het-jobs; other schedulers receive only the first component with a warning. |

---

## Examples

See the `examples/` directory for complete working examples:

- `examples/simple-job.yaml` — minimal Tier 1 job
- `examples/multi-task-job.yaml` — multi-role gang-scheduled job
- `examples/array-job.yaml` — parametric array sweep
- `examples/hpc-mpi-job.yaml` — MPI job targeting HPC schedulers
- `examples/ml-training-job.yaml` — distributed PyTorch training (K8s schedulers)
- `examples/container-hpc-bridge.yaml` — same job targeting both container and HPC schedulers
- `examples/full-reference.yaml` — complete reference spec with all fields annotated

---

## Versioning

The SPLAT format uses `apiVersion: bammm.io/v1alpha1`. During the `v1alpha1` phase, field names
may change between BAMMM releases; migrations will be provided. Promotion to `v1beta1` will
signal API stability.

---

## Appendix: Why SPLAT?

**SPLAT** — Scheduler-Portable Language for Abstracting Tasks.

In keeping with BAMMM's sound-effect naming tradition, SPLAT captures the idea of a job
specification being "splatted" across any scheduler surface it encounters. Where BAMMM is the
mechanism that multiplexes, SPLAT is the shape the job takes before it hits.

Alternative names considered:

| Acronym | Expansion | Notes |
|---|---|---|
| **BABEL** | Batch Abstraction for Bridging Execution Layers | Tower of Babel theme (universal translator); conflicts with Babel.js |
| **BLEND** | Batch Language for ENvironment Description | Blending scheduler formats; less memorable |
| **KOINE** | Kind Of INterchangeable Notation for Everything | Koine Greek = common lingua franca; possibly too obscure |
| **MASH** | Multi-scheduler Abstraction Specification for HPC | Simple; MASH is also a TV show |
