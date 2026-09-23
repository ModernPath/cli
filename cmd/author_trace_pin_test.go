package cmd

// REQ-CROSS-376 (EPIC-CLI-017): `author trace` defaults its pin by purpose —
// the packet aggregate for cold-review/entry/completion, the requirement's
// content hash for lower — and validates a given value: a prefix is refused,
// the wrong class is refused by name, a full hash matching nothing is warned
// about (flag-first) and recorded. The pin is read from the server, never
// typed from memory: a trace pinned to a display prefix can never match and
// can never be removed.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	pinAggregate   = "f54758b841017af674a3b86f2f2e05865d749ddedc15172353e6ed62440b2f01"
	pinContentHash = "baaf2aa5a8f0dedfa54265d224cc3cf12c4795b7bdf07b2a4096713acb78e870"
	pinUnknown     = "0000000000000000000000000000000000000000000000000000000000000000"
	// REQ-CROSS-431: the user requirement an upper trace pins to.
	pinURContentHash = "1f9a3c6d20b5e47fa8c0d13e75b2648af0cd93e15b7a4c28d6e0f31b9a475c62"
)

// pinServer serves the two reads resolveTracePin needs: the caller-scoped
// delivery context (aggregate) and the requirements list (content hashes). An
// empty aggregate models a server that cannot serve one for the scope.
func pinServer(t *testing.T, aggregate string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/work-selection", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"active_release": []any{map[string]any{"slug": "r", "status": "active"}},
			"current": map[string]any{"scope_external_id": "EPIC-P", "scope_kind": "epic",
				"members": []string{"REQ-P-1"}, "phase": "build"},
			"suspended": []any{}, "history": []any{},
		}})
	})
	mux.HandleFunc("/api/v1/sync/delivery-context", func(w http.ResponseWriter, r *http.Request) {
		var agg any = aggregate
		// The real server resolves ?scope= only against the caller's HELD pieces
		// (work_selections.ex resolve_current): a member SR named as the scope
		// yields no aggregate. Honour that here so the lower-purpose path is
		// tested against what the server actually serves.
		if aggregate == "" || (r.URL.Query().Get("scope") != "" && r.URL.Query().Get("scope") != "EPIC-P") {
			agg = nil
		}
		// The frozen member list holds only the SRs; the epic's user requirement
		// is found through the stored membership the facts serve, as on the
		// real server (resolvePiece; REQ-CROSS-431).
		var facts any
		if r.URL.Query().Get("scope") == "EPIC-P" {
			facts = map[string]any{
				"scope": map[string]any{"external_id": "EPIC-P", "kind": "epic", "status": "IN_PROGRESS"},
				"members": []map[string]any{
					{"external_id": "REQ-P-1", "kind": "sr", "status": "IN_PROGRESS", "content_fingerprint": pinContentHash},
					{"external_id": "UR-P", "kind": "ur", "status": "IN_PROGRESS", "content_fingerprint": pinURContentHash},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"packet_fingerprint": agg, "process_revision": strings.Repeat("c", 40), "checks": map[string]any{},
			"facts": facts,
		}})
	})
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirements":      []map[string]any{{"external_id": "REQ-P-1", "fingerprint": pinContentHash}},
			"user_requirements": []map[string]any{{"external_id": "UR-P", "fingerprint": pinURContentHash}},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAuthorTraceDefaultsThePinByPurpose(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))

	pin, _, err := resolveTracePin(env, "lower", []string{"REQ-P-1"}, "")
	if err != nil || pin != pinContentHash {
		t.Fatalf("purpose lower must default to the requirement's content hash, got %q, %v", pin, err)
	}
	pin, _, err = resolveTracePin(env, "completion", []string{"EPIC-P", "REQ-P-1"}, "")
	if err != nil || pin != pinAggregate {
		t.Fatalf("purpose completion must default to the packet aggregate, got %q, %v", pin, err)
	}
	pin, _, err = resolveTracePin(env, "cold-review", []string{"EPIC-P"}, "")
	if err != nil || pin != pinAggregate {
		t.Fatalf("purpose cold-review must default to the packet aggregate, got %q, %v", pin, err)
	}
}

func TestAuthorTraceRefusesAPrefix(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))
	_, _, err := resolveTracePin(env, "completion", []string{"EPIC-P"}, pinAggregate[:12])
	if err == nil || !strings.Contains(err.Error(), "64") || !strings.Contains(err.Error(), "12") {
		t.Fatalf("a display prefix must be refused naming the expected and given lengths, got %v", err)
	}
}

func TestAuthorTraceRefusesTheWrongClassByName(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))
	_, _, err := resolveTracePin(env, "lower", []string{"REQ-P-1"}, pinAggregate)
	if err == nil || !strings.Contains(err.Error(), "content hash") || !strings.Contains(err.Error(), "packet aggregate") {
		t.Fatalf("the aggregate given for a lower trace must be refused naming both classes, got %v", err)
	}
	_, _, err = resolveTracePin(env, "completion", []string{"EPIC-P", "REQ-P-1"}, pinContentHash)
	if err == nil || !strings.Contains(err.Error(), "content hash") || !strings.Contains(err.Error(), "packet aggregate") {
		t.Fatalf("a content hash given for a completion trace must be refused naming both classes, got %v", err)
	}
}

