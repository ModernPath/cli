// Package rdd — the CLI-bundled parsers for the RDD process-document formats
// (REQ-CROSS-013, EPIC-SYNC-004 TASK-SY-404). Originally a faithful Go port of
// a node extractor + op-builder that lived in the workspace: same fields, same
// heuristics, same content hashes, so the switch was hash-stable. That node
// side is retired; these parsers are the only implementation, and the hash
// pins in ops_test.go are what still holds them to the ported behaviour.
//
// Formats: rdd-ledger-v1 (tasks/<CTX>-REQUIREMENTS.md), rdd-worklist-v1
// (WORKLIST.md), rdd-epic-v1 (epics/*), rdd-review-queue-v1 (docs/85),
// rdd-open-questions-v1 (process/08).
package rdd

const bodyCap = 15000 // extract.js BODY_CAP (UTF-16 code units, like JS .slice)

// Req is one requirements-ledger row (+ its detail block).
type Req struct {
	ID      string
	Title   string
	Stage   string
	Status  string
	Source  string
	Ctx     string
	CtxName string // the context's human name from the ledger H1; "" = unnamed
	// Columns 6 and 7 of the dashboard row. The node mirror parsed them and this
	// did not, so every code and test citation in every ledger stopped at the
	// workspace boundary — the compliance views reported "Code: Missing" on all
	// 1,014 rows of a real system (`USER:2026-08-12`).
	Tests string
	Code  string
	// The user requirement this row serves, when the ledger carries a UR column.
	UR     string
	Detail string // "" = none (JS null)
	// REQ-CROSS-222: first-class capture of the Priority/Owner/Release
	// columns, and every other column the header names that the extractor
	// does not recognize — carried, never dropped.
	Priority string
	Owner    string
	Release  string
	Extras   []NamedCell
}

// NamedCell is one unrecognized ledger column's header and cell value
// (REQ-CROSS-222 §222.2: carried as a named extra).
type NamedCell struct {
	Name  string
	Value string
}

// Epic is one WORKLIST rollup row.
type Epic struct {
	ID     string
	State  string // proposed | awaiting-approval | done | in-progress | other
	Record string // workspace-relative epic record path ("" = none) — payload identity (origin_ref), never rebased
	// RecordFS is the path that resolves the record from the SYNC root when it
	// differs from Record (manifest pointing one level up — SCN-SY-046).
	// Empty = Record already resolves. Never enters op payloads.
	RecordFS string
	// Specs are the folder epic's specs/*.md files (REQ-CROSS-023), sorted by
	// name. Rel is payload identity (derived from Record's directory, never
	// RecordFS); Content is the file body (capped at op build).
	Specs []EpicSpec
	// Upper/Lower are the WORKLIST row's own "Upper status" / "Lower status"
	// cells (REQ-CROSS-028). 127 of 146 rows carry both, and they were read by
	// nothing — the loop state the app shows comes from here, NOT from
	// epics.status, which is the board column (SERVER-SYNC-DESIGN §1.3).
	// "" = the cell was empty or an em-dash: absence, not a status.
	Upper string
	Lower string
	// REQ-CROSS-223: the PROCESS.md status token leading the WORKLIST
	// Overall-status cell — the epic lifecycle the store must carry.
	// "" = the cell was empty or carried no recognizable token.
	ProcessStatus string
	// URCell is the Epic Rollup's user-requirements cell verbatim. It names
	// the denominator even when the named epic record does not repeat the id;
	// statements never come from this cell.
	URCell string
	// ApprovalCell is the WORKLIST row's "Human approval" cell, verbatim
	// (REQ-CROSS-248): the rollup's recorded acceptance — an APP-* register
	// reference and/or a USER: tag. "" = empty or no row.
	ApprovalCell string
	// TasksCell is the Epic Rollup's Tasks cell verbatim (REQ-CROSS-264). It
	// is harvested BEFORE the range-row drop and the first-wins dedupe: those
	// two run on the row's epic IDENTITY, and a shadowed row's Tasks cell
	// still names tasks that exist. "" = empty or no row.
	TasksCell string
}

