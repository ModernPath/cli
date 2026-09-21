package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-348 (EPIC-CLI-018): a working-set snapshot header names the store
// revision it came from — sourced from the response (x-modernpath-store-revision)
// — never the local CLI build. A store that serves none says so in the header,
// and the CLI's own build appears as its own labelled line.
func TestREQCROSS348SnapshotHeaderNamesTheServedStoreRevision(t *testing.T) {
	fx := &wsFixture{requirements: []any{wsReq("REQ-SR-001", "served rev")}, storeRevision: "srv-abc123"}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"REQ-SR-001"}, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-SR-001.md"))
	line := headerLine(string(raw), "Source store/revision")
	if !strings.Contains(line, "store srv-abc123") {
		t.Fatalf("the header must name the served store revision:\n%s", line)
	}
	if strings.Contains(line, Version) {
		t.Fatalf("the store line must not carry the CLI build %q:\n%s", Version, line)
	}
	if build := headerLine(string(raw), "CLI build"); !strings.Contains(build, Version) {
		t.Fatalf("the CLI build belongs on its own labelled line, got %q", build)
	}
}

func TestREQCROSS348SnapshotHeaderSaysWhenNoStoreRevisionIsServed(t *testing.T) {
	fx := &wsFixture{requirements: []any{wsReq("REQ-SR-002", "no rev")}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"REQ-SR-002"}, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-SR-002.md"))
	line := headerLine(string(raw), "Source store/revision")
	if !strings.Contains(line, "not served by this server") || strings.Contains(line, Version) {
		t.Fatalf("with no served revision the header says so and never substitutes the CLI build:\n%s", line)
	}
}

func headerLine(body, key string) string {
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, "**"+key+":**") {
			return l
		}
	}
	return ""
}
