package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// REQ-CROSS-332: only an absent endpoint allows required-section fallback;
// other failed reads must leave the authoring tree untouched.
func TestRequiredSectionsFallbackUsesHTTPStatus(t *testing.T) {
	for _, status := range []int{404, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fx := scaffoldFixture()
			env, dir := pulledScope(t, fx)
			before := scopeTreeBytes(t, dir)
			fx.deliveryContextStatus = status
			fx.epics[0].(map[string]any)["title"] = "refreshed title"
			_, err := readDeliveryContextFor(env, "EPIC-CLI-008")
			var responseErr *deliveryContextHTTPError
			if !errors.As(fmt.Errorf("required sections: %w", err), &responseErr) || responseErr.StatusCode != status {
				t.Fatalf("wrapped delivery-context error lost HTTP %d: %v", status, err)
			}
			out := captureWarnings(t, func() { err = workingSetPullScope(env, false, wsNow) })
			if status != 404 {
				if err == nil || !reflect.DeepEqual(before, scopeTreeBytes(t, dir)) {
					t.Fatalf("HTTP %d must refuse without authoring writes: %v", status, err)
				}
				if strings.Contains(out, "frozen member list") {
					t.Fatalf("HTTP %d incorrectly fell back: %s", status, out)
				}
				return
			}
			if err != nil || !strings.Contains(out, "frozen member list") {
				t.Fatalf("absent endpoint must use the named fallback: %v\n%s", err, out)
			}
			if !strings.Contains(readScopeFile(t, filepath.Join(dir, "EPIC-CLI-008.md")), "refreshed title") {
				t.Fatal("404 fallback did not complete the pull")
			}
			if _, err := os.Stat(filepath.Join(dir, "packet", "20-enrichment-REQ-CROSS-310.md")); err != nil {
				t.Fatalf("fallback did not preserve the required member scaffold: %v", err)
			}
		})
	}
}
