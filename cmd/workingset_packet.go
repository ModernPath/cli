package cmd

import (
	"fmt"
	"strings"
)

func packetFileName(sectionKey string) string {
	switch {
	case sectionKey == "reconnaissance":
		return "10-recon.md"
	case sectionKey == "state_inventory":
		return "15-state-inventory.md"
	case sectionKey == "red_strategy":
		return "30-red-strategy.md"
	case sectionKey == "decisions":
		return "40-decisions.md"
	case strings.HasPrefix(sectionKey, "enrichment:"):
		return "20-enrichment-" + strings.TrimPrefix(sectionKey, "enrichment:") + ".md"
	default:
		return sectionKey + ".md"
	}
}

// reservedPacketCollision reports whether an EXTRA (non-canonical) section key's
// file name would collide with a canonical section's file, aliasing it on pull
// and being misread as the canonical key on push (#25).
func reservedPacketCollision(key string) bool {
	switch {
	case key == "reconnaissance", key == "state_inventory", key == "red_strategy", key == "decisions",
		strings.HasPrefix(key, "enrichment:"):
		return false
	}
	switch packetFileName(key) {
	case "10-recon.md", "15-state-inventory.md", "30-red-strategy.md", "40-decisions.md":
		return true
	}
	return strings.HasPrefix(packetFileName(key), "20-enrichment-")
}

// packetStubMarker is the single comment line `working-set pull` writes as an
// unfilled packet stub (REQ-CROSS-332) and `working-set push` strips before
// sending (REQ-CROSS-331). It names the section and the scope so an agent
// opening the file learns what belongs there, and says an unfilled stub is
// never pushed. One line, so the strip is a first-line comparison. The state
// inventory's marker also names the table's columns and the one-line form a
// change that touches no persisted or shared state writes instead
// (SR-CLI-028-001 C2) — still one line, so REQ-CROSS-331's strip and
// unfilled test hold unchanged.
func packetStubMarker(key, scopeKind, scopeExt string) string {
	if key == "state_inventory" {
		return fmt.Sprintf(
			"<!-- packet stub — state_inventory for %s:%s — replace this line with the section: one row per piece of state — columns: state; writers today / after, and each write shape; readers that branch on it; crash mid-write leaves; stale when; recovery; ending closed / residual / decided — or, for a change that touches no persisted or shared state, the one line 'No persisted or shared state is added or touched: <why>.'; an unfilled stub is never pushed -->",
			scopeKind, scopeExt)
	}
	return fmt.Sprintf(
		"<!-- packet stub — %s for %s:%s — replace this line with the section; an unfilled stub is never pushed -->",
		key, scopeKind, scopeExt)
}

// stripPacketStub returns the body actually pushed: if the first line, once a
// leading byte-order mark and trailing whitespace (spaces, tabs, a carriage
// return) are removed, equals the marker, that line is dropped; otherwise the
// content is unchanged (REQ-CROSS-331 D3). Trailing horizontal whitespace is
// normalised too \u2014 an editor re-saving the stub can leave a trailing space, and
// without this the marker text would be pushed as content (review RUN:2026-09-06).
// The marker never carries trailing whitespace, so a real authored line cannot
// match by accident. A marker anywhere but the first line is ordinary content.
func stripPacketStub(content, key, scopeKind, scopeExt string) string {
	first, rest, _ := strings.Cut(content, "\n")
	norm := strings.TrimRight(strings.TrimPrefix(first, "\uFEFF"), " \t\r")
	if norm == packetStubMarker(key, scopeKind, scopeExt) {
		return rest
	}
	return content
}

// unfilledPacketSection reports whether a packet file carries no authored
// content: blank, whitespace only, or the stub marker alone (REQ-CROSS-331 D3).
func unfilledPacketSection(content, key, scopeKind, scopeExt string) bool {
	return strings.TrimSpace(stripPacketStub(content, key, scopeKind, scopeExt)) == ""
}

func sectionKeyFromFile(name string) string {
	switch name {
	case "10-recon.md":
		return "reconnaissance"
	case "15-state-inventory.md":
		return "state_inventory"
	case "30-red-strategy.md":
		return "red_strategy"
	case "40-decisions.md":
		return "decisions"
	}
	if strings.HasPrefix(name, "20-enrichment-") {
		return "enrichment:" + strings.TrimSuffix(strings.TrimPrefix(name, "20-enrichment-"), ".md")
	}
	return strings.TrimSuffix(name, ".md")
}
