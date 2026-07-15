// Package splat defines the SPLAT intermediate representation.
// SPLAT (Scheduler-Portable Language for Abstracting Tasks) is the common
// format that all parsers produce and all emitters consume.
// See SPEC.md for the full field reference.
package splat

import "time"

// Job is the top-level SPLAT document.
type Job struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"` // bammm.io/v1alpha1
	Kind       string   `json:"kind" yaml:"kind"`             // Job
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

// Metadata holds identity, labels, and annotations for a job.
type Metadata struct {
	Name        string            `json:"name" yaml:"name"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	ClientID    string            `json:"clientId,omitempty" yaml:"clientId,omitempty"`
}

// Spec is the desired state of a job: what to run, with what resources, and how
// to schedule it.
type Spec struct {
	Schedule             Schedule             `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	Resources            Resources            `json:"resources,omitempty" yaml:"resources,omitempty"`
	Execution            Execution            `json:"execution,omitempty" yaml:"execution,omitempty"`
	Tasks                []Task               `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	Gang                 *Gang                `json:"gang,omitempty" yaml:"gang,omitempty"`
	Array                *Array               `json:"array,omitempty" yaml:"array,omitempty"`
	Dependencies         []Dependency         `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Lifecycle            Lifecycle            `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	Placement            Placement            `json:"placement,omitempty" yaml:"placement,omitempty"`
	Output               Output               `json:"output,omitempty" yaml:"output,omitempty"`
	Notifications        *Notifications       `json:"notifications,omitempty" yaml:"notifications,omitempty"`
	FileStaging          *FileStaging         `json:"fileStaging,omitempty" yaml:"fileStaging,omitempty"`
	Volumes              []Volume             `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	WorkloadType         WorkloadType         `json:"workloadType,omitempty" yaml:"workloadType,omitempty"`
	DistributedFramework DistributedFramework `json:"distributedFramework,omitempty" yaml:"distributedFramework,omitempty"`
	Extensions           Extensions           `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// Schedule controls queue routing, priority, and timing.
type Schedule struct {
	Queue           string     `json:"queue,omitempty" yaml:"queue,omitempty"`
	Partition       string     `json:"partition,omitempty" yaml:"partition,omitempty"` // alias for queue (Slurm)
	PriorityClass   string     `json:"priorityClass,omitempty" yaml:"priorityClass,omitempty"`
	Priority        int        `json:"priority,omitempty" yaml:"priority,omitempty"` // 0–1000 normalized
	Account         string     `json:"account,omitempty" yaml:"account,omitempty"`
	Project         string     `json:"project,omitempty" yaml:"project,omitempty"`
	Bank            string     `json:"bank,omitempty" yaml:"bank,omitempty"` // Flux
	QOS             string     `json:"qos,omitempty" yaml:"qos,omitempty"`
	Walltime        *Duration  `json:"walltime,omitempty" yaml:"walltime,omitempty"`
	WalltimeMin     *Duration  `json:"walltimeMin,omitempty" yaml:"walltimeMin,omitempty"` // Slurm --time-min
	BeginAfter      *time.Time `json:"beginAfter,omitempty" yaml:"beginAfter,omitempty"`
	Deadline        *time.Time `json:"deadline,omitempty" yaml:"deadline,omitempty"`
	SignalBeforeEnd string     `json:"signalBeforeEnd,omitempty" yaml:"signalBeforeEnd,omitempty"` // e.g. "USR1@120"
	Hold            bool       `json:"hold,omitempty" yaml:"hold,omitempty"`
	Reservation     string     `json:"reservation,omitempty" yaml:"reservation,omitempty"`
	Exclusive       bool       `json:"exclusive,omitempty" yaml:"exclusive,omitempty"`
	ExclusiveMode   string     `json:"exclusiveMode,omitempty" yaml:"exclusiveMode,omitempty"`
	Oversubscribe   bool       `json:"oversubscribe,omitempty" yaml:"oversubscribe,omitempty"`
}

// Resources describes per-task resource requirements.
type Resources struct {
	Nodes            int             `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	Tasks            int             `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	TasksPerNode     int             `json:"tasksPerNode,omitempty" yaml:"tasksPerNode,omitempty"`
	TasksPerSocket   int             `json:"tasksPerSocket,omitempty" yaml:"tasksPerSocket,omitempty"`
	TasksPerCore     int             `json:"tasksPerCore,omitempty" yaml:"tasksPerCore,omitempty"`
	CPUsPerTask      int             `json:"cpusPerTask,omitempty" yaml:"cpusPerTask,omitempty"`
	MemoryPerTask    *Quantity       `json:"memoryPerTask,omitempty" yaml:"memoryPerTask,omitempty"`
	MemoryPerCPU     *Quantity       `json:"memoryPerCpu,omitempty" yaml:"memoryPerCpu,omitempty"`
	GPU              *GPURequest     `json:"gpu,omitempty" yaml:"gpu,omitempty"`
	DiskPerTask      *Quantity       `json:"diskPerTask,omitempty" yaml:"diskPerTask,omitempty"`
	GenericResources map[string]int  `json:"genericResources,omitempty" yaml:"genericResources,omitempty"`
	Limits           *ResourceLimits `json:"limits,omitempty" yaml:"limits,omitempty"`
}

// GPURequest describes GPU requirements. Count is a float to allow fractional
// GPUs (Run.ai); other schedulers round to whole GPUs.
type GPURequest struct {
	Count      float64   `json:"count,omitempty" yaml:"count,omitempty"` // float for fractions
	Type       string    `json:"type,omitempty" yaml:"type,omitempty"`
	Memory     *Quantity `json:"memory,omitempty" yaml:"memory,omitempty"`
	Fraction   float64   `json:"fraction,omitempty" yaml:"fraction,omitempty"`
	MIGProfile string    `json:"migProfile,omitempty" yaml:"migProfile,omitempty"`
	Exclusive  bool      `json:"exclusive,omitempty" yaml:"exclusive,omitempty"`
}

// ResourceLimits caps resource usage above the requested amount.
type ResourceLimits struct {
	CPUsPerTask   int       `json:"cpusPerTask,omitempty" yaml:"cpusPerTask,omitempty"`
	MemoryPerTask *Quantity `json:"memoryPerTask,omitempty" yaml:"memoryPerTask,omitempty"`
}

// Execution describes what runs: container (K8s) or script/executable (HPC).
// Both may be present; emitters choose the appropriate one.
type Execution struct {
	Container   *ContainerExecution `json:"container,omitempty" yaml:"container,omitempty"`
	Script      string              `json:"script,omitempty" yaml:"script,omitempty"`
	Executable  string              `json:"executable,omitempty" yaml:"executable,omitempty"`
	Arguments   string              `json:"arguments,omitempty" yaml:"arguments,omitempty"`
	Shell       string              `json:"shell,omitempty" yaml:"shell,omitempty"`
	WorkingDir  string              `json:"workingDir,omitempty" yaml:"workingDir,omitempty"`
	Environment Environment         `json:"environment,omitempty" yaml:"environment,omitempty"`
	Stdin       string              `json:"stdin,omitempty" yaml:"stdin,omitempty"`
}

// ContainerExecution describes an OCI container workload.
type ContainerExecution struct {
	Image            string      `json:"image,omitempty" yaml:"image,omitempty"`
	ImagePullPolicy  string      `json:"imagePullPolicy,omitempty" yaml:"imagePullPolicy,omitempty"`
	ImagePullSecrets []string    `json:"imagePullSecrets,omitempty" yaml:"imagePullSecrets,omitempty"`
	Command          []string    `json:"command,omitempty" yaml:"command,omitempty"`
	Args             []string    `json:"args,omitempty" yaml:"args,omitempty"`
	Environment      Environment `json:"environment,omitempty" yaml:"environment,omitempty"`
}

// Environment holds environment variables and references injected into a job.
type Environment struct {
	Vars                 map[string]string  `json:"vars,omitempty" yaml:"vars,omitempty"`
	Secrets              []EnvFromSecret    `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	ConfigMaps           []EnvFromConfigMap `json:"configMaps,omitempty" yaml:"configMaps,omitempty"`
	InheritFromSubmitter bool               `json:"inheritFromSubmitter,omitempty" yaml:"inheritFromSubmitter,omitempty"`
}

