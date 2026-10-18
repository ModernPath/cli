package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const reCheckGoodGroup = `{"requirements":[
 {"external_id":"UR-A-1","kind":"user","title":"Start charging at a chosen connector","actor":"MobilePay customer with a saved card","intended_use":"Start a session at the chosen connector.","description":"The customer starts charging at a free connector.","criteria":[{"external_id":"UR-A-1-SC1","given":"a free connector","when":"the customer taps start","then":"the session starts"}]},
 {"external_id":"SR-A-1","kind":"system","title":"Authorize the card before a session starts","parent_external_ids":["UR-A-1"],"description":"A declined authorization sends no start command.","source_citations":[{"kind":"code","path":"src/Application/StartSessionCommand.cs"}]}
]}`

func writeGroupFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The built-in known-bad set proves every check finds its seeded failure and
// leaves the known-good pair alone.
func TestReverseCheckSelfTestFindsEverySeededFailure(t *testing.T) {
	out, err := reCommand(t, nil, "", "check", "--self-test")
	if err != nil {
		t.Fatalf("self-test failed: %v\n%s", err, out)
	}
	for _, name := range []string{"unique-ids", "distinct-statements", "template-scenarios", "actor-outcome", "domain-titles", "ur-sr-join", "citation-precision"} {
		if !strings.Contains(out, "DETECTED  "+name) {
			t.Errorf("self-test did not report %s as detected:\n%s", name, out)
		}
	}
}

func TestReverseCheckPassesAGoodGroupAndFailsABadOne(t *testing.T) {
	dir := writeGroupFiles(t, map[string]string{"good.json": reCheckGoodGroup})
	if out, err := reCommand(t, nil, "", "check", "--dir", dir); err != nil {
		t.Fatalf("good group refused: %v\n%s", err, out)
	}
	bad := strings.Replace(reCheckGoodGroup, `"actor":"MobilePay customer with a saved card","intended_use":"Start a session at the chosen connector."`, `"actor":"user","outcome":"Start a session."`, 1)
	file := filepath.Join(writeGroupFiles(t, map[string]string{"bad.json": bad}), "bad.json")
	out, err := reCommand(t, nil, "", "check", "--file", file, "--json")
	if err == nil || !strings.Contains(err.Error(), "actor-outcome") {
		t.Fatalf("bad group passed: %v", err)
	}
	var report reCheckReport
	if jerr := json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &report); jerr != nil {
		t.Fatalf("--json output: %v\n%s", jerr, out)
	}
	var details []string
	for _, check := range report.Checks {
		if check.Name == "actor-outcome" {
			for _, f := range check.Findings {
				details = append(details, f.Detail)
			}
		}
	}
	joined := strings.Join(details, "|")
	if !strings.Contains(joined, "catch-all actor") || !strings.Contains(joined, "intended_use") {
		t.Fatalf("actor-outcome findings: %v", details)
	}
}

// Files are checked as one set: a statement repeated in another group, and a
// parent published earlier, are both seen.
func TestReverseCheckReadsFilesAsOneSet(t *testing.T) {
	other := `{"requirements":[{"external_id":"SR-B-1","kind":"system","title":"Report the meter value","parent_external_ids":["UR-OLD-1"],"description":"A declined authorization sends no start command."}]}`
	dir := writeGroupFiles(t, map[string]string{"a.json": reCheckGoodGroup, "b.json": other})
	out, err := reCommand(t, nil, "", "check", "--dir", dir)
	if err == nil || !strings.Contains(out, "SR-A-1, SR-B-1: the same statement") || !strings.Contains(out, "UR-OLD-1") {
		t.Fatalf("cross-file findings missing: %v\n%s", err, out)
	}
	fixed := strings.Replace(other, "A declined authorization sends no start command.", "The final meter value is reported to settlement.", 1)
	dir = writeGroupFiles(t, map[string]string{"a.json": reCheckGoodGroup, "b.json": fixed})
	if out, err := reCommand(t, nil, "", "check", "--dir", dir, "--existing-parents", "UR-OLD-1,UR-OLD-2"); err != nil {
		t.Fatalf("existing parent not accepted: %v\n%s", err, out)
	}
}

