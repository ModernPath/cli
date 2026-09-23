package cmd

// REQ-CROSS-316 (SR-CLI-0087), EPIC-CLI-008: the `process` navigation group.
// `process check --phase` renders one phase's server-computed decision-table
// checks for the current work selection — a PURE READS the server owns; the CLI
// only renders and sets the exit code. SR-317 adds `process next` and
// `process reconcile` to this same group; SR-315 adds `process findings`.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// deliveryContextPath builds the caller-scoped delivery-context read path.
// REQ-CROSS-345: --piece names which of the caller's own current pieces to
// resolve (carried as ?scope=) when they hold several.
func deliveryContextPath(env *factoryEnv) string {
	return deliveryContextPathFor(env, processPiece)
}

// deliveryContextPathFor names an explicit piece — the ceremony verbs and the
// trace-pin default resolve a scope they were given, not the --piece flag.
func deliveryContextPathFor(env *factoryEnv, piece string) string {
	path := fmt.Sprintf("/api/v1/sync/delivery-context?system_id=%d", env.SystemID)
	if piece != "" {
		path += "&scope=" + url.QueryEscape(piece)
	}
	return path
}

var processCmd = &cobra.Command{
	Use:   "process",
	Short: "Navigate the delivery loop: phase checks (server-computed), rendered locally",
}

var processCheckPhase string

// REQ-CROSS-345: the navigation reads are caller-scoped server-side; --piece
// names which of the caller's own current pieces to resolve when they hold
// several (carried as ?scope= to delivery-context and as scope to reconcile).
var processPiece string

// The phase vocabulary `working-set select --phase` accepts.
var processPhases = []string{
	"source", "plan", "cold_review", "entry", "build", "verify", "completion", "triage",
}

var processCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Render one phase's decision-table checks for the current selection (a pure read)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processCheck(env, processCheckPhase)
	},
}

type phaseCheck struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type deliveryContextResponse struct {
	Data struct {
		PacketFingerprint string                  `json:"packet_fingerprint"`
		ProcessRevision   string                  `json:"process_revision"`
		Checks            map[string][]phaseCheck `json:"checks"`
		DerivedReason     string                  `json:"derived_reason"`
		// REQ-CROSS-317 (SR-CLI-0088)
		DerivedPhase  string `json:"derived_phase"`
		DeclaredPhase string `json:"declared_phase"`
		Divergence    bool   `json:"divergence"`
		Skill         string `json:"skill"`
		// REQ-CROSS-392: "defect" when a customer-blocking defect is on the
		// clock; "planned" or absent otherwise.
		Lane string `json:"lane"`
		// REQ-CROSS-374 (EPIC-CLI-017): the facts the ceremony verbs act on. A
		// pointer: nil means the server served none, and every verb refuses
		// before its first write on nil (D14) rather than acting on zero values.
		Facts *deliveryFacts `json:"facts"`
		// FactsState says why facts is nil on a server that serves the read:
		// "unavailable" (no current selection, or the gather failed). Absent
		// on a server that predates the read.
		FactsState string `json:"facts_state"`
	} `json:"data"`
}

// heldPiecesRefusal renders the server's REQ-CROSS-379 answer to an ambiguous
// scope — a 409 carrying `pieces` — as the remedy the caller can act on. ok is
// false when the body carries no pieces (any other refusal).
func heldPiecesRefusal(body map[string]any) (bool, error) {
	pieces := stringSlice(body["pieces"])
	if len(pieces) == 0 {
		return false, nil
	}
	return true, fmt.Errorf("you hold several current pieces (%d): %s — name one with --piece <id>",
		len(pieces), strings.Join(pieces, ", "))
}

func readDeliveryContext(env *factoryEnv) (*deliveryContextResponse, error) {
	return readDeliveryContextFor(env, processPiece)
}

func readDeliveryContextFor(env *factoryEnv, piece string) (*deliveryContextResponse, error) {
	status, body, err := env.call("GET",
		deliveryContextPathFor(env, piece), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		if ok, err := heldPiecesRefusal(body); ok {
			return nil, err
		}
		return nil, fmt.Errorf("delivery-context read returned HTTP %d", status)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var resp deliveryContextResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("delivery-context response was not the expected shape: %w", err)
	}
	return &resp, nil
}