// EnvFromSecret binds an environment variable to a key in a secret.
type EnvFromSecret struct {
	Name       string `json:"name" yaml:"name"`
	SecretName string `json:"secretName" yaml:"secretName"`
	SecretKey  string `json:"secretKey" yaml:"secretKey"`
}

// EnvFromConfigMap binds an environment variable to a key in a config map.
type EnvFromConfigMap struct {
	Name          string `json:"name" yaml:"name"`
	ConfigMapName string `json:"configMapName" yaml:"configMapName"`
	ConfigMapKey  string `json:"configMapKey" yaml:"configMapKey"`
}

// Task is one role in a multi-role job (e.g. master, worker, ps).
type Task struct {
	Name        string         `json:"name" yaml:"name"`
	Replicas    int            `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	MinReplicas int            `json:"minReplicas,omitempty" yaml:"minReplicas,omitempty"`
	Resources   *Resources     `json:"resources,omitempty" yaml:"resources,omitempty"`
	Execution   *Execution     `json:"execution,omitempty" yaml:"execution,omitempty"`
	Lifecycle   *TaskLifecycle `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	Placement   *Placement     `json:"placement,omitempty" yaml:"placement,omitempty"`
	DependsOn   []TaskDep      `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
}

// TaskDep names another task this task depends on.
type TaskDep struct {
	Name string `json:"name" yaml:"name"`
}

// TaskLifecycle holds per-task retry and event-policy configuration.
type TaskLifecycle struct {
	MaxRetries int           `json:"maxRetries,omitempty" yaml:"maxRetries,omitempty"`
	Policies   []EventPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
}

// EventPolicy is a Volcano-style event→action rule (stored in task lifecycle).
type EventPolicy struct {
	Event   string    `json:"event,omitempty" yaml:"event,omitempty"`
	Events  []string  `json:"events,omitempty" yaml:"events,omitempty"`
	Action  string    `json:"action,omitempty" yaml:"action,omitempty"`
	Timeout *Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// Gang describes gang-scheduling requirements (all-or-nothing co-scheduling).
type Gang struct {
	MinAvailable int       `json:"minAvailable,omitempty" yaml:"minAvailable,omitempty"`
	Style        GangStyle `json:"style,omitempty" yaml:"style,omitempty"`
	Timeout      *Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// GangStyle selects hard vs soft gang-scheduling semantics.
type GangStyle string

// Gang-scheduling styles.
const (
	GangStyleSoft GangStyle = "soft"
	GangStyleHard GangStyle = "hard"
)

// Array describes an array job (many near-identical tasks over an index range).
type Array struct {
	Indices       string `json:"indices,omitempty" yaml:"indices,omitempty"` // "0-99", "1-50:2", "0-999%50"
	MaxConcurrent int    `json:"maxConcurrent,omitempty" yaml:"maxConcurrent,omitempty"`
}

// Dependency expresses an ordering constraint on another job.
type Dependency struct {
	Scheme DependencyScheme `json:"scheme" yaml:"scheme"`
	Value  string           `json:"value,omitempty" yaml:"value,omitempty"`
	Count  int              `json:"count,omitempty" yaml:"count,omitempty"` // PBS on:N
}

// DependencyScheme names the kind of dependency (after-ok, after-any, etc.).
type DependencyScheme string

// Dependency schemes.
const (
	DepAfterOK    DependencyScheme = "afterok"
	DepAfterNotOK DependencyScheme = "afternotok"
	DepAfterAny   DependencyScheme = "afterany"
	DepAfter      DependencyScheme = "after"
	DepSingleton  DependencyScheme = "singleton"
	DepBeginTime  DependencyScheme = "begin_time"
)

// Lifecycle holds job-level retry and cleanup configuration.
type Lifecycle struct {
	MaxRetries       int       `json:"maxRetries,omitempty" yaml:"maxRetries,omitempty"`
	RequeueOnFailure bool      `json:"requeueOnFailure,omitempty" yaml:"requeueOnFailure,omitempty"`
	SuccessExitCodes []int     `json:"successExitCodes,omitempty" yaml:"successExitCodes,omitempty"`
	TTLAfterFinished *Duration `json:"ttlAfterFinished,omitempty" yaml:"ttlAfterFinished,omitempty"`
}

// Placement constrains where a job's tasks may run.
type Placement struct {
	NodeSelector   map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`
	Tolerations    []interface{}     `json:"tolerations,omitempty" yaml:"tolerations,omitempty"`
	Affinity       interface{}       `json:"affinity,omitempty" yaml:"affinity,omitempty"`
	TopologySpread []interface{}     `json:"topologySpread,omitempty" yaml:"topologySpread,omitempty"`
	Topology       TopologyMode      `json:"topology,omitempty" yaml:"topology,omitempty"`
	GroupBy        string            `json:"groupBy,omitempty" yaml:"groupBy,omitempty"`
	Constraint     string            `json:"constraint,omitempty" yaml:"constraint,omitempty"`
	Prefer         string            `json:"prefer,omitempty" yaml:"prefer,omitempty"`
	Exclusive      bool              `json:"exclusive,omitempty" yaml:"exclusive,omitempty"`
	NodePools      []string          `json:"nodePools,omitempty" yaml:"nodePools,omitempty"`
}

