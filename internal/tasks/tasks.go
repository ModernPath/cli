package tasks

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
)

// Summary is a Task row from GET /api/work/epics/:id/tasks.
type Summary struct {
	ID                   string `json:"id"`
	Code                 string `json:"code"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	Status               string `json:"status"`
	EstimatedStoryPoints int    `json:"estimated_story_points"`
}

func (s Summary) DisplayTitle() string {
	return s.Title
}

// Context is the full development context from GET /api/work/tasks/:id/context.
type Context struct {
	Task struct {
		ID                   string `json:"id"`
		Code                 string `json:"code"`
		Title                string `json:"title"`
		Description          string `json:"description"`
		Status               string `json:"status"`
		EstimatedStoryPoints int    `json:"estimated_story_points"`
	} `json:"task"`
	Epic struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"epic"`
	System   json.RawMessage `json:"system"`
	Subtasks []struct {
		ID                 string        `json:"id"`
		Code               string        `json:"code"`
		Title              string        `json:"title"`
		Description        string        `json:"description"`
		SubtaskType        string        `json:"subtask_type"`
		Status             string        `json:"status"`
		EstimatedPoints    int           `json:"estimated_points"`
		AcceptanceCriteria []interface{} `json:"acceptance_criteria"`
	} `json:"subtasks"`
	DevelopmentWorkflows []struct {
		Name         string `json:"name"`
		WorkflowType string `json:"workflow_type"`
		Description  string `json:"description"`
		Content      string `json:"content"`
	} `json:"development_workflows"`
	CodingInstructions []map[string]interface{} `json:"coding_instructions"`
	Patterns           []map[string]interface{} `json:"patterns"`
	Specifications     []struct {
		ID           int             `json:"id"`
		Name         string          `json:"name"`
		ArtifactType string          `json:"artifact_type"`
		Category     string          `json:"category"`
		Content      json.RawMessage `json:"content"`
	} `json:"specifications"`
}

// FileName returns the on-disk filename for a task context file.
func FileName(taskID, title string) string {
	slug := config.Slugify(title)
	if slug == "" {
		return taskID + ".md"
	}
	return taskID + "-" + slug + ".md"
}

// TasksDir returns the absolute path to .modernpath/tasks (epic workspace parent).
func TasksDir() (string, error) {
	configDir, err := config.WorkspaceConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "tasks"), nil
}

