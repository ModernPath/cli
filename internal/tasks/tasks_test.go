package tasks

import (
	"strings"
	"testing"
)

func TestFileName(t *testing.T) {
	got := FileName("553ee95e-8648-43f5-a825-e390506113ab", "Repository Analysis & Blueprint Visualization")
	want := "553ee95e-8648-43f5-a825-e390506113ab-repository-analysis-blueprint-visualization.md"
	if got != want {
		t.Fatalf("FileName() = %q, want %q", got, want)
	}
}

func TestSummaryDisplayTitle(t *testing.T) {
	s := Summary{Title: "OAuth login", Name: "ignored"}
	if got := s.DisplayTitle(); got != "OAuth login" {
		t.Fatalf("DisplayTitle() = %q", got)
	}

	s = Summary{Name: "Legacy name"}
	if got := s.DisplayTitle(); got != "Legacy name" {
		t.Fatalf("DisplayTitle() = %q", got)
	}
}

func TestRenderMarkdownIncludesSections(t *testing.T) {
	ctx := &Context{}
	ctx.Epic.Code = "EPIC-001"
	ctx.Epic.Title = "Repository Analysis"
	ctx.Epic.Description = "Analyze the repository."
	ctx.Epic.Status = "todo"
	ctx.Epic.StoryPoints = 40
	ctx.Initiative.ID = 157
	ctx.Initiative.Title = "Simplify roles"
	ctx.Stories = append(ctx.Stories, struct {
		ID                 string        `json:"id"`
		Code               string        `json:"code"`
		Title              string        `json:"title"`
		Description        string        `json:"description"`
		StoryType          string        `json:"story_type"`
		Status             string        `json:"status"`
		StoryPoints        int           `json:"story_points"`
		AcceptanceCriteria []interface{} `json:"acceptance_criteria"`
	}{
		Code: "STORY-001", Title: "Map modules", Description: "Create module map", Status: "todo",
	})

	md := RenderMarkdown(ctx, Summary{ID: "abc", Code: "EPIC-001", Title: "Repository Analysis"})
	for _, want := range []string{"# Repository Analysis", "## Description", "## Initiative", "## Stories", "STORY-001"} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderMarkdown() missing %q\n%s", want, md)
		}
	}
}