// TopologyMode selects how tasks are spread across or packed onto nodes.
type TopologyMode string

// Topology placement modes.
const (
	TopologyScatter TopologyMode = "scatter"
	TopologyPack    TopologyMode = "pack"
	TopologyFree    TopologyMode = "free"
)

// Output controls stdout/stderr redirection for a job.
type Output struct {
	Stdout      string     `json:"stdout,omitempty" yaml:"stdout,omitempty"`
	Stderr      string     `json:"stderr,omitempty" yaml:"stderr,omitempty"`
	MergeStderr bool       `json:"mergeStderr,omitempty" yaml:"mergeStderr,omitempty"`
	OpenMode    OutputMode `json:"openMode,omitempty" yaml:"openMode,omitempty"`
}

// OutputMode selects whether output files are truncated or appended to.
type OutputMode string

// Output open modes.
const (
	OutputTruncate OutputMode = "truncate"
	OutputAppend   OutputMode = "append"
)

// Notifications configures email notifications for job lifecycle events.
type Notifications struct {
	Email  string              `json:"email,omitempty" yaml:"email,omitempty"`
	Events []NotificationEvent `json:"events,omitempty" yaml:"events,omitempty"`
}

// NotificationEvent names a job lifecycle event that can trigger a notification.
type NotificationEvent string

