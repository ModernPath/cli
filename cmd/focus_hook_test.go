package cmd

import (
	"os"
	"strings"
	"testing"
)

// REQ-PLN-135 §135.4/§135.9 (EPIC-NEXT-005): the context hook records a
// refs-only prompt signal locally and spawns a detached `focus --infer` only
// when the rule concludes — the prompt text never reaches the buffer, and a
// prompt that concludes nothing makes no focus request at all. Factory sync
// learns the server's current focus from the heartbeat echo.

func TestHookRecordsPromptRefSignalAndSpawnsOnlyOnConclusion(t *testing.T) {
	root := t.TempDir()

	var spawned [][2]string
	orig := focusInferSpawn
	focusInferSpawn = func(ref, source string) { spawned = append(spawned, [2]string{ref, source}) }
	defer func() { focusInferSpawn = orig }()

	// one sighting concludes nothing → no spawn, no request
	recordFocusSignalFromPrompt(root, "let's finish REQ-PLN-135 — the token is sentinel-XYZ")
	if len(spawned) != 0 {
		t.Fatalf("one sighting must not conclude, spawned %v", spawned)
	}

	// the prompt text (sentinel) never reached the refs-only buffer
	raw, _ := os.ReadFile(focusSignalsPath(root))
	if strings.Contains(string(raw), "sentinel-XYZ") {
		t.Fatal("prompt text leaked into the buffer")
	}
	if !strings.Contains(string(raw), "REQ-PLN-135") {
		t.Fatal("the extracted ref should be recorded")
	}

	// a second sighting confirms → exactly one detached conclusion
	recordFocusSignalFromPrompt(root, "still on REQ-PLN-135")
	if len(spawned) != 1 || spawned[0] != [2]string{"REQ-PLN-135", "prompt"} {
		t.Fatalf("spawned = %v, want one REQ-PLN-135/prompt", spawned)
	}

	// a prompt with no ref records nothing and never spawns
	before := len(spawned)
	recordFocusSignalFromPrompt(root, "how do I center a div in CSS?")
	if len(spawned) != before {
		t.Fatalf("a ref-less prompt must not spawn, spawned %v", spawned)
	}
}

func TestLearnFocusCurrentFromHeartbeatEcho(t *testing.T) {
	root := t.TempDir()

	// the echo carries the caller's current focus LANES (an array, REQ-PLN-143
	// §143.1) → the buffer learns them per ref
	learnFocusCurrentFromEcho(root, map[string]any{
		"data": map[string]any{
			"focus": []any{
				map[string]any{"ref_external_id": "REQ-PLN-133", "set_by": "declared", "source": "web"},
				map[string]any{"ref_external_id": "REQ-PLN-142", "set_by": "inferred", "source": "branch"},
			},
		},
	})
	buf, _ := loadFocusBuffer(root)
	byRef := map[string]focusLane{}
	for _, lane := range buf.current {
		byRef[lane.ref] = lane
	}
	if len(buf.current) != 2 || byRef["REQ-PLN-133"].setBy != "declared" || byRef["REQ-PLN-142"].setBy != "inferred" {
		t.Fatalf("current = %+v, want REQ-PLN-133/declared + REQ-PLN-142/inferred", buf.current)
	}

	// an empty focus array → the current set is cleared
	learnFocusCurrentFromEcho(root, map[string]any{"data": map[string]any{"focus": []any{}}})
	buf, _ = loadFocusBuffer(root)
	if len(buf.current) != 0 {
		t.Fatalf("current = %+v, want cleared", buf.current)
	}

	// a null focus (no array) also clears — the array is the only expected shape
	learnFocusCurrentFromEcho(root, map[string]any{"data": map[string]any{"focus": nil}})
	buf, _ = loadFocusBuffer(root)
	if len(buf.current) != 0 {
		t.Fatalf("current = %+v, want cleared on null focus", buf.current)
	}
}
