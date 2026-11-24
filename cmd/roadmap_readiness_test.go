package cmd

// Epic 338 (D4), test case 6fd3853c-134d-4044-b58b-02ae6f669be5: "D4 ·
// roadmap show prints a Modernization Release's readiness, what the ledger
// misses and the latest replays". Criterion 27d5ddca ("One read gives a
// Modernization Release's readiness the way the Heartbeat counts it, with
// built and accepted" at 7): roadmap show --readiness prints, for each
// Modernization Release, in the Release page's words, the proved figure of the
// kept or changed requirements with its percent; before the reference
// revision is set, that proof is read once it is set; when nothing is kept or
// changed, a sentence in place of the percent; drafted and not proved yet, not
// drafted yet, undecided and dropped; built of the kept or changed
// requirements, and accepted of the kept or changed user requirements; every
// file the ledger misses under its context; and the latest replays line, or
// that no replay has been captured. For a Release whose legacy system was
// deleted, its readiness block is that one sentence, and without --readiness
// the command prints none of this. Its help and the CLI guide say that the
// block's Built counts what Proved counts, while each context line's built
// counts over every decided row of the context (plan review 9, note 2).
//
// The test server serves the Roadmap read with the new fields, with or
// without the parameter. On the first slice, roadmap show takes --readiness,
// sends readiness=true and prints nothing more, so the assertions on the
// printed lines, the help and the guide fail. That the flag sends
// readiness=true, and that nothing of the readiness prints without it, hold on
// the slice and guard them. The test uses no new Go symbol.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// d4ReadinessServer answers the reachability probe and records every other
// request's method and URL, replying with body.
func d4ReadinessServer(t *testing.T, body string) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	seen := []*http.Request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		seen = append(seen, r.Clone(r.Context()))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

