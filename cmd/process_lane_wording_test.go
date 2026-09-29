package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// REQ-CROSS-456 (PR #694 review, #4): `process lane complete --log` records
// the given string as the run; nothing verifies it. The brief the approver
// reads says so — the run the agent reported — instead of calling it a
// passing run.
func TestREQCROSS456LaneBatchBriefCallsTheRunTheAgentsReport(t *testing.T) {
	l, _ := laneBatchWorld(t)

	out, err := runRoot(t, "process", "lane", "complete", "--log", "https://ci.example/run/42")
	if err != nil {
		t.Fatalf("lane complete: %v\n%s", err, out)
	}
	var gate map[string]any
	for _, p := range l.lanePosts("create") {
		if rec, _ := p["record"].(map[string]any); rec["purpose"] == "lane-batch" {
			gate = rec
		}
	}
	if gate == nil {
		t.Fatalf("no lane-batch gate was opened\n%s", out)
	}
	brief, _ := gate["brief"].(map[string]any)
	text := strings.Join(briefValues(brief), "\n")
	if !strings.Contains(text, "the run the agent reported") || !strings.Contains(text, "https://ci.example/run/42") {
		t.Errorf("the brief says the run is the agent's report and names it: %v", brief)
	}
	if strings.Contains(text, "with a passing run and") {
		t.Errorf("the brief must not present the reported run as a verified one: %v", brief)
	}
}

// REQ-CROSS-458 (PR #694 review): the shipped help of the lane verbs is for
// the person running them — no decision ids, server action names, JSON key
// lists or pilot notes.
func TestREQCROSS458LaneHelpCarriesNoInternalJargon(t *testing.T) {
	jargon := []string{"DL-12", "lane_approve", "during the pilot", "{classes", "{verdict", "raw_payload", "exact scope system:"}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		texts := []string{c.Use, c.Short, c.Long}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) { texts = append(texts, f.Usage) })
		for _, text := range texts {
			for _, j := range jargon {
				if strings.Contains(text, j) {
					t.Errorf("%s help carries %q: %s", c.CommandPath(), j, text)
				}
			}
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(processLaneCmd)
}