// Notification events.
const (
	NotifyBegin       NotificationEvent = "begin"
	NotifyEnd         NotificationEvent = "end"
	NotifyFail        NotificationEvent = "fail"
	NotifyRequeue     NotificationEvent = "requeue"
	NotifyTimeLimit50 NotificationEvent = "time_limit_50"
	NotifyTimeLimit80 NotificationEvent = "time_limit_80"
	NotifyTimeLimit90 NotificationEvent = "time_limit_90"
)

// FileStaging describes files moved in/out of the job sandbox and checkpoints.
type FileStaging struct {
	Inputs             []FileTransfer `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Outputs            []FileTransfer `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	EmbeddedFiles      []EmbeddedFile `json:"embeddedFiles,omitempty" yaml:"embeddedFiles,omitempty"`
	TransferPolicy     string         `json:"transferPolicy,omitempty" yaml:"transferPolicy,omitempty"`
	CheckpointFiles    []string       `json:"checkpointFiles,omitempty" yaml:"checkpointFiles,omitempty"`
	CheckpointExitCode int            `json:"checkpointExitCode,omitempty" yaml:"checkpointExitCode,omitempty"`
}

// FileTransfer is a single source→destination file copy.
type FileTransfer struct {
	Src string `json:"src" yaml:"src"`
	Dst string `json:"dst" yaml:"dst"`
}

