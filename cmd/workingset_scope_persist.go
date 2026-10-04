package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// atomicWrite writes via a same-directory temp file and rename, so a failure
// at any point leaves the previous content untouched.
func atomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func newContextID(mode string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return mode + "-" + hex.EncodeToString(b)
}

func packetFingerprintManifest(dir string) string {
	return filepath.Join(dir, "packet", ".served-fingerprints.json")
}

func readPacketFingerprints(dir string) map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(packetFingerprintManifest(dir)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// readContextAggregate reads the packet aggregate a review pull stamped in the
// scope directory's `.context` (REQ-CROSS-449); empty when there is none.
func readContextAggregate(dir string) string {
	for _, line := range strings.Split(readContextFile(dir), "\n") {
		if strings.HasPrefix(line, "aggregate: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "aggregate: "))
		}
	}
	return ""
}

// readContextFile is the scope directory's `.context` stamp; empty when absent.
func readContextFile(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, contextFile))
	if err != nil {
		return ""
	}
	return string(raw)
}

func readContextStamp(dir string) (mode, ctxID string) {
	raw, err := os.ReadFile(filepath.Join(dir, contextFile))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "mode: ") {
			mode = strings.TrimSpace(strings.TrimPrefix(line, "mode: "))
		}
		if strings.HasPrefix(line, "context_id: ") {
			ctxID = strings.TrimSpace(strings.TrimPrefix(line, "context_id: "))
		}
	}
	return mode, ctxID
}

func writePacketFingerprints(dir string, m map[string]string) {
	blob, _ := json.Marshal(m)
	_ = atomicWrite(packetFingerprintManifest(dir), blob)
}
