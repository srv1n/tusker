package main

import (
	"io/fs"
	"sync"
	"time"
)

type serveServer struct {
	vaultPath         string
	repoRoot          string
	addr              string
	store             *RuntimeStore
	assets            fs.FS
	now               func() time.Time
	stream            *serveStreamBroker
	operatorActor     string
	reconcileStatus   func(string) adaptiveProjectReconcileStatus
	snapshotMu        sync.Mutex
	snapshots         map[string]*serveSnapshotEntry
	refreshMu         sync.Mutex
	refreshedAt       map[string]time.Time
	summaryMu         sync.Mutex
	summary           *serveSnapshot
	summaryAt         time.Time
	mutationToken     string
	requireCapability bool
	requestAdmission  chan struct{}
	streamAdmission   chan struct{}
}

type serveSnapshotEntry struct {
	project     RegisteredProject
	snapshot    serveSnapshot
	contentHash string
	err         error
	ready       bool
	building    bool
	invalid     bool
	done        chan struct{}
	builtAt     time.Time
	buildCount  uint64
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
	docs              []serveDocListEntry
	needs             []serveNeedItem
	notesByID         map[string]Note
	runs              []RunStatus
	queue             map[string]automationTaskExplanation
	openP0Escalation  bool
}

type serveProjectSummary struct {
	ID                      string                            `json:"id"`
	Name                    string                            `json:"name"`
	RepoRoot                string                            `json:"repoRoot"`
	VaultRoot               string                            `json:"vaultRoot"`
	AutomationEnabled       bool                              `json:"automationEnabled"`
	AutomationSource        string                            `json:"automationSource"`
	DispatchScope           automationDispatchScopeProjection `json:"dispatchScope"`
	WorkspaceMode           string                            `json:"workspaceMode"`
	WorkspaceSource         string                            `json:"workspaceSource"`
	MaxActiveRunsPerProject int                               `json:"maxActiveRunsPerProject"`
	ConcurrencySource       string                            `json:"concurrencySource"`
	Health                  string                            `json:"health"`
	LastError               any                               `json:"lastError"`
	NeedsCount              int                               `json:"needsCount"`
	ActiveRuns              int                               `json:"activeRuns"`
	WorstLiveness           any                               `json:"worstLiveness"`
	DaemonConnected         bool                              `json:"daemonConnected"`
	LastPollAt              any                               `json:"lastPollAt"`
	Reconciliation          adaptiveProjectReconcileStatus    `json:"reconciliation"`
}

type serveDaemonStatus struct {
	Connected                  bool                `json:"connected"`
	Addr                       string              `json:"addr"`
	ActiveRuns                 int                 `json:"activeRuns"`
	MaxActiveRuns              int                 `json:"maxActiveRuns"`
	QueuedTasks                int                 `json:"queuedTasks"`
	LastPollAt                 any                 `json:"lastPollAt"`
	StateRoot                  string              `json:"stateRoot"`
	ProjectCount               int                 `json:"projectCount"`
	Projects                   []RegisteredProject `json:"projects"`
	CrashLoop                  any                 `json:"crashLoop"`
	InvariantCircuit           any                 `json:"invariantCircuit"`
	DiskPressure               DiskPressureStatus  `json:"diskPressure"`
	DaemonAlive                bool                `json:"daemonAlive"`
	DaemonDownReason           any                 `json:"daemonDownReason"`
	DaemonPID                  int                 `json:"daemonPid"`
	DaemonStartedAt            any                 `json:"daemonStartedAt"`
	DaemonLastPollAt           any                 `json:"daemonLastPollAt"`
	ManagedByLaunchd           bool                `json:"managedByLaunchd"`
	LaunchdInstalled           bool                `json:"launchdInstalled"`
	DaemonRunMode              string              `json:"daemonRunMode"`
	LastRestartCause           string              `json:"lastRestartCause,omitempty"`
	PersistentEscalationBanner bool                `json:"persistentEscalationBanner"`
}

type serveActionResult struct {
	OK              bool                `json:"ok"`
	Refused         bool                `json:"refused,omitempty"`
	Reason          string              `json:"reason"`
	Command         string              `json:"command,omitempty"`
	Output          string              `json:"output,omitempty"`
	Issue           *Issue              `json:"issue,omitempty"`
	TaskID          string              `json:"taskId,omitempty"`
	GateID          string              `json:"gateId,omitempty"`
	EvidenceID      string              `json:"evidenceId,omitempty"`
	FeedbackPath    string              `json:"feedbackPath,omitempty"`
	ProjectID       string              `json:"projectId,omitempty"`
	CanonicalStatus string              `json:"canonicalStatus,omitempty"`
	Discard         *serveDiscardImpact `json:"discard,omitempty"`
	Task            *serveTaskDetail    `json:"task,omitempty"`
	Gate            *serveGateDetail    `json:"gate,omitempty"`
	Evidence        *serveEvidenceDoc   `json:"evidence,omitempty"`
	Daemon          *serveDaemonStatus  `json:"daemon,omitempty"`
}

