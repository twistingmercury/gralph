package runlog

// The ledger's lines, one type per event. Field order is the order the
// keys are written in, and every line starts with the stamp.

type stamp struct {
	Time  string `json:"time"`
	Event string `json:"event"`
}

type runStarted struct {
	stamp
	Version         string `json:"version"`
	TasksFile       string `json:"tasks_file"`
	Permissions     string `json:"permissions"`
	SandboxSettings string `json:"sandbox_settings,omitempty"`
	GateTimeout     string `json:"gate_timeout,omitempty"`
	Commit          bool   `json:"commit"`
}

type taskStarted struct {
	stamp
	Task int16  `json:"task"`
	Name string `json:"name"`
}

// stepFinished is the shape session_finished and task_finished share.
type stepFinished struct {
	stamp
	Task     int16  `json:"task"`
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration"`
}

type gateFinished struct {
	stamp
	Task     int16  `json:"task"`
	Cmd      string `json:"cmd"`
	Result   string `json:"result"`
	Timeout  string `json:"timeout"`
	Duration string `json:"duration"`
}

type committed struct {
	stamp
	Task int16  `json:"task"`
	Hash string `json:"hash"`
}

type runFinished struct {
	stamp
	Result   string `json:"result"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration"`
}
