package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
	"github.com/spf13/cobra"
)

// REQ-CROSS-387 (EPIC-CLI-019): `modernpath feedback "<line>"` files a
// tooling gap — something the CLI, the store or the harness lacked — as a
// BACKLOG-TOOL-<n> record in the ModernPath workspace (a REQ-CROSS-393 create
// of kind tooling), with the CLI build, the server contract and the time
// captured; --last attaches the previous user-run command and its output.
// An unverified destination is refused without a customer-store or local-file write.

var (
	feedbackLast bool
	feedbackRef  int
)

var feedbackCmd = &cobra.Command{
	Use:   "feedback <line>",
	Short: "File a tooling gap only in the ModernPath workspace",
	Long: `File a tooling gap — a surface the CLI, the store, Mission Control or the
harness lacked — as one BACKLOG-TOOL-<n> record in the current ModernPath
workspace, attributed to you, with the CLI build, server contract version and
time captured. The credential must belong to the ModernPath organization; the
server must identify the checkout's bound system with slug modernpath. Feedback
refuses customer workspaces rather than changing the destination.
The record id is printed; read it from a checkout bound to ModernPath with
'working-set pull <id>'.

--last also attaches the previous modernpath command you ran, its exit status
and the tail of its output (from the local history .modernpath/cli-history.log;
hook invocations and 'auth' are never in it), and shows the entry it is about
to attach before the record is written. --ref <n> attaches the n-th most
recent command instead (--ref 1 is --last) and implies --last; a value beyond
the recorded count is refused naming how many entries exist. Nothing else is
captured.

When the credential or destination cannot be verified, the command fails
without writing a backlog record or a local fallback file. A create the server
refuses is reported verbatim.

Examples:
  modernpath feedback "process check prints only a check name"
  modernpath feedback --last "working-set pull refuses the release gate"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFeedback(args[0], feedbackLast, feedbackRef)
	},
}

func init() {
	feedbackCmd.Flags().BoolVar(&feedbackLast, "last", false, "attach the previous modernpath command, its exit status and output tail")
	feedbackCmd.Flags().IntVar(&feedbackRef, "ref", 0, "attach the n-th most recent recorded command instead (1 is --last; implies --last)")
	rootCmd.AddCommand(feedbackCmd)
}

func runFeedback(line string, attachLast bool, ref int) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return fmt.Errorf("feedback needs the gap in one line")
	}
	if ref < 0 {
		return fmt.Errorf("--ref counts back from the most recent command: 1 is --last")
	}
	observation := line
	if attachLast || ref > 0 {
		if ref == 0 {
			ref = 1
		}
		// REQ-CROSS-410: the entry is shown before anything is written, so the
		// session sees what the record will carry and can pick another with --ref.
		note, err := previousInvocationNoteAt(ref)
		if err != nil {
			return err
		}
		printInfo("attaching: %s\n", note.shown)
		observation += "\n\n" + note.body
	}

	env, err := feedbackEnvLoad()
	if err != nil {
		return err
	}

	id, err := allocateToolingID(env)
	if err != nil {
		return err
	}

	for attempt := 0; attempt < 2; attempt++ {
		record := map[string]any{
			"kind":            "backlog",
			"external_id":     id,
			"backlog_kind":    "tooling",
			"title":           line,
			"observation":     observation,
			"why_unrouted":    "a tooling gap met in a session; the surface belongs to the CLI or the store",
			"candidate_route": "CLI or store fix",
			"metadata": map[string]any{
				"cli_version":     Version,
				"server_contract": serverContractOr(env),
				"filed_at":        time.Now().UTC().Format(time.RFC3339),
			},
		}
		body := map[string]any{"action": "create", "record": record, "system_id": env.SystemID, "actor": authorActor}
		status, resp, err := env.call("POST", "/api/v1/sync/author", body)
		switch {
		case err != nil:
			return err
		case status == 409 && attempt == 0:
			id = nextToolingID(id)
			continue
		case status != 200:
			return serverRefusal("feedback refused", status, resp)
		}
		row, _ := dataOf(resp)["backlog"].(map[string]any)
		printSuccess("filed %s in ModernPath (kind tooling, %s)", firstNonEmpty(str(row, "external_id"), id), firstNonEmpty(str(row, "disposition"), "OPEN"))
		if fp := str(row, "fingerprint"); fp != "" {
			printInfo("fingerprint: %s (pass as --expected-fingerprint to author update --kind backlog)", fp)
		}
		printInfo("read it from a checkout bound to ModernPath with 'modernpath working-set pull %s'", firstNonEmpty(str(row, "external_id"), id))
		return nil
	}
	return fmt.Errorf("feedback: the allocated id was taken twice — retry")
}

// The production organization is an identity-provider ID, not a server-specific
// system ID. Verified with `modernpath auth status` on 2026-09-27.
const feedbackOrganizationID = "371734807656268047"

func feedbackEnvLoad() (*factoryEnv, error) {
	env, err := factoryCredentialLoad()
	if err != nil {
		return nil, err
	}
	claims, ok := zitadel.TokenClaims(env.token)
	if !ok || claims.OrganizationID != feedbackOrganizationID || claims.Issuer != zitadel.ProdProfile.Issuer {
		return nil, fmt.Errorf("tooling feedback requires a credential for the ModernPath production workspace; nothing was written")
	}
	systems, err := listSystemsFn(env.APIURL, env.token)
	if err != nil {
		return nil, fmt.Errorf("could not verify the ModernPath feedback destination: %w", err)
	}
	for _, system := range systems {
		if system.ID == env.SystemID && system.ID > 0 && system.Slug == "modernpath" {
			return env, nil
		}
	}
	return nil, fmt.Errorf("tooling feedback requires this checkout to be bound to the ModernPath workspace; nothing was written")
}

// allocateToolingID is the next BACKLOG-TOOL-<n> after the highest served.
func allocateToolingID(env *factoryEnv) (string, error) {
	rows, err := fetchList(env, fmt.Sprintf("/api/v1/sync/backlog?system_id=%d&kind=tooling", env.SystemID), "backlog")
	if err != nil {
		return "", err
	}
	highest := 0
	for _, r := range rows {
		m, _ := r.(map[string]any)
		if n, ok := toolingNumber(str(m, "external_id")); ok && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("BACKLOG-TOOL-%d", highest+1), nil
}

func toolingNumber(id string) (int, bool) {
	const prefix = "BACKLOG-TOOL-"
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	return n, err == nil
}

func nextToolingID(id string) string {
	n, _ := toolingNumber(id)
	return fmt.Sprintf("BACKLOG-TOOL-%d", n+1)
}

// serverContractOr is the contract version the server advertised on this
// process's responses, or "unknown" until REQ-CROSS-390 serves one.
func serverContractOr(env *factoryEnv) string {
	if env.contractVersion != "" {
		return env.contractVersion
	}
	return "unknown"
}

// invocationNote is one recorded command as the record carries it (body) and
// as the session sees it before the write (shown: the command line, its exit
// and the first output line).
type invocationNote struct {
	shown string
	body  string
}

// previousInvocationNoteAt renders the ref-th most recent recorded user-run
// command (1 is the last). A ref beyond the recorded count is refused naming
// how many entries exist; with none recorded the note says so.
func previousInvocationNoteAt(ref int) (invocationNote, error) {
	root := workspaceRootOrCwd()
	entries := historyEntries(root)
	if len(entries) == 0 {
		return invocationNote{shown: "(no previous command recorded)", body: "(--last: no previous command recorded)"}, nil
	}
	if ref > len(entries) {
		return invocationNote{}, fmt.Errorf("--ref %d: only %d command(s) are recorded — pick 1..%d (1 is the most recent)", ref, len(entries), len(entries))
	}
	prev := entries[len(entries)-ref]
	command := fmt.Sprintf("modernpath %s (exit %d)", strings.Join(prev.Argv, " "), prev.Exit)
	var b strings.Builder
	fmt.Fprintf(&b, "Previous command: %s", command)
	shown := command
	if out := strings.TrimSpace(prev.Output); out != "" {
		fmt.Fprintf(&b, "\nOutput:\n%s", out)
		shown += "\n  " + strings.SplitN(out, "\n", 2)[0]
	} else {
		b.WriteString("\n(output was not captured)")
		shown += "\n  (output was not captured)"
	}
	return invocationNote{shown: shown, body: b.String()}, nil
}

func workspaceRootOrCwd() string {
	if cfgDir, err := config.FindConfigDir(); err == nil && cfgDir != "" {
		return filepath.Dir(cfgDir)
	}
	cwd, _ := os.Getwd()
	return cwd
}