func TestAuthorTraceWarnsWhenThePinMatchesNothing(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))
	pin, warnings, err := resolveTracePin(env, "completion", []string{"EPIC-P"}, pinUnknown)
	if err != nil || pin != pinUnknown {
		t.Fatalf("a full hash matching nothing is recorded (a guard flags, it does not delete), got %q, %v", pin, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "matches no current packet aggregate or content hash") {
		t.Fatalf("the mismatch must be warned about flag-first, got %v", warnings)
	}
}

func TestAuthorTraceClassUnverifiedWhenTheReadIsUnavailable(t *testing.T) {
	env := wsEnv(t, pinServer(t, ""))
	pin, warnings, err := resolveTracePin(env, "completion", []string{"EPIC-P"}, pinAggregate)
	if err != nil || pin != pinAggregate {
		t.Fatalf("an explicit full hash is recorded when the reference is unreadable, got %q, %v", pin, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "class unverified") {
		t.Fatalf("the unverified class must be warned about, got %v", warnings)
	}
	_, _, err = resolveTracePin(env, "completion", []string{"EPIC-P"}, "")
	if err == nil || !strings.Contains(err.Error(), "--fingerprint") {
		t.Fatalf("an omitted pin with no readable reference is refused naming --fingerprint, got %v", err)
	}
}

// --- REQ-CROSS-431: `--purpose upper` (BACKLOG-TOOL-93) ---
//
// A user requirement reaches IN_REVIEW through its own upper trace, which the
// kit skill and `process advance`'s own refusal both spell as
// `--purpose upper --scope <UR> --fingerprint <its content hash>`. `upper` fell
// to resolveTracePin's default arm, so an omitted --fingerprint posted no pin at
// all and the server refused with "fingerprint is required"; resolveTransition
// had no entry for it either, so the one trace the loop tells you to write by
// hand needed two values typed from memory.

func TestAuthorTraceUpperDefaultsToTheURContentHash(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))

	pin, warnings, err := resolveTracePin(env, "upper", []string{"UR-P"}, "")
	if err != nil || pin != pinURContentHash {
		t.Fatalf("purpose upper must default to the user requirement's content hash, got %q, %v", pin, err)
	}
	if len(warnings) != 0 {
		t.Fatalf("a pin read from the store warns about nothing, got %v", warnings)
	}
}

func TestAuthorTraceUpperValidatesAGivenPin(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))

	if pin, _, err := resolveTracePin(env, "upper", []string{"UR-P"}, pinURContentHash); err != nil || pin != pinURContentHash {
		t.Fatalf("the matching content hash must be accepted, got %q, %v", pin, err)
	}
	_, _, err := resolveTracePin(env, "upper", []string{"UR-P"}, pinURContentHash[:12])
	if err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("a display prefix must be refused for upper as it is for lower, got %v", err)
	}
}

// REQ-CROSS-431 (F-CLI024-R1-01): the packet aggregate given for an upper
// trace is the wrong class, and is refused naming both.
func TestAuthorTraceUpperRefusesTheAggregateByName(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))

	_, _, err := resolveTracePin(env, "upper", []string{"UR-P"}, pinAggregate)
	if err == nil || !strings.Contains(err.Error(), "content hash") || !strings.Contains(err.Error(), "packet aggregate") {
		t.Fatalf("the aggregate given for an upper trace must be refused naming both classes, got %v", err)
	}
}

func TestAuthorTraceUpperIsRefusedWhenTheURServesNoHash(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))

	_, _, err := resolveTracePin(env, "upper", []string{"UR-ABSENT"}, "")
	if err == nil || !strings.Contains(err.Error(), "--fingerprint") {
		t.Fatalf("an unreadable content hash with no --fingerprint is refused naming the flag, got %v", err)
	}
}

func TestAuthorTraceUpperInfersItsTransition(t *testing.T) {
	got, err := resolveTransition("trace", "upper", "", "", "")
	if err != nil || got != "build->verify" {
		t.Fatalf("an upper trace must infer build->verify, got %q, %v", got, err)
	}
	// An explicit pair still wins, and the remaining free purposes still name
	// the flags rather than inventing a transition.
	if got, err := resolveTransition("trace", "upper", "", "verify", "completion"); err != nil || got != "verify->completion" {
		t.Fatalf("--from/--to must still win, got %q, %v", got, err)
	}
	if _, err := resolveTransition("trace", "custom_check", "", "", ""); err == nil || !strings.Contains(err.Error(), "upper") {
		t.Fatalf("the refusal must list upper among the inferring purposes, got %v", err)
	}
}

func TestAuthorTraceLeavesAFreePurposeAlone(t *testing.T) {
	env := wsEnv(t, pinServer(t, pinAggregate))
	pin, warnings, err := resolveTracePin(env, "custom_check", []string{"REQ-P-1"}, "anything")
	if err != nil || pin != "anything" || len(warnings) != 0 {
		t.Fatalf("a free purpose keeps its value untouched, got %q, %v, %v", pin, warnings, err)
	}
}
