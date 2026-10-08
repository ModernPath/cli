package cmd

// SR-RDD-ONBOARD-034: `author context --file` sets the bounded context code
// and name of many requirements with one write. The file lists the
// requirements; the CLI reads each one's current fingerprint and kind, and
// sends one set_context request with every entry. The server checks every
// entry before writing any, so a refusal names each requirement it refused
// and nothing is written.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	authorContextFile string
	authorContextName string
)

// maxContextEntries is the server's limit on the requirements of one
// set_context call.
const maxContextEntries = 500

type contextEntry struct {
	ExternalID  string `json:"external_id"`
	Context     string `json:"context"`
	ContextName string `json:"context_name"`
}

var authorContextCmd = &cobra.Command{
	Use:   "context --file <entries.json>",
	Short: "Set the bounded context of many requirements in one write",
	Long: `Set the bounded context code and name of many requirements in one write.

The file is a JSON array. Each element is either an object

  {"external_id": "<requirement id>", "context": "<code>", "context_name": "<name>"}

or a requirement id. --context and --context-name give the context of every
element that does not name its own. Every element needs a code and a name.
The file lists at most 500 requirements; split a longer list into several
files.

The command reads the current fingerprint of each requirement and sends one
request with every entry. The context code and name are the only fields
written. If any requirement changed since it was read, is not found, is
deleted or OBSOLETE, or has current execution proof that a new context would
void, the whole list is refused, each such requirement is named with its
reason, and nothing is written. A server that does not offer this write is
refused before any requirement is read.

Each line of the result shows a requirement, its new context and its
fingerprint before and after the write. A working-set pull of these
requirements is out of date afterwards; pull them again before editing them.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		entries, err := readContextEntries(authorContextFile, authorContext, authorContextName)
		if err != nil {
			return err
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return runAuthorContext(env, entries)
	},
}

func init() {
	authorContextCmd.Flags().StringVar(&authorContextFile, "file", "", "JSON array of requirement ids or {external_id, context, context_name} objects (required)")
	authorContextCmd.Flags().StringVar(&authorContext, "context", "", "context code for every element that names none")
	authorContextCmd.Flags().StringVar(&authorContextName, "context-name", "", "context name for every element that names none")
	_ = authorContextCmd.MarkFlagRequired("file")
	authorCmd.AddCommand(authorContextCmd)
}

// readContextEntries reads the file and gives every entry its code and name,
// refusing the whole file, before any request, when an entry lacks either.
func readContextEntries(path, context, name string) ([]contextEntry, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("--file: %w", err)
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		return nil, fmt.Errorf("--file %s: a JSON array of requirement ids or objects is required: %w", path, err)
	}
	if len(elements) == 0 {
		return nil, fmt.Errorf("--file %s lists no requirement", path)
	}
	if len(elements) > maxContextEntries {
		return nil, fmt.Errorf("--file %s lists %d requirements: at most %d requirements per call; split the file", path, len(elements), maxContextEntries)
	}
	var entries []contextEntry
	var problems []string
	seen := map[string]bool{}
	for i, element := range elements {
		var entry contextEntry
		var id string
		if json.Unmarshal(element, &id) == nil {
			entry.ExternalID = id
		} else {
			dec := json.NewDecoder(bytes.NewReader(element))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&entry); err != nil {
				problems = append(problems, fmt.Sprintf("element %d: %v", i+1, err))
				continue
			}
		}
		entry.ExternalID = strings.TrimSpace(entry.ExternalID)
		if entry.Context == "" {
			entry.Context = context
		}
		if entry.ContextName == "" {
			entry.ContextName = name
		}
		switch {
		case entry.ExternalID == "":
			problems = append(problems, fmt.Sprintf("element %d: no requirement id", i+1))
		case seen[entry.ExternalID]:
			problems = append(problems, fmt.Sprintf("%s is listed more than once", entry.ExternalID))
		case strings.TrimSpace(entry.Context) == "":
			problems = append(problems, fmt.Sprintf("%s: no context code — give it in the file or with --context", entry.ExternalID))
		case strings.TrimSpace(entry.ContextName) == "":
			problems = append(problems, fmt.Sprintf("%s: no context name — give it in the file or with --context-name", entry.ExternalID))
		default:
			entries = append(entries, entry)
		}
		seen[entry.ExternalID] = true
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%d problem(s) in %s; nothing was sent:\n  %s", len(problems), path, strings.Join(problems, "\n  "))
	}
	return entries, nil
}

func runAuthorContext(env *factoryEnv, entries []contextEntry) error {
	if err := env.requireServerCapability("author.set_context", "set_context",
		"this server cannot set the context of many requirements in one write"); err != nil {
		return err
	}
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ExternalID
	}
	items, err := fetchDirectItems(env, ids, true, false)
	if err != nil {
		return err
	}
	var problems []string
	before := map[string]string{}
	sent := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		item, ok := items[entry.ExternalID]
		kind := map[string]string{"system": "SR", "user": "UR"}[item.kind]
		fingerprint := str(item.payload, "fingerprint")
		switch {
		case !ok:
			problems = append(problems, entry.ExternalID+": not found on the store")
		case kind == "":
			problems = append(problems, fmt.Sprintf("%s: not a requirement (the store serves it as %s)", entry.ExternalID, item.kind))
		case fingerprint == "":
			problems = append(problems, entry.ExternalID+": the store serves no fingerprint for it")
		default:
			before[entry.ExternalID] = fingerprint
			sent = append(sent, map[string]any{"kind": kind, "external_id": entry.ExternalID,
				"expected_fingerprint": fingerprint, "context": entry.Context, "context_name": entry.ContextName})
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d requirement(s) cannot be set; nothing was sent:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	data, err := authorPost(env, map[string]any{"action": "set_context", "record": map[string]any{
		"entries": sent, "authoring_context_id": newContextID("authoring"),
	}})
	if err != nil {
		return err
	}
	result, _ := data["set_context"].(map[string]any)
	written, ok := result["requirements"].([]any)
	if !ok {
		return fmt.Errorf("the server answered without the requirements it set; read them before retrying")
	}
	width := 0
	for _, id := range ids {
		width = max(width, len(id))
	}
	for _, raw := range written {
		row, _ := raw.(map[string]any)
		id := str(row, "external_id")
		fmt.Printf("%-*s  %s (%s)  %s -> %s\n", width, id, str(row, "context"), str(row, "context_name"), before[id], str(row, "fingerprint"))
	}
	printSuccess("set the context of %d requirement(s)", len(written))
	return nil
}
