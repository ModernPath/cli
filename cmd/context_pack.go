package cmd

// REQ-CROSS-183 — `modernpath context <REQ-id>` assembles the requirement's
// working context pack from platform data: the ledger row, its upward UR/SCN
// trace, its cited files with resolution state, server enrichment when the
// workspace is bound, and the citations' coverage/test-axis state. Read-only.
//
// The pack degrades gracefully: sections 1-3 and 5 come from the local
// ledger/requirements parse (internal/rdd); the server section reports
// exactly why it is absent rather than inventing content. `--json` emits the
// same pack machine-readable for agent harnesses.
//
// This file plugs into the existing `context` verb (cmd/context.go, the hook
// context command) by wrapping its RunE: a single argument shaped like a REQ
// id is a pack request, anything else stays a prompt. New file on purpose —
// the shared commands are not refactored.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/rdd"
	"github.com/spf13/cobra"
)

var contextPackJSON bool

func init() {
	contextCmd.Flags().BoolVar(&contextPackJSON, "json", false,
		"REQ-id mode: emit the context pack as JSON")

	orig := contextCmd.RunE // runContext — captured before the wrap
	contextCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 && isContextPackID(args[0]) {
			cmd.SilenceUsage = true
			return runContextPack(args[0])
		}
		return orig(cmd, args)
	}
}

var contextPackIDRe = regexp.MustCompile(`^REQ-[A-Z][A-Z0-9]*-\d+$`)

// isContextPackID separates a pack request from a hook-context prompt. Exact
// shape only: a REQ id inside a sentence is still a prompt.
func isContextPackID(arg string) bool {
	return contextPackIDRe.MatchString(strings.TrimSpace(arg))
}

func runContextPack(reqID string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	pack, err := buildContextPack(root, reqID, serverFromWorkspace())
	if err != nil {
		return err
	}
	if contextPackJSON {
		raw, err := json.MarshalIndent(pack, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	renderContextPack(os.Stdout, pack)
	return nil
}

// ---------------------------------------------------------------- the pack

type contextPack struct {
	Requirement packRow        `json:"requirement"`
	Trace       packTrace      `json:"upward_trace"`
	Citations   []packCitation `json:"citations"`
	Server      packServer     `json:"server"`
	Coverage    packCoverage   `json:"coverage"`
}

type packRow struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Statement  string   `json:"statement,omitempty"`
	Criteria   []string `json:"criteria,omitempty"`
	Status     string   `json:"status"`
	Stage      string   `json:"stage,omitempty"`
	Context    string   `json:"context"`
	CtxName    string   `json:"context_name,omitempty"`
	Ledger     string   `json:"ledger"`
	DetailLine int      `json:"detail_line,omitempty"` // 1-based `### REQ-…` heading; 0 = no detail block
	UR         string   `json:"ur,omitempty"`
}

type packTrace struct {
	UR   *packUR `json:"ur"`
	Note string  `json:"note,omitempty"` // why the trace is absent — stated, never guessed
}

type packUR struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Statement string    `json:"statement,omitempty"`
	Status    string    `json:"status"`
	File      string    `json:"file"`
	Scenarios []packSCN `json:"scenarios,omitempty"`
}

type packSCN struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

