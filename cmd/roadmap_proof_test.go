package cmd

// Epic 336 (D2), test case TC-D2-18 (f702c229), criterion 18533831 (SR-D2-6
// at 3): `modernpath roadmap proof --initiative <id>` reads the proof route and
// prints the Initiative and its Release, the counts and one line per kept or
// changed requirement with its outcome, the level it should reach with its
// basis and the level reached with its reason, in the Proof tab's words;
// `--json` writes the route's answer; it refuses without --initiative and
// prints a refusal's sentence. The command's help and its section of the kit
// copy of the CLI guide name --initiative and --json, give the five levels in
// the Proof tab's words, say that proof is read once the Release's reference
// revision is set, and that only a run of the modernized system at the commit
// that merged the change, or at a commit that contains it, counts.
//
// On the build's first slice `proof` is no command of the roadmap group, so the
// test server receives no request and every assertion fails. The test uses no
// new production symbol.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const d2ProofInitiative = "66666666-6666-4666-8666-666666666666"
const d2ProofPath = "/api/v1/systems/1/roadmap/proof"

const d2ProofEntry = `{"data":{
 "initiative":{"id":"` + d2ProofInitiative + `","title":"Provider connections","context":"INTG"},
 "release":{"id":21,"name":"Billing modernization","reference_revision":"c0ffee1c0ffee1c0ffee1c0ffee1c0ffee1c0ffe"},
 "legacy_system":{"id":41,"name":"Legacy billing"},
 "in_place":false,
 "proof_waits_for_reference_revision":false,
 "counts":{"kept_or_changed":3,"proved":1,"proved_below":1,"proved_below_accepted":0,"not_yet":1,"revised_since_selected":0,"capture_not_reproducible":1},
 "rows":[
  {"requirement_kind":"system_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"SR-12-0","title":"Export invoices","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":"compared","expected_level_reason":"Auditors read the files","set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[{"kind":"system_requirement","id":"b2b2b2b2-2222-4222-8222-222222222222","display_ref":"SR-40-0","title":"Export invoices as PDF","level":"compared","criteria":[]}],
   "reached_level":"compared","reached_reason":null,"expected_level":"compared","expected_level_basis":"person","proved":true,"proof_state":"as_agreed","lower_level_acceptance":null,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null},
  {"requirement_kind":"system_requirement","requirement_id":"a2a2a2a2-2222-4222-8222-222222222222","display_ref":"SR-13-0","title":"Retry the partner feed","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],
   "reached_level":"tests_only","reached_reason":"proved below the agreed level","expected_level":"compared","expected_level_basis":"estimated","proved":false,"proof_state":"below","lower_level_acceptance":null,"drafted":true,
   "capture_not_reproducible":true,"revised_since_selected":true,"legacy_changed_since_recorded":null,"run":null},
  {"requirement_kind":"system_requirement","requirement_id":"a3a3a3a3-3333-4333-8333-333333333333","display_ref":"SR-14-0","title":"Bill monthly","context":"INTG",
   "outcome":"change","decision":{"reason":"A new API","expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],
   "reached_level":"not_yet","reached_reason":"waiting for the merge commit to be recorded on Provider feed","expected_level":"tests_only","expected_level_basis":"changed","proved":false,"proof_state":"not_yet","lower_level_acceptance":null,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":null,"run":null}
 ]}}`

const d2ProofRefusal = `{"error":{"code":"reference_revision_required","message":"Proof is read once the release's reference revision is set."}}`

// d2ProofServer answers the reachability probe and the proof route with
// status and body, recording the proof requests' queries.
func d2ProofServer(t *testing.T, status int, body string) (*httptest.Server, *[]url.Values) {
	t.Helper()
	seen := []url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != d2ProofPath {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found"}`))
			return
		}
		seen = append(seen, r.URL.Query())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func TestD2_f702c229_ProofPrintsTheInitiativeTheCountsAndOneLinePerRow(t *testing.T) {
	server, seen := d2ProofServer(t, http.StatusOK, d2ProofEntry)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err != nil {
		t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
	}
	if len(*seen) != 1 || (*seen)[0].Get("line_id") != d2ProofInitiative {
		t.Fatalf("the proof route received %d requests, want 1 with line_id=%s: %#v", len(*seen), d2ProofInitiative, *seen)
	}
	plain := g3Plain(out)
	for _, want := range []string{
		"Provider connections",
		"Billing modernization",
		"Legacy billing",
		"3 kept or changed",
		"1 capture not reproducible",
		"SR-12-0 Export invoices",
		"keep",
		"reached Compared",
		"SR-13-0 Retry the partner feed",
		"capture not reproducible",
		"revised since selected",
		"SR-14-0 Bill monthly",
		"change",
		// Epic 459 (H2): the counts' and the rows' proof words moved to
		// TestRoadmapProofPrintsTheCountsAndEachRowInTheProofTabsWords (T14).
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("roadmap proof does not print %q:\n%s", want, out)
		}
	}
	for _, code := range []string{"tests_only", "not_yet", "screens_compared", "reviewed_only", "expected_level_basis"} {
		if strings.Contains(out, code) {
			t.Errorf("roadmap proof prints the code %q instead of its words:\n%s", code, out)
		}
	}
}