// The Roadmap read of System 1 as the server answers readiness=true, in the
// shape of builder A's Core.Roadmap.ReleaseReadiness, LedgerMisses and
// SuiteComparison.latest_against_previous/2 (Epic 338's RED commit c3d645c9be).
// Cutover replaces Legacy billing and has its reference revision: proved,
// built and accepted, files the ledger misses in Invoicing and two not placed
// in a context, one of them with no role under a migrations directory, and
// replays that changed, were captured once or not yet. Autumn is a Delivery
// Release. Renewal modernizes in place with no reference revision, a legacy
// system with no analysis and no captured replay. Quiet keeps or changes
// nothing. Sunset is closed and its legacy system was deleted.
const d4RoadmapReadinessReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"BILL","name":"Billing"},{"code":"INV","name":"Invoicing"}],
"releases":[
 {"id":21,"name":"Cutover","status":"active","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["BILL","INV"],"legacy_reference_revision":"a1b2c3d4",
  "modernization":{
   "contexts":[
    {"code":"BILL","name":"Billing","in_scope":4,"undecided":3,"decided":1,"drafted":1,"built":1,
     "standing":{"proved":0,"drafted_not_proved":1,"proved_below":0,"proved_below_accepted":0,"drafted_not_proved_yet":1,"not_drafted":0,"undecided":3,"dropped":0,"kept_or_changed":1,"decided_elsewhere":0},
     "accepted":{"count":0,"of":1},"ledger_misses":0},
    {"code":"INV","name":"Invoicing","in_scope":4,"undecided":0,"decided":4,"drafted":3,"built":2,
     "standing":{"proved":1,"drafted_not_proved":1,"proved_below":1,"proved_below_accepted":1,"drafted_not_proved_yet":0,"not_drafted":1,"undecided":0,"dropped":1,"kept_or_changed":3,"decided_elsewhere":0},
     "accepted":{"count":1,"of":1},"ledger_misses":2}],
   "proof_waits_for_reference_revision":false,
   "standing":{"proved":1,"drafted_not_proved":2,"proved_below":1,"proved_below_accepted":1,"drafted_not_proved_yet":1,"not_drafted":1,"undecided":3,"dropped":1,"kept_or_changed":4,"decided_elsewhere":0},
   "readiness":{"proved":1,"of":4,"percent":25},
   "built":1,
   "accepted":{"count":1,"of":2},
   "ledger_misses":{"analysed":true,"message":null,"total":4,
    "contexts":[
     {"code":"BILL","name":"Billing","count":0,"files":[]},
     {"code":"INV","name":"Invoicing","count":2,"files":[
      {"path":"legacy/invoices/InvoiceView.tsx","role":"view","holds":["screen"],"repository":"legacy-billing"},
      {"path":"legacy/invoices/invoice_controller.rb","role":"controller","holds":["interface"],"repository":"legacy-billing"}]}],
    "not_placed":{"count":2,"files":[
     {"path":"legacy/db/migrations/README.md","role":null,"holds":["stored_data"],"repository":"legacy-billing"},
     {"path":"legacy/jobs/nightly_worker.rb","role":"worker","holds":["interface"],"repository":"legacy-billing"}]}},
   "replays":{"captured":true,"message":null,"replays":3,"with_result":2,"match":1,
    "newest":{"pass":1,"fail":1,"error":0,"skip":0,"inconclusive":0},
    "previous":{"pass":1,"fail":0,"error":0,"skip":0,"inconclusive":0},
    "newest_capture_at":"2026-11-10T09:30:00.000000Z",
    "changed":[{"test_case_id":"44444444-4444-4444-8444-444444444441","title":"Invoice totals replay",
      "previous":{"outcome":"pass","commit":"1a2b3c4","captured_at":"2026-11-01T08:00:00.000000Z"},
      "newest":{"outcome":"fail","commit":"5d6e7f8","captured_at":"2026-11-10T09:30:00.000000Z"}}],
    "unchanged":[],
    "captured_once":[{"test_case_id":"44444444-4444-4444-8444-444444444442","title":"Credit note replay",
      "newest":{"outcome":"pass","commit":"5d6e7f8","captured_at":"2026-11-10T09:30:00.000000Z"}}],
    "not_captured":[{"test_case_id":"44444444-4444-4444-8444-444444444443","title":"Dunning replay"}]}},
  "initiatives":[
   {"id":"11111111-1111-4111-8111-111111111111","text":"Invoice runs","horizon":"now","context":"INV","context_name":"Invoicing","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[51]}]},
 {"id":22,"name":"Autumn","status":"planned","planning_context":{},
  "release_type":"delivery","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,"modernization":null,
  "initiatives":[
   {"id":"22222222-2222-4222-8222-222222222222","text":"Gift cards","horizon":"next","context":"BILL","context_name":"Billing","new_context":false,"work_kind":"new_work","release_id":22,"epic_ids":[]}]},
 {"id":23,"name":"Renewal","status":"planned","planning_context":{},
  "release_type":"modernization","legacy_system_id":1,"legacy_system_name":"T","in_place":true,
  "legacy_scope":null,"legacy_reference_revision":null,
  "modernization":{
   "contexts":[{"code":"BILL","name":"Billing","in_scope":5,"undecided":2,"decided":3,"drafted":2,"built":0,
    "standing":{"drafted_not_proved":2,"drafted_not_proved_yet":2,"not_drafted":1,"undecided":2,"dropped":0,"kept_or_changed":3,"decided_elsewhere":0},
    "accepted":{"count":0,"of":1},"ledger_misses":0}],
   "proof_waits_for_reference_revision":true,
   "standing":{"drafted_not_proved":2,"drafted_not_proved_yet":2,"not_drafted":1,"undecided":2,"dropped":0,"kept_or_changed":3,"decided_elsewhere":0},
   "readiness":null,
   "built":0,
   "accepted":{"count":0,"of":1},
   "ledger_misses":{"analysed":false,"reason":"not_analysed","message":"T has no analysed file yet.",
    "total":null,"contexts":[],"not_placed":null},
   "replays":{"captured":false,"message":"No replay has been captured in this Release yet.","replays":1,"with_result":0,"match":0,
    "newest":{"pass":0,"fail":0,"error":0,"skip":0,"inconclusive":0},
    "previous":{"pass":0,"fail":0,"error":0,"skip":0,"inconclusive":0},
    "newest_capture_at":null,"changed":[],"unchanged":[],"captured_once":[],
    "not_captured":[{"test_case_id":"55555555-5555-4555-8555-555555555551","title":"Partner feed replay"}]}},
  "initiatives":[]},
 {"id":24,"name":"Quiet","status":"planned","planning_context":{},
  "release_type":"modernization","legacy_system_id":42,"legacy_system_name":"Legacy ledger","in_place":false,
  "legacy_scope":["BILL"],"legacy_reference_revision":"9f8e7d6c",
  "modernization":{
   "contexts":[{"code":"BILL","name":"Billing","in_scope":2,"undecided":2,"decided":0,"drafted":0,"built":0,
    "standing":{"proved":0,"drafted_not_proved":0,"proved_below":0,"proved_below_accepted":0,"drafted_not_proved_yet":0,"not_drafted":0,"undecided":2,"dropped":0,"kept_or_changed":0,"decided_elsewhere":0},
    "accepted":{"count":0,"of":0},"ledger_misses":0}],
   "proof_waits_for_reference_revision":false,
   "standing":{"proved":0,"drafted_not_proved":0,"proved_below":0,"proved_below_accepted":0,"drafted_not_proved_yet":0,"not_drafted":0,"undecided":2,"dropped":0,"kept_or_changed":0,"decided_elsewhere":0},
   "readiness":{"proved":0,"of":0,"percent":null},
   "built":0,
   "accepted":{"count":0,"of":0},
   "ledger_misses":{"analysed":true,"message":null,"total":0,"contexts":[{"code":"BILL","name":"Billing","count":0,"files":[]}],
    "not_placed":{"count":0,"files":[]}},
   "replays":{"captured":false,"message":"No replay has been captured in this Release yet.","replays":0,"with_result":0,"match":0,
    "newest":{"pass":0,"fail":0,"error":0,"skip":0,"inconclusive":0},
    "previous":{"pass":0,"fail":0,"error":0,"skip":0,"inconclusive":0},
    "newest_capture_at":null,"changed":[],"unchanged":[],"captured_once":[],"not_captured":[]}},
  "initiatives":[]},
 {"id":25,"name":"Sunset","status":"closed","planning_context":{},
  "release_type":"modernization","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":["BILL"],"legacy_reference_revision":"77aa88bb",
  "modernization":{"contexts":[]},
  "initiatives":[
   {"id":"33333333-3333-4333-8333-333333333333","text":"Old ledger","horizon":"later","context":"BILL","context_name":"Billing","new_context":false,"work_kind":"replaces_legacy","release_id":25,"epic_ids":[61]}]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

// d4ReadinessLines are lines or sentences only the readiness block prints.
var d4ReadinessLines = []string{
	"Readiness",
	"Proved as agreed:",
	"Proved below the agreed level:",
	"Proof is read once the reference revision is set.",
	"Drafted, not proved yet:",
	"Not drafted yet:",
	"in no Initiative yet",
	"Built, their Epic complete:",
	"accepted by a domain expert:",
	"what the ledger misses",
	"legacy files with behaviour and no requirement",
	"Not placed in a context",
	"Latest replays",
	"No replay has been captured in this Release yet.",
	"The legacy system was deleted, so its readiness can no longer be read.",
}

// d4ReleaseBlocks cuts the output into each Release's block, from its heading
// to the next heading, and fails unless the Releases come in the served order.
func d4ReleaseBlocks(t *testing.T, out string) map[string]string {
	t.Helper()
	names := []string{"Cutover", "Autumn", "Renewal", "Quiet", "Sunset"}
	heads := make([]string, 0, len(names)+1)
	for _, name := range names {
		heads = append(heads, "Release "+name+" (")
	}
	heads = append(heads, "\nNo release")
	at := make([]int, len(heads))
	for i, head := range heads {
		at[i] = strings.Index(out, head)
		if at[i] < 0 || (i > 0 && at[i] < at[i-1]) {
			t.Fatalf("output lacks %q in the served order of the Releases:\n%s", head, out)
		}
	}
	blocks := map[string]string{}
	for i, name := range names {
		blocks[name] = out[at[i]:at[i+1]]
	}
	return blocks
}

func TestD4_6fd3853c_RoadmapShowReadinessPrintsEachModernizationReleasesReadiness(t *testing.T) {
	server, seen := d4ReadinessServer(t, d4RoadmapReadinessReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show", "--readiness")
	if err != nil {
		t.Fatalf("roadmap show --readiness: %v\n%s", err, out)
	}

	t.Run("the flag sends readiness=true (guard)", func(t *testing.T) {
		if len(*seen) != 1 {
			t.Fatalf("requests = %d; want one GET of the Roadmap", len(*seen))
		}
		got := (*seen)[0]
		if got.Method != http.MethodGet || got.URL.Path != "/api/v1/systems/1/roadmap" {
			t.Fatalf("request = %s %s; want GET /api/v1/systems/1/roadmap", got.Method, got.URL.Path)
		}
		if query := got.URL.Query(); query.Get("readiness") != "true" || len(query) != 1 {
			t.Errorf("query = %q; want readiness=true and nothing else", got.URL.RawQuery)
		}
	})

	blocks := d4ReleaseBlocks(t, out)
	requireIn := func(t *testing.T, release string, phrases ...string) {
		t.Helper()
		for _, phrase := range phrases {
			if !strings.Contains(blocks[release], phrase) {
				t.Errorf("the block of Release %s lacks %q:\n%s", release, phrase, out)
			}
		}
	}
	forbidIn := func(t *testing.T, release string, phrases ...string) {
		t.Helper()
		for _, phrase := range phrases {
			if strings.Contains(blocks[release], phrase) {
				t.Errorf("the block of Release %s prints %q:\n%s", release, phrase, out)
			}
		}
	}

	t.Run("the four figures with the proved percent, in the Release page's words", func(t *testing.T) {
		requireIn(t, "Cutover",
			"Proved as agreed: 1 of 4 kept or changed, proved by runs captured after the change · 25%",
			"Drafted, not proved yet: 1 in a saved Epic plan",
			"Not drafted yet: 1 kept or changed, in no Epic yet",
			"Undecided: 3 in no Initiative yet, with 1 dropped")
	})

	t.Run("built, accepted and what the ledger misses on one line", func(t *testing.T) {
		requireIn(t, "Cutover",
			"Built, their Epic complete: 1 · accepted by a domain expert: 1 of 2 kept or changed user requirements · what the ledger misses: 4 legacy files with behaviour and no requirement, in Invoicing; 2 of them not placed in a context")
	})

	t.Run("every file the ledger misses under its context, then those not placed", func(t *testing.T) {
		requireIn(t, "Cutover",
			"Invoicing (INV): 2 files",
			"legacy/invoices/InvoiceView.tsx (view)",
			"legacy/invoices/invoice_controller.rb (controller)",
			"Not placed in a context: 2 files",
			"legacy/db/migrations/README.md (no role)",
			"legacy/jobs/nightly_worker.rb (worker)")
		forbidIn(t, "Cutover", "Billing (BILL): 0 files")
		block := blocks["Cutover"]
		if context, notPlaced := strings.Index(block, "Invoicing (INV): 2 files"), strings.Index(block, "Not placed in a context:"); context < 0 || notPlaced < 0 ||
			strings.Index(block, "legacy/invoices/InvoiceView.tsx") < context || strings.Index(block, "legacy/jobs/nightly_worker.rb") < notPlaced {
			t.Errorf("the missed files are not listed under their context and then under not placed:\n%s", block)
		}
	})

	t.Run("the latest replays line with the changed, the captured once and the not captured replays", func(t *testing.T) {
		requireIn(t, "Cutover",
			"Latest replays, newest capture 2026-11-10: 1 of 2 match · 1 changed since the capture before",
			"Invoice totals replay: passed at 1a2b3c4 on 2026-11-01, then failed at 5d6e7f8 on 2026-11-10",
			"Credit note replay: captured once, passed at 5d6e7f8 on 2026-11-10",
			"Dunning replay: not captured yet")
	})

	t.Run("before the reference revision is set: no proved figure, the other figures stay", func(t *testing.T) {
		requireIn(t, "Renewal",
			"Proof is read once the reference revision is set.",
			"Drafted, not proved yet: 2 in a saved Epic plan",
			"Not drafted yet: 1 kept or changed, in no Epic yet",
			"Undecided: 2 in no Initiative yet, with 0 dropped")
		forbidIn(t, "Renewal", "Proved as agreed:", "Proved below the agreed level:", "%")
	})

	t.Run("a legacy system with no analysis and a Release with no captured replay give their sentences, never a zero", func(t *testing.T) {
		requireIn(t, "Renewal",
			// Epic 460 (H3), SR-5 criterion 0: the read's message in place of
			// the sentence that the analysis has not run yet.
			"Built, their Epic complete: 0 · accepted by a domain expert: 0 of 1 kept or changed user requirement · T has no analysed file yet.",
			"No replay has been captured in this Release yet.",
			"Partner feed replay: not captured yet")
		forbidIn(t, "Renewal", "0 legacy files", "Latest replays")
	})

	t.Run("nothing kept or changed: a sentence in place of the percent", func(t *testing.T) {
		requireIn(t, "Quiet",
			"Proved as agreed: 0 of 0 kept or changed, proved by runs captured after the change. No legacy requirement is kept or changed yet, so there is no percent.")
		forbidIn(t, "Quiet", "%")
	})

	t.Run("a deleted legacy system: the one sentence and nothing more", func(t *testing.T) {
		requireIn(t, "Sunset", "The legacy system was deleted, so its readiness can no longer be read.")
		forbidIn(t, "Sunset", "Proved as agreed:", "Proved below the agreed level:", "Proof is read once", "Drafted, not proved yet:", "Built, their Epic complete:", "Latest replays", "No replay has been captured")
	})

	t.Run("a Delivery Release prints none of the readiness", func(t *testing.T) {
		forbidIn(t, "Autumn", d4ReadinessLines...)
	})
}

func TestD4_6fd3853c_RoadmapShowWithoutReadinessSendsNoParameterAndPrintsNoneOfIt(t *testing.T) {
	server, seen := d4ReadinessServer(t, d4RoadmapReadinessReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	if len(*seen) != 1 || (*seen)[0].URL.Path != "/api/v1/systems/1/roadmap" || (*seen)[0].URL.RawQuery != "" {
		t.Fatalf("requests = %v; want one GET /api/v1/systems/1/roadmap with no query", d4RequestURLs(*seen))
	}
	for _, line := range d4ReadinessLines {
		if strings.Contains(out, line) {
			t.Errorf("roadmap show without --readiness prints %q:\n%s", line, out)
		}
	}
	// The lines roadmap show prints today stay (guard).
	for _, want := range []string{"Modernization · replacing Legacy billing", "Reference revision: a1b2c3d4", "Invoicing (INV): 0 of 4 undecided, 4 decided, 3 drafted, 2 built"} {
		if !strings.Contains(out, want) {
			t.Errorf("roadmap show lacks %q:\n%s", want, out)
		}
	}
}

func d4RequestURLs(requests []*http.Request) []*url.URL {
	urls := make([]*url.URL, 0, len(requests))
	for _, r := range requests {
		urls = append(urls, r.URL)
	}
	return urls
}

// d4BuiltSentence is what the help and the CLI guide say each built counts.
const d4BuiltSentence = "The block's Built counts the kept or changed requirements of the Initiatives shown under the Release, as Proved does, while the context lines' built counts over every decided row of the context."

func TestD4_6fd3853c_RoadmapShowHelpGivesReadinessAndWhatEachBuiltCounts(t *testing.T) {
	out, err := runRoot(t, "roadmap", "show", "--help")
	if err != nil {
		t.Fatalf("roadmap show --help: %v\n%s", err, out)
	}
	plain := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{"--readiness", "readiness=true", d4BuiltSentence} {
		if !strings.Contains(plain, want) {
			t.Errorf("the help of roadmap show does not say %q:\n%s", want, out)
		}
	}
}

func TestD4_6fd3853c_CLIGuideDescribesRoadmapShowReadiness(t *testing.T) {
	guide, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	section := d2GuideSection(string(guide), "roadmap show --readiness")
	if section == "" {
		t.Fatalf("the kit's CLI guide has no section that runs modernpath roadmap show --readiness")
	}
	plain := strings.Join(strings.Fields(strings.ReplaceAll(section, "`", "")), " ")
	for _, want := range []string{
		"readiness=true",
		"Proof is read once the reference revision is set.",
		"No legacy requirement is kept or changed yet, so there is no percent.",
		"what the ledger misses",
		"Latest replays",
		"The legacy system was deleted, so its readiness can no longer be read.",
		d4BuiltSentence,
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("the CLI guide's Roadmap section does not say %q", want)
		}
	}
}

// Epic 459 (H2), SR "One set of status and proof words in the command line",
// criterion 2 (fb3285c0): roadmap show --readiness prints Proved as agreed with
// the percent, Proved below the agreed level with how many were accepted by
// name, Drafted, not proved yet from drafted_not_proved_yet (no longer
// drafted_not_proved, which also counts the rows proved below the agreed
// level), Not drafted yet and Undecided, in that order. While the Release has
// no reference revision the read gives no proved_below, and no such line
// prints. The served read is the core's after this Epic (contract §4).
//
// At the Epic's base the block prints "Proved:" with "proved as well as
// expected", no Proved below the agreed level line, and Drafted, not proved
// yet from drafted_not_proved (2 where 1 is served), so the assertions fail.
// The test uses no new Go symbol.

// 2bab0b76-1edc-4212-9f66-5b61d452d930
func TestRoadmapShowReadinessPrintsProvedAsAgreedApartFromProvedBelowTheAgreedLevel(t *testing.T) {
	server, _ := d4ReadinessServer(t, d4RoadmapReadinessReply)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "show", "--readiness")
	if err != nil {
		t.Fatalf("roadmap show --readiness: %v\n%s", err, out)
	}
	blocks := d4ReleaseBlocks(t, out)

	t.Run("the five figures in order, with the rows below the agreed level apart", func(t *testing.T) {
		want := []string{
			"    Proved as agreed: 1 of 4 kept or changed, proved by runs captured after the change · 25%",
			"    Proved below the agreed level: 1 (1 accepted by name)",
			"    Drafted, not proved yet: 1 in a saved Epic plan",
			"    Not drafted yet: 1 kept or changed, in no Epic yet",
			"    Undecided: 3 in no Initiative yet, with 1 dropped",
		}
		block := blocks["Cutover"]
		at := -1
		for _, line := range want {
			next := strings.Index(block, line+"\n")
			if next < 0 {
				t.Errorf("the block of Cutover lacks the line %q:\n%s", line, block)
				continue
			}
			if next < at {
				t.Errorf("the line %q is out of order:\n%s", line, block)
			}
			at = next
		}
	})

	t.Run("no rows kept or changed: the count with none accepted", func(t *testing.T) {
		if !strings.Contains(blocks["Quiet"], "    Proved below the agreed level: 0 (0 accepted by name)\n") {
			t.Errorf("the block of Quiet lacks its Proved below the agreed level line:\n%s", blocks["Quiet"])
		}
	})

	t.Run("no reference revision: no proved figure of either kind, drafted from drafted_not_proved_yet", func(t *testing.T) {
		block := blocks["Renewal"]
		for _, absent := range []string{"Proved as agreed:", "Proved below the agreed level:"} {
			if strings.Contains(block, absent) {
				t.Errorf("the block of Renewal prints %q:\n%s", absent, block)
			}
		}
		if !strings.Contains(block, "    Drafted, not proved yet: 2 in a saved Epic plan\n") {
			t.Errorf("the block of Renewal lacks Drafted, not proved yet: 2:\n%s", block)
		}
	})

	for _, word := range []string{"as well as expected", "less well", "less than expected", "should reach", "Proved:"} {
		if strings.Contains(out, word) {
			t.Errorf("roadmap show --readiness says %q:\n%s", word, out)
		}
	}
}

// Epic 460 (H3), TestCase TC-H3-S9, SR-5 criterion 0: under a Modernization
// Release with no reference revision, roadmap show --readiness prints the
// step the reference revision waits for, from the read's
// modernization.progress.reference_revision.state: while no result is
// recorded (results_not_recorded), to record the legacy System's test results
// and then have a person choose; once results are recorded
// (waits_for_a_person), that a person chooses. A chosen revision, a read from
// an older server with no progress, and a Release whose legacy System was
// deleted print no Next line. In place of the sentence that the analysis has
// not run yet, the block prints the message of the read of what the ledger
// misses, which says why it cannot be read.
//
// Before the Epic the block prints no Next line and the fixed sentence for a
// ledger that is not analysed, so the assertions fail. The test uses no new Go
// symbol.

const h3NextRecord = "Next: record the legacy System's test results under this Release with modernpath modernization record (onto a new system with --legacy-checkout), then a person chooses the reference revision on the Release page's Modernization tab."
const h3NextChoose = "Next: a person chooses the reference revision on the Release page's Modernization tab."
const h3NoRepositoryMessage = "Legacy orders has no analysed source: no repository is linked to it. A hosted repository can be added with Add sources on its Administration tab; source from a local folder cannot be added to a System made in the web app yet."

// h3Modernization is a readiness block of the Roadmap read with nothing
// decided yet, the given ledger read and the given progress (raw JSON, or ""
// for a read without it).
func h3Modernization(ledger, progress string) string {
	body := `{"contexts":[],"proof_waits_for_reference_revision":true,
   "standing":{"drafted_not_proved":0,"drafted_not_proved_yet":0,"not_drafted":0,"undecided":2,"dropped":0,"kept_or_changed":0,"decided_elsewhere":0},
   "readiness":null,"built":0,"accepted":{"count":0,"of":0},
   "ledger_misses":` + ledger + `,
   "replays":{"captured":false,"message":"No replay has been captured in this Release yet.","replays":0,"with_result":0,"match":0,
    "newest":{"pass":0,"fail":0,"error":0,"skip":0,"inconclusive":0},"previous":{"pass":0,"fail":0,"error":0,"skip":0,"inconclusive":0},
    "newest_capture_at":null,"changed":[],"unchanged":[],"captured_once":[],"not_captured":[]}`
	if progress != "" {
		body += `,"progress":` + progress
	}
	return body + "}"
}

const h3LedgerAnalysed = `{"analysed":true,"message":null,"total":0,"contexts":[],"not_placed":{"count":0,"files":[]}}`

// h3RoadmapNextReply serves five Modernization Releases: Orders with no
// result recorded and no repository linked to its legacy System, Payments
// with results recorded, Chosen with its reference revision, Older from a
// server that gives no progress, and Reopened whose legacy System was deleted.
var h3RoadmapNextReply = `{"data":{"system_id":1,"plan_state":"agreed","contexts":[],
"releases":[
 {"id":31,"name":"Orders","status":"active","planning_context":{},"release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy orders","in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,
  "modernization":` + h3Modernization(`{"analysed":false,"reason":"no_repository","message":"`+h3NoRepositoryMessage+`","total":null,"contexts":[],"not_placed":null}`,
	`{"reference_revision":{"state":"results_not_recorded","revision":null,"chosen_by":null,"chosen_at":null,"taken_over_from":null,"taken_from":null}}`) + `,
  "initiatives":[]},
 {"id":32,"name":"Payments","status":"planned","planning_context":{},"release_type":"modernization","legacy_system_id":42,"legacy_system_name":"Legacy payments","in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,
  "modernization":` + h3Modernization(h3LedgerAnalysed,
	`{"reference_revision":{"state":"waits_for_a_person","revision":null,"chosen_by":null,"chosen_at":null,"taken_over_from":null,"taken_from":null}}`) + `,
  "initiatives":[]},
 {"id":33,"name":"Chosen","status":"planned","planning_context":{},"release_type":"modernization","legacy_system_id":43,"legacy_system_name":"Legacy stock","in_place":false,
  "legacy_scope":null,"legacy_reference_revision":"9f8e7d6c",
  "modernization":` + h3Modernization(h3LedgerAnalysed,
	`{"reference_revision":{"state":"chosen","revision":"9f8e7d6c","chosen_by":{"id":5,"name":"Pia Planner"},"chosen_at":"2026-11-20T10:00:00Z","taken_over_from":null,"taken_from":null}}`) + `,
  "initiatives":[]},
 {"id":34,"name":"Older","status":"planned","planning_context":{},"release_type":"modernization","legacy_system_id":44,"legacy_system_name":"Legacy ledger","in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,
  "modernization":` + h3Modernization(h3LedgerAnalysed, "") + `,
  "initiatives":[]},
 {"id":35,"name":"Reopened","status":"active","planning_context":{},"release_type":"modernization","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,
  "modernization":{"contexts":[],"progress":{"legacy_system_deleted":true}},
  "initiatives":[]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

// h3ReleaseBlocks cuts the output into each named Release's block, from its
// heading to the next heading, in the served order.
func h3ReleaseBlocks(t *testing.T, out string, names ...string) map[string]string {
	t.Helper()
	heads := make([]string, 0, len(names)+1)
	for _, name := range names {
		heads = append(heads, "Release "+name+" (")
	}
	heads = append(heads, "\nNo release")
	at := make([]int, len(heads))
	for i, head := range heads {
		at[i] = strings.Index(out, head)
		if at[i] < 0 || (i > 0 && at[i] < at[i-1]) {
			t.Fatalf("output lacks %q in the served order of the Releases:\n%s", head, out)
		}
	}
	blocks := map[string]string{}
	for i, name := range names {
		blocks[name] = out[at[i]:at[i+1]]
	}
	return blocks
}

func TestH3_RoadmapShowReadinessPrintsTheNextStepOfTheReferenceRevision(t *testing.T) {
	server, _ := d4ReadinessServer(t, h3RoadmapNextReply)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "show", "--readiness")
	if err != nil {
		t.Fatalf("roadmap show --readiness: %v\n%s", err, out)
	}
	blocks := h3ReleaseBlocks(t, out, "Orders", "Payments", "Chosen", "Older", "Reopened")

	t.Run("no result recorded: record the results, then a person chooses", func(t *testing.T) {
		block := blocks["Orders"]
		if !strings.Contains(block, "    "+h3NextRecord+"\n") {
			t.Errorf("the block of Orders lacks the line %q:\n%s", h3NextRecord, block)
		}
		if strings.Contains(block, h3NextChoose) {
			t.Errorf("the block of Orders prints the step once results are recorded:\n%s", block)
		}
		if strings.Index(block, "Readiness:") > strings.Index(block, "Next:") {
			t.Errorf("the Next line is not in the readiness block:\n%s", block)
		}
	})
	t.Run("results recorded: a person chooses", func(t *testing.T) {
		block := blocks["Payments"]
		if !strings.Contains(block, "    "+h3NextChoose+"\n") {
			t.Errorf("the block of Payments lacks the line %q:\n%s", h3NextChoose, block)
		}
		if strings.Contains(block, "record the legacy System's test results") {
			t.Errorf("the block of Payments still says to record the results:\n%s", block)
		}
	})
	t.Run("a chosen revision, an older server and a deleted legacy System print no Next line", func(t *testing.T) {
		for _, name := range []string{"Chosen", "Older", "Reopened"} {
			if strings.Contains(blocks[name], "Next:") {
				t.Errorf("the block of %s prints a Next line:\n%s", name, blocks[name])
			}
		}
	})
	t.Run("the ledger's message in place of the sentence that the analysis has not run yet", func(t *testing.T) {
		block := blocks["Orders"]
		want := "Built, their Epic complete: 0 · accepted by a domain expert: 0 of 0 kept or changed user requirements · " + h3NoRepositoryMessage + "\n"
		if !strings.Contains(block, want) {
			t.Errorf("the block of Orders lacks the line ending with the ledger's message %q:\n%s", want, block)
		}
		if strings.Contains(out, "which has not run yet") {
			t.Errorf("roadmap show --readiness still says the analysis has not run yet:\n%s", out)
		}
	})
}
