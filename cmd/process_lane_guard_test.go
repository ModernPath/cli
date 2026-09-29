package cmd

import (
	"slices"
	"strings"
	"testing"
)

// REQ-CROSS-458 (EPIC-RDD-LANE): the lane's write verbs are denied to a
// delegated agent wherever flags sit; the bare `process lane` is a read.
func TestLaneGuardDeniesTheLaneWriteVerbsToASubagent(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		"modernpath process lane authorize --classes wording --appliers 7 --expires 2026-10-20 --cap 5",
		"modernpath process lane authorize --file lane.json",
		"modernpath process lane review REQ-1 --file review.json",
		"modernpath process lane enter REQ-1",
		"modernpath process lane check REQ-1 --commit abc1234",
		"modernpath process lane complete",
		"modernpath process lane complete --apply",
		"modernpath process --piece EPIC-1 lane enter REQ-1",
		"modernpath process lane -v enter REQ-1",
		"modernpath -v process lane check REQ-1 --commit abc --base def",
		"modernpath process next && modernpath process lane enter REQ-1",
	} {
		out := processGateHookPayload(hookPayload(sub, cmd))
		if !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Errorf("subagent %q was not denied: %s", cmd, out)
		}
	}
}

func TestLaneGuardPassesTheBareLaneReadAndTheMainSession(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		"modernpath process lane",
		"modernpath process lane --help",
		"modernpath process lane enter --help",
		"modernpath -v process lane",
	} {
		if out := processGateHookPayload(hookPayload(sub, cmd)); out != "{}" {
			t.Errorf("subagent read %q was not passed through: %s", cmd, out)
		}
	}
	for _, cmd := range []string{
		"modernpath process lane enter REQ-1",
		"modernpath process lane complete --apply",
	} {
		if out := processGateHookPayload(hookPayload(nil, cmd)); out != "{}" {
			t.Errorf("main-session write %q was denied: %s", cmd, out)
		}
	}
}

// The lane verbs are routine writes, allowed like process advance, except
// authorize: it prepares a standing authorization, so it asks.
func TestLanePermissionsAllowTheRoutineLaneVerbsAndAskOnAuthorize(t *testing.T) {
	rules := kitPermissionRules()
	matches := func(list, command string) bool {
		for _, r := range rules[list] {
			inner := strings.TrimSuffix(strings.TrimPrefix(r, "Bash("), ")")
			if prefix, wild := strings.CutSuffix(inner, "*"); wild && strings.HasPrefix(command, prefix) || inner == command {
				return true
			}
		}
		return false
	}
	for _, command := range []string{
		"modernpath process lane review REQ-1 --file review.json",
		"modernpath process lane enter REQ-1",
		"modernpath process lane check REQ-1 --commit abc1234",
		"modernpath process lane complete",
		"modernpath process lane complete --apply",
		".modernpath/bin/modernpath process lane enter REQ-1",
	} {
		if !matches("allow", command) {
			t.Errorf("no allow rule covers %q", command)
		}
		if matches("ask", command) {
			t.Errorf("%q is a routine write and must not ask", command)
		}
		if !storeWriteCommand(command) {
			t.Errorf("the subagent guard does not know %q as a store write", command)
		}
	}
	for _, command := range []string{
		"modernpath process lane authorize --file lane.json",
		".modernpath/bin/modernpath process lane authorize --classes wording",
	} {
		if !matches("ask", command) {
			t.Errorf("%q must ask", command)
		}
		if matches("allow", command) {
			t.Errorf("%q must not also be allowed", command)
		}
	}
}

// DL-12: `process lane approve` answers a standing authorization for a whole
// System. The subagent guard denies it wherever flags sit, and the kit ships
// it in ask only: no allow rule may prefix-match it under either binary prefix.
func TestLaneApproveIsDeniedToASubagentAndAsksInTheMainSession(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		"modernpath process lane approve LANE-AUTH",
		"modernpath process lane approve LANE-AUTH --text 'approve the pilot'",
		"modernpath process lane approve --text ok LANE-AUTH",
		"modernpath process lane -v approve LANE-AUTH",
		"modernpath process --piece EPIC-1 lane approve LANE-AUTH",
		"modernpath -v process lane approve LANE-AUTH",
		".modernpath/bin/modernpath process lane approve LANE-AUTH",
		"modernpath process next && modernpath process lane approve LANE-AUTH",
	} {
		out := processGateHookPayload(hookPayload(sub, cmd))
		if !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Errorf("subagent %q was not denied: %s", cmd, out)
		}
	}
	if out := processGateHookPayload(hookPayload(sub, "modernpath process lane approve --help")); out != "{}" {
		t.Errorf("the approve help is a read: %s", out)
	}

	rules := kitPermissionRules()
	prefixMatches := func(list, command string) []string {
		var hit []string
		for _, r := range rules[list] {
			inner := strings.TrimSuffix(strings.TrimPrefix(r, "Bash("), ")")
			if prefix, wild := strings.CutSuffix(inner, "*"); (wild && strings.HasPrefix(command, prefix)) || inner == command {
				hit = append(hit, r)
			}
		}
		return hit
	}
	for _, bin := range kitPermissionPrefixes {
		for _, command := range []string{
			bin + " process lane approve",
			bin + " process lane approve LANE-AUTH",
			bin + " process lane approve LANE-AUTH --text 'approve the pilot'",
		} {
			if hit := prefixMatches("allow", command); len(hit) > 0 {
				t.Errorf("%q must not be allowed, matched by %v", command, hit)
			}
			if len(prefixMatches("ask", command)) == 0 {
				t.Errorf("%q must ask", command)
			}
		}
		if !slices.Contains(rules["ask"], kitPermissionRule(bin, "process lane approve*")) {
			t.Errorf("the ask list holds %s", kitPermissionRule(bin, "process lane approve*"))
		}
	}
}