// List fetches Task summaries for an Epic.
func List(baseURL string, epicID int, doGet func(string, time.Duration) (*http.Response, error)) ([]Summary, error) {
	url := fmt.Sprintf("%s/api/work/epics/%d/tasks", baseURL, epicID)
	resp, err := doGet(url, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data []Summary `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// FetchContext downloads full context for one Task.
func FetchContext(baseURL, taskID string, doGet func(string, time.Duration) (*http.Response, error)) (*Context, error) {
	url := fmt.Sprintf("%s/api/work/tasks/%s/context", baseURL, taskID)
	resp, err := doGet(url, 60*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data Context `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// FetchAll downloads context for every Task in an Epic into the Epic workspace
// at .modernpath/tasks/<id>-<slug>/ (task .md files at the workspace root).
// Returns the number saved and any per-task warnings (partial failures).
func FetchAll(baseURL string, epicID int, epicTitle string, doGet func(string, time.Duration) (*http.Response, error)) (int, []string, error) {
	workspaceDir, err := config.EpicWorkspaceAbsDir(epicID, epicTitle)
	if err != nil {
		return 0, nil, err
	}
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return 0, nil, fmt.Errorf("failed to create epic workspace directory: %w", err)
	}

	summaries, err := List(baseURL, epicID, doGet)
	if err != nil {
		return 0, nil, err
	}

	saved := 0
	var fetchErrors []string
	for _, summary := range summaries {
		ctx, err := FetchContext(baseURL, summary.ID, doGet)
		if err != nil {
			fetchErrors = append(fetchErrors, fmt.Sprintf("%s (%s): %v", summary.Code, summary.ID, err))
			continue
		}

		title := summary.DisplayTitle()
		if title == "" && ctx.Task.Title != "" {
			title = ctx.Task.Title
		}

		filename := FileName(summary.ID, title)
		content := RenderMarkdown(ctx, summary)
		path := filepath.Join(workspaceDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			fetchErrors = append(fetchErrors, fmt.Sprintf("%s (%s): write failed: %v", summary.Code, summary.ID, err))
			continue
		}
		saved++
	}

	if saved == 0 && len(fetchErrors) > 0 {
		return 0, fetchErrors, fmt.Errorf("failed to fetch any task context")
	}

	return saved, fetchErrors, nil
}

func RenderMarkdown(ctx *Context, summary Summary) string {
	var b strings.Builder

	title := summary.DisplayTitle()
	if title == "" {
		title = ctx.Task.Title
	}
	code := summary.Code
	if code == "" {
		code = ctx.Task.Code
	}

	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("> Task **%s** | Status: **%s** | Estimated points: **%d**\n\n", code, ctx.Task.Status, ctx.Task.EstimatedStoryPoints))

	if ctx.Task.Description != "" {
		b.WriteString("## Description\n\n")
		b.WriteString(ctx.Task.Description)
		b.WriteString("\n\n")
	}

	if ctx.Epic.Title != "" {
		b.WriteString("## Epic\n\n")
		b.WriteString(fmt.Sprintf("- **ID:** %d\n", ctx.Epic.ID))
		b.WriteString(fmt.Sprintf("- **Title:** %s\n", ctx.Epic.Title))
		if ctx.Epic.Description != "" {
			b.WriteString("\n")
			b.WriteString(ctx.Epic.Description)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(ctx.System) > 0 && string(ctx.System) != "null" {
		b.WriteString("## System\n\n")
		b.WriteString("```json\n")
		b.WriteString(prettyJSON(ctx.System))
		b.WriteString("\n```\n\n")
	}

	if len(ctx.Subtasks) > 0 {
		b.WriteString("## Subtasks\n\n")
		for _, subtask := range ctx.Subtasks {
			b.WriteString(fmt.Sprintf("### %s — %s\n\n", subtask.Code, subtask.Title))
			b.WriteString(fmt.Sprintf("- **Status:** %s\n", subtask.Status))
			if subtask.SubtaskType != "" {
				b.WriteString(fmt.Sprintf("- **Type:** %s\n", subtask.SubtaskType))
			}
			if subtask.EstimatedPoints > 0 {
				b.WriteString(fmt.Sprintf("- **Estimated points:** %d\n", subtask.EstimatedPoints))
			}
			if subtask.Description != "" {
				b.WriteString("\n")
				b.WriteString(subtask.Description)
				b.WriteString("\n")
			}
			if len(subtask.AcceptanceCriteria) > 0 {
				b.WriteString("\n**Acceptance criteria:**\n\n")
				for _, item := range subtask.AcceptanceCriteria {
					b.WriteString(fmt.Sprintf("- %s\n", formatValue(item)))
				}
			}
			b.WriteString("\n")
		}
	}

	if len(ctx.Specifications) > 0 {
		b.WriteString("## Specifications\n\n")
		for _, spec := range ctx.Specifications {
			b.WriteString(fmt.Sprintf("### %s (%s / %s)\n\n", spec.Name, spec.Category, spec.ArtifactType))
			b.WriteString(renderSpecContent(spec.Content))
			b.WriteString("\n")
		}
	}

	if len(ctx.DevelopmentWorkflows) > 0 {
		b.WriteString("## Development Workflows\n\n")
		for _, wf := range ctx.DevelopmentWorkflows {
			b.WriteString(fmt.Sprintf("### %s (%s)\n\n", wf.Name, wf.WorkflowType))
			if wf.Description != "" {
				b.WriteString(wf.Description)
				b.WriteString("\n\n")
			}
			if wf.Content != "" {
				b.WriteString(wf.Content)
				b.WriteString("\n\n")
			}
		}
	}

	if len(ctx.CodingInstructions) > 0 {
		b.WriteString("## Coding Instructions\n\n")
		for _, instr := range ctx.CodingInstructions {
			b.WriteString(renderMapSection(instr))
			b.WriteString("\n")
		}
	}

	if len(ctx.Patterns) > 0 {
		b.WriteString("## Patterns\n\n")
		for _, pattern := range ctx.Patterns {
			name, _ := pattern["name"].(string)
			category, _ := pattern["category"].(string)
			description, _ := pattern["description"].(string)
			b.WriteString(fmt.Sprintf("### %s", name))
			if category != "" {
				b.WriteString(fmt.Sprintf(" (%s)", category))
			}
			b.WriteString("\n\n")
			if description != "" {
				b.WriteString(description)
				b.WriteString("\n\n")
			}
		}
	}

	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("_Generated by ModernPath CLI from epic context API for task `%s`._\n", summary.ID))

	return b.String()
}

func renderSpecContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString + "\n"
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if text, ok := asMap["text"].(string); ok {
			return text + "\n"
		}
		if body, ok := asMap["body"].(string); ok {
			return body + "\n"
		}
		return "```json\n" + prettyJSON(raw) + "\n```\n"
	}

	return string(raw) + "\n"
}

func renderMapSection(m map[string]interface{}) string {
	var b strings.Builder
	if name, ok := m["name"].(string); ok && name != "" {
		b.WriteString(fmt.Sprintf("### %s\n\n", name))
	}
	if title, ok := m["title"].(string); ok && title != "" && b.Len() == 0 {
		b.WriteString(fmt.Sprintf("### %s\n\n", title))
	}
	for _, key := range []string{"description", "content", "summary", "instructions"} {
		if val, ok := m[key]; ok {
			text := formatValue(val)
			if text != "" {
				b.WriteString(text)
				b.WriteString("\n\n")
			}
		}
	}
	if b.Len() == 0 {
		b.WriteString("```json\n")
		b.WriteString(prettyJSON(m))
		b.WriteString("\n```\n")
	}
	return b.String()
}

func prettyJSON(v interface{}) string {
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(bytes)
}

func formatValue(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]interface{}:
		if text, ok := t["text"].(string); ok {
			return text
		}
		return prettyJSON(t)
	default:
		return fmt.Sprintf("%v", v)
	}
}
