package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

func newModernizationCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "modernization", Short: "Capture and read historical modernization test results"}
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Atomically capture test outcomes, with their commit chain, for a Modernization Release",
		Long:  modernizationRecordHelp,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			releaseID, _ := cmd.Flags().GetInt("release-id")
			if releaseID < 1 {
				return fmt.Errorf("--release-id must be a positive integer")
			}
			file, _ := cmd.Flags().GetString("file")
			payload, err := readObjectInput(cmd, file)
			if err != nil {
				return err
			}
			if err := validateModernizationCapture(payload); err != nil {
				return err
			}
			// Epic 460 (H3): a --legacy-checkout that is not a git checkout is
			// refused before anything is sent.
			legacyCheckout, _ := cmd.Flags().GetString("legacy-checkout")
			if legacyCheckout != "" && !modernizationIsCheckout(legacyCheckout) {
				return fmt.Errorf("--legacy-checkout %s is not a git checkout.", legacyCheckout)
			}
			sha := payload["sha"].(string)
			// Epic 390: the commit chain is read from the checkout the command
			// runs in. Its two refusals come before anything is sent.
			checkout, err := modernizationCheckoutCommit(".", sha)
			if err != nil {
				return err
			}
			// Epic 460 (H3): the commit's message, author and date, from this
			// folder when it holds the commit, else from --legacy-checkout.
			var details map[string]any
			if checkout.Held {
				details, err = modernizationCommitDetails(".", checkout.FullID)
			} else if legacyCheckout != "" {
				details, err = modernizationCommitDetails(legacyCheckout, sha)
			}
			if err != nil {
				return err
			}
			if details != nil {
				payload["commit_details"] = details
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "the run carries no commit details: neither this folder nor --legacy-checkout holds commit %s.\n", sha)
			}
			env, err := load()
			if err != nil {
				return err
			}
			if checkout.Held {
				payload["sha"] = checkout.FullID
				stops, err := modernizationChainStops(env, releaseID)
				if err != nil {
					return err
				}
				chain, err := modernizationCommitChain(".", checkout.FullID, stops)
				if err != nil {
					return err
				}
				if len(chain.Entries) == 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "the run carries no commit chain: the commit object of %s alone passes the limit of %d bytes\n", checkout.FullID, modernizationChainMaxBytes)
				} else {
					payload["commit_chain"] = chain.payload()
					fmt.Fprintln(cmd.ErrOrStderr(), chain.summary())
				}
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "the run carries no commit chain: %s\n", checkout.Reason)
			}
			payload["system_id"] = env.SystemID
			route := fmt.Sprintf("/api/compliance/releases/%d/modernization/test-runs?system_id=%s", releaseID, url.QueryEscape(fmt.Sprint(env.SystemID)))
			status, body, err := env.write("POST", route, payload)
			if err != nil {
				return err
			}
			// Epic 460 (H3): a server older than commit_details refuses the
			// unknown key with invalid_modernization_results, so the capture is
			// sent once more without it. The new server refuses bad details with
			// invalid_commit_details, which is not sent again.
			recordedWithoutDetails := false
			if _, sent := payload["commit_details"]; sent && status == http.StatusUnprocessableEntity && str(body, "error") == "invalid_modernization_results" {
				delete(payload, "commit_details")
				status, body, err = env.write("POST", route, payload)
				if err != nil {
					return err
				}
				recordedWithoutDetails = status >= 200 && status < 300
			}
			if status < 200 || status >= 300 {
				return serverRefusal("modernization record refused", status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("modernization response is missing its data object")
			}
			if _, ok := data["run"].(map[string]any); !ok {
				return fmt.Errorf("modernization response is missing its run")
			}
			if _, ok := data["results"].([]any); !ok {
				return fmt.Errorf("modernization response is missing its results")
			}
			if recordedWithoutDetails {
				fmt.Fprintln(cmd.ErrOrStderr(), "the server keeps no commit details yet; the run was recorded without them.")
			}
			return writeRecord(cmd, data)
		},
	}
	cmd.Flags().Int("release-id", 0, "the open Modernization Release of the bound System that authorizes the capture and names the walk's stop commits (required)")
	cmd.MarkFlagRequired("release-id")
	cmd.Flags().String("file", "", "modernization capture JSON file, or - for stdin (required)")
	cmd.MarkFlagRequired("file")
	cmd.Flags().String("legacy-checkout", "", "a git checkout of the legacy repository that holds the commit, read for the commit's message, author and date when the folder the command runs in does not hold it (onto a new system)")
	cmd.Flags().Bool("json", false, "emit JSON")
	root.AddCommand(cmd)

	history := &cobra.Command{
		Use: "history", Short: "Read bounded history for a legacy System or the Release's own System, with or without a Release",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			systemID, _ := cmd.Flags().GetInt64("system-id")
			if systemID < 1 {
				return fmt.Errorf("--system-id must be a positive legacy System ID")
			}
			commit, _ := cmd.Flags().GetString("commit")
			if cmd.Flags().Changed("commit") && strings.TrimSpace(commit) == "" {
				return fmt.Errorf("--commit must be nonempty when supplied")
			}
			env, err := load()
			if err != nil {
				return err
			}
			query := url.Values{"system_id": {fmt.Sprint(systemID)}}
			if commit != "" {
				query.Set("commit", commit)
			}
			status, body, err := env.get("/api/compliance/modernization/history?" + query.Encode())
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("modernization history refused", status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("modernization history response is missing its data object")
			}
			return writeModernizationHistory(cmd, data)
		},
	}
	history.Flags().Int64("system-id", 0, "the System whose history to read, the legacy System or the Release's own System; required, including when it is the bound System")
	_ = history.MarkFlagRequired("system-id")
	history.Flags().String("commit", "", "Commit to inspect; omit for passing choices and recorded-results indicator")
	history.Flags().Bool("json", false, "Emit JSON")
	root.AddCommand(history)
	return root
}

