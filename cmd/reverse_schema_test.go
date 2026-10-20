package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReverseEngineerSchema(t *testing.T) {
	const schema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"future_field":{"type":"string"}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/systems/4/reverse-engineering/schema" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":` + schema + `}`))
	}))
	defer server.Close()
	command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"schema"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"future_field"`)) {
		t.Fatalf("schema was not passed through: %s", out.String())
	}
}

func TestReverseEngineerSchemaRejectsUnavailableModel(t *testing.T) {
	for _, response := range []struct {
		name   string
		status int
		body   string
	}{
		{"old server", 404, `{"error":"not_found"}`},
		{"missing schema", 200, `{"data":{}}`},
	} {
		t.Run(response.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(response.status)
				_, _ = w.Write([]byte(response.body))
			}))
			defer server.Close()
			command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			var out bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&out)
			command.SetArgs([]string{"schema"})
			if err := command.Execute(); err == nil {
				t.Fatal("must refuse instead of inventing a fallback schema")
			}
		})
	}
}