type packCitation struct {
	Ref          string `json:"ref"`   // as cited (tag stripped)
	Field        string `json:"field"` // code | tests
	Resolution   string `json:"resolution"`
	ResolvedPath string `json:"resolved_path,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	Language     string `json:"language,omitempty"`
}

type packServer struct {
	Available    bool             `json:"available"`
	Reason       string           `json:"reason,omitempty"`
	Requirement  map[string]any   `json:"requirement,omitempty"`
	UserReq      map[string]any   `json:"user_requirement,omitempty"`
	OpenGates    []map[string]any `json:"open_gates,omitempty"`
	DocHints     []string         `json:"doc_hints,omitempty"`
	DocHintBasis string           `json:"doc_hint_basis,omitempty"`
	Notes        []string         `json:"notes,omitempty"`
}

type packCoverage struct {
	Citations int    `json:"citations"`
	AtPath    int    `json:"at_path"`
	Basename  int    `json:"basename_resolved"`
	Nowhere   int    `json:"nowhere"`
	TestAxis  string `json:"test_axis"` // tests-cited | dash | none
}

// serverPackFn supplies section 4. The seam exists so tests (and an unbound
// workspace) get an honest "unavailable" instead of an invented one.
type serverPackFn func(reqID string, cites []packCitation) packServer

func serverUnavailable(reason string) serverPackFn {
	return func(string, []packCitation) packServer {
		return packServer{Available: false, Reason: reason}
	}
}

// ---------------------------------------------------------------- build

func buildContextPack(root, reqID string, server serverPackFn) (*contextPack, error) {
	row, ledgerRel, detailLine, err := findLedgerRow(root, reqID)
	if err != nil {
		return nil, err
	}

	pack := &contextPack{
		Requirement: packRow{
			ID:         row.ID,
			Title:      row.Title,
			Statement:  detailField(row.Detail, "Statement"),
			Criteria:   detailCriteria(row.Detail),
			Status:     row.Status,
			Stage:      row.Stage,
			Context:    row.Ctx,
			CtxName:    row.CtxName,
			Ledger:     ledgerRel,
			DetailLine: detailLine,
			UR:         row.UR,
		},
	}

	pack.Trace = upwardTrace(root, row.UR)
	pack.Citations = resolveCitations(root, extractRowCitations(row))
	pack.Coverage = coverageOf(pack.Citations, row)
	pack.Server = server(reqID, pack.Citations)

	// Doc pointers: no server endpoint answers "which knowledge-core doc owns
	// this file" by REQ id, so the hints come from the local export — scanned,
	// with the basis stated, whatever the server section's own availability.
	if hints, basis := localDocHints(root, pack.Citations); len(hints) > 0 {
		pack.Server.DocHints = hints
		pack.Server.DocHintBasis = basis
	}
	return pack, nil
}

// findLedgerRow scans tasks/*.md through the same parser sync uses. The
// detail-block line is located textually so the pack can say where to read.
func findLedgerRow(root, reqID string) (rdd.Req, string, int, error) {
	dir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return rdd.Req{}, "", 0, fmt.Errorf("unknown requirement id %s: no tasks/ ledger directory under %s", reqID, root)
	}
	var searched []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := filepath.Join("tasks", e.Name())
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		content := string(body)
		for _, req := range rdd.ParseLedger(rel, content) {
			if req.ID != reqID {
				continue
			}
			line := 0
			headRe := regexp.MustCompile(`(?m)^###\s+` + regexp.QuoteMeta(reqID) + `\b`)
			if loc := headRe.FindStringIndex(content); loc != nil {
				line = 1 + strings.Count(content[:loc[0]], "\n")
			}
			return req, rel, line, nil
		}
		searched = append(searched, rel)
	}
	return rdd.Req{}, "", 0, fmt.Errorf("unknown requirement id %s: not found in any ledger under tasks/ (searched %d files)", reqID, len(searched))
}

// ---------------------------------------------------------------- detail parse

var detailBulletRe = regexp.MustCompile(`(?m)^-\s+\*\*([A-Za-z -]+):\*\*[ \t]*`)

// detailField reads one `- **Name:** …` bullet from a detail block, re-flowed
// to a single line, stopping at the next top-level bullet.
func detailField(detail, name string) string {
	locs := detailBulletRe.FindAllStringSubmatchIndex(detail, -1)
	for i, loc := range locs {
		if detail[loc[2]:loc[3]] != name {
			continue
		}
		end := len(detail)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		return strings.Join(strings.Fields(detail[loc[1]:end]), " ")
	}
	return ""
}

// detailCriteria returns the acceptance criteria as one entry per sub-bullet;
// inline criteria (text on the same line) come back as a single entry.
func detailCriteria(detail string) []string {
	locs := detailBulletRe.FindAllStringSubmatchIndex(detail, -1)
	for i, loc := range locs {
		if detail[loc[2]:loc[3]] != "Acceptance criteria" {
			continue
		}
		end := len(detail)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := detail[loc[1]:end]

		var out []string
		var inline []string
		for _, raw := range strings.Split(body, "\n") {
			line := strings.TrimSpace(raw)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "- ") {
				out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
				continue
			}
			if len(out) > 0 { // continuation of the previous sub-bullet
				out[len(out)-1] += " " + line
			} else {
				inline = append(inline, line)
			}
		}
		if len(out) == 0 && len(inline) > 0 {
			out = []string{strings.Join(inline, " ")}
		}
		return out
	}
	return nil
}

// ---------------------------------------------------------------- upward trace