const modernizationRecordHelp = `Capture one completed test run as modernization history, with its commit
chain, under an open Modernization Release of the bound System.

The JSON file names one commit and the actual outcome of each test case:
  {"sha":"<commit>","tests":[{"test_case_id":"<UUID>","result":"pass","criterion_ids":["<UUID>"]}]}

A file holds the tests of one System: the Release's legacy System's tests in
the Release's scope or, onto a new system, the tests of the Release's own
System, each entry naming the criteria its test case verifies. A capture test
case's entry names no criterion: its criterion_ids is an empty list, its result
relates to the test case and verifies nothing, so no proof changes. An entry
without criterion_ids, or with a criterion that is not a UUID, is refused
before anything is sent. The server refuses the whole file with
not_a_capture_test_case when such an entry's test case is not a capture test
case, and with invalid_verification_endpoint when the file mixes the two
Systems' test cases or names a criterion of another System.

Run the command in the git checkout that holds the commit, and the capture
carries the commit chain: the commit objects of that commit and of its
ancestors, as git stores them, which the server checks by hashing and keeps
beside the run. The walk lists commits with git rev-list --topo-order from the
commit and stops once it has listed every stop commit of the Release (the
chain_stops of the Release read: in place its reference revision), at 2,000
commits, or before the objects would pass 8,000,000 bytes; with no stop commit
it goes on to the start of the history or a limit. The command prints how many
commits it sent and how the walk ended. A walk that ended at a limit or at the
edge of a shallow checkout leaves the chain cut, so record from a full-depth
checkout soon after the merge. In such a checkout, give the commit as its full
40- or 64-character id; it is sent in lower case. When the commit is the
checkout's HEAD, uncommitted changes to tracked files are refused: commit or
stash them and run the tests again. From a folder that is not a git checkout,
or one that does not hold the commit, the run carries no commit chain, the
command says so, and the commit may be any text without spaces.

The capture also carries the commit's details as commit_details: the first
line of its message, cut to 200 characters (code points), its author's name,
cut to 255, and its author date. They are read with git from the folder the
command runs in when it holds the commit, else from the folder
--legacy-checkout names: onto a new system, record from a folder bound to the
new system and give --legacy-checkout a checkout of the legacy repository.
With neither, the run carries no commit details and the command says so. A
--legacy-checkout that is not a git checkout is refused before anything is
sent. A server that keeps no commit details yet refuses the key with
invalid_modernization_results; the command then sends the capture once more
without it and says the run was recorded without them. Details that break the
server's limits are refused with invalid_commit_details and not sent again.

The server stores the run, its results, their traces and the chain atomically,
or refuses the whole file and stores nothing; a broken chain is refused with
invalid_commit_chain. Nothing here writes current criterion proof, and there is
no run registration or finalization step.`

