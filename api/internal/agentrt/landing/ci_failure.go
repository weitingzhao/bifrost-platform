package landing

// CIFailure is what the landing CI reader saw of a red run (R5:5.7-10):
// the pipeline and run, and each failed TaskRun with its failure message
// and, when the reader could read it, the end of its failed step's log.
// ReadError says why the TaskRuns could not be read, when they could not.
// It goes into the failed landing_ci check the task's next round is told of.
type CIFailure struct {
	Pipeline  string         `json:"pipeline"`
	Run       string         `json:"run"`
	TaskRuns  []CIFailedTask `json:"task_runs"`
	ReadError string         `json:"read_error,omitempty"`
}

// CIFailedTask is one failed TaskRun of a red landing CI.
type CIFailedTask struct {
	Name         string `json:"name"`
	PipelineTask string `json:"pipeline_task,omitempty"`
	Step         string `json:"step,omitempty"`
	Message      string `json:"message,omitempty"`
	LogTail      string `json:"log_tail,omitempty"`
}

// Bounds of a CIFailure: the reader keeps at most CIFailedTasksMax failed
// TaskRuns, each message cut to CIFailureMessageBytes and each log to its
// last CIFailureLogBytes.
const (
	CIFailedTasksMax      = 8
	CIFailureMessageBytes = 2048
	CIFailureLogBytes     = 4096
)