type serveDiscardDependent struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type serveDiscardImpact struct {
	TaskID             string                  `json:"taskId"`
	Title              string                  `json:"title"`
	Status             string                  `json:"status"`
	DirectDependents   []serveDiscardDependent `json:"directDependents"`
	CascadeDependents  []serveDiscardDependent `json:"cascadeDependents"`
	OpenGates          []string                `json:"openGates"`
	RequiresResolution bool                    `json:"requiresResolution"`
	PreservesHistory   bool                    `json:"preservesHistory"`
}

type serveEpicSummary struct {
	ID     string         `json:"id"`
	Title  string         `json:"title"`
	Counts map[string]int `json:"counts"`
}

type serveWaveSummary struct {
	ID            string                 `json:"id"`
	Title         string                 `json:"title"`
	Status        string                 `json:"status"`
	LandedAt      any                    `json:"landedAt"`
	MemberIDs     []string               `json:"memberIds"`
	Members       []serveWaveTaskSummary `json:"members"`
	Counts        map[string]int         `json:"counts"`
	Authorization map[string]any         `json:"authorization"`
	Brief         waveBrief              `json:"brief"`
}

type serveWaveTaskSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Group  string `json:"group"`
	Status string `json:"status"`
	Proof  string `json:"proof"`
}

// serveReviewBatch is the wave-boundary review projection. Members are the
// terminal task records relevant to the review surface (review or done);
// readiness is derived from every canonical wave member so a partial wave
// cannot be mistaken for a reviewable batch.
type serveReviewBatch struct {
	Waves   []serveReviewWave  `json:"waves"`
	Unwaved []serveTaskCapsule `json:"unwaved"`
}

type serveReviewWave struct {
	WaveID         string             `json:"waveId"`
	Title          string             `json:"title"`
	Authorization  map[string]any     `json:"authorization"`
	ReadyForReview bool               `json:"readyForReview"`
	Members        []serveTaskCapsule `json:"members"`
}

type serveTaskCapsule struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	WaveID          string            `json:"waveId,omitempty"`
	WaveTitle       string            `json:"waveTitle,omitempty"`
	WaveTerminal    bool              `json:"waveTerminal,omitempty"`
	EpicID          string            `json:"epicId"`
	EpicTitle       string            `json:"epicTitle"`
	Status          string            `json:"status"`
	Readiness       string            `json:"readiness"`
	Priority        string            `json:"priority"`
	Risk            string            `json:"risk"`
	HasGate         bool              `json:"hasGate"`
	OpenGates       []serveGateDetail `json:"openGates"`
	UpdatedAt       string            `json:"updatedAt"`
	ProjectID       string            `json:"projectId"`
	RawStatus       string            `json:"rawStatus"`
	RawReadiness    string            `json:"rawReadiness"`
	ReworkCount     int               `json:"reworkCount"`
	Blockers        []string          `json:"blockers"`
	Dispatchable    bool              `json:"dispatchable"`
	NextOwner       string            `json:"nextOwner"`
	NextAction      string            `json:"nextAction"`
	WorkRevision    int               `json:"workRevision"`
	ReadinessSource string            `json:"readinessSource"`
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

// serveHumanAction is a deterministic projection of one open human-owned V7
// gate. It deliberately carries the gate's existing contract rather than
// introducing a second persisted human-action record.
type serveHumanAction struct {
	Kind                string               `json:"kind"`
	RawKind             string               `json:"rawKind"`
	Title               string               `json:"title"`
	Action              string               `json:"action"`
	WhyAgentCannot      string               `json:"whyAgentCannot"`
	CompletionCondition string               `json:"completionCondition"`
	GateID              string               `json:"gateId"`
	BlockedTaskIDs      []string             `json:"blockedTaskIds"`
	Covers              []string             `json:"covers"`
	Acceptance          []serveAcceptanceRow `json:"acceptance"`
}