func validateModernizationCapture(payload map[string]any) error {
	for key := range payload {
		if key != "sha" && key != "tests" {
			return fmt.Errorf("unsupported modernization capture field %q", key)
		}
	}
	if sha, ok := payload["sha"].(string); !ok || sha == "" {
		return fmt.Errorf("input must include a nonempty sha")
	}
	tests, ok := payload["tests"].([]any)
	if !ok || len(tests) == 0 {
		return fmt.Errorf("input must include a nonempty tests array")
	}
	if len(tests) > 500 {
		return fmt.Errorf("tests may contain at most 500 entries")
	}
	testCases := map[string]bool{}
	criteriaSeen := map[string]bool{}
	for i, raw := range tests {
		test, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("tests[%d] must be an object", i)
		}
		for key := range test {
			if key != "test_case_id" && key != "result" && key != "criterion_ids" {
				return fmt.Errorf("tests[%d] has unsupported field %q", i, key)
			}
		}
		id, ok := test["test_case_id"].(string)
		if !ok || !requirementUUIDPattern.MatchString(id) {
			return fmt.Errorf("tests[%d] must include test_case_id", i)
		}
		if testCases[id] {
			return fmt.Errorf("tests[%d] duplicates test_case_id", i)
		}
		testCases[id] = true
		if result, ok := test["result"].(string); !ok || !map[string]bool{"pass": true, "fail": true, "error": true, "skip": true, "inconclusive": true}[result] {
			return fmt.Errorf("tests[%d].result must be pass, fail, error, skip or inconclusive", i)
		}
		// Epic 390: an empty list is a capture test case's entry, which names
		// no criterion; the key itself is still required.
		criteria, ok := test["criterion_ids"].([]any)
		if !ok {
			return fmt.Errorf("tests[%d] must include criterion_ids, a list of criterion UUIDs (an empty list for a capture test case)", i)
		}
		pairCriteria := map[string]bool{}
		for j, criterion := range criteria {
			criterionID, ok := criterion.(string)
			if !ok || !requirementUUIDPattern.MatchString(criterionID) {
				return fmt.Errorf("tests[%d].criterion_ids[%d] must be a nonempty UUID", i, j)
			}
			if pairCriteria[criterionID] {
				return fmt.Errorf("tests[%d].criterion_ids duplicates %s", i, criterionID)
			}
			pairCriteria[criterionID] = true
			criteriaSeen[criterionID] = true
		}
	}
	if len(criteriaSeen) > 50 {
		return fmt.Errorf("capture may cover at most 50 distinct criteria")
	}
	return nil
}

// The longest commit details the server keeps, in code points (Epic 460, H3).
const (
	modernizationSubjectMax = 200
	modernizationAuthorMax  = 255
)