// BacklogRow is one BACKLOG.md discovery row or gap-register row
// (REQ-CROSS-223: they gain a store home for the ledger import).
type BacklogRow struct {
	ExternalID string // explicit GAP-* id, else a stable derived BL-… id
	Kind       string // "backlog" | "gap"
	Title      string
	NotesMD    string
	Route      string
	SourcePath string
	Line       int
	// REQ-CROSS-251: the folded facts, typed. Empty = the row states none.
	RaisedAt       string // YYYY-MM-DD from the title's provenance parenthetical
	RaisedBy       string
	Disposition    string // OPEN | DEFERRED | ROUTED to <id> | … (the store's words)
	DispositionRef string
	CandidateRoute string
	WhyUnrouted    string
	GapKind        string // capability (register) | specification (### GAP block)
	AffectedIDs    []string
	RawBody        string // the row verbatim — the split is a transform, not a loss
}

// EpicSpec is one epic-folder spec file (EPIC-SYNC-006).
type EpicSpec struct {
	Rel     string
	Name    string
	Content string
}

// RQ is one docs/85 review-queue block.
type RQ struct {
	ID             string
	Title          string
	Heading        string
	Date           string // "" = none
	State          string // closed | delegated | decision-needed | finding
	Line           int
	Body           string
	Pool           []string
	Options        []Option
	Recommendation string // "" = none
	// Optional trace-evaluation identity carried independently from content hash.
	EvaluatedScopeFingerprint string
}

type Option struct {
	Label string
	Text  string
}

// OQ is one process/08 open-questions row.
type OQ struct {
	ID    string
	Title string
	Full  string
	// Suggested is the row's "*Suggested default:*" clause — the author's
	// recommendation, which rides to the gate so the question is answerable
	// without opening the source doc (REQ-PLN-059).
	Suggested string
	Resolved  bool
	Line      int
	Pool      []string
	// Raw is the question's body with its line structure intact. Full collapses
	// whitespace for display; a **Brief:** block is a bullet list and cannot be
	// parsed once its newlines are gone (REQ-CROSS-142).
	Raw string
	// Optional trace-evaluation identity carried independently from content hash.
	EvaluatedScopeFingerprint string
}

// Commit is one recent git-log entry (newest first).
type Commit struct {
	Hash    string
	Date    string
	Subject string
	IDs     []string
}

// Data is the workspace snapshot the op-builder consumes — the Go analogue of
// extract.js buildData(), restricted to the fields ops.js uses.
type Data struct {
	Reqs []Req
	// UserReqs are the file-derived user requirements
	// (requirements/*-USER-REQUIREMENTS.md, SR-SY-1403). Empty in a workspace
	// that has none; the type is optional, never mandated.
	UserReqs []FileUR
	Epics    []Epic
	RQs      []RQ
	OQs      []OQ
	Commits  []Commit
	// REQ-CROSS-084: the system-scoped explanatory documents and the discovery
	// guides. Empty in a workspace that has neither, and an empty set is a
	// no-op on sync rather than a deletion.
	Docs []Document
	// REQ-CROSS-223: BACKLOG.md discovery rows and gap-register rows.
	// Empty in a workspace without the files.
	Backlog []BacklogRow
	// REQ-CROSS-237: gate records from `file-state/GATES.md`, and the path they
	// came from (the op's origin_ref). Empty in a workspace with no gate file.
	Gates    []GateRecord
	GatesRef string
	// REQ-CROSS-264: the WORKLIST rows OUTSIDE the Epic Rollup that declare
	// tasks — the Work Rows and Blocked/Deferred sections, whose TASK-led
	// rows the `worklist-row` loss population assigns to the tasks carrier.
	WorklistTaskRows []WorklistTaskRow
	// ArchivalFiles are the flip-retired carrier files (worklist, review
	// queue, open questions) as workspace-relative paths, resolved through
	// the manifest by Snapshot — a binding that relocates them keeps its
	// carriers. Nil means "not resolved": BuildOps falls back to the
	// default root-relative names.
	ArchivalFiles []string
}
