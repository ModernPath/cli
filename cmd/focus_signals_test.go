package cmd

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// REQ-PLN-135 §135.2/§135.3 (EPIC-NEXT-005) + REQ-PLN-143 §143.1 (EPIC-NEXT-009):
// the local, gitignored, refs-only buffer .modernpath/focus-signals and the
// hysteresis rule that runs over it — a ref is concluded when it appears in ≥ 2
// of the last five ref-bearing signals. Multi-lane (§143.1) makes the rule
// per-ref: EVERY qualifying ref not already a current lane is concluded (not just
// the max), and keep-current / declared-wins suppress only their own ref, so
// several lanes conclude independently. Supersedes §135.3's single-current rule
// (recorded in EPIC-NEXT-009 §Supersession).

func TestFocusBufferRefsOnlyAndTrimsToFive(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)

	// an observation with no ref writes nothing
	if err := appendFocusSignal(root, "prompt", nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(focusSignalsPath(root)); err == nil {
		t.Fatal("a ref-less observation must not create the buffer")
	}

	refs := []string{"REQ-A-1", "REQ-A-2", "REQ-A-3", "REQ-A-4", "REQ-A-5", "REQ-A-6", "REQ-A-7"}
	for i, r := range refs {
		if err := appendFocusSignal(root, "commit", []string{r}, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	buf, err := loadFocusBuffer(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(buf.signals) != 5 {
		t.Fatalf("kept %d signals, want the last 5", len(buf.signals))
	}
	if got := buf.signals[0].refs[0]; got != "REQ-A-3" {
		t.Fatalf("oldest kept = %s, want REQ-A-3 (REQ-A-1/2 dropped)", got)
	}

	// refs only — a prompt sentinel never reaches the file, the extracted ref does
	if err := appendFocusSignal(root, "prompt", ExtractRefs("do REQ-PLN-135 now secret=sentinel-9f3"), now); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(focusSignalsPath(root))
	if strings.Contains(string(raw), "sentinel-9f3") {
		t.Fatal("prompt text leaked into the refs-only buffer")
	}
	if !strings.Contains(string(raw), "REQ-PLN-135") {
		t.Fatal("the extracted ref should be recorded")
	}
}

func TestFocusHysteresis(t *testing.T) {
	now := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)

	type step struct {
		source     string
		ref        string
		concludes  []string // refs expected concluded this step (nil = none)
		fromSource string   // when exactly one is expected, its source (optional)
	}

	run := func(t *testing.T, initial []focusLane, steps []step) {
		t.Helper()
		root := t.TempDir()
		if len(initial) > 0 {
			if err := setFocusLanes(root, initial); err != nil {
				t.Fatal(err)
			}
		}
		for i, s := range steps {
			if err := appendFocusSignal(root, s.source, []string{s.ref}, now.Add(time.Duration(i)*time.Minute)); err != nil {
				t.Fatal(err)
			}
			got := decideFocus(root)

			gotRefs := make([]string, len(got))
			for j, c := range got {
				gotRefs[j] = c.ref
			}
			sort.Strings(gotRefs)
			want := append([]string(nil), s.concludes...)
			sort.Strings(want)
			if strings.Join(gotRefs, ",") != strings.Join(want, ",") {
				t.Fatalf("step %d (%s %s): concluded %v, want %v", i, s.source, s.ref, gotRefs, want)
			}
			if s.fromSource != "" {
				if len(got) != 1 || got[0].source != s.fromSource {
					t.Fatalf("step %d: conclusions %+v, want one from %q", i, got, s.fromSource)
				}
			}
			// the CLI records each conclusion (post-success) so its ref is held next
			for _, c := range got {
				if err := mergeFocusLane(root, c.ref, "inferred", c.source, now); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	t.Run("one sighting concludes nothing", func(t *testing.T) {
		run(t, nil, []step{{"prompt", "REQ-A-1", nil, ""}})
	})

	t.Run("prompt A · branch X · commit A: A at the third, source commit", func(t *testing.T) {
		run(t, nil, []step{
			{"prompt", "REQ-A-1", nil, ""},
			{"branch", "EPIC-X-1", nil, ""},
			{"commit", "REQ-A-1", []string{"REQ-A-1"}, "commit"},
		})
	})

	t.Run("A B A B: A at the third, B at the fourth — independent lanes (§143.1)", func(t *testing.T) {
		// under EPIC-NEXT-005 keep-current suppressed B once A was current; under
		// multi-lane B concludes its own lane while A is held.
		run(t, nil, []step{
			{"prompt", "REQ-A-1", nil, ""},
			{"prompt", "REQ-B-1", nil, ""},
			{"prompt", "REQ-A-1", []string{"REQ-A-1"}, ""},
			{"prompt", "REQ-B-1", []string{"REQ-B-1"}, ""},
		})
	})

	t.Run("A A A B B B B: A at the second, B at the fifth — both hold, per ref", func(t *testing.T) {
		run(t, nil, []step{
			{"prompt", "REQ-A-1", nil, ""},
			{"prompt", "REQ-A-1", []string{"REQ-A-1"}, ""},
			{"prompt", "REQ-A-1", nil, ""},
			{"prompt", "REQ-B-1", nil, ""},
			{"prompt", "REQ-B-1", []string{"REQ-B-1"}, ""},
			{"prompt", "REQ-B-1", nil, ""},
			{"prompt", "REQ-B-1", nil, ""},
		})
	})

	t.Run("a declared lane suppresses only its own ref (per-ref short-circuit)", func(t *testing.T) {
		// REQ-A-1 is declared → never inferred; REQ-B-1 still concludes its lane.
		run(t, []focusLane{{ref: "REQ-A-1", setBy: "declared"}}, []step{
			{"branch", "REQ-A-1", nil, ""},
			{"commit", "REQ-A-1", nil, ""},
			{"branch", "REQ-B-1", nil, ""},
			{"commit", "REQ-B-1", []string{"REQ-B-1"}, ""},
		})
	})
}

// TestFocusConcludesEveryQualifyingRef is the headline §143.1 change: two refs
// each at the threshold in the window conclude TWO lanes in one decideFocus call
// (EPIC-NEXT-005 returned only the single highest-count ref). RED against a
// max-only decideFocus.
func TestFocusConcludesEveryQualifyingRef(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)

	seq := []struct{ src, ref string }{
		{"prompt", "REQ-A-1"}, {"prompt", "REQ-B-1"},
		{"commit", "REQ-A-1"}, {"branch", "REQ-B-1"},
	}
	for i, s := range seq {
		if err := appendFocusSignal(root, s.src, []string{s.ref}, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	got := decideFocus(root)
	bySource := map[string]string{}
	for _, c := range got {
		bySource[c.ref] = c.source
	}
	if len(got) != 2 {
		t.Fatalf("concluded %+v, want both REQ-A-1 and REQ-B-1 (two lanes)", got)
	}
	// each conclusion carries the source of the newest signal naming its ref
	if bySource["REQ-A-1"] != "commit" || bySource["REQ-B-1"] != "branch" {
		t.Fatalf("sources = %v, want REQ-A-1/commit REQ-B-1/branch", bySource)
	}
}

// TestLoadFocusBufferBackwardCompatSingleLine: an EPIC-NEXT-005 single-line
// `current <ref> <set_by>` buffer still loads as one lane, and that lane is held
// (declared-wins/keep-current) so a stale buffer never re-posts its ref (§143.1).
func TestLoadFocusBufferBackwardCompatSingleLine(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)

	// create the .modernpath dir, then plant an old single-line buffer over it
	if err := appendFocusSignal(root, "seed", []string{"REQ-SEED-0"}, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(focusSignalsPath(root), []byte("current REQ-OLD-1 inferred\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	buf, err := loadFocusBuffer(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(buf.current) != 1 || buf.current[0].ref != "REQ-OLD-1" || buf.current[0].setBy != "inferred" {
		t.Fatalf("loaded current = %+v, want one REQ-OLD-1/inferred lane from the old buffer", buf.current)
	}

	// the old lane is held → its ref is suppressed while a new ref concludes
	for i, ref := range []string{"REQ-OLD-1", "REQ-OLD-1", "REQ-NEW-2", "REQ-NEW-2"} {
		if err := appendFocusSignal(root, "prompt", []string{ref}, now.Add(time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got := decideFocus(root)
	if len(got) != 1 || got[0].ref != "REQ-NEW-2" {
		t.Fatalf("concluded %+v, want only REQ-NEW-2 (REQ-OLD-1 held from the old buffer)", got)
	}
}
