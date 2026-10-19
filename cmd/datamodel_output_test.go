package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestDatamodelExportMissingArtifactSuggestsSupportedPreparation(t *testing.T) {
	cause := errors.New(`export failed: {"error":"datamodel_not_found","message":"No data model artifact found"}`)
	err := datamodelExportError(cause)
	if err == nil {
		t.Fatal("expected missing artifact error")
	}
	for _, want := range []string{
		"datamodel_not_found",
		"modernpath analysis start --wait",
		"modernpath analysis reanalyze <repository-id> --wait",
		"modernpath analysis status",
		"modernpath datamodel export",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestDatamodelExportPreservesOtherErrors(t *testing.T) {
	cause := errors.New("network unavailable")
	if got := datamodelExportError(cause); got != cause {
		t.Fatalf("error changed: got %v, want original %v", got, cause)
	}
}