func processCheck(env *factoryEnv, phase string) error {
	if phase == "" {
		return fmt.Errorf("--phase is required (one of: %v)", processPhases)
	}
	if !contains(processPhases, phase) {
		return fmt.Errorf("unknown phase %q (one of: %v)", phase, processPhases)
	}

	status, body, err := env.call("GET",
		deliveryContextPath(env), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		if ok, err := heldPiecesRefusal(body); ok {
			return err
		}
		return fmt.Errorf("delivery-context read returned HTTP %d", status)
	}

	// env.call returns the already-parsed JSON body; re-marshal to decode it into
	// the typed shape.
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var resp deliveryContextResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("delivery-context response was not the expected shape: %w", err)
	}

	checks := resp.Data.Checks[phase]
	if len(checks) == 0 {
		fmt.Printf("phase %s: no applicable checks for the current selection\n", phase)
		return nil
	}

	failed := false
	for _, c := range checks {
		fmt.Printf("  %-12s %s\n", c.State, c.Name)
		if c.State == "FAIL" {
			failed = true
		}
		// REQ-CROSS-383 / REQ-CROSS-408: the server serves why a check is not
		// met; the lines under a FAIL, UNAVAILABLE or NOT_APPLICABLE name it.
		// The CLI renders the facts and computes nothing.
		for _, line := range checkReasonLines(c, resp.Data.Facts) {
			fmt.Printf("               %s\n", line)
		}
	}
	printFingerprints(resp.Data.PacketFingerprint, resp.Data.ProcessRevision)

	if failed {
		return fmt.Errorf("phase %s is not satisfied — see the FAIL checks above", phase)
	}
	return nil
}

// REQ-CROSS-317 (SR-CLI-0088): `process next` — a PURE READ. It GETs the
// delivery context and renders the derived phase, whether it diverges from the
// declared phase, the reason, and the owning skill. It never issues a POST.
var processNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show where the delivery loop stands and the skill to run (a pure read)",
	Long: `Read the delivery context and print the derived phase, why, and the skill
to run. It never writes.

The read is caller-scoped. When you hold SEVERAL current pieces and name
none, the server refuses by name — "you hold N current pieces: A, B — name
one with --piece <id>" — and nothing is routed; "no current selection" is
printed only when you hold none. Pass --piece <id> (a scope you took with
working-set select) to route one.

The packet aggregate is independent of the process revision: it folds the
scope's content, membership and canonical sections — not the server's process
pin — so a process-package repin deploy moves no aggregate and voids no open
gate. -v prints the full process_revision beside the aggregate as
information, so which process text a packet was reviewed under stays readable;
it just no longer pins a decision.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processNext(env)
	},
}

func processNext(env *factoryEnv) error {
	resp, err := readDeliveryContext(env)
	if err != nil {
		return err
	}
	d := resp.Data
	if note := storeBackedNote(env.Root); note != "" {
		fmt.Println(note)
	}
	if d.DerivedPhase == "" {
		// A nil route is not always "no selection": the decision table also returns
		// a nil route with derived_reason "complete" (the loop is finished) or
		// "entry_origin_unavailable" (a live selection whose routing evidence is
		// missing). Render on the reason so neither is mislabeled as an absent
		// selection.
		switch d.DerivedReason {
		case "", "no_current_selection":
			fmt.Println("no current selection — nothing to route")
		case "complete":
			fmt.Println("loop complete — every selected member is delivered and accepted")
			printLane(d.Lane)
			printFingerprints(d.PacketFingerprint, d.ProcessRevision)
		default:
			fmt.Printf("no route derived — %s\n", d.DerivedReason)
			// A reason with a known remedy names it here; the others print the
			// reason alone, because a wrong remedy is worse than none. The
			// entry origin is the MEMBER's own fact, so the remedy names the
			// members: `process reenter` resolves the entry gate of the id it
			// is given, and the scope's own entry is a different gate.
			if d.DerivedReason == "entry_origin_unavailable" {
				if ids := membersWithoutLiveEntry(d.Facts); len(ids) > 0 {
					fmt.Printf("remedy:         no live entry origin (retired by a demotion, or never entered): %s — `process reenter <member-id>` for each, at a fresh cold review and a human approval\n", strings.Join(ids, ", "))
				} else {
					fmt.Println("remedy:         a member whose evidence is not current has no live entry origin (retired by a demotion, or never entered) — `process reenter <member-id>` re-establishes that member's entry, at a fresh cold review and a human approval; `process check --phase build` names the member")
				}
			}
			printLane(d.Lane)
			printFingerprints(d.PacketFingerprint, d.ProcessRevision)
		}
		return nil
	}
	fmt.Printf("derived phase:  %s\n", d.DerivedPhase)
	div := ""
	if d.Divergence {
		div = "  ⚠ diverges from the derived phase"
	}
	fmt.Printf("declared phase: %s%s\n", d.DeclaredPhase, div)
	printLane(d.Lane)
	if d.DerivedReason != "" {
		fmt.Printf("why:            %s\n", d.DerivedReason)
	}
	if d.Skill != "" {
		fmt.Printf("run:            %s\n", d.Skill)
	}
	printFingerprints(d.PacketFingerprint, d.ProcessRevision)
	return nil
}

type reconcileResponse struct {
	Data struct {
		Applied     bool `json:"applied"`
		Transitions []struct {
			ExternalID string `json:"external_id"`
			Kind       string `json:"kind"`
			From       string `json:"from"`
			To         string `json:"to"`
			Basis      string `json:"basis"`
		} `json:"transitions"`
		Fails []struct {
			ExternalID string `json:"external_id"`
			Reason     string `json:"reason"`
		} `json:"fails"`
	} `json:"data"`
}

var processReconcileApply bool

// REQ-CROSS-317 (SR-CLI-0088): the ONLY navigation writer. Without --apply it
// lists the legal forward transitions it would apply and changes nothing; with
// --apply it applies them server-side in one transaction.
var processReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Apply the legal automatic lifecycle transitions from current trace proofs (--apply to write)",
	Long: `Compute — and with --apply, apply — the automatic transitions the current
