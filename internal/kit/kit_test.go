package kit

import (
	"strings"
	"testing"
)

// REQ-CROSS-029: `modernpath install` must refresh the block that points
// non-Claude agents at the process WITHOUT touching anything the client wrote.
// AGENTS.md is the one file the install shares with its owner, so the merge is
// the seam that decides whether upgrading is safe.
func TestMergeManagedBlockInsertsAfterTheTitle(t *testing.T) {
	doc := "# AGENTS.md — acme\n\nOur stack is Rails.\n"
	got, err := MergeManagedBlock(doc, "Read the process first.")
	if err != nil {
		t.Fatal(err)
	}
	want := "# AGENTS.md — acme\n\n" + BeginMarker + "\nRead the process first.\n" + EndMarker + "\n\nOur stack is Rails.\n"
	if got != want {
		t.Fatalf("insert wrong:\n got: %q\nwant: %q", got, want)
	}
}

func TestMergeManagedBlockReplacesInPlaceAndKeepsClientContent(t *testing.T) {
	doc := "# acme\n\n" + BeginMarker + "\nOLD TEXT\n" + EndMarker + "\n\nOur stack is Rails.\nDeploy with make ship.\n"
	got, err := MergeManagedBlock(doc, "NEW TEXT")
	if err != nil {
		t.Fatal(err)
	}
	want := "# acme\n\n" + BeginMarker + "\nNEW TEXT\n" + EndMarker + "\n\nOur stack is Rails.\nDeploy with make ship.\n"
	if got != want {
		t.Fatalf("replace wrong:\n got: %q\nwant: %q", got, want)
	}
}

// Upgrading twice must not drift the file — otherwise every install shows a diff
// and clients stop trusting that the tool leaves their content alone.
func TestMergeManagedBlockIsIdempotent(t *testing.T) {
	doc := "# acme\n\nStack: Rails.\n"
	once, err := MergeManagedBlock(doc, "Read the process first.")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := MergeManagedBlock(once, "Read the process first.")
	if err != nil {
		t.Fatal(err)
	}
	if once != twice {
		t.Fatalf("not idempotent:\n once: %q\ntwice: %q", once, twice)
	}
}

func TestMergeManagedBlockHandlesNoTitle(t *testing.T) {
	got, err := MergeManagedBlock("Stack: Rails.\n", "Read the process first.")
	if err != nil {
		t.Fatal(err)
	}
	want := BeginMarker + "\nRead the process first.\n" + EndMarker + "\n\nStack: Rails.\n"
	if got != want {
		t.Fatalf("no-title case wrong:\n got: %q\nwant: %q", got, want)
	}
	// a file that does not exist yet is the same as an empty one
	fresh, err := MergeManagedBlock("", "Read the process first.")
	if err != nil {
		t.Fatal(err)
	}
	if fresh != BeginMarker+"\nRead the process first.\n"+EndMarker+"\n" {
		t.Fatalf("empty-doc case wrong: %q", fresh)
	}
}

// A half-written marker pair means someone edited by hand or an install was
// interrupted. Refuse rather than guess — silently rewriting could delete the
// client's work, which is the one outcome that must never happen.
func TestMergeManagedBlockRefusesMalformedMarkers(t *testing.T) {
	for name, doc := range map[string]string{
		"begin without end": "# acme\n" + BeginMarker + "\nhalf\n",
		"end without begin": "# acme\n" + EndMarker + "\n",
		"end before begin":  "# acme\n" + EndMarker + "\nx\n" + BeginMarker + "\n",
	} {
		if _, err := MergeManagedBlock(doc, "NEW"); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

// With two blocks present only the first was
// refreshed, leaving the second a permanently stale orphan that silently
// contradicts the first after any process change. Ambiguous ownership is the
// damaged-markers case.
func TestMergeManagedBlockRefusesDuplicateBlocks(t *testing.T) {
	doc := "# T\n\n" +
		BeginMarker + "\nold\n" + EndMarker + "\n\nclient rule\n\n" +
		BeginMarker + "\nold\n" + EndMarker + "\n"
	if _, err := MergeManagedBlock(doc, "new"); err == nil {
		t.Fatal("a document with two managed blocks must be refused, not half-refreshed")
	}
}

// REQ-CROSS-478 (EPIC-TOOL-CLEANUP): the three places the CLI installs that
// tell an agent where codebase knowledge comes from state one local-first
// order — a repository search for the requirement id, code and tests; the
// working-set pull; the docs export — and `modernpath ask` only for the why
// and how the local search cannot answer. The skill once said only "rg the
// export", so an agent went to the export before the code it was changing.
func TestKnowledgeAssetsStateOneLocalFirstOrder(t *testing.T) {
	const anchor = "Search locally first"
	steps := []string{
		"requirement id",
		"working-set pull",
		"docs export",
		"modernpath ask",
	}
	for _, asset := range []string{
		"assets/skills/mp-knowledge-search/SKILL.md",
		"assets/agents-block.md",
		"assets/skills/mp-process-cli/SKILL.md",
	} {
		b, err := assets.ReadFile(asset)
		if err != nil {
			t.Fatalf("%s: %v", asset, err)
		}
		text := strings.Join(strings.Fields(string(b)), " ")
		at := strings.Index(text, anchor)
		if at < 0 {
			t.Errorf("%s does not say %q", asset, anchor)
			continue
		}
		rest := text[at:]
		last := -1
		for _, step := range steps {
			i := strings.Index(rest, step)
			if i < 0 {
				t.Errorf("%s: the local-first order does not name %q", asset, step)
				break
			}
			if i < last {
				t.Errorf("%s: %q comes before an earlier step of the local-first order", asset, step)
				break
			}
			last = i
		}
	}
}
