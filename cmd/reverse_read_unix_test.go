//go:build unix

package cmd

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestSRRDDONBOARD009RejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	pipe := filepath.Join(root, "source.xml")
	if err := syscall.Mkfifo(pipe, 0600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := reverseRead(root, "source.xml"); result <- err }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("FIFO was accepted")
		}
	case <-time.After(time.Second):
		// Unblock the legacy reader before failing, rather than leak a goroutine.
		writer, err := os.OpenFile(pipe, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			writer.Close()
		}
		t.Fatal("FIFO source blocked before regular-file validation")
	}
}

func TestSRRDDONBOARD009ConcurrentParentSwapNeverReadsOutsideRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	source, parked := filepath.Join(root, "src"), filepath.Join(root, "parked")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "data.xml"), []byte("authorized"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "data.xml"), []byte("outside-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if os.Rename(source, parked) != nil {
				continue
			}
			_ = os.Symlink(outside, source)
			_ = os.Remove(source)
			_ = os.Rename(parked, source)
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	for i := 0; i < 2000; i++ {
		content, err := reverseRead(root, "src/data.xml")
		if err == nil && string(content) != "authorized" {
			t.Fatalf("read outside declared root: %q", content)
		}
	}
}