// modernizationIsCheckout says whether dir is inside a git checkout.
func modernizationIsCheckout(dir string) bool {
	out, err := chainGit(dir, nil, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// modernizationCommitDetails reads, in dir, the commit_details of commit: the
// first line of its message cut to 200 code points, its author name cut to 255
// and its author date in strict ISO 8601, as git's %aI gives it. It gives nil
// when dir does not hold the commit (`git cat-file -e <commit>^{commit}`).
func modernizationCommitDetails(dir, commit string) (map[string]any, error) {
	if strings.HasPrefix(commit, "-") {
		// git would read it as an option.
		return nil, nil
	}
	if _, err := chainGit(dir, nil, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return nil, nil
	}
	out, err := chainGit(dir, nil, "log", "-1", "--no-show-signature", "--format=%an%x00%aI%x00%B", commit+"^{commit}", "--")
	if err != nil {
		return nil, fmt.Errorf("reading the message, author and date of commit %s: %w", commit, err)
	}
	fields := strings.SplitN(string(out), "\x00", 3)
	if len(fields) != 3 {
		return nil, fmt.Errorf("reading the message, author and date of commit %s: git log gave %q", commit, out)
	}
	// The first line of the whole message, not git's %s, which joins the
	// lines of the first paragraph.
	subject, _, _ := strings.Cut(fields[2], "\n")
	return map[string]any{
		"subject":      modernizationCut(strings.TrimSuffix(subject, "\r"), modernizationSubjectMax),
		"author":       modernizationCut(fields[0], modernizationAuthorMax),
		"committed_at": strings.TrimSpace(fields[1]),
	}, nil
}

// modernizationCut keeps the first max code points of text.
func modernizationCut(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max])
}

// writeModernizationHistory prints the history read. In text, each passing
// revision is a line with how many of its recorded results passed and its
// commit's message, author and date, as the server gives them (Epic 460, H3);
// every other field prints as writeRecord prints it. --json prints the read.
func writeModernizationHistory(cmd *cobra.Command, data map[string]any) error {
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeRecord(cmd, data)
	}
	fields := make([]string, 0, len(data))
	for field := range data {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	out := cmd.OutOrStdout()
	for _, field := range fields {
		revisions, ok := data[field].([]any)
		if field != "passing_revisions" || !ok || len(revisions) == 0 {
			if err := writeRecord(cmd, map[string]any{field: data[field]}); err != nil {
				return err
			}
			continue
		}
		fmt.Fprintln(out, "passing_revisions:")
		for _, r := range revisions {
			revision, _ := r.(map[string]any)
			fmt.Fprintln(out, "  "+modernizationRevisionLine(revision))
		}
	}
	return nil
}

// modernizationRevisionLine is one passing revision: its commit, then
// "<passed> of <recorded> passed", the commit's message and author, or that
// they are not recorded, and its author date, else the day its results were
// recorded.
func modernizationRevisionLine(revision map[string]any) string {
	var parts []string
	if recorded, ok := roadmapNumber(revision["recorded"]); ok {
		passed, _ := roadmapNumber(revision["passed"])
		parts = append(parts, fmt.Sprintf("%d of %d passed", int(passed), int(recorded)))
	}
	subject, author := str(revision, "subject"), str(revision, "author")
	if subject == "" && author == "" {
		parts = append(parts, "message and author not recorded")
	}
	for _, text := range []string{subject, author} {
		if text != "" {
			parts = append(parts, text)
		}
	}
	if committed := str(revision, "committed_at"); committed != "" {
		parts = append(parts, modernizationDay(committed))
	} else if recordedAt := str(revision, "recorded_at"); recordedAt != "" {
		parts = append(parts, "results recorded "+modernizationDay(recordedAt))
	}
	line := orDash(str(revision, "revision"))
	if len(parts) > 0 {
		line += "  " + strings.Join(parts, " · ")
	}
	return line
}

// modernizationDay is the day of a served timestamp in its own offset, so an
// author date reads as the author's day, or the value as served.
func modernizationDay(value string) string {
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at.Format("2006-01-02")
	}
	return value
}
