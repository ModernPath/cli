package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEpicGetSendsNoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if len(body) != 0 {
			t.Errorf("GET body = %q; gateway requires no request body", body)
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = w.Write([]byte(`{"data":{"id":287}}`))
	}))
	defer server.Close()
	status, _, err := (&cliEnv{APIURL: server.URL}).get("/api/work/epics/287")
	if err != nil || status != http.StatusOK {
		t.Fatalf("Epic GET failed: status=%d err=%v", status, err)
	}
}