// upwardTrace resolves the row's UR against requirements/*-USER-REQUIREMENTS.md
// (the shape internal/rdd.ParseUserRequirements defines). Absence is reported
// with its reason.
func upwardTrace(root, urID string) packTrace {
	if urID == "" {
		return packTrace{Note: "the row names no UR (no UR column value and no detail-block UR line)"}
	}
	dir := filepath.Join(root, "requirements")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return packTrace{Note: fmt.Sprintf("the row names %s but the workspace has no requirements/ directory", urID)}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "-USER-REQUIREMENTS.md") {
			continue
		}
		rel := filepath.Join("requirements", e.Name())
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		urs, _ := rdd.ParseUserRequirements(rel, string(body))
		for _, ur := range urs {
			if ur.ID != urID {
				continue
			}
			out := &packUR{
				ID:        ur.ID,
				Title:     ur.Title,
				Statement: ur.Statement,
				Status:    ur.Status,
				File:      rel,
			}
			for _, scn := range ur.Scenarios {
				out.Scenarios = append(out.Scenarios, packSCN{ID: scn.ID, Body: scn.Body})
			}
			return packTrace{UR: out}
		}
	}
	return packTrace{Note: fmt.Sprintf("the row names %s but no requirements/*-USER-REQUIREMENTS.md file defines it", urID)}
}

// ---------------------------------------------------------------- citations

// The citation shape mirrors internal/gate/citation.go (REQ-CROSS-165): a
// declaration field (`- **Code:**` / `- **Tests:**`) is what separates a
// citation from an illustration. Backticked tokens are accepted more widely
// than the gate's regex, because real ledgers carry `CODE:`-tagged refs and
// Next.js-style bracketed paths the gate's strict class rejects.
var (
	packCodeFieldRe  = regexp.MustCompile(`^\s*-\s+\*\*Code:\*\*(.*)$`)
	packTestsFieldRe = regexp.MustCompile(`^\s*-\s+\*\*Tests:\*\*(.*)$`)
	packTickTokenRe  = regexp.MustCompile("`([^`]+)`")
	packCiteTagRe    = regexp.MustCompile(`^(?:CODE|TESTS?):`)
	packPathishRe    = regexp.MustCompile(`^[^\s]+\.[A-Za-z0-9]{1,5}$`)
)

type rawCitation struct {
	ref   string
	field string // code | tests
}

// extractRowCitations gathers refs from the dashboard Tests/Code cells and the
// detail block's Code:/Tests: lines, first mention wins, order preserved.
func extractRowCitations(row rdd.Req) []rawCitation {
	var out []rawCitation
	seen := map[string]bool{}
	add := func(field, tok string) {
		ref := citationRef(tok)
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		out = append(out, rawCitation{ref: ref, field: field})
	}
	harvest := func(field, text string) {
		ticks := packTickTokenRe.FindAllStringSubmatch(text, -1)
		for _, m := range ticks {
			add(field, m[1])
		}
		if len(ticks) == 0 { // a bare cell like `my-deals-page.tsx` without backticks
			for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' }) {
				add(field, tok)
			}
		}
	}
	harvest("tests", row.Tests)
	harvest("code", row.Code)
	for _, line := range strings.Split(row.Detail, "\n") {
		if m := packTestsFieldRe.FindStringSubmatch(line); m != nil {
			harvest("tests", m[1])
		} else if m := packCodeFieldRe.FindStringSubmatch(line); m != nil {
			harvest("code", m[1])
		}
	}
	return out
}

// citationRef normalizes one token to a citable file ref, or "" when the
// token is not one (a dash, a directory, prose, an id).
func citationRef(tok string) string {
	ref := strings.TrimSpace(packCiteTagRe.ReplaceAllString(strings.TrimSpace(tok), ""))
	// `path.ex:symbol` — the file part is the citation
	if i := strings.Index(ref, ":"); i > 0 && strings.Contains(ref[:i], ".") {
		ref = ref[:i]
	}
	if !packPathishRe.MatchString(ref) {
		return ""
	}
	if strings.HasSuffix(ref, ".md") && !strings.ContainsAny(ref, "/") {
		// bare doc basenames are ambiguous everywhere; keep pathed ones only
		return ""
	}
	return ref
}

