package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	questionHeadRe = regexp.MustCompile(`^##\s+(Q-[A-Z]+-\d+)\b(.*)$`)
	titleSepRe     = regexp.MustCompile(`^\s*[—–-]\s*\S`)
)

// CheckRegisterHeadings — REQ-CROSS-128: every question in the register has a
// title and a unique id.
//
// The extractor parses `## Q-<CTX>-<NNN> — <title>`. Both failures this guards
// were made in the same session that wrote them up as rules:
//
//   - **No title.** `## Q-ARCH-013` splits into id "Q-ARCH" and title "013", so
//     two such questions collide on ONE gate and the second overwrites the first.
//   - **Duplicate id.** Two headings claiming one id make the extractor's choice
//     arbitrary — and if the one it picks has a resolution word in its title, an
//     open decision syncs as answered (REQ-CROSS-168).
//
// Neither produces an error anywhere else: the file reads correctly, the sync
// reports success, and the queue quietly holds the wrong thing.
func CheckRegisterHeadings(root string) ([]Violation, error) {
	rel := filepath.Join("process", "08-open-questions.md")
	body, err := os.ReadFile(filepath.Join(root, rel))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []Violation
	seen := map[string]int{}
	inFence := false
	for i, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := questionHeadRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, rest := m[1], m[2]
		if !titleSepRe.MatchString(rest) {
			out = append(out, Violation{
				Rule: "malformed-question-heading", File: rel, Subject: id,
				Detail: fmt.Sprintf("line %d: %s has no title — the id parses as %q and questions collide on one gate", i+1, id, truncateID(id)),
			})
		}
		seen[id]++
		if seen[id] == 2 {
			out = append(out, Violation{
				Rule: "duplicate-question-heading", File: rel, Subject: id,
				Detail: fmt.Sprintf("line %d: %s has a second heading — which one the extractor uses, and whether it looks resolved, is arbitrary", i+1, id),
			})
		}
	}
	return out, nil
}

// truncateID shows what a title-less heading actually syncs as: the id up to its
// last hyphen, with the number becoming the title.
func truncateID(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}