func TestReverseCheckFlagRules(t *testing.T) {
	dir := writeGroupFiles(t, map[string]string{"good.json": reCheckGoodGroup})
	for _, args := range [][]string{
		{"check"},
		{"check", "--dir", dir, "--file", filepath.Join(dir, "good.json")},
		{"check", "--self-test", "--dir", dir},
	} {
		if _, err := reCommand(t, nil, "", args...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}

// publishRecorder answers group publishes, refusing the group named refuse.
func publishRecorder(t *testing.T, refuse string, paths *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		*paths = append(*paths, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/groups/"+refuse) {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"error":"stale_corpus"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"receipt"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestReversePublishDirPublishesEachFileUnderItsName(t *testing.T) {
	second := strings.NewReplacer("UR-A-1", "UR-C-1", "SR-A-1", "SR-C-1", "chosen connector", "reserved connector", "free connector", "reserved connector", "A declined", "An expired").Replace(reCheckGoodGroup)
	dir := writeGroupFiles(t, map[string]string{"02-stop.json": second, "01-start.json": reCheckGoodGroup})
	var paths []string
	out, err := reCommand(t, publishRecorder(t, "", &paths), "", "publish", "--run", "r", "--dir", dir)
	if err != nil {
		t.Fatalf("publish --dir: %v\n%s", err, out)
	}
	want := []string{"PUT /api/v1/systems/4/reverse-engineering/runs/r/groups/01-start", "PUT /api/v1/systems/4/reverse-engineering/runs/r/groups/02-stop"}
	if strings.Join(paths, "|") != strings.Join(want, "|") {
		t.Fatalf("published %v, want %v", paths, want)
	}
}

func TestReversePublishDirStopsAtTheFirstRefusal(t *testing.T) {
	second := strings.NewReplacer("UR-A-1", "UR-C-1", "SR-A-1", "SR-C-1", "chosen connector", "reserved connector", "free connector", "reserved connector", "A declined", "An expired").Replace(reCheckGoodGroup)
	dir := writeGroupFiles(t, map[string]string{"a.json": reCheckGoodGroup, "b.json": second, "c.json": reCheckGoodGroup})
	var paths []string
	_, err := reCommand(t, publishRecorder(t, "b", &paths), "", "publish", "--run", "r", "--dir", dir, "--skip-check")
	if err == nil || !strings.Contains(err.Error(), "stale_corpus") || !strings.Contains(err.Error(), "published: a.json") || !strings.Contains(err.Error(), "not published: b.json, c.json") {
		t.Fatalf("refusal report: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("publishing went on after the refusal: %v", paths)
	}
}

func TestReversePublishDirChecksFirstAndRefusesMixedFlags(t *testing.T) {
	dir := writeGroupFiles(t, map[string]string{"a.json": reCheckGoodGroup, "b.json": reCheckGoodGroup})
	var paths []string
	server := publishRecorder(t, "", &paths)
	if _, err := reCommand(t, server, "", "publish", "--run", "r", "--dir", dir); err == nil || !strings.Contains(err.Error(), "unique-ids") {
		t.Fatalf("a failed check did not stop the publish: %v", err)
	}
	for _, args := range [][]string{
		{"publish", "--run", "r", "--dir", dir, "--group", "g"},
		{"publish", "--run", "r", "--dir", dir, "--file", filepath.Join(dir, "a.json")},
		{"publish", "--run", "r"},
		{"publish", "--run", "r", "--file", "-"},
	} {
		if _, err := reCommand(t, server, "{}", args...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if len(paths) != 0 {
		t.Fatalf("a refused invocation reached the server: %v", paths)
	}
}

// A group of assessments whose reasons are one generated sentence, and an
// owned repository's file left unresolved, fail the check that publish runs.
func TestReverseCheckRefusesGeneratedAssessments(t *testing.T) {
	dir := t.TempDir()
	var items []string
	for i := 0; i < 5; i++ {
		items = append(items, fmt.Sprintf(`{"repository_key":"Libraries","path":"src/F%d.cs","sha256":"%064x","snapshot_digest":"x","outcome":"reviewed","reason":"Read the exact captured bytes and verified SHA-256 %064x. No requirement cites this path."}`, i, i, i))
	}
	items = append(items, `{"repository_key":"CarWash","path":"infrastructure/main.bicep","sha256":"x","snapshot_digest":"x","outcome":"unresolved","reason":"Sets the location sync schedule; not yet linked."}`)
	file := filepath.Join(dir, "assessments.json")
	if err := os.WriteFile(file, []byte(`{"requirements":[],"source_assessments":[`+strings.Join(items, ",")+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := reverseCheckFiles([]string{file}, map[string]bool{}, map[string]bool{"CarWash": true})
	if err != nil {
		t.Fatal(err)
	}
	failed := map[string]bool{}
	for _, check := range report.Checks {
		if !check.Passed && check.Level == "fail" {
			failed[check.Name] = true
		}
	}
	if report.Passed || !failed["assessment-reasons"] || !failed["unresolved-owned"] {
		t.Fatalf("generated reasons and an owned unresolved file must fail, got %+v", failed)
	}
	if report.Assessments != 6 {
		t.Fatalf("the report counts the assessments, got %d", report.Assessments)
	}
	if without := reverseCheckWithout(report, "ur-sr-join"); without.Passed {
		t.Fatal("dropping the join must not hide the assessment failures")
	}
}