// EmbeddedFile is a file whose content is inlined in the spec (e.g. a config).
type EmbeddedFile struct {
	Name        string `json:"name" yaml:"name"`
	Content     string `json:"content" yaml:"content"`
	Encoding    string `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	Permissions string `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

// Volume is a storage volume mounted into the job (K8s schedulers).
type Volume struct {
	Name      string  `json:"name" yaml:"name"`
	MountPath string  `json:"mountPath" yaml:"mountPath"`
	ReadOnly  bool    `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	PVC       string  `json:"pvc,omitempty" yaml:"pvc,omitempty"`
	ConfigMap string  `json:"configMap,omitempty" yaml:"configMap,omitempty"`
	Secret    string  `json:"secret,omitempty" yaml:"secret,omitempty"`
	HostPath  string  `json:"hostPath,omitempty" yaml:"hostPath,omitempty"`
	EmptyDir  bool    `json:"emptyDir,omitempty" yaml:"emptyDir,omitempty"`
	NFS       *NFSVol `json:"nfs,omitempty" yaml:"nfs,omitempty"`
}

// NFSVol is an NFS-backed volume source.
type NFSVol struct {
	Server string `json:"server" yaml:"server"`
	Path   string `json:"path" yaml:"path"`
}

// WorkloadType categorizes the job's workload (training, inference, etc.).
type WorkloadType string

// Workload types.
const (
	WorkloadTraining    WorkloadType = "training"
	WorkloadInteractive WorkloadType = "interactive"
	WorkloadInference   WorkloadType = "inference"
	WorkloadBatch       WorkloadType = "batch"
)

// DistributedFramework names the distributed-training framework the job uses.
type DistributedFramework string

// Distributed frameworks.
const (
	DistPyTorch    DistributedFramework = "pytorch"
	DistTensorFlow DistributedFramework = "tensorflow"
	DistMPI        DistributedFramework = "mpi"
	DistHorovod    DistributedFramework = "horovod"
	DistXGBoost    DistributedFramework = "xgboost"
	DistNone       DistributedFramework = "none"
)

// Extensions holds scheduler-specific passthrough fields for round-trip fidelity.
// All fields are raw JSON/YAML to avoid coupling to specific scheduler versions.
type Extensions struct {
	Slurm    map[string]interface{} `json:"slurm,omitempty" yaml:"slurm,omitempty"`
	PBS      map[string]interface{} `json:"pbs,omitempty" yaml:"pbs,omitempty"`
	LSF      map[string]interface{} `json:"lsf,omitempty" yaml:"lsf,omitempty"`
	HTCondor map[string]interface{} `json:"htcondor,omitempty" yaml:"htcondor,omitempty"`
	Flux     map[string]interface{} `json:"flux,omitempty" yaml:"flux,omitempty"`
	Armada   map[string]interface{} `json:"armada,omitempty" yaml:"armada,omitempty"`
	Volcano  map[string]interface{} `json:"volcano,omitempty" yaml:"volcano,omitempty"`
	Kueue    map[string]interface{} `json:"kueue,omitempty" yaml:"kueue,omitempty"`
	YuniKorn map[string]interface{} `json:"yunikorn,omitempty" yaml:"yunikorn,omitempty"`
	RunAI    map[string]interface{} `json:"runai,omitempty" yaml:"runai,omitempty"`
}

// Duration wraps time.Duration with ISO 8601 / HH:MM:SS / seconds parsing.
// See internal/splat/duration.go for parsing logic.
type Duration struct {
	d time.Duration
}

// DurationOf wraps a time.Duration as a SPLAT Duration.
func DurationOf(d time.Duration) *Duration { return &Duration{d: d} }

// Duration returns the underlying time.Duration.
func (d *Duration) Duration() time.Duration { return d.d }

// Quantity wraps a resource quantity with multi-format parsing.
// Accepts: "4Gi", "4G", "4096M", "4096" (bare = MB), "500m" (millicores for CPU).
// See internal/splat/quantity.go for parsing logic.
type Quantity struct {
	bytes int64 // canonical: bytes
}
