//go:build linux || darwin

package cmd

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPlanningImageUploadRefusesAnUnwrittenFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := planningImageUpload(path, "fixture")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("expected a regular-file refusal, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("upload blocked opening a named pipe")
	}
}