proofs allow. The proofs differ by kind.

A SYSTEM requirement is entry-gated. TODO -> IN_PROGRESS needs BOTH an
applied entry gate at the current packet aggregate AND a recorded RED;
IN_PROGRESS -> IN_REVIEW needs that same live entry plus a passing lower
trace at the requirement's content fingerprint. An entry approval pinned to
a superseded aggregate holds the SR where it is however fresh the proofs
are — clear it with process reenter <SR> (or process reapply-entry when the
aggregate moved for an immaterial reason). One exception: an SR reopened by
an applied defect demotion that no entry gate names at all (an epic entered
through a legacy approval gate) returns to IN_REVIEW on its RED and passing
lower trace alone.

A USER requirement has no entry gate of its own. It enters on its own
recorded RED OR on a required SR that is already IN_PROGRESS, and reaches
IN_REVIEW when every required SR is reviewed AND its upper trace passes.

The epic takes two steps of its own: TODO or READY -> IN_PROGRESS as soon as
ANY member is IN_PROGRESS, and IN_PROGRESS -> IN_REVIEW once EVERY member is
reviewed and its applicable traces pass.

It never applies a human-gated transition (entry, completion) and
never creates evidence or a trace; when it reports an unmet transition, the
FAIL line names what is missing.

Run without --apply first: "nothing to do" can mean the proofs are not there
yet, or that the read resolved a different scope — with several current
selections held, name the one you mean with --piece. A failing result demotes
nothing while the system's cascade mode is report (process cascade-mode).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processReconcile(env, processReconcileApply)
	},
}

func processReconcile(env *factoryEnv, apply bool) error {
	resp, err := reconcileOnce(env, processPiece, apply)
	if err != nil {
		return err
	}

	verb := "would apply"
	if resp.Data.Applied {
		verb = "applied"
	}
	// "nothing to do" is only honest when there is genuinely nothing — neither a
	// transition nor a fail. A fail with no transition is NOT nothing to do.
	if len(resp.Data.Transitions) == 0 && len(resp.Data.Fails) == 0 {
		fmt.Println("reconcile: nothing to do")
	}
	for _, t := range resp.Data.Transitions {
		fmt.Printf("  %s  %s %s->%s (%s)\n", verb, t.ExternalID, t.From, t.To, t.Basis)
	}
	for _, f := range resp.Data.Fails {
		fmt.Printf("  FAIL     %s — %s\n", f.ExternalID, f.Reason)
	}
	// A reported fail (a green-without-RED proof, or a rolled-back apply) must make
	// the command exit non-zero, or shell automation reads the failure as success.
	if n := len(resp.Data.Fails); n > 0 {
		return fmt.Errorf("reconcile reported %d unmet transition(s) — see the FAIL line(s) above", n)
	}
	return nil
}

