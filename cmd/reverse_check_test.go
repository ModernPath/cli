package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests reach the check only through the command tree, so before the
// command exists they fail on its output instead of failing to build.

const (
	chkUR    = "a1000000-0000-4000-8000-000000000001"
	chkSR    = "a1000000-0000-4000-8000-000000000002"
	chkRef   = "a1000000-0000-4000-8000-000000000003"
	chkCrit1 = "a1000000-0000-4000-8000-000000000011"
	chkCrit2 = "a1000000-0000-4000-8000-000000000012"
)

func chkGoodUR() map[string]any {
	return map[string]any{
		"id": chkUR, "kind": "user_requirement", "title": "Add a payment card",
		"description":  "A signed-in customer saves a payment card and can pay with it at the pump.",
		"actor":        "Signed-in mobile app customer",
		"intended_use": "Pay at the pump with a saved card",
		"criteria": []any{map[string]any{"id": chkCrit1, "kind": "scenario",
			"given": "a signed-in customer with no saved card", "when": "they add a valid Visa card",
			"then": "the card is listed as the default payment method"}},
	}
}

func chkGoodSR() map[string]any {
	return map[string]any{
		"id": chkSR, "kind": "system_requirement", "title": "Card vault tokenizes cards",
		"description": "The cards API exchanges the card number for a provider token and stores only the token.",
		"boundary":    "cards API",
		"criteria": []any{map[string]any{"id": chkCrit2, "kind": "criterion",
			"statement": "POST /cards returns 201 with a token and never stores the card number"}},
	}
}

func chkGoodTraces() []any {
	return []any{
		map[string]any{"source_type": "user_requirement", "source_id": chkUR, "target_type": "system_requirement", "target_id": chkSR, "link_kind": "derives"},
		map[string]any{"source_type": "user_requirement", "source_id": chkUR, "target_type": "system_requirement", "target_id": chkRef, "link_kind": "derives"},
		map[string]any{"source_type": "system_requirement", "source_id": chkSR, "target_type": "source_file", "repository_key": "app", "path": "svc-a/cards.go", "link_kind": "implements"},
		map[string]any{"source_type": "source_file", "repository_key": "app", "path": "svc-a/cards_test.go", "target_type": "system_requirement", "target_id": chkSR, "link_kind": "verifies"},
	}
}

func chkGoodSet() map[string]any {
	return map[string]any{
		"requirements": []any{chkGoodUR(), chkGoodSR(), map[string]any{"kind": "system_requirement", "id": chkRef}},
		"traces":       chkGoodTraces(),
	}
}

