package cmd

// The record write engine `working-set push` and `author apply` share
// (REQ-CROSS-442). A caller turns its input — a pulled item
// file, or a plan record — into an edited authoring.Record; the engine diffs
// it against the served record and plans one atomic `author patch` per
// record: fields, criteria, relations and membership together, under the
// fingerprint the edit was made against, carrying the authoring context. A
// record the store does not know is created first with its identity fields,
// then patched under the fingerprint the create returned; only `author apply`
// plans a create — push refuses an unknown id before it reaches the engine.
// Every record is planned before the first write; every create runs before
// the first patch, so a relation or a member always names a record that
// exists.

import (
	"fmt"
	"strings"

	"github.com/modernpath/cli/internal/authoring"
	"github.com/modernpath/cli/internal/authoring/diff"
)

// recordWrite is one record's planned write.
type recordWrite struct {
	ext      string
	kind     string // the render kind: epic | user | system
	label    string // where the edit came from, for a refusal
	expected string // the fingerprint the edit guards on; set by a create
	create   map[string]any
	patch    *diff.Patch
}

// changes reports whether the write sends anything.
func (w *recordWrite) changes() bool {
	return w.create != nil || (w.patch != nil && !w.patch.Empty())
}

// describe is the write's line in the op plan.
func (w *recordWrite) describe() string {
	var parts []string
	if w.create != nil {
		parts = append(parts, fmt.Sprintf("create %s %s", w.kind, w.ext))
	}
	if w.patch != nil && !w.patch.Empty() {
		parts = append(parts, describePatch(w.ext, w.patch))
	}
	return strings.Join(parts, "; ")
}

// planRecordWrite diffs edited against the served record. base is nil for a
// record the store does not know: the plan then creates it with the identity
// fields edited states (a title, and a context for a requirement) and patches
// the rest. members is the fallback membership of a served epic.
func planRecordWrite(label string, base *scopeRecord, edited authoring.Record, members []string) (*recordWrite, error) {
	w := &recordWrite{ext: edited.ExternalID, kind: edited.Kind, label: label}
	var baseRec authoring.Record
	if base != nil {
		if base.kind != edited.Kind {
			return nil, fmt.Errorf("%s (%s): the store holds a %s record, not a %s one", w.ext, label, base.kind, edited.Kind)
		}
		baseRec = recordFromPayload(*base, members)
	} else {
		scalars := map[string]string{}
		for _, s := range edited.Scalars {
			scalars[s.Key] = s.Value
		}
		if strings.TrimSpace(scalars["title"]) == "" {
			return nil, fmt.Errorf("%s (%s): a new record needs a title", w.ext, label)
		}
		w.create = map[string]any{"kind": patchKind(edited.Kind), "external_id": w.ext, "title": scalars["title"]}
		baseRec = authoring.Record{Kind: edited.Kind, ExternalID: w.ext,
			Scalars: []authoring.Scalar{{Key: "title", Value: scalars["title"]}}}
		switch edited.Kind {
		case "user", "system":
			if strings.TrimSpace(scalars["context"]) == "" {
				return nil, fmt.Errorf("%s (%s): a new requirement needs a context", w.ext, label)
			}
			w.create["requirement_kind"] = edited.Kind
			w.create["context"] = scalars["context"]
			baseRec.Scalars = append(baseRec.Scalars, authoring.Scalar{Key: "context", Value: scalars["context"]})
		case "epic":
		default:
			return nil, fmt.Errorf("%s (%s): %q is not a kind apply can create — user, system or epic", w.ext, label, edited.Kind)
		}
		baseRec.Projections = edited.Projections
	}
	p, ref := diff.Diff(baseRec, edited)
	if ref != nil {
		return nil, fmt.Errorf("%s (%s): %s", w.ext, label, ref.Error())
	}
	w.patch = p
	return w, nil
}

// writeResult is what one record's writes did.
type writeResult struct {
	created, patched, conflict bool
	exists                     bool // a create answered 409: the record already exists
	fingerprint                string
	item                       *scopeRecord // the canonical record the last write returned
}

// patchPending reports whether the record was created and its planned patch
// never landed.
func (r *writeResult) patchPending(w *recordWrite) bool {
	return r.created && !r.patched && w.patch != nil && !w.patch.Empty()
}

// applyRecordWrites issues the planned writes: every create, then every
// patch. A 409 is that record's conflict and the rest continue; any other
// refusal stops the run and is returned with what was written so far.
func applyRecordWrites(env *factoryEnv, ctxID string, writes []*recordWrite) (map[string]*writeResult, []string, error) {
	results := map[string]*writeResult{}
	var conflicts []string
	post := func(action string, w *recordWrite, record map[string]any) (bool, error) {
		status, resp, err := postAuthor(env, map[string]any{"action": action, "record": record})
		if err != nil {
			return false, fmt.Errorf("%s: %v", w.ext, err)
		}
		res := results[w.ext]
		switch {
		case status == 409:
			res.conflict = true
			conflicts = append(conflicts, w.ext)
			return false, nil
		case status != 200:
			return false, serverRefusal(w.ext, status, resp)
		}
		data, _ := resp["data"].(map[string]any)
		if row, ok := data[patchKind(w.kind)].(map[string]any); ok {
			res.fingerprint = str(row, "fingerprint")
		}
		if item, ok := data["sync_item"].(map[string]any); ok {
			if payload, ok := item["item"].(map[string]any); ok && str(payload, "external_id") == w.ext && validDirectItemKind(str(item, "kind")) {
				res.item = &scopeRecord{kind: str(item, "kind"), payload: payload}
				if fp := str(payload, "fingerprint"); fp != "" {
					res.fingerprint = fp
				}
			}
		}
		return true, nil
	}
	for _, w := range writes {
		results[w.ext] = &writeResult{}
	}
	for _, w := range writes {
		if w.create == nil {
			continue
		}
		record := map[string]any{"authoring_context_id": ctxID}
		for k, v := range w.create {
			record[k] = v
		}
		body := map[string]any{"action": "create", "record": record}
		if env.CurrentRelease != "" {
			// REQ-CROSS-367: a birth joins the workspace's current release.
			body["current_release"] = env.CurrentRelease
		}
		status, resp, err := postAuthor(env, body)
		if err != nil {
			return results, conflicts, fmt.Errorf("%s: %v", w.ext, err)
		}
		res := results[w.ext]
		switch {
		case status == 409:
			res.conflict, res.exists = true, true
			conflicts = append(conflicts, w.ext)
			continue
		case status != 200:
			return results, conflicts, serverRefusal(w.ext, status, resp)
		}
		data, _ := resp["data"].(map[string]any)
		row, _ := data[patchKind(w.kind)].(map[string]any)
		res.created, res.fingerprint = true, str(row, "fingerprint")
		w.expected = res.fingerprint
	}
	for _, w := range writes {
		res := results[w.ext]
		if res.conflict || w.patch == nil || w.patch.Empty() {
			continue
		}
		ok, err := post("patch", w, buildPatchRecord(w.kind, w.ext, w.expected, ctxID, w.patch))
		if err != nil {
			return results, conflicts, err
		}
		res.patched = ok
	}
	return results, conflicts, nil
}