// reconcileOnce posts one reconcile for the named piece (REQ-CROSS-345:
// caller-scoped; "" resolves the caller's single piece) and returns the decoded
// response. `processReconcile` renders it and owns the exit rule; the ceremony
// verbs (REQ-CROSS-374) judge the response against the item they name.
func reconcileOnce(env *factoryEnv, piece string, apply bool) (*reconcileResponse, error) {
	payload := map[string]any{
		"system_id": env.SystemID,
		"apply":     apply,
		"actor":     map[string]any{"kind": "agent", "agent_slug": "modernpath-author"},
	}
	if piece != "" {
		payload["scope"] = piece
	}

	status, body, err := env.call("POST", "/api/v1/sync/reconcile", payload)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		if ok, err := heldPiecesRefusal(body); ok {
			return nil, err
		}
		return nil, fmt.Errorf("reconcile returned HTTP %d", status)
	}
	raw, _ := json.Marshal(body)
	var resp reconcileResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("reconcile response was not the expected shape: %w", err)
	}
	return &resp, nil
}

// REQ-CROSS-318 (SR-CLI-0089): `process supersede <item> --reason` posts the
// supersede author action. By default a dry run (report) unless the system's
// cascade mode is enforce; --apply performs the cascade for THIS one item on an
// attributed USER: source while the system mode stays unchanged (BACKLOG-TOOL-39).
var (
	processSupersedeReason     string
	processSupersedeSource     string
	processSupersedeSupersedes string
	processSupersedeApply      bool
)

var processSupersedeCmd = &cobra.Command{
	Use:   "supersede <external-id>",
	Short: "Supersede a traced item, computing the invalidation cascade (a dry run unless --apply)",
	Long: `Supersede a traced item — compute its invalidation cascade: result validity
-> STALE, dependent gates -> STALE/SUPERSEDED, and IN_REVIEW/DONE items demoted
to IN_PROGRESS through declared relations.

By default this is a DRY RUN that prints what would change and writes nothing
(unless the system's cascade mode is already 'enforce'). --apply performs the
cascade on an attributable --source USER: reference while the system's cascade
mode stays unchanged — the scoped, attributed way to reopen a shipped (DONE)
item without flipping the whole system into enforce.

--apply is scoped to this ONE supersede, but the cascade still follows declared
relations: it demotes the named item AND its dependent IN_REVIEW/DONE closure
(derived URs, and the Epics that contain the item or those URs). Review the
'would demote' list from the dry run before you --apply. --supersedes records the
prior decision the reversal overrides, so the drift event captures it old->new.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processSupersede(env, args[0], processSupersedeReason, processSupersedeSource, processSupersedeSupersedes, processSupersedeApply)
	},
}

func processSupersede(env *factoryEnv, item, reason, source, supersedes string, apply bool) error {
	// --apply is a real, scoped demotion; it must ride an attributable USER:
	// source. The server enforces the same guard — this is the fast local refusal.
	if apply && !strings.HasPrefix(source, "USER:") {
		return fmt.Errorf("--apply performs a scoped demotion and requires --source USER:<date>:<why>")
	}

	reqBody := map[string]any{
		"system_id": env.SystemID,
		"action":    "supersede",
		"record":    map[string]any{"external_id": item},
		"reason":    reason,
		"actor":     map[string]any{"kind": "agent", "agent_slug": "modernpath-author"},
	}
	if source != "" {
		reqBody["source"] = source
	}
	if supersedes != "" {
		reqBody["supersedes"] = supersedes
	}
	if apply {
		reqBody["scoped_enforce"] = true
	}

	status, body, err := env.call("POST", "/api/v1/sync/author", reqBody)
	if err != nil {
		return err
	}
	if status != 200 {
		// REQ-CROSS-372: the server's refusal, not only the HTTP code.
		return serverRefusal("supersede refused", status, body)
	}

	raw, _ := json.Marshal(body)
	var resp struct {
		Data struct {
			Supersede struct {
				Mode                string   `json:"mode"`
				WouldDemote         []string `json:"would_demote"`
				WouldSupersede      []string `json:"would_supersede"`
				WouldStaleResults   int      `json:"would_stale_results"`
				Applied             bool     `json:"applied"`
				Source              string   `json:"source"`
				Supersedes          string   `json:"supersedes"`
				AttributionRecorded bool     `json:"attribution_recorded"`
			} `json:"supersede"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("supersede response was not the expected shape: %w", err)
	}

	r := resp.Data.Supersede
	verb := "would demote"
	if r.Applied {
		verb = "demoted"
	}
	fmt.Printf("supersede %s (%s mode)\n", item, r.Mode)
	for _, id := range r.WouldDemote {
		fmt.Printf("  %s  %s\n", verb, id)
	}
	for _, g := range r.WouldSupersede {
		fmt.Printf("  supersede gate  %s\n", g)
	}
	fmt.Printf("  %d evidence result(s) staled\n", r.WouldStaleResults)
	if r.Applied && r.Source != "" {
		switch {
		case r.AttributionRecorded && r.Supersedes != "":
			fmt.Printf("  captured: supersedes %s (source %s)\n", r.Supersedes, r.Source)
		case r.AttributionRecorded:
			fmt.Printf("  captured: source %s\n", r.Source)
		default:
			// The reversal was already recorded for this exact source (an idempotent
			// retry) — do not claim a fresh capture over a silent dedupe.
			fmt.Printf("  already recorded for source %s — attribution unchanged\n", r.Source)
		}
	}
	if !r.Applied {
		fmt.Printf("  dry run — re-run with --apply --source USER:<date>:<why> to perform; review the demote list above first\n")
	}
	return nil
}