func chkWrite(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var body []byte
	switch v := value.(type) {
	case string:
		body = []byte(v)
	default:
		var err error
		if body, err = json.MarshalIndent(v, "", "  "); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// chkRun runs reverse-engineer with a load function that must not be called:
// check is offline.
func chkRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	command := newReverseEngineerCommand(func() (*cliEnv, error) {
		t.Fatal("check signed in or read the binding; it must run offline")
		return nil, nil
	})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

func TestReverseCheckPassesAGoodSetOffline(t *testing.T) {
	dir := t.TempDir()
	chkWrite(t, filepath.Join(dir, "01-cards.json"), chkGoodSet())
	// A refused file fixed in place: its refusal note holds the old content.
	fixed := chkGoodSet()
	fixed["requirements"] = []any{map[string]any{"kind": "system_requirement", "id": chkRef}}
	fixed["traces"] = []any{}
	chkWrite(t, filepath.Join(dir, "02-fixed.json"), fixed)
	chkWrite(t, filepath.Join(dir, "recorded", "02-fixed.json.refused"), map[string]any{"message": "invalid_requirement", "content": map[string]any{"requirements": []any{}, "traces": []any{}}})
	// One requirement with four equal criteria is not a template.
	same := chkGoodSet()
	sr := chkGoodSR()
	sr["id"], sr["title"] = "a1000000-0000-4000-8000-000000000004", "Card vault deletes cards"
	sr["description"] = "The cards API deletes the provider token when the customer removes a card."
	var crit []any
	for i := 0; i < 4; i++ {
		crit = append(crit, map[string]any{"id": fmt.Sprintf("a1000000-0000-4000-8000-00000000002%d", i), "kind": "criterion", "statement": "the token is deleted at the provider"})
	}
	sr["criteria"] = crit
	same["requirements"], same["traces"] = []any{sr}, []any{}
	chkWrite(t, filepath.Join(dir, "03-delete.json"), same)

	out, err := chkRun(t, "check", "--dir", dir)
	if err != nil {
		t.Fatalf("a good set failed check: %v\n%s", err, out)
	}
	for _, name := range []string{"ids", "receipts", "changed-after-record", "distinct-statements", "template-scenarios", "actor-outcome", "field-limits"} {
		if !strings.Contains(out, "PASS  "+name) {
			t.Errorf("check did not report PASS for %s:\n%s", name, out)
		}
	}
}

// chkBad writes the good set and one extra file, runs check, and returns its output.
func chkBad(t *testing.T, extra map[string]any, mutate func(dir string)) (string, error) {
	t.Helper()
	dir := t.TempDir()
	chkWrite(t, filepath.Join(dir, "01-cards.json"), chkGoodSet())
	if extra != nil {
		chkWrite(t, filepath.Join(dir, "02-extra.json"), extra)
	}
	if mutate != nil {
		mutate(dir)
	}
	return chkRun(t, "check", "--dir", dir)
}

func chkSet(requirements ...map[string]any) map[string]any {
	list := make([]any, 0, len(requirements))
	for _, r := range requirements {
		list = append(list, r)
	}
	return map[string]any{"requirements": list, "traces": []any{}}
}

// chkCritID derives a distinct criterion UUID from a requirement UUID.
func chkCritID(id string) string { return id[:len(id)-4] + "c" + id[len(id)-3:] }

func chkURWith(id, title, actor, intended string) map[string]any {
	ur := chkGoodUR()
	ur["id"], ur["title"], ur["actor"], ur["intended_use"] = id, title, actor, intended
	ur["description"] = "A customer " + strings.ToLower(title) + " from the app."
	ur["criteria"] = []any{map[string]any{"id": chkCritID(id), "kind": "scenario", "given": "a signed-in customer", "when": "they " + strings.ToLower(title), "then": "the app confirms " + strings.ToLower(title)}}
	return ur
}

func TestReverseCheckFailsEachContentCheck(t *testing.T) {
	noID := chkGoodSR()
	delete(noID, "id")
	noID["title"], noID["description"] = "Card vault lists cards", "The cards API returns the saved cards of the signed-in customer."
	conflict := chkGoodSR()
	conflict["title"] = "Card vault tokenizes cards differently"
	notUUID := chkGoodSR()
	notUUID["id"], notUUID["title"], notUUID["description"] = "SR-BEN-046", "Card vault deletes cards", "The cards API deletes a saved card on request."
	notUUID["criteria"] = []any{map[string]any{"id": chkCritID("a1000000-0000-4000-8000-000000000046"), "kind": "criterion", "statement": "a deleted card is gone from the list"}}
	critNotUUID := chkGoodSR()
	critNotUUID["id"], critNotUUID["title"], critNotUUID["description"] = "a1000000-0000-4000-8000-000000000047", "Card vault renames cards", "The cards API renames a saved card on request."
	critNotUUID["criteria"] = []any{map[string]any{"id": "not-a-uuid", "kind": "criterion", "statement": "a renamed card shows its new name in the list"}}
	exact := chkGoodSR()
	exact["id"], exact["title"] = "a1000000-0000-4000-8000-000000000005", "Card vault keeps tokens"
	titled := func(id, title, desc string) map[string]any {
		sr := chkGoodSR()
		sr["id"], sr["title"], sr["description"] = id, title, desc
		sr["criteria"] = []any{map[string]any{"id": chkCritID(id), "kind": "criterion", "statement": desc + " within 2 seconds"}}
		return sr
	}
	template := func(n int, noun string) map[string]any {
		id := fmt.Sprintf("a1000000-0000-4000-8000-0000000003%02d", n)
		return map[string]any{"id": id, "kind": "system_requirement", "title": "List " + noun, "description": "The API lists the " + noun + " of account " + noun + "-" + fmt.Sprint(n) + ".",
			"criteria": []any{map[string]any{"id": chkCritID(id), "kind": "criterion", "statement": "the " + noun + " list is returned in pages of 50"}}}
	}
	long := chkURWith("a1000000-0000-4000-8000-000000000041", "Remove a card", strings.Repeat("a", 256), "Stop paying with a card")
	cases := []struct {
		name, check, want string
		extra             map[string]any
		mutate            func(dir string)
		args              []string
	}{
		{name: "missing id", check: "ids", want: "Card vault lists cards", extra: chkSet(noID)},
		{name: "id that is not a UUID", check: "ids", want: "SR-BEN-046 is not a UUID", extra: chkSet(notUUID)},
		{name: "criterion id that is not a UUID", check: "ids", want: "criterion not-a-uuid of a1000000-0000-4000-8000-000000000047 \"Card vault renames cards\" is not a UUID", extra: chkSet(critNotUUID)},
		{name: "one UUID, two contents", check: "ids", want: chkSR, extra: chkSet(conflict)},
		{name: "changed after record", check: "changed-after-record", want: "01-cards.json", mutate: func(dir string) {
			old := chkGoodSet()
			old["requirements"] = []any{chkGoodUR()}
			chkWrite(t, filepath.Join(dir, "recorded", "01-cards.json"), old)
			chkWrite(t, filepath.Join(dir, "recorded", "01-cards.json.receipt"), `{"requirements":[],"trace_count":0}`)
		}},
		{name: "empty receipt", check: "receipts", want: "01-cards.json", mutate: func(dir string) {
			chkWrite(t, filepath.Join(dir, "recorded", "01-cards.json"), chkGoodSet())
			chkWrite(t, filepath.Join(dir, "recorded", "01-cards.json.receipt"), "")
		}},
		{name: "receipt without copy", check: "receipts", want: "01-cards.json", mutate: func(dir string) {
			chkWrite(t, filepath.Join(dir, "recorded", "01-cards.json.receipt"), `{"requirements":[],"trace_count":0}`)
		}},
		{name: "same statement", check: "distinct-statements", want: "a1000000-0000-4000-8000-000000000005", extra: chkSet(exact)},
		{name: "same statement without title words", check: "distinct-statements", want: "a1000000-0000-4000-8000-000000000032", extra: chkSet(
			titled("a1000000-0000-4000-8000-000000000031", "Export invoices", "Export invoices as CSV for the month"),
			titled("a1000000-0000-4000-8000-000000000032", "Export receipts", "Export receipts as CSV for the month"))},
		{name: "one criterion frame across four requirements", check: "template-scenarios", want: "a1000000-0000-4000-8000-000000000304", extra: chkSet(
			template(1, "invoices"), template(2, "receipts"), template(3, "orders"), template(4, "refunds"))},
		{name: "no actor", check: "actor-outcome", want: "a1000000-0000-4000-8000-000000000042", extra: chkSet(chkURWith("a1000000-0000-4000-8000-000000000042", "Rename a card", "", "Find the card by name"))},
		{name: "no intended use", check: "actor-outcome", want: "a1000000-0000-4000-8000-000000000043", extra: chkSet(chkURWith("a1000000-0000-4000-8000-000000000043", "Hide a card", "Signed-in mobile app customer", ""))},
		{name: "catch-all actor", check: "actor-outcome", want: "a1000000-0000-4000-8000-000000000044", extra: chkSet(chkURWith("a1000000-0000-4000-8000-000000000044", "Sort cards", "the user", "Find a card"))},
		{name: "roles joined by or", check: "actor-outcome", want: "a1000000-0000-4000-8000-000000000045", extra: chkSet(chkURWith("a1000000-0000-4000-8000-000000000045", "Lock a card", "Customer or cashier", "Stop misuse"))},
		{name: "roles joined by a slash", check: "actor-outcome", want: "a1000000-0000-4000-8000-000000000046", extra: chkSet(chkURWith("a1000000-0000-4000-8000-000000000046", "Unlock a card", "Customer/cashier", "Use the card again"))},
		{name: "field over 255 characters", check: "field-limits", want: "a1000000-0000-4000-8000-000000000041", extra: chkSet(long)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := chkBad(t, tc.extra, tc.mutate)
			if err == nil {
				t.Fatalf("check passed a set with %s:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "FAIL  "+tc.check) || !strings.Contains(out, tc.want) {
				t.Fatalf("check did not fail %s naming %s:\n%s", tc.check, tc.want, out)
			}
		})
	}

	t.Run("one UUID, two contents across two folders", func(t *testing.T) {
		a, b := t.TempDir(), t.TempDir()
		chkWrite(t, filepath.Join(a, "01-cards.json"), chkGoodSet())
		chkWrite(t, filepath.Join(b, "01-other.json"), chkSet(conflict))
		out, err := chkRun(t, "check", "--dir", a, "--dir", b)
		if err == nil || !strings.Contains(out, "FAIL  ids") || !strings.Contains(out, chkSR) {
			t.Fatalf("check did not fail a UUID conflict across folders: %v\n%s", err, out)
		}
	})
}

func TestReverseCheckWarningsDoNotFail(t *testing.T) {
	lonely := chkURWith("a1000000-0000-4000-8000-000000000051", "Share a card", "Signed-in mobile app customer", "Let family pay")
	set := chkSet(lonely)
	set["traces"] = []any{
		map[string]any{"source_type": "user_requirement", "source_id": "a1000000-0000-4000-8000-000000000051", "target_type": "system_requirement", "target_id": "a1000000-0000-4000-8000-000000000099", "link_kind": "relates"},
		map[string]any{"source_type": "source_file", "repository_key": "app", "path": "svc-a/share.go", "target_type": "user_requirement", "target_id": "a1000000-0000-4000-8000-000000000051", "link_kind": "relates"},
		map[string]any{"source_type": "source_file", "repository_key": "app", "path": "svc-a/share_test.go", "target_type": "criterion", "target_id": chkCritID("a1000000-0000-4000-8000-000000000051"), "link_kind": "verifies"},
	}
	dir := t.TempDir()
	chkWrite(t, filepath.Join(dir, "01-share.json"), set)

	out, err := chkRun(t, "check", "--dir", dir)
	if err != nil {
		t.Fatalf("warnings failed the command: %v\n%s", err, out)
	}
	if !strings.Contains(out, "WARN  ur-without-sr") || !strings.Contains(out, "a1000000-0000-4000-8000-000000000051") {
		t.Fatalf("no ur-without-sr warning:\n%s", out)
	}
	if !strings.Contains(out, "WARN  trace-endpoints") || !strings.Contains(out, "a1000000-0000-4000-8000-000000000099") {
		t.Fatalf("no trace-endpoints warning for an unknown UUID:\n%s", out)
	}
	if strings.Contains(out, "svc-a/share.go") {
		t.Fatalf("trace-endpoints reported a file endpoint:\n%s", out)
	}
	if strings.Contains(out, chkCritID("a1000000-0000-4000-8000-000000000051")) {
		t.Fatalf("trace-endpoints reported a criterion the set holds:\n%s", out)
	}

	out, err = chkRun(t, "check", "--dir", dir, "--served-by-store", "a1000000-0000-4000-8000-000000000051", "--served-by-store", "a1000000-0000-4000-8000-000000000099")
	if err != nil || !strings.Contains(out, "PASS  ur-without-sr") || !strings.Contains(out, "PASS  trace-endpoints") {
		t.Fatalf("--served-by-store <uuid> did not silence the warnings: %v\n%s", err, out)
	}
	list := filepath.Join(t.TempDir(), "served.txt")
	chkWrite(t, list, "a1000000-0000-4000-8000-000000000051\na1000000-0000-4000-8000-000000000099\n")
	out, err = chkRun(t, "check", "--dir", dir, "--served-by-store", "@"+list)
	if err != nil || !strings.Contains(out, "PASS  ur-without-sr") || !strings.Contains(out, "PASS  trace-endpoints") {
		t.Fatalf("--served-by-store @file did not silence the warnings: %v\n%s", err, out)
	}
}

func TestReverseCheckJSONReportsTheSameChecks(t *testing.T) {
	dir := t.TempDir()
	chkWrite(t, filepath.Join(dir, "01-cards.json"), chkGoodSet())
	out, err := chkRun(t, "check", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("check --json failed: %v\n%s", err, out)
	}
	var report struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("--json is not one JSON object: %v\n%s", err, out)
	}
	if len(report.Checks) < 9 {
		t.Fatalf("--json lists %d checks, want every check:\n%s", len(report.Checks), out)
	}
	for _, c := range report.Checks {
		if c.Status != "PASS" {
			t.Errorf("%s = %s on a good set", c.Name, c.Status)
		}
	}
}

func TestReverseCheckSelfTestFindsEverySeededFailure(t *testing.T) {
	out, err := chkRun(t, "check", "--self-test")
	if err != nil {
		t.Fatalf("self-test failed: %v\n%s", err, out)
	}
	for _, name := range []string{"ids", "receipts", "changed-after-record", "distinct-statements", "template-scenarios", "actor-outcome", "field-limits", "ur-without-sr", "trace-endpoints"} {
		if !strings.Contains(out, "DETECTED  "+name) {
			t.Errorf("self-test did not detect %s:\n%s", name, out)
		}
	}
	if strings.Contains(out, "MISSED") {
		t.Errorf("self-test missed a seeded failure:\n%s", out)
	}
	for _, finding := range []string{"04-empty.json has an empty or unparsable receipt", "05-nocopy.json has a receipt without its copy", "03-recorded.json changed after it was recorded"} {
		if !strings.Contains(out, finding) {
			t.Errorf("self-test did not report the seeded finding %q:\n%s", finding, out)
		}
	}
	if _, err := chkRun(t, "check", "--self-test", "--dir", t.TempDir()); err == nil {
		t.Error("--self-test with --dir was accepted")
	}
}

func chkGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestReverseCheckCoverage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo := t.TempDir()
	files := []string{"svc-a/cards.go", "svc-a/cards_test.go", "svc-a/vendor/lib.go", "svc-a/kört.go", "svc-b/other.go"}
	for i := 0; i < 55; i++ {
		files = append(files, fmt.Sprintf("svc-a/zz/f%02d.go", i))
	}
	for _, f := range files {
		chkWrite(t, filepath.Join(repo, f), "package x\n")
	}
	chkGit(t, repo, "init", "-q")
	chkGit(t, repo, "add", ".")
	chkGit(t, repo, "commit", "-q", "-m", "fixture")

	staged := t.TempDir()
	set := chkGoodSet()
	lonely := chkURWith("a1000000-0000-4000-8000-000000000061", "Share a card", "Signed-in mobile app customer", "Let family pay")
	untested := chkGoodSR()
	untested["id"], untested["title"] = "a1000000-0000-4000-8000-000000000062", "Card vault expires tokens"
	untested["description"] = "The cards API deletes provider tokens that were not used for a year."
	byCriterion := chkGoodSR()
	byCriterion["id"], byCriterion["title"] = "a1000000-0000-4000-8000-000000000063", "Card vault rotates tokens"
	byCriterion["description"] = "The cards API replaces a provider token before it expires."
	byCriterion["criteria"] = []any{map[string]any{"id": chkCritID("a1000000-0000-4000-8000-000000000063"), "kind": "criterion", "statement": "a token is replaced 7 days before it expires"}}
	set["requirements"] = append(set["requirements"].([]any), lonely, untested, byCriterion)
	set["traces"] = append(set["traces"].([]any), map[string]any{"source_type": "source_file", "repository_key": "app", "path": "svc-a/cards_test.go", "target_type": "criterion", "target_id": chkCritID("a1000000-0000-4000-8000-000000000063"), "link_kind": "verifies"})
	chkWrite(t, filepath.Join(staged, "01-cards.json"), set)

	args := []string{"check", "--dir", staged, "--repo", "app=" + filepath.Join(repo, "svc-a"), "--exclude", "svc-a/vendor/**",
		"--repo", "nobody=" + filepath.Join(repo, "svc-b"), "--repo", "loose=" + t.TempDir()}
	out, err := chkRun(t, args...)
	if err != nil {
		t.Fatalf("coverage changed the exit code of a passing set: %v\n%s", err, out)
	}
	for _, want := range []string{
		"app: 59 files, 2 linked, 1 excluded, 56 unresolved",
		"svc-a/kört.go",
		"and 6 more",
		"a1000000-0000-4000-8000-000000000062", // SR with no verifies trace
		"a1000000-0000-4000-8000-000000000061", // UR with no derives SR
		"nobody: 1 files, 0 linked, 0 excluded, 1 unresolved (no trace in the set uses this key)",
		"loose: not inside a git checkout",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("coverage output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a1000000-0000-4000-8000-000000000063") {
		t.Errorf("coverage listed a system requirement whose criterion a test verifies:\n%s", out)
	}
	if app, _, _ := strings.Cut(out, "nobody:"); strings.Contains(app, "svc-b/other.go") {
		t.Errorf("coverage counted a file outside the scoped directory:\n%s", out)
	}

	out, err = chkRun(t, append(args, "--json", "--served-by-store", "a1000000-0000-4000-8000-000000000061")...)
	if err != nil {
		t.Fatalf("coverage --json failed: %v\n%s", err, out)
	}
	var report struct {
		Coverage []struct {
			Key        string   `json:"key"`
			Unresolved []string `json:"unresolved"`
		} `json:"coverage"`
		URsWithoutSR []string `json:"user_requirements_without_system_requirement"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("--json is not one JSON object: %v\n%s", err, out)
	}
	if len(report.Coverage) == 0 || report.Coverage[0].Key != "app" || len(report.Coverage[0].Unresolved) != 56 {
		t.Fatalf("--json does not carry every unresolved path: %+v", report.Coverage)
	}
	for _, id := range report.URsWithoutSR {
		if id == "a1000000-0000-4000-8000-000000000061" {
			t.Fatal("coverage ignored --served-by-store for user requirements without a system requirement")
		}
	}
}

func TestReverseCheckGlobDoubleStarMatchesAnyDepth(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"**/go.sum", "go.sum", true},
		{"**/go.sum", "svc/go.sum", true},
		{"**/go.sum", "a/b/go.sum", true},
		{"**/go.sum", "go.summary", false},
		{"svc/**", "svc/a/b.go", true},
		{"**/testdata/**", "testdata/x.json", true},
		{"**/testdata/**", "pkg/testdata/x.json", true},
		{"*.lock", "a/b.lock", false},
	}
	for _, tc := range cases {
		re, err := reCheckGlob(tc.glob)
		if err != nil {
			t.Fatalf("%s: %v", tc.glob, err)
		}
		if got := re.MatchString(tc.path); got != tc.want {
			t.Errorf("%s on %s = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}

// g3Plain folds whitespace and drops Markdown code and bold marks, so a phrase
// may wrap or carry marks; it keeps letter case (Epic 341, G3 contract §6).
func g3Plain(text string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("**", "", "`", "").Replace(text)), " ")
}

// g3CheckReport is the part of check --json the G3 tests read.
type g3CheckReport struct {
	Checks []struct {
		Name     string `json:"name"`
		Status   string `json:"status"`
		Findings []struct {
			Detail string `json:"detail"`
		} `json:"findings"`
	} `json:"checks"`
}

func g3CheckJSON(t *testing.T, out string) g3CheckReport {
	t.Helper()
	var report g3CheckReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Errorf("check --json is not one JSON object: %v\n%s", err, out)
	}
	return report
}

// g3CheckSR is a system requirement content entry with one criterion.
func g3CheckSR(id, title, description, statement string) map[string]any {
	return map[string]any{
		"id": id, "kind": "system_requirement", "title": title, "description": description, "context": "CARDS",
		"criteria": []any{map[string]any{"id": chkCritID(id), "kind": "criterion", "statement": statement}},
	}
}

const (
	g3CheckNullID   = "a1000000-0000-4000-8000-000000000071"
	g3CheckStringID = "a1000000-0000-4000-8000-000000000072"
	g3CheckEmptyID  = "a1000000-0000-4000-8000-000000000073"
)

// Epic 341 (G3), test case 2547db91 (TC-G3-20), criterion 8dc926cd: check
// fails a staged set whose entries, a reference entry among them, carry a
// what_it_has that is null, not a list or holds an element other than
// interface, stored_data and screen, with the check what-it-has naming each
// file and requirement id. On the first slice no check reads what_it_has, so
// the set passes every check (the other checks passing it is a guard).
func TestG3_2547db91_CheckFailsAWhatItHasTheServerRefuses(t *testing.T) {
	dir := t.TempDir()
	chkWrite(t, filepath.Join(dir, "01-cards.json"), chkGoodSet())
	withNull := g3CheckSR(g3CheckNullID, "Card vault lists cards", "The cards API returns the saved cards of the signed-in customer.", "the saved cards are listed newest first")
	withNull["what_it_has"] = nil
	chkWrite(t, filepath.Join(dir, "02-null.json"), chkSet(withNull))
	withString := g3CheckSR(g3CheckStringID, "Card vault renames cards", "The cards API renames a saved card on request.", "a renamed card shows its new name in the list")
	withString["what_it_has"] = "screen"
	chkWrite(t, filepath.Join(dir, "03-string.json"), chkSet(withString))
	// A reference entry under the id 01-cards.json holds with its content.
	chkWrite(t, filepath.Join(dir, "04-api.json"), chkSet(map[string]any{"kind": "system_requirement", "id": chkSR, "what_it_has": []any{"api"}}))
	bad := []struct{ file, id string }{{"02-null.json", g3CheckNullID}, {"03-string.json", g3CheckStringID}, {"04-api.json", chkSR}}

	out, err := chkRun(t, "check", "--dir", dir, "--json")
	report := g3CheckJSON(t, out)
	if err == nil {
		t.Errorf("check passed a set whose what_it_has values are null, \"screen\" and [\"api\"]")
	}
	found := false
	for _, c := range report.Checks {
		if c.Name != "what-it-has" {
			// Guard: the set is clean for every other check.
			if c.Status != "PASS" {
				t.Errorf("check %s = %s; the set is built to pass every check but what-it-has", c.Name, c.Status)
			}
			continue
		}
		found = true
		if c.Status != "FAIL" {
			t.Errorf("check what-it-has = %s; want FAIL", c.Status)
		}
		for _, b := range bad {
			named := false
			for _, f := range c.Findings {
				named = named || (strings.Contains(f.Detail, b.file) && strings.Contains(f.Detail, b.id))
			}
			if !named {
				t.Errorf("check what-it-has has no finding naming the file %s and the requirement %s", b.file, b.id)
			}
		}
	}
	if !found {
		t.Errorf("check reported no check what-it-has for a set whose what_it_has values are null, \"screen\" and [\"api\"]")
	}

	text, _ := chkRun(t, "check", "--dir", dir)
	if !strings.Contains(text, "FAIL  what-it-has") {
		t.Errorf("check does not print FAIL  what-it-has")
	}
	if t.Failed() {
		t.Logf("check --dir printed:\n%s", text)
	}
}

// Guard (holds on the first slice): a reference entry {kind, id, what_it_has}
// under an id an earlier file holds with its content passes every check, so do
// a reference entry with two values and a new requirement recorded as having
// none of these; a new requirement without what_it_has draws no warning.
func TestG3_2547db91_CheckPassesReferenceEntriesThatCarryWhatItHas(t *testing.T) {
	dir := t.TempDir()
	chkWrite(t, filepath.Join(dir, "01-cards.json"), chkGoodSet())
	none := g3CheckSR(g3CheckEmptyID, "Card vault expires tokens", "The cards API deletes provider tokens that were not used for a year.", "a token unused for 12 months is deleted at the provider")
	none["what_it_has"] = []any{}
	chkWrite(t, filepath.Join(dir, "02-what-it-has.json"), chkSet(
		map[string]any{"kind": "system_requirement", "id": chkSR, "what_it_has": []any{"screen"}},
		map[string]any{"kind": "system_requirement", "id": chkRef, "what_it_has": []any{"interface", "stored_data"}},
		none,
	))
	out, err := chkRun(t, "check", "--dir", dir)
	if err != nil {
		t.Fatalf("check failed reference entries that carry what_it_has: %v\n%s", err, out)
	}
	if strings.Contains(out, "FAIL") || strings.Contains(out, "WARN") {
		t.Errorf("check failed or warned on reference entries that carry what_it_has, or on new requirements without it:\n%s", out)
	}
	for _, name := range []string{"ids", "receipts", "changed-after-record", "distinct-statements", "template-scenarios", "actor-outcome", "field-limits"} {
		if !strings.Contains(out, "PASS  "+name) {
			t.Errorf("check did not report PASS for %s:\n%s", name, out)
		}
	}
}

// Epic 341 (G3), test case 2547db91 (TC-G3-20): --self-test detects the
// check what-it-has on the embedded known-bad set, through the staged file
// 06-what-it-has.json, and the good set's reference entries with a value pass.
// The self-test passing and the good set staying good hold on the first slice
// (guards); no line names what-it-has there.
func TestG3_2547db91_SelfTestDetectsWhatItHas(t *testing.T) {
	out, err := chkRun(t, "check", "--self-test")
	if err != nil {
		t.Errorf("self-test failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "GOOD SET") || strings.Contains(out, "MISSED") {
		t.Errorf("self-test missed a seeded failure or failed the good set:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	at := -1
	for i, line := range lines {
		if strings.TrimRight(line, " ") == "DETECTED  what-it-has" {
			at = i
		}
	}
	if at < 0 {
		t.Errorf("self-test prints no line DETECTED  what-it-has:\n%s", out)
		return
	}
	named := false
	for _, line := range lines[at+1:] {
		if !strings.HasPrefix(line, "          ") {
			break
		}
		named = named || strings.Contains(line, "06-what-it-has.json")
	}
	if !named {
		t.Errorf("self-test's what-it-has finding does not name the known-bad file 06-what-it-has.json:\n%s", out)
	}
}

// Epic 341 (G3), test case 150d7fb9 (TC-G3-13): check's changed-after-record
// finding tells the agent to add what a requirement has in a new file of
// reference entries under the stored ids. That it still says "changed after it
// was recorded" holds on the first slice (guard).
func TestG3_150d7fb9_CheckChangedAfterRecordAddsWhatItHasByReference(t *testing.T) {
	out, err := chkBad(t, nil, func(dir string) {
		old := chkGoodSet()
		old["requirements"] = []any{chkGoodUR()}
		chkWrite(t, filepath.Join(dir, "recorded", "01-cards.json"), old)
		chkWrite(t, filepath.Join(dir, "recorded", "01-cards.json.receipt"), `{"requirements":[],"trace_count":0}`)
	})
	if err == nil || !strings.Contains(out, "FAIL  changed-after-record") {
		t.Errorf("check did not fail changed-after-record for an edited recorded file: %v\n%s", err, out)
	}
	plain := g3Plain(out)
	if !strings.Contains(plain, "01-cards.json changed after it was recorded") {
		t.Errorf("check's finding does not say the file changed after it was recorded:\n%s", out)
	}
	if phrase := g3Plain("add what a requirement has in a new file of reference entries under the stored ids"); !strings.Contains(plain, phrase) {
		t.Errorf("check's changed-after-record finding does not say %q:\n%s", phrase, out)
	}
}

// Epic 341 (G3), test case 3981fe20 (TC-G3-19): check --help lists the check
// what-it-has among the checks that fail and names what_it_has among a
// reference entry's keys.
func TestG3_3981fe20_CheckHelpNamesWhatItHas(t *testing.T) {
	out, err := runRoot(t, "reverse-engineer", "check", "--help")
	if err != nil || !strings.Contains(out, "Checks that fail:") {
		t.Fatalf("reverse-engineer check --help printed no check help (err %v):\n%s", err, out)
	}
	failing := out[strings.Index(out, "Checks that fail:"):]
	if end := strings.Index(failing, "Warnings:"); end >= 0 {
		failing = failing[:end]
	}
	listed := false
	for _, line := range strings.Split(failing, "\n") {
		listed = listed || strings.HasPrefix(strings.TrimSpace(line), "what-it-has")
	}
	if !listed {
		t.Errorf("check --help has no line starting with what-it-has in its list of checks that fail:\n%s", failing)
	}
	if phrase := "kind, id and what_it_has"; !strings.Contains(g3Plain(out), phrase) {
		t.Errorf("check --help does not name %q among a reference entry's keys:\n%s", phrase, out)
	}
}