// resolveCitations triages each ref: at-path, basename→resolved, or nowhere —
// the same working-tree fallback rule the citation gate uses, because
// gitignored files are real. Resolved files carry size and a language guess.
func resolveCitations(root string, raw []rawCitation) []packCitation {
	var byBase map[string][]string
	baseIndex := func() map[string][]string {
		if byBase != nil {
			return byBase
		}
		byBase = map[string][]string{}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				switch info.Name() {
				case ".git", "node_modules", "_build", "deps", ".elixir_ls", "coverage":
					return filepath.SkipDir
				}
				return nil
			}
			if rel, err := filepath.Rel(root, path); err == nil {
				byBase[info.Name()] = append(byBase[info.Name()], rel)
			}
			return nil
		})
		return byBase
	}

	out := make([]packCitation, 0, len(raw))
	for _, rc := range raw {
		c := packCitation{Ref: rc.ref, Field: rc.field, Resolution: "nowhere"}
		if info, err := os.Stat(filepath.Join(root, rc.ref)); err == nil && !info.IsDir() {
			c.Resolution = "at-path"
			c.SizeBytes = info.Size()
			c.Language = languageOf(rc.ref)
		} else {
			candidates := baseIndex()[filepath.Base(rc.ref)]
			sort.Strings(candidates)
			for _, cand := range candidates {
				if cand == rc.ref || strings.HasSuffix(cand, string(filepath.Separator)+rc.ref) {
					c.Resolution = "basename"
					c.ResolvedPath = cand
					if info, err := os.Stat(filepath.Join(root, cand)); err == nil {
						c.SizeBytes = info.Size()
					}
					c.Language = languageOf(cand)
					break
				}
			}
		}
		out = append(out, c)
	}
	return out
}

func languageOf(path string) string {
	switch strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".") {
	case "go":
		return "Go"
	case "ex", "exs":
		return "Elixir"
	case "ts":
		return "TypeScript"
	case "tsx":
		return "TypeScript (React)"
	case "js", "mjs", "cjs":
		return "JavaScript"
	case "jsx":
		return "JavaScript (React)"
	case "py":
		return "Python"
	case "rb":
		return "Ruby"
	case "rs":
		return "Rust"
	case "sh", "bash":
		return "Shell"
	case "sql":
		return "SQL"
	case "json":
		return "JSON"
	case "yml", "yaml":
		return "YAML"
	case "md":
		return "Markdown"
	case "css":
		return "CSS"
	case "html", "heex", "eex":
		return "HTML"
	default:
		return ""
	}
}

// ---------------------------------------------------------------- coverage

func coverageOf(cites []packCitation, row rdd.Req) packCoverage {
	cov := packCoverage{Citations: len(cites)}
	testsCited := false
	for _, c := range cites {
		switch c.Resolution {
		case "at-path":
			cov.AtPath++
		case "basename":
			cov.Basename++
		default:
			cov.Nowhere++
		}
		if c.Field == "tests" {
			testsCited = true
		}
	}
	switch {
	case testsCited:
		cov.TestAxis = "tests-cited"
	case isDashCell(row.Tests) || isDashCell(detailField(row.Detail, "Tests")):
		cov.TestAxis = "dash"
	default:
		cov.TestAxis = "none"
	}
	return cov
}

func isDashCell(s string) bool {
	switch strings.TrimSpace(s) {
	case "—", "–", "-":
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------- doc hints

// Words too generic to identify an owning subsystem — "page.tsx" must not
// claim every layout doc in the export.
var docHintStopWords = map[string]bool{
	"page": true, "index": true, "main": true, "util": true, "test": true,
	"spec": true, "type": true, "config": true, "component": true, "app": true,
	"lib": true, "src": true, "data": true, "base": true, "core": true,
	"common": true, "shared": true, "helper": true, "client": true,
	"server": true, "module": true, "view": true, "file": true,
}

// docHintWords splits a name into its identifying words: lowercase, non-word
// separators, a naive plural stem, length ≥ 4, stop-words dropped.
func docHintWords(name string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	}) {
		w = strings.TrimSuffix(w, "s")
		if len(w) >= 4 && !docHintStopWords[w] {
			out[w] = true
		}
	}
	return out
}

func docHintWordsMatch(a, b map[string]bool) bool {
	for w := range a {
		if b[w] {
			return true
		}
	}
	return false
}

