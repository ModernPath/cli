package rdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-286 (`USER:2026-08-27`): "all requirements comes as orphan".
//
// `rdd-reverse-engineer` writes proposed relations as prose in the Candidate
// packet. Nothing read that shape, so a whole derived estate synced with no
// edges: 126 orphan URs and 778 orphan SRs.
func TestPacketRelationsReadsWhatTheReverseEngineeringPassWrites(t *testing.T) {
	cases := []struct {
		name     string
		detail   string
		serves   []string
		requires []string
	}{
		{
			name:     "requires names the children",
			detail:   "- **Candidate packet:** Inference sources — CODE:x.cs. Proposed relations (CANDIDATE): requires SR-KERNEL-030, SR-KERNEL-031, SR-KERNEL-032. Conflict — none.",
			requires: []string{"SR-KERNEL-030", "SR-KERNEL-031", "SR-KERNEL-032"},
		},
		{
			name:   "serves names the parent",
			detail: "Proposed relations (CANDIDATE): serves UR-KERNEL-002.",
			serves: []string{"UR-KERNEL-002"},
		},
		{
			name:     "an elided list keeps both ends",
			detail:   "Proposed relations (CANDIDATE): requires SR-ACC-010 … SR-ACC-015.",
			requires: []string{"SR-ACC-010", "SR-ACC-015"},
		},
		{
			// The narrowness that makes this safe: a requirement MENTIONED in
			// prose is a citation, and inventing an edge from it would put a
			// wrong parent on a row nobody can see is wrong.
			name:   "prose that merely mentions an id declares nothing",
			detail: "- **Candidate packet:** Conflict — SR-KERNEL-033 records that the profile short-circuits to full rights.",
		},
		{
			name:   "no packet at all",
			detail: "- **Status:** DONE\n- **Statement:** anything.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serves, requires := PacketRelations(tc.detail)
			if strings.Join(serves, ",") != strings.Join(tc.serves, ",") {
				t.Errorf("serves = %v, want %v", serves, tc.serves)
			}
			if strings.Join(requires, ",") != strings.Join(tc.requires, ",") {
				t.Errorf("requires = %v, want %v", requires, tc.requires)
			}
		})
	}
}

func TestApplyPacketRelationsFillsOnlyEmptyParents(t *testing.T) {
	reqs := []Req{
		{ID: "UR-K-001", Detail: "Proposed relations (CANDIDATE): requires SR-K-010, SR-K-011."},
		{ID: "SR-K-010"},
		// A parent declared in a dedicated field is an author's statement; a
		// packet sentence elsewhere must not overwrite it.
		{ID: "SR-K-011", UR: "UR-K-999"},
		{ID: "SR-K-012", Detail: "Proposed relations (CANDIDATE): serves UR-K-001."},
		{ID: "SR-K-013", Detail: "Proposed relations (CANDIDATE): requires SR-GONE-001."},
	}

	filled, unresolved, unrepresentable := ApplyPacketRelations(reqs)

	if reqs[1].UR != "UR-K-001" {
		t.Errorf("SR-K-010 parent = %q, want UR-K-001", reqs[1].UR)
	}
	if reqs[2].UR != "UR-K-999" {
		t.Errorf("SR-K-011 parent = %q — a declared parent was overwritten", reqs[2].UR)
	}
	if reqs[3].UR != "UR-K-001" {
		t.Errorf("SR-K-012 parent = %q, want UR-K-001 (serves)", reqs[3].UR)
	}
	if filled != 2 {
		t.Errorf("filled = %d, want 2", filled)
	}
	if len(unresolved) != 1 || unresolved[0] != "SR-GONE-001" {
		t.Errorf("unresolved = %v, want [SR-GONE-001] — a dangling id is a corpus defect and must be said", unresolved)
	}
	if len(unrepresentable) != 0 {
		t.Errorf("unrepresentable = %v, want none here", unrepresentable)
	}
}