func TestD2_f702c229_ProofJSONWritesTheRouteAnswer(t *testing.T) {
	server, _ := d2ProofServer(t, http.StatusOK, d2ProofEntry)
	cobraWorkspace(t, server, 1)

	stdout, err := runRootStdout(t, "roadmap", "proof", "--initiative", d2ProofInitiative, "--json")
	if err != nil {
		t.Fatalf("roadmap proof --json: %v\n%s", err, stdout)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--json did not print one JSON document: %v\n%s", err, stdout)
	}
	var served map[string]any
	if err := json.Unmarshal([]byte(d2ProofEntry), &served); err != nil {
		t.Fatal(err)
	}
	data, _ := served["data"].(map[string]any)
	want, _ := json.Marshal(data)
	have, _ := json.Marshal(got)
	if string(want) != string(have) {
		t.Errorf("--json does not write the route's answer as served:\n got %s\nwant %s", have, want)
	}
}

func TestD2_f702c229_ProofRefusesWithoutInitiativeAndPrintsARefusalSentence(t *testing.T) {
	server, seen := d2ProofServer(t, http.StatusConflict, d2ProofRefusal)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "proof")
	if err == nil {
		t.Errorf("roadmap proof without --initiative ended without an error:\n%s", out)
	}
	if !strings.Contains(b3Text(out, err), "--initiative") {
		t.Errorf("the refusal without --initiative does not name the flag:\n%s", b3Text(out, err))
	}
	if len(*seen) != 0 {
		t.Errorf("a refused command sent %d requests; want none", len(*seen))
	}

	out, err = runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	text := b3Text(out, err)
	if err == nil {
		t.Errorf("a refused read ended without an error:\n%s", out)
	}
	if !strings.Contains(text, "Proof is read once the release's reference revision is set.") {
		t.Errorf("roadmap proof does not print the refusal's sentence:\n%s", text)
	}
}

// Criterion 18533831: without a bound System the command refuses before it sends anything (review round 2, note 6).
func TestD2_f702c229_ProofRefusesWithoutABoundSystem(t *testing.T) {
	server, seen := d2ProofServer(t, http.StatusOK, d2ProofEntry)
	cobraWorkspace(t, server, 0)

	_, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err == nil || !strings.Contains(err.Error(), "bound System") {
		t.Fatalf("error = %v; want a refusal that names the bound System", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("an unbound workspace reached the server: %#v", *seen)
	}
}

// d2ProofRequirePhrases checks the words a text about roadmap proof must hold.
func d2ProofRequirePhrases(t *testing.T, what, text string) {
	t.Helper()
	plain := g3Plain(text)
	for _, want := range []string{
		"--initiative",
		"--json",
		"Compared",
		"Screens compared",
		"Tests only",
		"Reviewed only",
		"Not yet",
		"reference revision is set",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("%s does not say %q", what, want)
		}
	}
	if !regexp.MustCompile(`at the commit that merged the change, or at a commit that contains it`).MatchString(plain) {
		t.Errorf("%s does not say that only a run of the modernized system at the commit that merged the change, or at a commit that contains it, counts", what)
	}
}

func TestD2_f702c229_ProofHelpGivesItsFlagsTheLevelsAndTheRule(t *testing.T) {
	out, err := runRoot(t, "roadmap", "proof", "--help")
	if err != nil {
		t.Fatalf("roadmap proof --help: %v\n%s", err, out)
	}
	d2ProofRequirePhrases(t, "the help of roadmap proof", out)
}

func TestD2_f702c229_CLIGuideHasARoadmapProofSection(t *testing.T) {
	guide, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	section := d2GuideSection(string(guide), "roadmap proof")
	if section == "" {
		t.Fatalf("the kit's CLI guide has no section that documents roadmap proof")
	}
	d2ProofRequirePhrases(t, "the CLI guide's roadmap proof section", section)
}