type serveGateDetail struct {
	serveGate
	Title               string   `json:"title"`
	Status              string   `json:"status"`
	Blocking            bool     `json:"blocking"`
	Blocks              []string `json:"blocks"`
	Reason              string   `json:"reason,omitempty"`
	UpdatedAt           string   `json:"updatedAt,omitempty"`
	Body                string   `json:"body,omitempty"`
	Action              string   `json:"action,omitempty"`
	WhyAgentCannot      string   `json:"whyAgentCannot,omitempty"`
	CompletionCondition string   `json:"completionCondition,omitempty"`
	HumanOwned          bool     `json:"humanOwned"`
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
	HumanAction    *serveHumanAction      `json:"humanAction,omitempty"`
	HumanActions   []serveHumanAction     `json:"humanActions"`
	RunHistory     []serveRunSummary      `json:"runHistory"`
	RunDirective   *serveRunDirective     `json:"runDirective,omitempty"`
}

type serveRunDirective struct {
	State     string `json:"state"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	Reason    string `json:"reason,omitempty"`
}

type serveRunSummary struct {
	TaskID            string                     `json:"taskId"`
	TaskTitle         string                     `json:"taskTitle"`
	ProjectID         string                     `json:"projectId"`
	Runner            string                     `json:"runner"`
	RunnerName        string                     `json:"runnerName"`
	RunnerProfile     string                     `json:"runnerProfile"`
	Model             any                        `json:"model"`
	Lane              string                     `json:"lane"`
	LeaseState        string                     `json:"leaseState"`
	LeaseStateRaw     string                     `json:"leaseStateRaw"`
	HandRun           bool                       `json:"handRun"`
	ProcessRunning    bool                       `json:"processRunning"`
	Outcome           string                     `json:"outcome"`
	ElapsedSec        int                        `json:"elapsedSec"`
	SinceLastEventSec int                        `json:"sinceLastEventSec"`
	Liveness          string                     `json:"liveness"`
	AttemptCount      int                        `json:"attemptCount"`
	Terminal          any                        `json:"terminal"`
	Error             any                        `json:"error"`
	Infrastructure    *RunnerInfrastructureBlock `json:"infrastructure,omitempty"`
	LastHeartbeatAt   any                        `json:"lastHeartbeatAt"`
	NextWakeAt        any                        `json:"nextWakeAt"`
	WorkspacePath     string                     `json:"workspacePath"`
	WorkspaceMode     string                     `json:"workspaceMode"`
	StartedAt         string                     `json:"startedAt"`
	UpdatedAt         string                     `json:"updatedAt"`
}

type serveAttempt struct {
	N           int    `json:"n"`
	Outcome     string `json:"outcome"`
	DurationSec int    `json:"durationSec"`
	StartedAt   string `json:"startedAt"`
}

type serveRunEvent struct {
	TS    string `json:"ts"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Level string `json:"level,omitempty"`
}

type serveRunDetail struct {
	serveRunSummary
	WorkspacePath string               `json:"workspacePath"`
	Attempts      []serveAttempt       `json:"attempts"`
	Events        []serveRunEvent      `json:"events"`
	Authorization *RunAuthorization    `json:"authorization,omitempty"`
	Identity      *RunIdentityMetadata `json:"identity,omitempty"`
	Session       *RunnerSession       `json:"session,omitempty"`
	Resume        runResumeCapability  `json:"resume"`
	Delivery      serveRunDelivery     `json:"delivery"`
}

type serveRunDelivery struct {
	Summary      string `json:"summary,omitempty"`
	Verification string `json:"verification,omitempty"`
	ProofStatus  string `json:"proofStatus"`
	Artifact     string `json:"artifact,omitempty"`
}

type serveAttemptDetail struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"taskId"`
	ProjectID      string          `json:"projectId"`
	Runner         string          `json:"runner"`
	Lane           string          `json:"lane"`
	Outcome        string          `json:"outcome"`
	StartedAt      string          `json:"startedAt"`
	FinishedAt     string          `json:"finishedAt,omitempty"`
	DurationSec    int             `json:"durationSec"`
	WorkspacePath  string          `json:"workspacePath,omitempty"`
	BranchName     string          `json:"branchName,omitempty"`
	PullRequestURL string          `json:"pullRequestUrl,omitempty"`
	PromptPath     string          `json:"promptPath,omitempty"`
	EventSinkPath  string          `json:"eventSinkPath,omitempty"`
	RawLogPath     string          `json:"rawLogPath,omitempty"`
	StatusPath     string          `json:"statusPath,omitempty"`
	LastError      string          `json:"lastError,omitempty"`
	LogsSummary    string          `json:"logsSummary,omitempty"`
	FinalSummary   string          `json:"finalSummary,omitempty"`
	Turns          []RunTurn       `json:"turns"`
	Events         []serveRunEvent `json:"events"`
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
