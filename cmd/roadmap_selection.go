package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 332 (B3): modernpath roadmap selection — write the legacy requirements
// of one context of a Modernization Release, or one Initiative's keep, change
// and drop decisions, to one JSON document, every page of the selection read in
// one file. An agent decides the rows in the file and applies them with a
// set_selection operation in 'roadmap apply', on a person's word.

// roadmapSelectionDocument is the file the command writes: the read's head as
// the first page serves it and every page's requirements, unchanged.
type roadmapSelectionDocument struct {
	Release      any   `json:"release"`
	LegacySystem any   `json:"legacy_system"`
	Context      any   `json:"context"`
	Initiative   any   `json:"initiative"`
	Counts       any   `json:"counts"`
	Requirements []any `json:"requirements"`
}

func newRoadmapSelectionCommand(load func() (*cliEnv, error)) *cobra.Command {
	selection := &cobra.Command{Use: "selection", Short: "Write the legacy requirements of a context, or an Initiative's decisions, to one JSON document", Args: cobra.NoArgs,
		Long: `Read the legacy requirements of one context of a Modernization Release, or the
keep, change and drop decisions of one Initiative, and write them as one JSON
document, every page of the read in one file.

--context takes a context of the legacy system by code or name. Without
--release the command reads the Roadmap and takes the one open (planned or
active) Modernization Release whose scope holds the context; it refuses when
none or several do, naming them, and --release <id> picks one. --initiative
<id> reads the rows of that Initiative, in every context. --out <file> writes
the document and says how many legacy requirements it holds; --json prints it.

The document holds release, legacy_system, context (null for an Initiative),
initiative (null for a context), counts (in_scope, undecided, decided, keep,
change, drop, drafted, built) and requirements: each legacy requirement with its
requirement_kind, requirement_id, display_ref, title, description, context,
work_status, legacy_proof (the legacy system's test results), group (the user
requirement a system requirement belongs to), dependents, flags (agreed,
revised_since_selected, context_changed_since_decided, not_counted,
new_since_agreement) and decision (null while undecided, else its outcome,
reason, expected level, Initiative, who set it, when, and the agent).

Each requirement also carries expected_level, the level of proof it should
reach (compared, screens_compared or tests_only), and expected_level_basis,
the code that says where that level comes from. The Selection tab gives each
code in words. changed is "changed requirement": a row decided change
expects tests_only. person is "set by {person}", with the reason: a person
set the level on a row decided keep, and the reason is the decision's
expected_level_reason. estimated is "estimated from its files": the level
comes from the roles of the source files the requirement's traces reach, and
an undecided or dropped row gets the level it should reach if kept. Read the
codes as an open list: a planned change adds recorded, "recorded by reverse
engineering".

With the basis estimated, expected_level_files gives the roles the estimate
found and how many files it read, such as
{"roles": ["component", "view"], "count": 2}; with any other basis it is null.

When the Release modernizes its own System in place, at_risk lists the
change rows behind At risk, the Selection tab's filter of kept requirements
at risk: for a row decided keep, each change row of the same Initiative whose
requirement shares a traced source file with it, with its requirement_kind,
requirement_id and display_ref. at_risk is empty for any other row and for
every row onto a new system. The command writes these four fields as the
server serves them and prints no words for them.

Each requirement also carries counterpart, the new requirements and Epics that
draft or build it: a list of {requirement, epic, state}, state drafted or
built, empty when nothing does. Onto a new system an entry names the new
requirement of a modernizes trace and the Epic whose saved plan holds it (null
once only the written trace remains); in place it names the Epic and no
requirement. holder is null, or what holds the row's decision so it cannot
change: {kind, id, title, approver}, an approved Epic ("epic") or the new
requirement of a written modernizes trace ("requirement"), with the person who
approved it.

Decide the rows in the file, then apply them with a set_selection operation
in 'roadmap apply' (see 'roadmap apply --help'), on the person's word.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			contextValue, _ := cmd.Flags().GetString("context")
			initiative, _ := cmd.Flags().GetString("initiative")
			releaseID, _ := cmd.Flags().GetInt64("release")
			outPath, _ := cmd.Flags().GetString("out")
			machine, _ := cmd.Flags().GetBool("json")
			contextValue, initiative = strings.TrimSpace(contextValue), strings.TrimSpace(initiative)
			switch {
			case contextValue != "" && initiative != "":
				return fmt.Errorf("give --context <code or name> or --initiative <id>, not both")
			case contextValue == "" && initiative == "":
				return fmt.Errorf("roadmap selection needs --context <code or name> or --initiative <id>")
			case cmd.Flags().Changed("release") && initiative != "":
				return fmt.Errorf("--release goes with --context; --initiative reads the Initiative's own Release")
			case cmd.Flags().Changed("release") && releaseID <= 0:
				return fmt.Errorf("--release must be a Release id")
			case outPath != "" && machine:
				return fmt.Errorf("give --out <file> or --json, not both")
			case outPath == "" && !machine:
				return fmt.Errorf("roadmap selection needs --out <file> or --json")
			}

			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			query := url.Values{}
			if initiative != "" {
				query.Set("line_id", initiative)
			} else {
				code, release, err := roadmapSelectionRelease(env, contextValue, releaseID)
				if err != nil {
					return err
				}
				query.Set("context", code)
				query.Set("release_id", strconv.FormatInt(release, 10))
			}
			doc, err := readRoadmapSelection(env, query)
			if err != nil {
				return err
			}

			var buf bytes.Buffer
			encoder := json.NewEncoder(&buf)
			encoder.SetEscapeHTML(false)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(doc); err != nil {
				return err
			}
			if machine {
				_, err := cmd.OutOrStdout().Write(buf.Bytes())
				return err
			}
			if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s to %s\n", roadmapLegacyRequirements(len(doc.Requirements)), outPath)
			return nil
		}}
	selection.Flags().String("context", "", "A context of the legacy system, by code or name")
	selection.Flags().Int64("release", 0, "The Modernization Release to read the context in (default: the one open Modernization Release whose scope holds it)")
	selection.Flags().String("initiative", "", "The id of an Initiative: read its keep, change and drop decisions")
	selection.Flags().String("out", "", "Write the JSON document to this file")
	selection.Flags().Bool("json", false, "Print the JSON document")
	return selection
}

// roadmapSelectionRelease reads the Roadmap and gives the context's code and
// the Release to read it in: the --release given, else the one open
// Modernization Release whose scope holds the context. The context is matched
// as Roadmap plans match a context value, by code, then by code or name in any
// case; a value the Release's scope does not hold is sent as given, for the
// server to answer.
func roadmapSelectionRelease(env *cliEnv, value string, releaseID int64) (string, int64, error) {
	status, body, err := env.get("/api/v1/systems/" + strconv.Itoa(env.SystemID) + "/roadmap")
	data, err := roadmapData(status, body, err)
	if err != nil {
		return "", 0, err
	}
	releases, _ := data["releases"].([]any)
	if releaseID > 0 {
		for _, r := range releases {
			release, ok := r.(map[string]any)
			if !ok || roadmapID(release["id"]) != releaseID {
				continue
			}
			if code := roadmapScopeContext(release, value); code != "" {
				return code, releaseID, nil
			}
		}
		return value, releaseID, nil
	}

	type candidate struct {
		id         int64
		name, code string
	}
	var found []candidate
	for _, r := range releases {
		release, ok := r.(map[string]any)
		if !ok || str(release, "release_type") != "modernization" {
			continue
		}
		if status := str(release, "status"); status != "planned" && status != "active" {
			continue
		}
		if code := roadmapScopeContext(release, value); code != "" {
			found = append(found, candidate{id: roadmapID(release["id"]), name: str(release, "name"), code: code})
		}
	}
	switch len(found) {
	case 0:
		return "", 0, fmt.Errorf("no open Modernization Release has the context %q in its scope; 'roadmap show' lists each Release's contexts", value)
	case 1:
		return found[0].code, found[0].id, nil
	default:
		names := make([]string, 0, len(found))
		for _, c := range found {
			names = append(names, fmt.Sprintf("%s (id %d)", c.name, c.id))
		}
		return "", 0, fmt.Errorf("the context %q is in the scope of several open Modernization Releases: %s; choose one with --release <id>",
			value, strings.Join(names, ", "))
	}
}

// roadmapScopeContext is the code of the context of a Modernization Release's
// scope that value names, or "" when none does.
func roadmapScopeContext(release map[string]any, value string) string {
	modernization, _ := release["modernization"].(map[string]any)
	contexts, _ := modernization["contexts"].([]any)
	lower := strings.ToLower(value)
	var byCode, byName []string
	for _, c := range contexts {
		context, ok := c.(map[string]any)
		if !ok {
			continue
		}
		code := str(context, "code")
		if code == value {
			return code
		}
		if strings.ToLower(code) == lower {
			byCode = append(byCode, code)
		}
		if strings.ToLower(str(context, "name")) == lower {
			byName = append(byName, code)
		}
	}
	if len(byCode) == 1 {
		return byCode[0]
	}
	if len(byName) == 1 {
		return byName[0]
	}
	return ""
}

// readRoadmapSelection reads every page of the selection read, following
// next_cursor until it is null.
func readRoadmapSelection(env *cliEnv, query url.Values) (*roadmapSelectionDocument, error) {
	path := "/api/v1/systems/" + strconv.Itoa(env.SystemID) + "/roadmap/selection"
	doc := &roadmapSelectionDocument{Requirements: []any{}}
	cursors := map[string]bool{}
	for page := 0; ; page++ {
		status, body, err := env.get(path + "?" + query.Encode())
		data, err := roadmapData(status, body, err)
		if err != nil {
			return nil, err
		}
		if page == 0 {
			doc.Release, doc.LegacySystem, doc.Context = data["release"], data["legacy_system"], data["context"]
			doc.Initiative, doc.Counts = data["initiative"], data["counts"]
		}
		rows, ok := data["requirements"].([]any)
		if !ok {
			return nil, fmt.Errorf("the selection read is missing its requirements")
		}
		doc.Requirements = append(doc.Requirements, rows...)
		next, _ := data["next_cursor"].(string)
		if next == "" {
			return doc, nil
		}
		if cursors[next] {
			return nil, fmt.Errorf("the selection read gave the cursor %s twice", next)
		}
		cursors[next] = true
		query.Set("cursor", next)
	}
}

// roadmapID reads a served id, a json.Number as the client decodes it or a
// float64.
func roadmapID(value any) int64 {
	switch v := value.(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	default:
		return 0
	}
}
