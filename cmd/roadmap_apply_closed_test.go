package cmd

// Epic 462 (H5a), TestCase TC-H5-S5, SR ff34255d criterion a2bacea5: a
// set_selection refused with release_closed, because the Initiative's
// Modernization Release is released or archived, is printed by
// `roadmap apply --dry-run` as its code followed by its sentence, and the
// command exits non-zero. Before the build roadmapPlanReasons had no entry for
// release_closed, so the line ended at the code and the assertion fails.

import (
	"net/http"
	"strings"
	"testing"
)

const h5ClosedPlanFile = `{"request_id":"h5-closed","operations":[
 {"op":"set_selection","line_id":"66666666-6666-4666-8666-666666666666","rows":[
  {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","outcome":"keep"}]}]}`

const h5ClosedDryRunReply = `{"data":{"operations":[
  {"index":1,"op":"set_selection","summary":"Set the selection of initiative \"Provider sync\": 1 keep, 0 change, 0 drop, 0 cleared, 0 moved","release":{"id":21,"ref":null,"name":"Cutover"}}],
 "errors":[{"index":1,"op":"set_selection","reason":"release_closed","rows":[]}],"dry_run":true}}`

// 22959baa-8cde-4123-9292-11aaaf38c780
func TestH5_RoadmapApplyDryRunPrintsReleaseClosedWithItsSentence(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, h5ClosedDryRunReply)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "plan.json", h5ClosedPlanFile)

	out, err := runRoot(t, "roadmap", "apply", "--file", plan, "--dry-run")
	text := b3Text(out, err)

	// SR ff34255d criterion a2bacea5: the exact line.
	want := "error: operation 1 (set_selection): release_closed — The initiative's release is closed, so this initiative can no longer change its decisions."
	found := false
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			found = true
		}
	}
	if !found {
		t.Errorf("roadmap apply --dry-run does not print\n  %s\ngot:\n%s", want, text)
	}
	if err == nil {
		t.Errorf("roadmap apply --dry-run of a refused plan exited zero:\n%s", out)
	}
	if got := len(*seen); got != 1 {
		t.Errorf("roadmap apply --dry-run sent %d requests, want 1", got)
	}
}