// d2GuideSection cuts out of the guide the level-2 or level-3 section whose
// text holds `command` in a shell block, up to the next heading of its level
// or above; headings inside code fences do not count.
func d2GuideSection(guide, command string) string {
	type heading struct {
		line, level int
	}
	lines := strings.Split(guide, "\n")
	var headings []heading
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if m := regexp.MustCompile(`^(#{1,6})\s+(.*)$`).FindStringSubmatch(line); m != nil {
			headings = append(headings, heading{line: i, level: len(m[1])})
		}
	}
	for i, h := range headings {
		if h.level < 2 || h.level > 3 {
			continue
		}
		end := len(lines)
		for _, next := range headings[i+1:] {
			if next.level <= h.level {
				end = next.line
				break
			}
		}
		section := strings.Join(lines[h.line:end], "\n")
		if strings.Contains(section, "modernpath "+command) {
			return section
		}
	}
	return ""
}

// Epic 459 (H2), SR "One set of status and proof words in the command line",
// criterion 1 (c77915c7): roadmap proof prints the counts in the Proof tab's
// summary words, each row with its agreed level and the level reached, a row
// below its agreed level with proved below the agreed level and, when an
// acceptance applies, accepted by the person's name with their words, a row
// not proved yet as Not proved yet (plan review 3, note 2), and under Done when
// the line Proved as agreed with where it stands and its waits. No line says
// less well, as well as expected, less than expected or should reach. The
// served entries are the proof read's after this Epic (contract §1 and §3).
//
// At the Epic's base the command prints "proved as well as expected", "less
// well", "should reach" and "reached Not yet", no accepted by and no Proved
// as agreed line, so the assertions fail. The tests use no new Go symbol.

// h2ProofOldWords are the words no line of roadmap proof says.
var h2ProofOldWords = []string{"less well", "as well as expected", "less than expected", "should reach", "Should reach", "reached Not yet", "Completed"}

func h2RequireNoOldWords(t *testing.T, out string) {
	t.Helper()
	for _, word := range h2ProofOldWords {
		if strings.Contains(out, word) {
			t.Errorf("roadmap proof says %q:\n%s", word, out)
		}
	}
}

// 40d4e9ab-aeee-4713-a924-7fa752c7fec1
func TestRoadmapProofPrintsTheCountsAndEachRowInTheProofTabsWords(t *testing.T) {
	server, _ := d2ProofServer(t, http.StatusOK, d2ProofEntry)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err != nil {
		t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
	}
	for _, line := range []struct{ what, pattern string }{
		{"the counts in the Proof tab's summary words",
			`^3 kept or changed · 1 proved as agreed · 1 proved below the agreed level · 1 not proved yet · 0 revised since selected · 1 capture not reproducible$`},
		{"a row proved as agreed with its agreed level and the level reached",
			`^  SR-12-0 Export invoices: keep · agreed level Compared, set by Pia Planner: Auditors read the files · reached Compared$`},
		{"a row below its agreed level",
			`^  SR-13-0 Retry the partner feed: keep · agreed level Compared, estimated from its files · capture not reproducible · reached Tests only, proved below the agreed level · revised since selected$`},
		{"a row not proved yet with its reason",
			`^  SR-14-0 Bill monthly: change · agreed level Tests only, changed requirement · Not proved yet, waiting for the merge commit to be recorded on Provider feed$`},
	} {
		if d3LineMatching(out, line.pattern) == "" {
			t.Errorf("roadmap proof does not print %s: no line matches %s\n%s", line.what, line.pattern, out)
		}
	}
	if strings.Contains(out, "accepted by") {
		t.Errorf("roadmap proof prints accepted by for an entry with no acceptance:\n%s", out)
	}
	h2RequireNoOldWords(t, out)
}

const h2ProofAccepted = `{"reached_level":"screens_compared","expected_level":"compared","accepted_by":{"id":5,"name":"Pia Planner"},"words":"Screens are enough here: the feed is checked by hand each month","accepted_at":"2026-11-22T10:00:00Z"}`

// h2ProofItem is the proof_as_agreed item of h2ProofEntry; the tests replace it.
const h2ProofItem = `{"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"counts":{"kept_or_changed":5,"as_agreed":2,"below":1,"below_accepted":1,"not_yet":1}}`

