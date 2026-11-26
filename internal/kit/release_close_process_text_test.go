package kit

import (
	"strings"
	"testing"
)

// Epic 530 (E1c), test case df9463a9, criterion 8b15161d: PROCESS.md, which
// the kit embeds, names in its section Human approvals, in a paragraph of its
// own after the paragraph on human completion approval, the close of an active
// in-place Modernization Release as the human act outside an Epic that ends
// published behavior, with its three exceptions; and that section's sentence
// on what completion approval publishes begins "Within an Epic". The file
// wraps its sentences, so each line break is read as a space. The installed
// copy (.modernpath/rdd/PROCESS.md) is compared with this asset byte for byte
// by TestInstalledFilesMatchTheirSource and by `modernpath install --check`.

const e1cProcessParagraph = "Outside an Epic, one more human act ends published behavior: the close of an active in-place Modernization Release, by the person who closes it, sets to `OBSOLETE` each published requirement that an Initiative of that Release, done apart from proof when the close plan is read, dropped, unless active work owns it, a small change is open for it or it is unfinished work of that Release. The close page lists each one before the close, and the close is refused unless its request names each one. The close raises each one's revision once and keeps an immutable version of kind `release_close` with that person, the time and the drop's reason."

// e1cHumanApprovals is the section Human approvals of the kit's PROCESS.md,
// split into paragraphs, each with its line breaks read as spaces.
func e1cHumanApprovals(t *testing.T) []string {
	t.Helper()
	body, err := assets.ReadFile("assets/rdd/PROCESS.md")
	if err != nil {
		t.Fatalf("the kit must embed assets/rdd/PROCESS.md: %v", err)
	}
	text := string(body)
	start := strings.Index(text, "\n## Human approvals\n")
	if start < 0 {
		t.Fatal("PROCESS.md has no section Human approvals")
	}
	section := text[start+len("\n## Human approvals\n"):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	var paragraphs []string
	for _, p := range strings.Split(section, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			paragraphs = append(paragraphs, strings.ReplaceAll(p, "\n", " "))
		}
	}
	return paragraphs
}

// df9463a9-769c-4072-a9e3-109d2f719541
func TestE1c_df9463a9_HumanApprovalsNamesTheInPlaceCloseInAParagraphOfItsOwn(t *testing.T) {
	paragraphs := e1cHumanApprovals(t)
	completion, own := -1, -1
	for i, p := range paragraphs {
		if strings.HasPrefix(p, "Human completion approval is separate.") {
			completion = i
		}
		if p == e1cProcessParagraph {
			own = i
		}
	}
	if completion < 0 {
		t.Fatal("Human approvals has no paragraph on human completion approval")
	}
	if own < 0 {
		t.Fatalf("Human approvals has no paragraph of its own that reads %q", e1cProcessParagraph)
	}
	if own <= completion {
		t.Errorf("the in-place close paragraph (paragraph %d) must come after the paragraph on human completion approval (paragraph %d)", own+1, completion+1)
	}
}

// df9463a9-769c-4072-a9e3-109d2f719541
func TestE1c_df9463a9_HumanApprovalsSaysWithinAnEpicOnlyThatApprovalPublishes(t *testing.T) {
	section := strings.Join(e1cHumanApprovals(t), " ")
	if !strings.Contains(section, "Within an Epic, only that approval publishes the approved requirement content") {
		t.Error(`Human approvals has no sentence that begins "Within an Epic, only that approval publishes the approved requirement content"`)
	}
	if strings.Contains(section, "Only that approval publishes the approved requirement content") {
		t.Error(`Human approvals still has a sentence that begins "Only that approval publishes the approved requirement content"`)
	}
}