// A parent is only representable on a system requirement. Six fills on the
// estate that found this landed on UR rows and vanished at payload build, so
// the reported count exceeded what actually synced (`RUN:2026-08-27`).
func TestApplyPacketRelationsReportsWhatItCannotCarry(t *testing.T) {
	reqs := []Req{
		{ID: "UR-K-001", Detail: "Proposed relations (CANDIDATE): requires UR-K-002."},
		{ID: "UR-K-002"},
	}

	filled, _, unrepresentable := ApplyPacketRelations(reqs)

	if filled != 0 {
		t.Errorf("filled = %d — a UR parent cannot be synced and must not be counted", filled)
	}
	if reqs[1].UR != "" {
		t.Errorf("UR-K-002 parent = %q — assigning it only to drop it later is the silent truncation the rule forbids", reqs[1].UR)
	}
	if len(unrepresentable) != 1 || unrepresentable[0] != "UR-K-002→UR-K-001" {
		t.Errorf("unrepresentable = %v, want [UR-K-002→UR-K-001]", unrepresentable)
	}
}

// The other half: an SR naming `requires SR-x` is an SR→SR dependency. It is a
// real declaration, and the payload has no field for it. Six of these were
// assigned and then dropped at payload build on the estate that found this.
func TestApplyPacketRelationsRefusesSrToSrParentage(t *testing.T) {
	reqs := []Req{
		{ID: "SR-ACC-010", Detail: "Proposed relations (CANDIDATE): requires SR-ACC-049."},
		{ID: "SR-ACC-049"},
	}

	filled, _, unrepresentable := ApplyPacketRelations(reqs)

	if filled != 0 || reqs[1].UR != "" {
		t.Errorf("filled=%d parent=%q — an SR parent cannot be synced; assigning it only hides the loss", filled, reqs[1].UR)
	}
	if len(unrepresentable) != 1 || unrepresentable[0] != "SR-ACC-049→SR-ACC-010" {
		t.Errorf("unrepresentable = %v, want [SR-ACC-049→SR-ACC-010]", unrepresentable)
	}
}

