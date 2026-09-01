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
	s := Summary{Title: "OAuth login"}
	if got := s.DisplayTitle(); got != "OAuth login" {
		t.Fatalf("DisplayTitle() = %q", got)
	}

}

func TestRenderMarkdownIncludesSections(t *testing.T) {
	ctx := &Context{}
	ctx.Task.Code = "TASK-001"
	ctx.Task.Title = "Repository Analysis"
	ctx.Task.Description = "Analyze the repository."
	ctx.Task.Status = "todo"
	ctx.Task.EstimatedStoryPoints = 40
	ctx.Epic.ID = 157
	ctx.Epic.Title = "Simplify roles"
	ctx.Subtasks = append(ctx.Subtasks, struct {
		ID                 string        `json:"id"`
		Code               string        `json:"code"`
		Title              string        `json:"title"`
		Description        string        `json:"description"`
		SubtaskType        string        `json:"subtask_type"`
		Status             string        `json:"status"`
		EstimatedPoints    int           `json:"estimated_points"`
		AcceptanceCriteria []interface{} `json:"acceptance_criteria"`
	}{
		Code: "SUB-001", Title: "Map modules", Description: "Create module map", Status: "todo",
	})

	md := RenderMarkdown(ctx, Summary{ID: "abc", Code: "TASK-001", Title: "Repository Analysis"})
	for _, want := range []string{"# Repository Analysis", "## Description", "## Epic", "## Subtasks", "SUB-001"} {
		if !strings.Contains(md, want) {
			t.Fatalf("RenderMarkdown() missing %q\n%s", want, md)
		}
	}
}