// REQ-CROSS-318: `process cascade-mode <report|enforce> --source USER:...`
// sets the system's supersede cascade mode through the store's author action.
// The server refuses a mode outside {report,enforce} and a source not starting
// "USER:" (Core.Author.set_cascade_mode/3); the client checks the same up front.
var processCascadeSource string

var processCascadeModeCmd = &cobra.Command{
	Use:   "cascade-mode [report|enforce]",
	Short: "Read the system's cascade mode, or set it (report|enforce, requires a USER: source)",
	Long: "With no argument, report the current cascade mode (defaulted to report) and the\n" +
		"operations it governs — a read that never changes it. With report|enforce and a\n" +
		"USER: source, set the mode.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			// A read carries no attribution; refuse a --source here so a caller
			// who meant to SET a mode (but dropped the argument) is not silently
			// shown the current mode instead.
			if processCascadeSource != "" {
				return fmt.Errorf("--source is only used when setting the mode (report|enforce); the no-argument form reads it")
			}
			env, err := authorEnv()
			if err != nil {
				return err
			}
			return processCascadeModeShow(env)
		}
		mode := args[0]
		if mode != "report" && mode != "enforce" {
			return fmt.Errorf("cascade mode must be report or enforce, got %q", mode)
		}
		if !strings.HasPrefix(processCascadeSource, "USER:") {
			return fmt.Errorf("--source is required and must be a USER: attribution (e.g. USER:2026-09-05:enforce-cascade)")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processCascadeMode(env, mode, processCascadeSource)
	},
}

// REQ-CROSS-342: read the effective cascade mode (defaulted to report) and the
// operations it governs, without changing it — GET /sync/cascade-mode. No USER:
// source: a read must not carry the write's attribution.
func processCascadeModeShow(env *factoryEnv) error {
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/cascade-mode?system_id=%d", env.SystemID), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("cascade-mode read", status, body)
	}
	data, _ := dataOf(body)["cascade_mode"].(map[string]any)
	mode := str(data, "mode")
	if mode == "" {
		mode = "report"
	}
	// A read, not a mutation — plain output, no success marker.
	fmt.Printf("cascade mode: %s\n", mode)
	if g := stringSlice(data["governs"]); len(g) > 0 {
		fmt.Printf("  governs: %s\n", strings.Join(g, ", "))
	}
	if ng := stringSlice(data["not_governed"]); len(ng) > 0 {
		fmt.Printf("  does not govern: %s\n", strings.Join(ng, ", "))
	}
	return nil
}