// The durable half of REQ-CROSS-286. Three corpora have now been fully
// orphaned by the same failure: a pass wrote parents in a shape the reader did
// not parse (REQ-CROSS-076 the Source cell, SR-SY-1402 the `**UR:**` bullet,
// this one the Candidate packet). Each was fixed in isolation, and the gap
// reopened the next time someone wrote a new shape.
//
// So the skill now declares its serialization in one place, and this test
// fails if a shape appears there with no reader behind it. A new relation form
// cannot reach a client's corpus without a parser arriving in the same change.
func TestSkillRelationShapesAllHaveReaders(t *testing.T) {
	root := repoRoot(t)
	skill := filepath.Join(root, ".modernpath", "rdd", "skills", "rdd-reverse-engineer", "SKILL.md")

	content, err := os.ReadFile(skill)
	if err != nil {
		t.Skipf("skill not installed here: %v", err)
	}

	block := section(string(content), "## Relation serialization")
	if block == "" {
		t.Fatal("rdd-reverse-engineer must declare its relation serialization under '## Relation serialization' — the writer and the reader are one contract")
	}

	// Every fenced example in that section must parse to at least one id.
	examples := fencedExamples(block)
	if len(examples) == 0 {
		t.Fatal("the relation-serialization section declares no example; a contract with no example is not checkable")
	}
	for _, ex := range examples {
		serves, requires := PacketRelations(ex)
		ur := urFromDetail(ex)
		if len(serves) == 0 && len(requires) == 0 && ur == "" {
			t.Errorf("the skill prescribes a relation shape no reader parses:\n%s", ex)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".modernpath", "rdd")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("not inside a workspace carrying .modernpath/rdd")
	return ""
}

func section(content, heading string) string {
	i := strings.Index(content, heading)
	if i < 0 {
		return ""
	}
	rest := content[i+len(heading):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

func fencedExamples(block string) []string {
	var out []string
	parts := strings.Split(block, "```")
	for i := 1; i < len(parts); i += 2 {
		body := parts[i]
		if k := strings.Index(body, "\n"); k >= 0 {
			body = body[k+1:]
		}
		if s := strings.TrimSpace(body); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// The clause boundary is the whole of "prose is a citation, not a relation".
// The existing prose case uses a detail with no verb in it, so it stays silent
// whether or not the packet clause is honoured — it proves the verbs are
// needed, not that the CLAUSE is. These fixtures carry the verb outside the
// clause, which is the shape a reverse-engineering pass actually produces:
// statements and conflicts routinely say "requires" about a neighbouring row.
func TestOnlyTheClauseDeclaresARelation(t *testing.T) {
	for _, tc := range []struct{ name, detail string }{
		{
			name:   "a statement using the word requires declares nothing",
			detail: "- **Statement:** Reading a profile requires SR-KERNEL-030 to have loaded the rights table first.",
		},
		{
			name: "a verb after the clause ends is outside it",
			detail: "- **Candidate packet:** Proposed relations (CANDIDATE): none. " +
				"Consequence — the caller requires SR-KERNEL-031 and serves UR-KERNEL-002 in the same breath.",
		},
		{
			name:   "a verb on a later line is outside it",
			detail: "Proposed relations (CANDIDATE): none.\nConflict — this requires SR-KERNEL-032.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serves, requires := PacketRelations(tc.detail)
			if len(serves) > 0 || len(requires) > 0 {
				t.Errorf("serves=%v requires=%v — an id mentioned outside the packet clause is a citation; "+
					"inventing an edge from it puts a wrong parent on a row nobody can see is wrong",
					serves, requires)
			}
		})
	}
}

// Everything below covers the `serves` arm of ApplyPacketRelations. Its three
// guards are the same three the `requires` arm has, and only the `requires` arm
// was tested — so each of these rules held on one path and was unenforced on
// the other.
func TestServesNeverOverwritesADeclaredParent(t *testing.T) {
	reqs := []Req{
		{ID: "UR-K-001"},
		{ID: "UR-K-002"},
		// The author stated this row's parent in a dedicated field. A packet
		// sentence must not win against it, whichever verb it uses.
		{ID: "SR-K-010", UR: "UR-K-002", Detail: "Proposed relations (CANDIDATE): serves UR-K-001."},
	}

	filled, _, _ := ApplyPacketRelations(reqs)

	if reqs[2].UR != "UR-K-002" {
		t.Errorf("SR-K-010 parent = %q, want UR-K-002 — a packet `serves` overwrote a parent declared in a field", reqs[2].UR)
	}
	if filled != 0 {
		t.Errorf("filled = %d, want 0", filled)
	}
}

func TestServesReportsAnIdNoRowCarries(t *testing.T) {
	reqs := []Req{
		{ID: "SR-K-010", Detail: "Proposed relations (CANDIDATE): serves UR-GONE-001."},
	}

	filled, unresolved, _ := ApplyPacketRelations(reqs)

	if filled != 0 || reqs[0].UR != "" {
		t.Errorf("filled=%d parent=%q — a parent that matches no row must not be assigned", filled, reqs[0].UR)
	}
	if len(unresolved) != 1 || unresolved[0] != "UR-GONE-001" {
		t.Errorf("unresolved = %v, want [UR-GONE-001] — naming an id no row carries is a corpus defect "+
			"and is reported, never dropped", unresolved)
	}
}

func TestServesRefusesAnSrParent(t *testing.T) {
	reqs := []Req{
		{ID: "SR-ACC-010", Detail: "Proposed relations (CANDIDATE): serves SR-ACC-049."},
		{ID: "SR-ACC-049"},
	}

	filled, _, unrepresentable := ApplyPacketRelations(reqs)

	if filled != 0 || reqs[0].UR != "" {
		t.Errorf("filled=%d parent=%q — parent_external_id carries an SR under a UR and nothing else; "+
			"assigning an SR parent only hides the loss at payload build", filled, reqs[0].UR)
	}
	if len(unrepresentable) != 1 || unrepresentable[0] != "SR-ACC-010→SR-ACC-049" {
		t.Errorf("unrepresentable = %v, want [SR-ACC-010→SR-ACC-049]", unrepresentable)
	}
}
