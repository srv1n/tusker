package main

import (
	"io/fs"
	"time"
)

type serveServer struct {
	vaultPath string
	repoRoot  string
	addr      string
	store     *RuntimeStore
	assets    fs.FS
	now       func() time.Time
	stream    *serveStreamBroker
}

type serveSnapshot struct {
	projectID         string
	projectName       string
	project           RegisteredProject
	projectRegistered bool
	workflow          Workflow
	tasks             []Note
	epics             []Note
	gates             []Note
	waves             []Note
	evidence          []Note
	decisions         []Note
	attemptNotes      []Note
	notesByID         map[string]Note
	runs              []RunStatus
	queue             map[string]automationTaskExplanation
}

type serveTokenTotals struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

type serveProjectSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Health          string `json:"health"`
	LastError       any    `json:"lastError"`
	NeedsCount      int    `json:"needsCount"`
	ActiveRuns      int    `json:"activeRuns"`
	WorstLiveness   any    `json:"worstLiveness"`
	DaemonConnected bool   `json:"daemonConnected"`
	LastPollAt      any    `json:"lastPollAt"`
}

type serveDaemonStatus struct {
	Connected        bool                `json:"connected"`
	Addr             string              `json:"addr"`
	ActiveRuns       int                 `json:"activeRuns"`
	MaxActiveRuns    int                 `json:"maxActiveRuns"`
	QueuedTasks      int                 `json:"queuedTasks"`
	LastPollAt       any                 `json:"lastPollAt"`
	StateRoot        string              `json:"stateRoot"`
	ProjectCount     int                 `json:"projectCount"`
	Projects         []RegisteredProject `json:"projects"`
	ParkedBudgetRuns int                 `json:"parkedBudgetRuns"`
	BudgetCircuit    any                 `json:"budgetCircuit"`
	InvariantCircuit any                 `json:"invariantCircuit"`
	DaemonAlive      bool                `json:"daemonAlive"`
	DaemonPID        int                 `json:"daemonPid"`
	DaemonStartedAt  any                 `json:"daemonStartedAt"`
	DaemonLastPollAt any                 `json:"daemonLastPollAt"`
}

type serveActionResult struct {
	OK              bool               `json:"ok"`
	Refused         bool               `json:"refused,omitempty"`
	Reason          string             `json:"reason"`
	Command         string             `json:"command,omitempty"`
	Output          string             `json:"output,omitempty"`
	Issue           *Issue             `json:"issue,omitempty"`
	TaskID          string             `json:"taskId,omitempty"`
	GateID          string             `json:"gateId,omitempty"`
	EvidenceID      string             `json:"evidenceId,omitempty"`
	FeedbackPath    string             `json:"feedbackPath,omitempty"`
	CanonicalStatus string             `json:"canonicalStatus,omitempty"`
	Task            *serveTaskDetail   `json:"task,omitempty"`
	Gate            *serveGateDetail   `json:"gate,omitempty"`
	Evidence        *serveEvidenceDoc  `json:"evidence,omitempty"`
	Daemon          *serveDaemonStatus `json:"daemon,omitempty"`
}

type serveEpicSummary struct {
	ID     string         `json:"id"`
	Title  string         `json:"title"`
	Counts map[string]int `json:"counts"`
}

type serveWaveSummary struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Status    string                 `json:"status"`
	LandedAt  any                    `json:"landedAt"`
	MemberIDs []string               `json:"memberIds"`
	Members   []serveWaveTaskSummary `json:"members"`
	Counts    map[string]int         `json:"counts"`
}

type serveWaveTaskSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Group  string `json:"group"`
	Status string `json:"status"`
	Proof  string `json:"proof"`
}

