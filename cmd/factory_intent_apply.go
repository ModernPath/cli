package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	processRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	fingerprintPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func applyRDDIntent(env *factoryEnv, intent map[string]any, jobRef string) (map[string]any, error) {
	request, err := buildIntentApplyRequest(env.Root, env.SystemID, intent, jobRef)
	if err != nil {
		return nil, err
	}

	externalID := str(intent, "external_id")
	status, body, err := env.call(
		"POST",
		"/api/v1/sync/intents/"+url.PathEscape(externalID)+"/apply",
		request,
	)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("intent apply failed for %s: server %d: %v", externalID, status, body["error"])
	}
	application, ok := dataOf(body)["application"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("intent apply failed for %s: server 200 response has no data.application", externalID)
	}
	return application, nil
}

func buildIntentApplyRequest(root string, systemID int, intent map[string]any, jobRef string) (map[string]any, error) {
	revision, err := readWorkspaceProcessRevision(root)
	if err != nil {
		return nil, err
	}

	externalID := str(intent, "external_id")
	contentFingerprint := str(intent, "content_fingerprint")
	evaluatedScopeFingerprint := str(intent, "evaluated_scope_fingerprint")
	if strings.TrimSpace(externalID) == "" {
		return nil, fmt.Errorf("pending intent has no external_id")
	}
	if !fingerprintPattern.MatchString(contentFingerprint) {
		return nil, fmt.Errorf("pending intent %s has invalid content_fingerprint", externalID)
	}
	if !fingerprintPattern.MatchString(evaluatedScopeFingerprint) {
		return nil, fmt.Errorf("pending intent %s has invalid evaluated_scope_fingerprint", externalID)
	}

	request := map[string]any{
		"system_id":                            systemID,
		"workspace_process_revision":           revision,
		"idempotency_key":                      intentApplyIdempotencyKey(systemID, externalID, revision, contentFingerprint, evaluatedScopeFingerprint),
		"expected_content_fingerprint":         contentFingerprint,
		"expected_evaluated_scope_fingerprint": evaluatedScopeFingerprint,
	}
	if strings.TrimSpace(jobRef) != "" {
		request["job_ref"] = jobRef
	}
	return request, nil
}

func readWorkspaceProcessRevision(root string) (string, error) {
	path := filepath.Join(root, ".modernpath", "rdd", ".source")
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w; run 'modernpath install' and retry with rdd-start", path, err)
	}

	var revisions []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "revision=") {
			revisions = append(revisions, strings.TrimPrefix(line, "revision="))
		}
	}
	if len(revisions) != 1 || !processRevisionPattern.MatchString(revisions[0]) {
		return "", fmt.Errorf("%s must contain exactly one lowercase full revision=<40 hex SHA>; run 'modernpath install' and retry with rdd-start", path)
	}
	return revisions[0], nil
}

func intentApplyIdempotencyKey(systemID int, externalID, revision, contentFingerprint, evaluatedScopeFingerprint string) string {
	identity := strings.Join([]string{
		"intent-apply",
		strconv.Itoa(systemID),
		externalID,
		revision,
		contentFingerprint,
		evaluatedScopeFingerprint,
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return "sha256:" + hex.EncodeToString(sum[:])
}
