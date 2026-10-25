package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// record --dir records a folder of staged files with one record request per
// file. recorded/ beside the files holds, for each file, the copy and receipt
// written after it was recorded, or the note of a content refusal. Those
// files only help a rerun resume; the store stays the authority.

type recordDirFile struct {
	File   string `json:"file"`
	Status string `json:"status"` // recorded, skipped, refused, changed, stopped
	Detail string `json:"detail,omitempty"`
}

type recordDirReport struct {
	Files    []recordDirFile `json:"files"`
	Recorded int             `json:"recorded"`
	Skipped  int             `json:"skipped"`
	Refused  int             `json:"refused"`
	Changed  int             `json:"changed"`
	// PreviouslyRefused counts skipped files whose earlier refusal still stands.
	PreviouslyRefused int    `json:"previously_refused"`
	StoppedAt         string `json:"stopped_at,omitempty"`
	StoppedReason     string `json:"stopped_reason,omitempty"`
	stopErr           error
}

func (r *recordDirReport) add(file, status, detail string) {
	r.Files = append(r.Files, recordDirFile{File: file, Status: status, Detail: detail})
	switch status {
	case "recorded":
		r.Recorded++
	case "skipped":
		r.Skipped++
	case "refused":
		r.Refused++
	case "changed":
		r.Changed++
	}
}

func (r *recordDirReport) stop(file, reason string, err error) {
	r.add(file, "stopped", fmt.Sprintf("%s: %v", reason, err))
	r.StoppedAt, r.StoppedReason, r.stopErr = file, reason, err
}

type recordDirRefusal struct {
	Message string          `json:"message"`
	Content json.RawMessage `json:"content"`
}