type serveTaskCapsule struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	WaveID          string   `json:"waveId,omitempty"`
	WaveTitle       string   `json:"waveTitle,omitempty"`
	EpicID          string   `json:"epicId"`
	EpicTitle       string   `json:"epicTitle"`
	Status          string   `json:"status"`
	Readiness       string   `json:"readiness"`
	Priority        string   `json:"priority"`
	Risk            string   `json:"risk"`
	HasGate         bool     `json:"hasGate"`
	UpdatedAt       string   `json:"updatedAt"`
	ProjectID       string   `json:"projectId"`
	RawStatus       string   `json:"rawStatus"`
	RawReadiness    string   `json:"rawReadiness"`
	ReworkCount     int      `json:"reworkCount"`
	Blockers        []string `json:"blockers"`
	Dispatchable    bool     `json:"dispatchable"`
	NextOwner       string   `json:"nextOwner"`
	NextAction      string   `json:"nextAction"`
	WorkRevision    int      `json:"workRevision"`
	ReadinessSource string   `json:"readinessSource"`
}

type serveAcceptanceRow struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Proof string `json:"proof"`
}

type serveVerificationRow struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Result  string `json:"result"`
	Detail  string `json:"detail,omitempty"`
}

type serveEvidenceCard struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
}

type serveGate struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Owner     string `json:"owner"`
	Satisfied bool   `json:"satisfied"`
	RawKind   string `json:"rawKind"`
	Question  any    `json:"question"`
	Ask       any    `json:"ask"`
	Path      any    `json:"path"`
	SpecTitle any    `json:"specTitle"`
	SpecPath  any    `json:"specPath"`
}

type serveGateDetail struct {
	serveGate
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Blocking  bool     `json:"blocking"`
	Blocks    []string `json:"blocks"`
	Reason    string   `json:"reason,omitempty"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
	Body      string   `json:"body,omitempty"`
}

type serveEvidenceDoc struct {
	ID            string   `json:"id"`
	TaskID        string   `json:"taskId"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	Covers        []string `json:"covers"`
	ArtifactPaths []string `json:"artifactPaths"`
	CreatedBy     string   `json:"createdBy"`
	CreatedAt     string   `json:"createdAt"`
	AcceptedBy    string   `json:"acceptedBy,omitempty"`
	AcceptedAt    string   `json:"acceptedAt,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	RelativePath  string   `json:"relativePath"`
}

type serveDecisionDoc struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	EpicID       string   `json:"epicId"`
	Status       string   `json:"status"`
	Decision     string   `json:"decision"`
	DecidedBy    string   `json:"decidedBy,omitempty"`
	DecidedAt    string   `json:"decidedAt,omitempty"`
	WorkStreams  []string `json:"workStreams"`
	RelativePath string   `json:"relativePath"`
}

type serveTaskDependency struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type serveTaskDetail struct {
	serveTaskCapsule
	Intent         string                 `json:"intent"`
	Acceptance     []serveAcceptanceRow   `json:"acceptance"`
	NonGoals       []string               `json:"nonGoals"`
	Verification   []serveVerificationRow `json:"verification"`
	Evidence       []serveEvidenceCard    `json:"evidence"`
	KnowledgeDelta string                 `json:"knowledgeDelta,omitempty"`
	Deps           []serveTaskDependency  `json:"deps"`
	Gates          []serveGate            `json:"gates"`
	RunHistory     []serveRunSummary      `json:"runHistory"`
}

type serveRunSummary struct {
	TaskID            string           `json:"taskId"`
	TaskTitle         string           `json:"taskTitle"`
	ProjectID         string           `json:"projectId"`
	Runner            string           `json:"runner"`
	RunnerName        string           `json:"runnerName"`
	Model             any              `json:"model"`
	Lane              string           `json:"lane"`
	LeaseState        string           `json:"leaseState"`
	LeaseStateRaw     string           `json:"leaseStateRaw"`
	Outcome           string           `json:"outcome"`
	ElapsedSec        int              `json:"elapsedSec"`
	SinceLastEventSec int              `json:"sinceLastEventSec"`
	Liveness          string           `json:"liveness"`
	Tokens            serveTokenTotals `json:"tokens"`
	AttemptCount      int              `json:"attemptCount"`
	Terminal          any              `json:"terminal"`
	Error             any              `json:"error"`
	LastHeartbeatAt   any              `json:"lastHeartbeatAt"`
	NextWakeAt        any              `json:"nextWakeAt"`
}

type serveAttempt struct {
	N           int              `json:"n"`
	Outcome     string           `json:"outcome"`
	DurationSec int              `json:"durationSec"`
	Tokens      serveTokenTotals `json:"tokens"`
	StartedAt   string           `json:"startedAt"`
}

type serveRunEvent struct {
	TS    string `json:"ts"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Level string `json:"level,omitempty"`
}