// h2ProofEntry is a replacing Initiative under an open Release with rows proved
// as agreed, one below its agreed level, one below with an acceptance that
// applies and one not proved yet; its work, acceptance and differences are met.
const h2ProofEntry = `{"data":{
 "initiative":{"id":"` + d2ProofInitiative + `","title":"Provider connections","context":"INTG"},
 "release":{"id":21,"name":"Billing modernization","reference_revision":"c0ffee1c0ffee1c0ffee1c0ffee1c0ffee1c0ffe","status":"active"},
 "legacy_system":{"id":41,"name":"Legacy billing"},
 "in_place":false,
 "proof_waits_for_reference_revision":false,
 "counts":{"kept_or_changed":5,"proved":2,"proved_below":2,"proved_below_accepted":1,"not_yet":1,"revised_since_selected":0,"capture_not_reproducible":0},
 "done":false,
 "done_when":[
  {"key":"work_complete","met":true,"counts":{"epic":{"done":2,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
  {"key":"acceptance","met":true,"step":null,"release":{"id":21,"name":"Billing modernization"},"report_id":"` + d3ProofReport + `","counts":{"passed":4,"excluded":0,"failed":0,"not_tested":0,"total":4}},
  {"key":"differences_resolved","met":true,"open":0},
  ` + h2ProofItem + `],
 "differences":[],
 "open_differences":0,
 "rows":[
  {"requirement_kind":"system_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"SR-12-0","title":"Export invoices","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],"reached_level":"compared","reached_reason":null,"expected_level":"compared","expected_level_basis":"recorded","proved":true,
   "proof_state":"as_agreed","lower_level_acceptance":null,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null},
  {"requirement_kind":"system_requirement","requirement_id":"a2a2a2a2-2222-4222-8222-222222222222","display_ref":"SR-13-0","title":"Retry the partner feed","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],"reached_level":"tests_only","reached_reason":"proved below the agreed level","expected_level":"compared","expected_level_basis":"estimated","proved":false,
   "proof_state":"below","lower_level_acceptance":null,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null},
  {"requirement_kind":"system_requirement","requirement_id":"a3a3a3a3-3333-4333-8333-333333333333","display_ref":"SR-14-0","title":"Show the partner feed","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],"reached_level":"screens_compared","reached_reason":"proved below the agreed level","expected_level":"compared","expected_level_basis":"estimated","proved":false,
   "proof_state":"below_accepted","lower_level_acceptance":` + h2ProofAccepted + `,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null},
  {"requirement_kind":"system_requirement","requirement_id":"a4a4a4a4-4444-4444-8444-444444444444","display_ref":"SR-15-0","title":"Bill monthly","context":"INTG",
   "outcome":"change","decision":{"reason":"A new API","expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],"reached_level":"not_yet","reached_reason":"its test fails","expected_level":"tests_only","expected_level_basis":"changed","proved":false,
   "proof_state":"not_yet","lower_level_acceptance":null,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null},
  {"requirement_kind":"user_requirement","requirement_id":"a5a5a5a5-5555-4555-8555-555555555555","display_ref":"UR-16-0","title":"Read the invoice","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":"tests_only","expected_level_reason":"A plain read","set_by":{"id":6,"name":"Dana Domain"}},
   "counterparts":[],"reached_level":"compared","reached_reason":null,"expected_level":"tests_only","expected_level_basis":"person","proved":true,
   "proof_state":"as_agreed","lower_level_acceptance":null,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null}
 ]}}`

// h2ProofWith is h2ProofEntry with its proof_as_agreed item replaced.
func h2ProofWith(t *testing.T, item string) string {
	t.Helper()
	entry := strings.Replace(h2ProofEntry, h2ProofItem, item, 1)
	if entry == h2ProofEntry && item != h2ProofItem {
		t.Fatal("setup: the proof item was not replaced")
	}
	return entry
}

