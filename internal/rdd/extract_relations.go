package rdd

import (
	"regexp"
	"sort"
	"strings"
)

// REQ-CROSS-286: the fourth parent shape — the one `rdd-reverse-engineer`
// itself writes. That pass records proposed relations as prose inside the
// Candidate packet:
//
//	Proposed relations (CANDIDATE): requires SR-KERNEL-030, SR-KERNEL-031.
//	Proposed relations (CANDIDATE): serves UR-KERNEL-002.
//
// Nothing read it, so an entire derived corpus synced as orphans — 126 orphan
// URs and 778 orphan SRs on the estate that found this (`RUN:2026-08-27`).
// That is the third corpus fully orphaned by the same gap between what a pass
// writes and what the reader parses (REQ-CROSS-076, SR-SY-1402 were the first
// two), which is why `TestSkillRelationShapesAllHaveReaders` now pins the set.
//
// Deliberately narrow, in the spirit of the earlier two: only ids that follow
// the verb, and only inside the packet clause. Prose that merely mentions a
// requirement is a citation, not a relation.
var (
	packetClauseRe = regexp.MustCompile(`Proposed relations \(CANDIDATE\):\s*([^.\n]*)`)
	packetServesRe = regexp.MustCompile(`\bserves\s+((?:(?:UR|SR)-[A-Z0-9]+-\d+)(?:\s*[,·]\s*(?:UR|SR)-[A-Z0-9]+-\d+)*)`)
	packetReqRe    = regexp.MustCompile(`\brequires\s+((?:(?:UR|SR)-[A-Z0-9]+-\d+)(?:\s*[,·…]\s*(?:UR|SR)-[A-Z0-9]+-\d+)*)`)
	packetIDRe     = regexp.MustCompile(`(?:UR|SR)-[A-Z0-9]+-\d+`)
)

// PacketRelations reads a detail block's Candidate packet and returns the ids
// it declares: `serves` names this row's parent, `requires` names rows that
// take this row as theirs. Either may be empty.
func PacketRelations(detail string) (serves []string, requires []string) {
	clause := packetClauseRe.FindStringSubmatch(detail)
	if clause == nil {
		return nil, nil
	}
	if m := packetServesRe.FindStringSubmatch(clause[1]); m != nil {
		serves = packetIDRe.FindAllString(m[1], -1)
	}
	if m := packetReqRe.FindStringSubmatch(clause[1]); m != nil {
		requires = packetIDRe.FindAllString(m[1], -1)
	}
	return serves, requires
}

// ApplyPacketRelations resolves Candidate-packet relations across the whole
// corpus — relations cross ledgers, so this cannot run per file. It returns how
// many parents it filled and which named ids matched no row.
//
// It only ever fills an EMPTY parent. A row that already names one through a
// column, a Source cell, or a `**UR:**` bullet keeps it: those are declarations
// the author made in a dedicated field, and a packet sentence must not override
// them.
// `parent_external_id` is a system requirement's USER requirement, so only one
// shape is representable: an SR child under a UR parent. A packet naming a UR
// as the child, or an SR as the parent (an SR→SR dependency — a real
// declaration the payload has no field for), is counted and reported rather
// than assigned and silently dropped at payload build. Both occurred on the
// estate that found this: 6 SR→SR edges vanished between a reported 294 links
// and 288 in the batch (`RUN:2026-08-27`). AGENTS.md — a guard says what it
// drops; silent truncation reads as "covered everything".
func ApplyPacketRelations(reqs []Req) (filled int, unresolved []string, unrepresentable []string) {
	byID := make(map[string]int, len(reqs))
	for i, r := range reqs {
		byID[r.ID] = i
	}

	seenMissing := map[string]bool{}
	note := func(id string) {
		if !seenMissing[id] {
			seenMissing[id] = true
			unresolved = append(unresolved, id)
		}
	}

	// `serves` first: a row naming its own parent is the more direct statement,
	// so it wins any race with a `requires` pointed at the same row.
	for i := range reqs {
		serves, _ := PacketRelations(reqs[i].Detail)
		for _, parent := range serves {
			if _, ok := byID[parent]; !ok {
				note(parent)
				continue
			}
			if reqs[i].UR != "" {
				break
			}
			if !representableEdge(reqs[i].ID, parent) {
				unrepresentable = append(unrepresentable, reqs[i].ID+"→"+parent)
				break
			}
			reqs[i].UR = parent
			filled++
			break
		}
	}

	for i := range reqs {
		_, requires := PacketRelations(reqs[i].Detail)
		for _, child := range requires {
			j, ok := byID[child]
			if !ok {
				note(child)
				continue
			}
			if reqs[j].UR != "" {
				continue
			}
			if !representableEdge(reqs[j].ID, reqs[i].ID) {
				unrepresentable = append(unrepresentable, reqs[j].ID+"→"+reqs[i].ID)
				continue
			}
			reqs[j].UR = reqs[i].ID
			filled++
		}
	}

	sort.Strings(unresolved)
	sort.Strings(unrepresentable)
	return filled, unresolved, unrepresentable
}

// representableEdge reports whether `child -> parent` survives to the payload:
// the child must not be a user requirement, and the parent must be one.
func representableEdge(child, parent string) bool {
	return !strings.HasPrefix(child, "UR-") && strings.HasPrefix(parent, "UR-")
}