type serveRunDetail struct {
	serveRunSummary
	WorkspacePath string          `json:"workspacePath"`
	Attempts      []serveAttempt  `json:"attempts"`
	Events        []serveRunEvent `json:"events"`
}

type serveAttemptDetail struct {
	ID             string           `json:"id"`
	TaskID         string           `json:"taskId"`
	ProjectID      string           `json:"projectId"`
	Runner         string           `json:"runner"`
	Lane           string           `json:"lane"`
	Outcome        string           `json:"outcome"`
	StartedAt      string           `json:"startedAt"`
	FinishedAt     string           `json:"finishedAt,omitempty"`
	DurationSec    int              `json:"durationSec"`
	Tokens         serveTokenTotals `json:"tokens"`
	WorkspacePath  string           `json:"workspacePath,omitempty"`
	BranchName     string           `json:"branchName,omitempty"`
	PullRequestURL string           `json:"pullRequestUrl,omitempty"`
	PromptPath     string           `json:"promptPath,omitempty"`
	EventSinkPath  string           `json:"eventSinkPath,omitempty"`
	RawLogPath     string           `json:"rawLogPath,omitempty"`
	StatusPath     string           `json:"statusPath,omitempty"`
	LastError      string           `json:"lastError,omitempty"`
	LogsSummary    string           `json:"logsSummary,omitempty"`
	FinalSummary   string           `json:"finalSummary,omitempty"`
	Turns          []RunTurn        `json:"turns"`
	Events         []serveRunEvent  `json:"events"`
}

type serveFeedbackDoc struct {
	ID              string            `json:"id"`
	Date            string            `json:"date"`
	Actor           string            `json:"actor"`
	Slug            string            `json:"slug"`
	RelativePath    string            `json:"relativePath"`
	Context         string            `json:"context"`
	Friction        string            `json:"friction"`
	ProductIdea     string            `json:"productIdea"`
	Impact          string            `json:"impact"`
	Related         []string          `json:"related"`
	Theme           string            `json:"theme,omitempty"`
	PriorityHint    string            `json:"priorityHint,omitempty"`
	AffectedCommand string            `json:"affectedCommand,omitempty"`
	Fields          map[string]string `json:"fields"`
	Issues          []Issue           `json:"issues,omitempty"`
}

type serveNeedBase struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	TaskID      string `json:"taskId"`
	TaskTitle   string `json:"taskTitle"`
	Blocking    int    `json:"blocking"`
	Priority    string `json:"priority"`
	Since       string `json:"since"`
}

type serveNeedItem map[string]any

type serveDocListEntry struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	UpdatedAt string `json:"updatedAt"`
}

type serveDocOutlineEntry struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Slug  string `json:"slug"`
}

type serveDocMeta struct {
	Path        string              `json:"path"`
	Title       string              `json:"title"`
	Kind        string              `json:"kind"`
	UpdatedAt   string              `json:"updatedAt"`
	Frontmatter []serveDocFrontitem `json:"frontmatter"`
}

type serveDocFrontitem struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Locked bool   `json:"locked"`
}

type serveDocContent struct {
	serveDocMeta
	Markdown string                 `json:"markdown"`
	Outline  []serveDocOutlineEntry `json:"outline"`
	Rev      string                 `json:"rev"`
}