// localDocHints matches cited file names against the local knowledge-core
// export's architecture docs (.modernpath/<slug>/architecture/…) by shared
// identifying words. This is a filename scan of the export cache, not a
// server answer — the basis says so.
func localDocHints(root string, cites []packCitation) ([]string, string) {
	mpRoot := filepath.Join(root, ".modernpath")
	slugs := listSystemExportSlugs(mpRoot)
	if len(slugs) == 0 {
		return nil, ""
	}

	tokens := map[string]bool{}
	for _, c := range cites {
		base := strings.TrimSuffix(filepath.Base(c.Ref), filepath.Ext(c.Ref))
		for w := range docHintWords(base) {
			tokens[w] = true
		}
	}

	var hints []string
	for _, slug := range slugs {
		dir, ok := resolveSystemRootDir(mpRoot, slug)
		if !ok {
			continue
		}
		arch := filepath.Join(dir, "architecture")
		if info, err := os.Stat(arch); err != nil || !info.IsDir() {
			continue
		}
		matched := false
		_ = filepath.Walk(arch, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") || len(hints) >= 8 {
				return nil
			}
			// The doc's own name plus its subsystem directory both identify it:
			// architecture/deals/domain-deals.md answers for `deal-constants.ts`.
			rel, err := filepath.Rel(arch, path)
			if err != nil {
				return nil
			}
			if docHintWordsMatch(tokens, docHintWords(strings.TrimSuffix(rel, ".md"))) {
				if wsRel, err := filepath.Rel(root, path); err == nil {
					hints = append(hints, wsRel)
					matched = true
				}
			}
			return nil
		})
		if !matched { // no doc named like a cited file: point at the map
			for _, idx := range []string{filepath.Join(arch, "INDEX.md"), arch} {
				if _, err := os.Stat(idx); err == nil {
					if rel, err := filepath.Rel(root, idx); err == nil {
						hints = append(hints, rel)
					}
					break
				}
			}
		}
	}
	return hints, "local export filename scan (no doc-pointer API endpoint addresses a REQ id; export is a cache — the API wins on disagreement)"
}

// ---------------------------------------------------------------- server

// serverFromWorkspace enriches from the bound platform: the synced requirement
// row (server-side work_status, release, criteria), the synced UR it derives
// from, and the open gates that mention or hold the id. Every failure path
// reports its reason — an absent section is stated, never invented.
func serverFromWorkspace() serverPackFn {
	return func(reqID string, _ []packCitation) packServer {
		env, err := factoryEnvLoad()
		if err != nil {
			return packServer{Available: false, Reason: err.Error()}
		}

		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID), nil)
		if err != nil {
			return packServer{Available: false, Reason: err.Error()}
		}
		if status != 200 {
			return packServer{Available: false, Reason: fmt.Sprintf("server %d on /api/v1/sync/requirements: %v", status, body["error"])}
		}

		out := packServer{Available: true}
		data := dataOf(body)

		if rows, ok := data["requirements"].([]any); ok {
			for _, r := range rows {
				m, _ := r.(map[string]any)
				if str(m, "external_id") != reqID {
					continue
				}
				row := map[string]any{}
				for _, k := range []string{"external_id", "title", "work_status", "stage", "context", "release", "release_status"} {
					if v, ok := m[k]; ok && v != nil {
						row[k] = v
					}
				}
				if criteria, ok := m["criteria"].([]any); ok {
					row["criteria_count"] = len(criteria)
				}
				out.Requirement = row
				break
			}
		}
		if out.Requirement == nil {
			out.Notes = append(out.Notes, fmt.Sprintf("%s is not among the synced requirements — the ledger row has not reached the server", reqID))
		}

		if urs, ok := data["user_requirements"].([]any); ok {
			for _, u := range urs {
				m, _ := u.(map[string]any)
				children, _ := m["system_requirement_external_ids"].([]any)
				serves := false
				for _, c := range children {
					if c == reqID {
						serves = true
						break
					}
				}
				if !serves {
					continue
				}
				out.UserReq = map[string]any{
					"external_id": m["external_id"],
					"title":       m["title"],
					"work_status": m["work_status"],
				}
				break
			}
		}

		gStatus, gBody, gErr := env.call("GET", fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID), nil)
		if gErr != nil || gStatus != 200 {
			out.Notes = append(out.Notes, fmt.Sprintf("open gates unavailable (gates read failed: status %d, %v)", gStatus, gErr))
		} else if gates, ok := dataOf(gBody)["gates"].([]any); ok {
			for _, g := range gates {
				m, _ := g.(map[string]any)
				raw, _ := json.Marshal(m)
				if !strings.Contains(string(raw), reqID) {
					continue
				}
				out.OpenGates = append(out.OpenGates, map[string]any{
					"external_id": m["external_id"],
					"kind":        m["kind"],
					"title":       m["title"],
				})
			}
		}

		out.Notes = append(out.Notes,
			"trace links (compliance_traces) are only addressable by internal entity id — no per-REQ endpoint; see trace_summary on /api/v1/sync/requirements for the system-level counts")
		return out
	}
}

// ---------------------------------------------------------------- render

