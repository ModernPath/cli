package cmd

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRecordFieldSelection(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		required []string
		want     map[string]any
		bad      string
	}{
		{"summary", nil, nil, map[string]any{"id": 7, "title": "Cart"}, ""},
		{"selected", []string{"--fields", "title,scope"}, nil, map[string]any{"title": "Cart", "scope": "Save carts"}, ""},
		{"native identity", []string{"--fields", "title"}, []string{"id"}, map[string]any{"id": 7, "title": "Cart"}, ""},
		{"full", []string{"--full"}, nil, map[string]any{"id": 7, "title": "Cart", "scope": "Save carts"}, ""},
		{"unknown", []string{"--fields", "secret"}, nil, nil, "unknown field"},
		{"empty", []string{"--fields", ""}, nil, nil, "empty"},
		{"blank item", []string{"--fields", "title,"}, nil, nil, "empty"},
		{"conflicting", []string{"--fields", "title", "--full"}, nil, nil, "mutually exclusive"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "get"}
			addRecordReadFlags(cmd)
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatal(err)
			}
			record := map[string]any{"id": 7, "title": "Cart", "scope": "Save carts", "internal": "private"}
			got, err := selectRecordFields(cmd, record, []string{"id", "title"}, []string{"id", "title", "scope"}, tt.required)
			if tt.bad != "" {
				if err == nil || !strings.Contains(err.Error(), tt.bad) {
					t.Fatalf("error = %v; want %s", err, tt.bad)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("selection = %#v, %v; want %#v", got, err, tt.want)
			}
		})
	}
}

func TestRecordOutputUsesOnlySelectedData(t *testing.T) {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().Bool("json", false, "JSON output")
	var out bytes.Buffer
	cmd.SetOut(&out)
	selected := map[string]any{"title": "Cart", "scope": "First line\nSecond line"}
	if err := writeRecord(cmd, selected); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\\n") || !strings.Contains(out.String(), "Second line") {
		t.Fatalf("text content escaped: %q", out.String())
	}
	out.Reset()
	cmd.Flags().Set("json", "true")
	if err := writeRecord(cmd, selected); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || !reflect.DeepEqual(decoded, selected) {
		t.Fatalf("JSON = %q, %v", out.String(), err)
	}
}