// recordDirWrite writes body to path through a uniquely named temporary file
// renamed into place, so a reader never sees a partial file.
func recordDirWrite(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func recordDirSame(a, b []byte) bool {
	na, okA := reCheckNormalizedJSON(a)
	nb, okB := reCheckNormalizedJSON(b)
	return okA && okB && na == nb
}

// recordDirStopReason names why a record request failed in a way that would
// fail every file, or "" for a content refusal of this file alone.
func recordDirStopReason(err error) string {
	var credential credentialError
	var limiting serverLimitingError
	var refusal serverRefusalError
	var transport *url.Error
	switch {
	case errors.As(err, &credential):
		return "sign-in"
	case errors.As(err, &limiting):
		return "rate-limit"
	case errors.As(err, &refusal):
		switch {
		case refusal.status == 422 || refusal.status == 409:
			return ""
		case refusal.status == 403:
			return "permission"
		case refusal.status >= 500:
			return "server"
		default:
			return "server-request"
		}
	case errors.As(err, &transport):
		return "connection"
	default:
		return "unknown-outcome"
	}
}

func reverseRecordDir(cmd *cobra.Command, load func() (*cliEnv, error), dir string, retryRefused bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	recorded := filepath.Join(dir, "recorded")
	report := &recordDirReport{Files: []recordDirFile{}}
	var env *cliEnv

files:
	for _, name := range names {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			report.stop(name, "local-read", err)
			break
		}
		copyPath, receiptPath, refusedPath := filepath.Join(recorded, name), filepath.Join(recorded, name+".receipt"), filepath.Join(recorded, name+".refused")
		if receipt, err := os.ReadFile(receiptPath); err == nil && len(receipt) > 0 {
			if copied, err := os.ReadFile(copyPath); err == nil && recordDirSame(raw, copied) {
				report.add(name, "skipped", "already recorded")
			} else {
				report.add(name, "changed", "changed after record: make a new file with new UUIDs")
			}
			continue
		}
		if note, err := os.ReadFile(refusedPath); err == nil && !retryRefused {
			var refusal recordDirRefusal
			if json.Unmarshal(note, &refusal) == nil && recordDirSame(raw, refusal.Content) {
				report.add(name, "skipped", "refused before and unchanged: "+refusal.Message)
				report.PreviouslyRefused++
				continue
			}
		}

		refuse := func(message string) bool {
			content := json.RawMessage(raw)
			if !json.Valid(raw) {
				content, _ = json.Marshal(string(raw))
			}
			note, _ := json.MarshalIndent(recordDirRefusal{Message: message, Content: content}, "", "  ")
			if err := os.MkdirAll(recorded, 0o755); err != nil {
				report.stop(name, "local-write", err)
				return false
			}
			if err := recordDirWrite(refusedPath, note); err != nil {
				report.stop(name, "local-write", err)
				return false
			}
			report.add(name, "refused", message)
			return true
		}

		payload, err := parseReverseRecord(raw)
		if err == nil {
			for _, r := range payload.Requirements {
				if id, _ := r["id"].(string); !requirementUUIDPattern.MatchString(strings.TrimSpace(id)) {
					err = fmt.Errorf("a requirement has no UUID id: give every requirement a UUID so a rerun reuses it")
					break
				}
			}
		}
		if err != nil {
			if !refuse(err.Error()) {
				break
			}
			continue
		}

		if env == nil {
			if env, err = load(); err != nil {
				// Nothing was sent: a failure the request classes do not name
				// is the credential step's.
				reason := recordDirStopReason(err)
				if errors.As(err, new(bindingError)) {
					reason = "binding"
				} else if reason == "" || reason == "unknown-outcome" {
					reason = "sign-in"
				}
				report.stop(name, reason, err)
				break
			}
			if env.SystemID <= 0 {
				report.stop(name, "binding", fmt.Errorf("connect this workspace to a system before reverse-engineering"))
				break
			}
		}
		body, err := reverseRecordCall(env, payload)
		if err != nil {
			reason := recordDirStopReason(err)
			if reason != "" {
				report.stop(name, reason, err)
				break
			}
			if !refuse(err.Error()) {
				break
			}
			continue
		}
		receipt, err := reverseRecordReceipt(body)
		if err != nil {
			report.stop(name, "unknown-outcome", err)
			break
		}
		receiptBody, _ := json.MarshalIndent(receipt, "", "  ")
		for _, step := range []struct {
			path string
			body []byte
		}{{copyPath, raw}, {receiptPath, receiptBody}} {
			if err := os.MkdirAll(recorded, 0o755); err != nil {
				report.stop(name, "local-write", err)
				break files
			}
			if err := recordDirWrite(step.path, step.body); err != nil {
				report.stop(name, "local-write", err)
				break files
			}
		}
		_ = os.Remove(refusedPath)
		report.add(name, "recorded", "")
	}

	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		out := cmd.OutOrStdout()
		for _, f := range report.Files {
			if f.Detail == "" {
				fmt.Fprintf(out, "%-9s %s\n", f.Status, f.File)
			} else {
				fmt.Fprintf(out, "%-9s %s  %s\n", f.Status, f.File, f.Detail)
			}
		}
		fmt.Fprintf(out, "%d recorded, %d skipped, %d refused, %d changed after record\n", report.Recorded, report.Skipped, report.Refused, report.Changed)
		if report.PreviouslyRefused > 0 {
			fmt.Fprintf(out, "%d still refused from an earlier run: fix the file, or rerun with --retry-refused\n", report.PreviouslyRefused)
		}
		if report.StoppedAt != "" {
			fmt.Fprintf(out, "stopped at %s (%s); rerun to resume\n", report.StoppedAt, report.StoppedReason)
		}
	}
	switch {
	case report.stopErr != nil:
		return fmt.Errorf("record --dir stopped at %s (%s): %w", report.StoppedAt, report.StoppedReason, report.stopErr)
	case report.Refused > 0 || report.Changed > 0:
		return fmt.Errorf("record --dir: %d file(s) refused, %d changed after record", report.Refused, report.Changed)
	}
	return nil
}