func renderContextPack(w io.Writer, p *contextPack) {
	r := p.Requirement

	fmt.Fprintf(w, "%s — %s\n", r.ID, r.Title)
	loc := r.Ledger
	if r.DetailLine > 0 {
		loc = fmt.Sprintf("%s · detail block at line %d", r.Ledger, r.DetailLine)
	}
	ctx := r.Context
	if r.CtxName != "" {
		ctx += " (" + r.CtxName + ")"
	}
	fmt.Fprintf(w, "ledger: %s · context: %s\n", loc, ctx)
	fmt.Fprintf(w, "status: %s", r.Status)
	if r.Stage != "" {
		fmt.Fprintf(w, " · stage: %s", r.Stage)
	}
	if r.UR != "" {
		fmt.Fprintf(w, " · UR: %s", r.UR)
	}
	fmt.Fprintln(w)

	if r.Statement != "" {
		fmt.Fprintf(w, "\nstatement:\n  %s\n", r.Statement)
	}
	if len(r.Criteria) > 0 {
		fmt.Fprintf(w, "\nacceptance criteria:\n")
		for _, c := range r.Criteria {
			fmt.Fprintf(w, "  - %s\n", c)
		}
	}

	fmt.Fprintf(w, "\nupward trace:\n")
	if ur := p.Trace.UR; ur != nil {
		fmt.Fprintf(w, "  %s — %s (%s) · %s\n", ur.ID, ur.Title, ur.Status, ur.File)
		if ur.Statement != "" {
			fmt.Fprintf(w, "    statement: %s\n", ur.Statement)
		}
		for _, scn := range ur.Scenarios {
			fmt.Fprintf(w, "    %s: %s\n", scn.ID, strings.Join(strings.Fields(scn.Body), " "))
		}
	} else {
		fmt.Fprintf(w, "  none — %s\n", p.Trace.Note)
	}

	fmt.Fprintf(w, "\ncited files:\n")
	if len(p.Citations) == 0 {
		fmt.Fprintf(w, "  none — the row cites no files\n")
	}
	for _, c := range p.Citations {
		detail := c.Resolution
		if c.Resolution == "basename" && c.ResolvedPath != "" {
			detail = "basename → " + c.ResolvedPath
		}
		size := ""
		if c.SizeBytes > 0 {
			size = " · " + humanBytes(c.SizeBytes)
		}
		lang := ""
		if c.Language != "" {
			lang = " · " + c.Language
		}
		fmt.Fprintf(w, "  [%s] %s — %s%s%s\n", c.Field, c.Ref, detail, size, lang)
	}

	fmt.Fprintf(w, "\nserver:\n")
	if !p.Server.Available {
		fmt.Fprintf(w, "  server: unavailable (%s)\n", p.Server.Reason)
	} else {
		if req := p.Server.Requirement; req != nil {
			line := fmt.Sprintf("  synced requirement: work_status=%v", req["work_status"])
			if rel, ok := req["release"]; ok {
				line += fmt.Sprintf(" · release=%v (%v)", rel, req["release_status"])
			}
			if n, ok := req["criteria_count"]; ok {
				line += fmt.Sprintf(" · %v criteria", n)
			}
			fmt.Fprintln(w, line)
		}
		if ur := p.Server.UserReq; ur != nil {
			fmt.Fprintf(w, "  synced UR: %v — %v (work_status=%v)\n", ur["external_id"], ur["title"], ur["work_status"])
		}
		if len(p.Server.OpenGates) == 0 {
			fmt.Fprintf(w, "  open gates mentioning %s: none\n", r.ID)
		}
		for _, g := range p.Server.OpenGates {
			fmt.Fprintf(w, "  open gate: %v [%v] %v\n", g["external_id"], g["kind"], g["title"])
		}
	}
	if len(p.Server.DocHints) > 0 {
		fmt.Fprintf(w, "  doc hints (%s):\n", p.Server.DocHintBasis)
		for _, h := range p.Server.DocHints {
			fmt.Fprintf(w, "    %s\n", h)
		}
	}
	for _, n := range p.Server.Notes {
		fmt.Fprintf(w, "  note: %s\n", n)
	}

	cov := p.Coverage
	fmt.Fprintf(w, "\ncoverage:\n")
	fmt.Fprintf(w, "  %d citation(s): %d at-path · %d basename-resolved · %d nowhere\n",
		cov.Citations, cov.AtPath, cov.Basename, cov.Nowhere)
	fmt.Fprintf(w, "  test axis: %s\n", cov.TestAxis)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