// 40d4e9ab-aeee-4713-a924-7fa752c7fec1
func TestRoadmapProofPrintsAnAcceptanceByNameAndProvedAsAgreedUnderDoneWhen(t *testing.T) {
	server, _ := d2ProofServer(t, http.StatusOK, h2ProofEntry)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err != nil {
		t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
	}
	for _, line := range []struct{ what, pattern string }{
		{"the counts with the rows accepted by name",
			`^5 kept or changed · 2 proved as agreed · 2 proved below the agreed level \(1 accepted by name\) · 1 not proved yet · 0 revised since selected · 0 capture not reproducible$`},
		{"a row below its agreed level, not accepted",
			`^  SR-13-0 Retry the partner feed: keep · agreed level Compared, estimated from its files · reached Tests only, proved below the agreed level$`},
		{"a row below its agreed level, accepted by name with the person's words",
			`^  SR-14-0 Show the partner feed: keep · agreed level Compared, estimated from its files · reached Screens compared, proved below the agreed level, accepted by Pia Planner: Screens are enough here: the feed is checked by hand each month$`},
		{"a row not proved yet",
			`^  SR-15-0 Bill monthly: change · agreed level Tests only, changed requirement · Not proved yet, its test fails$`},
		{"a row proved above its agreed level",
			`^  UR-16-0 Read the invoice: keep · agreed level Tests only, set by Dana Domain: A plain read · reached Compared$`},
		{"Done when, not done yet", `^Done when: Not done yet$`},
		{"Proved as agreed with where it stands",
			`^  Proved as agreed: not met, 2 of 5 · 2 proved below the agreed level \(1 accepted by name\) · 1 not proved yet$`},
	} {
		if d3LineMatching(out, line.pattern) == "" {
			t.Errorf("roadmap proof does not print %s: no line matches %s\n%s", line.what, line.pattern, out)
		}
	}
	want := []string{
		"  Waits for 1 proved below the agreed level: Prove it at the agreed level or accept the lower level by name, A person on the team",
		"  Waits for 1 not proved yet: Waits for a run that proves it, The team",
	}
	if waits := d3WaitLines(out); !slices.Equal(waits, want) {
		t.Errorf("roadmap proof waits for %q; want %q\n%s", waits, want, out)
	}
	h2RequireNoOldWords(t, out)
}

// 40d4e9ab-aeee-4713-a924-7fa752c7fec1
func TestRoadmapProofPrintsWhereProvedAsAgreedStandsInEachCase(t *testing.T) {
	const unreadable = "The initiative's release does not replace a legacy system."
	for _, c := range []struct {
		name, item, done, line string
		waits                  []string
	}{
		{"every row as agreed", `{"key":"proof_as_agreed","met":true,"waits_for_reference_revision":false,"counts":{"kept_or_changed":5,"as_agreed":5,"below":0,"below_accepted":0,"not_yet":0}}`,
			"Done", "  Proved as agreed: met, 5 of 5", []string{}},
		{"the rows below accepted by name", `{"key":"proof_as_agreed","met":true,"waits_for_reference_revision":false,"counts":{"kept_or_changed":5,"as_agreed":3,"below":0,"below_accepted":2,"not_yet":0}}`,
			"Done", "  Proved as agreed: met, 3 of 5 · 2 proved below the agreed level (2 accepted by name)", []string{}},
		{"rows below, none accepted", `{"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"counts":{"kept_or_changed":5,"as_agreed":3,"below":2,"below_accepted":0,"not_yet":0}}`,
			"Not done yet", "  Proved as agreed: not met, 3 of 5 · 2 proved below the agreed level",
			[]string{"  Waits for 2 proved below the agreed level: Prove it at the agreed level or accept the lower level by name, A person on the team"}},
		{"no row kept or changed", `{"key":"proof_as_agreed","met":true,"waits_for_reference_revision":true,"counts":{"kept_or_changed":0,"as_agreed":0,"below":0,"below_accepted":0,"not_yet":0}}`,
			"Done", "  Proved as agreed: met, nothing kept or changed to prove", []string{}},
		{"the proof waits for the reference revision", `{"key":"proof_as_agreed","met":false,"waits_for_reference_revision":true,"counts":{"kept_or_changed":2,"as_agreed":0,"below":0,"below_accepted":0,"not_yet":2}}`,
			"Not done yet", "  Proved as agreed: not met, read once the Release's reference revision is set",
			[]string{"  Waits for Reference revision: Set the Release's reference revision, A person on the team"}},
		{"the proof cannot be read", `{"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"reason":{"code":"release_not_modernization","message":"` + unreadable + `"}}`,
			"Not done yet", "  Proved as agreed: not met, proof cannot be read: " + unreadable,
			[]string{"  Waits for Proof cannot be read: The initiative's release does not replace a legacy system, A person on the team"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			entry := h2ProofWith(t, c.item)
			if c.done == "Done" {
				entry = strings.Replace(entry, `"done":false,`, `"done":true,`, 1)
			}
			server, _ := d2ProofServer(t, http.StatusOK, entry)
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
			if err != nil {
				t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
			}
			if d3LineMatching(out, `^Done when: `+c.done+`$`) == "" {
				t.Errorf("roadmap proof does not print Done when: %s\n%s", c.done, out)
			}
			if d3LineMatching(out, `^`+regexp.QuoteMeta(c.line)+`$`) == "" {
				t.Errorf("roadmap proof does not print the line %q\n%s", c.line, out)
			}
			if waits := d3WaitLines(out); !slices.Equal(waits, c.waits) {
				t.Errorf("roadmap proof waits for %q; want %q\n%s", waits, c.waits, out)
			}
			h2RequireNoOldWords(t, out)
		})
	}
}