func processCascadeMode(env *factoryEnv, mode, source string) error {
	status, body, err := env.call("POST", "/api/v1/sync/author", map[string]any{
		"system_id": env.SystemID,
		"action":    "cascade_mode",
		"mode":      mode,
		"source":    source,
		"actor":     map[string]any{"kind": "agent", "agent_slug": "modernpath-author"},
	})
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("cascade_mode", status, body)
	}
	set := mode
	if data, ok := dataOf(body)["cascade_mode"].(map[string]any); ok {
		if m, ok := data["mode"].(string); ok {
			set = m
		}
	}
	printSuccess("cascade mode set to %s (%s)", set, source)
	return nil
}

func shortFP(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// printFingerprints renders the compact packet/process summary line and, under
// -v/--verbose, the full-length values beneath it. The compact line is the
// default and is left unchanged on purpose — other tooling parses it — so the
// full values are only ever ADDED, never substituted.
//
// The full packet_fingerprint is what `author trace --fingerprint` needs: that
// flag requires the whole 64-char packet aggregate, and pasting the truncated
// 12-char summary yields a trace whose evaluated_scope_fingerprint silently
// never matches the current aggregate. Such a trace is inert, and the mismatch
// only surfaces later as an entry/completion gate 409 with no obvious link to
// the cause. `-v` is the supported way to read the full value without curling
// the delivery-context API by hand.
func printFingerprints(packet, revision string) {
	fmt.Printf("packet %s · process %s\n", shortFP(packet), shortFP(revision))
	if verbose {
		// REQ-CROSS-376: the three pin classes are named the same way everywhere —
		// packet aggregate, content hash, process revision. The line keys stay
		// parse-stable (the skill greps `full packet_fingerprint:`); the class
		// rides after the value.
		fmt.Printf("full packet_fingerprint: %s  (packet aggregate)\n", packet)
		fmt.Printf("full process_revision:   %s  (process revision)\n", revision)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func init() {
	processCheckCmd.Flags().StringVar(&processCheckPhase, "phase", "", "the phase to check")
	// REQ-CROSS-345: the navigation reads are caller-scoped; --piece names which
	// of the caller's own current pieces to resolve when they hold several.
	processCmd.PersistentFlags().StringVar(&processPiece, "piece", "",
		"when you hold several current selections, --piece names which one process next, check, reconcile, advance, complete and findings list resolve")
	processReconcileCmd.Flags().BoolVar(&processReconcileApply, "apply", false, "apply the transitions (default: dry run)")
	processSupersedeCmd.Flags().StringVar(&processSupersedeReason, "reason", "", "why the item's trace is superseded")
	processSupersedeCmd.Flags().StringVar(&processSupersedeSource, "source", "", "attributable USER: source for a scoped --apply, e.g. USER:2026-09-16:reverse-§061.6")
	processSupersedeCmd.Flags().StringVar(&processSupersedeSupersedes, "supersedes", "", "the prior decision this reversal overrides, e.g. USER:2026-08-29 (recorded on the drift event)")
	processSupersedeCmd.Flags().BoolVar(&processSupersedeApply, "apply", false, "perform the cascade for THIS one item (scoped) instead of a dry run; requires --source")
	processCascadeModeCmd.Flags().StringVar(&processCascadeSource, "source", "", "attributable USER: source (required to set the mode; not used by the no-argument read), e.g. USER:2026-09-05:enforce-cascade")
	processAdvanceCmd.Flags().StringVar(&advanceLog, "log", "", "the RUN: reference the lower trace cites (the passing run's command or report)")
	processAdvanceCmd.Flags().StringVar(&advanceBody, "body", "", "the lower trace's verdict details (RED and GREEN observations, cleanup)")
	processCmd.AddCommand(processAdvanceCmd)
	processEnterCmd.Flags().StringVar(&enterGateID, "gate-id", "", "the gate id to open (default ENTRY-<scope>; a successor when that id is taken)")
	processEnterCmd.Flags().StringVar(&enterBriefFile, "brief-file", "", "JSON brief object, overriding the packet's entry_brief section")
	processEnterCmd.Flags().BoolVar(&enterDryRun, "dry-run", false, "print the gate the verb would open and post nothing")
	processCmd.AddCommand(processEnterCmd)
	processCompleteCmd.Flags().StringVar(&completeLog, "log", "", "the delivered run's reference (CI url or command) the completion evidence cites (required)")
	processCompleteCmd.Flags().StringVar(&completeKind, "kind", "ci", "the evidence run kind: ci|local_test|browser_verification|manual")
	processCompleteCmd.Flags().StringVar(&completeBody, "body", "", "audit disclosures appended to the completion trace (gaps, deferrals, decisions)")
	processCompleteCmd.Flags().StringVar(&completeGateID, "gate-id", "", "the gate id to open (default COMPLETE-<scope>; a successor when that id is taken)")
	processCompleteCmd.Flags().StringVar(&completeBriefFile, "brief-file", "", "JSON brief object, overriding the packet's completion_brief section")
	processCompleteCmd.Flags().BoolVar(&completeDryRun, "dry-run", false, "print the ceremony the verb would run and post nothing")
	processCompleteCmd.Flags().BoolVar(&completeNoFetch, "no-fetch", false, "skip the fetch of the remote default branch (offline fixtures only)")
	processCmd.AddCommand(processCompleteCmd)
	processReapplyEntryCmd.Flags().StringVar(&processReapplyDecision, "decision", "", "attributable USER: source attesting the aggregate move was immaterial (required), e.g. USER:2026-09-14:process-repin-was-immaterial")
	processCmd.AddCommand(processReapplyEntryCmd)

	processReenterCmd.Flags().BoolVar(&processReenterApply, "apply", false, "re-pin the entry gate to the current aggregate (after the re-entry gate is answered approve); without it, open the re-entry approval gate")
	processReenterCmd.Flags().StringVar(&processReenterGateID, "gate-id", "", "the gate id to open (default REENTRY-<scope>; a successor when that id is taken, e.g. after a withdrawn re-entry gate reserved REENTRY-<scope>)")
	processCmd.AddCommand(processReenterCmd)
	processCmd.AddCommand(processCascadeModeCmd)
	processCmd.AddCommand(processCheckCmd)
	processCmd.AddCommand(processNextCmd)
	processCmd.AddCommand(processReconcileCmd)
	processCmd.AddCommand(processSupersedeCmd)
	rootCmd.AddCommand(processCmd)
}

// printLane says when a customer-blocking defect is on the clock. The lane is
// a fact of the selection, not of the route, so it prints whether or not a
// route derived (REQ-CROSS-392, F-CLI019-CR-01).
func printLane(lane string) {
	if lane == "defect" {
		fmt.Println("lane:           customer-blocking defect")
	}
}

// checkReasonLines — the reason lines under one check, from the served facts.
// A PASS line has none.
func checkReasonLines(c phaseCheck, facts *deliveryFacts) []string {
	if facts == nil || c.State == "PASS" {
		return nil
	}
	switch c.Name {
	case "canonical_sections":
		if c.State == "FAIL" && len(facts.Sections.Missing) > 0 {
			return []string{"missing or stale: " + strings.Join(facts.Sections.Missing, ", ")}
		}
	case "independent_verdict":
		cr := facts.ColdReview
		switch c.State {
		case "FAIL":
			var why string
			switch {
			case cr.Verdict != "pass":
				why = "verdict FAIL"
			case !cr.Independent:
				why = "not independent: " + independenceSentence(cr.IndependenceReason)
			default:
				why = "does not count"
			}
			return []string{fmt.Sprintf("trace %s (verdict %s): %s", cr.TraceExternalID, strings.ToUpper(cr.Verdict), why)}
		case "UNAVAILABLE":
			lines := []string{"no cold-review trace at the current packet aggregate"}
			for _, st := range cr.StaleTraces {
				lines = append(lines, fmt.Sprintf("stale: %s pinned at %s", st.ExternalID, st.Aggregate))
			}
			return lines
		}
	case "findings":
		if c.State == "FAIL" && len(facts.ColdReview.OpenFindingIDs) > 0 {
			return []string{"open or deferred material findings: " + strings.Join(facts.ColdReview.OpenFindingIDs, ", ")}
		}
	case "member_evidence_current":
		// The build and verify check. It is the members' own fact, so the
		// reason names every member that is not current with the proofs
		// reconcile reads — without them the check said only that something
		// was not current, and the member had to be found by hand.
		var lines []string
		for _, m := range facts.Members {
			if m.EvidenceState == "passing" {
				continue
			}
			line := fmt.Sprintf("%s · %s · evidence %s · %s · %s",
				m.ExternalID, presentPin(m.Status), presentPin(m.EvidenceState),
				factWord(m.RedRecorded, "RED recorded", "no RED recorded"),
				factWord(m.LowerTracePass, "lower trace passes", "no passing lower trace"))
			if m.DefectReopenWithoutEntry {
				line += " · reopened by a defect with no entry — its lower trace returns it to review"
			} else if m.EntryOrigin == nil {
				// The entry origin is this member's own fact, so the remedy
				// names this member: `process reenter` resolves the entry gate
				// of the id it is given.
				line += fmt.Sprintf(" · no live entry origin, retired by a demotion or never entered (`process reenter %s`)", m.ExternalID)
			}
			lines = append(lines, line)
		}
		return lines
	case "entry_applied":
		if c.State != "FAIL" {
			return nil
		}
		// Not "at the current aggregate": a live anchor counts whatever its
		// pin, so the fact the check reports is that no applied entry gate
		// counts as live for this scope.
		lines := []string{fmt.Sprintf("no live applied entry gate for %s", facts.Scope.ExternalID)}
		var stranded []string
		for _, m := range facts.Members {
			if m.EntryOrigin == nil && !m.DefectReopenWithoutEntry {
				stranded = append(stranded, fmt.Sprintf("%s (%s)", m.ExternalID, presentPin(m.Status)))
			}
		}
		if len(stranded) > 0 {
			lines = append(lines, "no live entry: "+strings.Join(stranded, ", "))
		}
		return lines
	case "completion_gate":
		cp := facts.Completion
		switch c.State {
		case "FAIL":
			if cp.SettledGateExternalID == "" {
				return []string{fmt.Sprintf("delivery is reconciled; no settled completion gate names %s — an answered gate counts once its answer is applied", facts.Scope.ExternalID)}
			}
			return []string{fmt.Sprintf("delivery is reconciled; gate %s is settled but the check is not met", cp.SettledGateExternalID)}
		case "NOT_APPLICABLE":
			if len(cp.MembersNotReviewed) > 0 {
				return []string{"not yet IN_REVIEW or DONE with current evidence: " + strings.Join(cp.MembersNotReviewed, ", ")}
			}
			return []string{"delivery is not reconciled"}
		}
	}
	return nil
}

// membersWithoutLiveEntry names the members behind `entry_origin_unavailable`:
// those whose evidence is NOT current and that carry no live applied entry. The
// server's routing reads the same pair in that order, and the evidence half is
// not optional — a member whose evidence passes is no blocker, and naming it
// here would send a human into a re-entry gate for the wrong requirement. It is
// a member fact, never the scope's.
func membersWithoutLiveEntry(facts *deliveryFacts) []string {
	if facts == nil {
		return nil
	}
	var ids []string
	for _, m := range facts.Members {
		if m.EvidenceState == "passing" {
			continue
		}
		if m.EntryOrigin == nil && !m.DefectReopenWithoutEntry {
			ids = append(ids, m.ExternalID)
		}
	}
	return ids
}

// factWord renders a served boolean fact as the words the reader needs, so a
// reason line reads as a sentence instead of "red_recorded=false".
func factWord(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

// independenceSentence says which arm of the independence rule the review
// context failed, in the words the facts serve it under.
func independenceSentence(reason string) string {
	switch reason {
	case "no_review_context":
		return "no review context on the trace"
	case "authored_section":
		return "the review context authored a section of this scope"
	case "authored_patch":
		return "the review context authored a patch in this scope"
	}
	return "the review context is not independent of the authoring"
}
