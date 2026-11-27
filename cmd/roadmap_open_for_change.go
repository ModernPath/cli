package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// SR-5102: modernpath roadmap open-for-change — Open for change of a locked
// planning context (Epic 329, A6) from the command line, on the routes the
// Initiative's page and the Release page call. The command checks only that
// it names one Initiative or one Release and a reason; every other rule is
// the server's.

func newRoadmapOpenForChangeCommand(load func() (*cliEnv, error)) *cobra.Command {
	open := &cobra.Command{Use: "open-for-change", Short: "Open a locked planning context of an Initiative or a Release for change, with a reason", Args: cobra.NoArgs,
		Long: `Open the locked planning context of the Initiative --initiative <id> or the
Release --release <id> of the bound System for change, with --reason <text>, as
Open for change on the Initiative's page or the Release page does. An Epic's
approval or an agreement of the Roadmap locks a planning context; while it is
locked, a changed value is refused with planning_context_locked, also by
'roadmap apply'. Any member of the workspace may open it. The opening is
recorded with the reason and the signed-in person; the business owner then
agrees the Roadmap again, which locks it again.

Give exactly one of --initiative and --release, and a reason that is not
blank; otherwise the command sends nothing. It prints that the planning
context is open, who opened it and why, the Initiatives whose agreement
reopens and the approved Epics that read the planning context, as the server
returns them. --json writes the server's answer.

A refusal prints the server's code and the command exits non-zero:
planning_context_not_locked (it is open already), release_closed (the Release
is released or archived), reason_required, and not_found when the bound System,
the Initiative or the Release is not found.
Example: modernpath roadmap open-for-change --initiative <uuid> --reason "The provider changed its API"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			initiative, _ := cmd.Flags().GetString("initiative")
			release, _ := cmd.Flags().GetString("release")
			reason, _ := cmd.Flags().GetString("reason")
			initiative, release = strings.TrimSpace(initiative), strings.TrimSpace(release)
			if (initiative == "") == (release == "") {
				return fmt.Errorf("roadmap open-for-change needs exactly one of --initiative <id> and --release <id>")
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("roadmap open-for-change needs --reason <text>: why the planning context opens for change")
			}
			var path, what string
			if initiative != "" {
				if !requirementUUIDPattern.MatchString(initiative) {
					return fmt.Errorf("--initiative %q is not an Initiative id (a UUID)", initiative)
				}
				path = "/api/roadmap/lines/" + url.PathEscape(initiative) + "/planning-context/open-for-change"
				what = "Initiative " + initiative
			} else {
				id, err := strconv.Atoi(release)
				if err != nil || id <= 0 {
					return fmt.Errorf("--release %q is not a Release id (a positive number)", release)
				}
				path = "/api/compliance/releases/" + strconv.Itoa(id) + "/planning-context/open-for-change"
				what = "Release " + strconv.Itoa(id)
			}

			env, err := roadmapEnv(load)
			if err != nil {
				return err
			}
			status, body, err := env.write(http.MethodPost, path, map[string]any{"system_id": env.SystemID, "reason": reason})
			data, err := roadmapData(status, body, err)
			if err != nil {
				return err
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, data)
			}
			writeRoadmapOpened(cmd, what, data)
			return nil
		}}
	open.Flags().String("initiative", "", "The Initiative whose planning context opens (its id)")
	open.Flags().String("release", "", "The Release whose planning context opens (its id)")
	open.Flags().String("reason", "", "Why the planning context opens for change (required)")
	open.Flags().Bool("json", false, "Emit JSON")
	return open
}

func writeRoadmapOpened(cmd *cobra.Command, what string, data map[string]any) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "The planning context of %s is open for change.\n", what)
	if opened, ok := data["opened"].(map[string]any); ok {
		by := "someone"
		if person, ok := opened["by"].(map[string]any); ok && str(person, "name") != "" {
			by = str(person, "name")
		}
		fmt.Fprintf(out, "Opened by %s at %s: %s\n", by, str(opened, "at"), str(opened, "reason"))
	}
	fmt.Fprintln(out, "Initiatives whose agreement reopens: "+roadmapOpenedList(data["reopens"], func(m map[string]any) string {
		return fmt.Sprintf("%s (%s)", str(m, "title"), str(m, "id"))
	}))
	fmt.Fprintln(out, "Approved Epics that read it: "+roadmapOpenedList(data["epics"], func(m map[string]any) string {
		return fmt.Sprintf("#%v %s", m["id"], str(m, "title"))
	}))
}

func roadmapOpenedList(value any, name func(map[string]any) string) string {
	items, _ := value.([]any)
	names := make([]string, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			names = append(names, name(m))
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
