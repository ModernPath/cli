package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlanningImageCommandsAreRegisteredForBothOwners(t *testing.T) {
	for _, owner := range []string{"epics", "initiative"} {
		for _, action := range []string{"list", "download", "upload", "remove"} {
			command, _, err := rootCmd.Find([]string{owner, "images", action})
			if err != nil || command.Name() != action {
				t.Errorf("expected %s images %s, got %v", owner, action, err)
			}
		}
	}
}

func TestPlanningImageCommandsUseTheBoundSystemAndExactOwner(t *testing.T) {
	const line = "12345678-1111-4444-8888-abcdef000001"
	const image = "12345678-1111-4444-8888-abcdef000002"
	const request = "12345678-1111-4444-8888-abcdef000003"
	png := []byte("\x89PNG\r\n\x1a\nfixture")

	for _, owner := range []struct{ name, path, id string }{
		{"Epic", "/api/work/epics/", "477"},
		{"Initiative", "/api/roadmap/lines/", line},
	} {
		t.Run(owner.name, func(t *testing.T) {
			base := owner.path + owner.id + "/planning-context/images"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RawQuery != "system_id=105" || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("missing bound System or authenticated request: %s", r.URL)
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == base:
					fmt.Fprintf(w, `{"data":[{"id":"%s","name":"design.png"}]}`, image)
				case r.Method == http.MethodGet && r.URL.Path == base+"/"+image+"/blob":
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(png)
				case r.Method == http.MethodPost && r.URL.Path == base:
					if r.FormValue("request_id") != request {
						t.Error("upload did not carry its request UUID")
					}
					file, header, err := r.FormFile("file")
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					defer file.Close()
					data, err := io.ReadAll(file)
					if err != nil || header.Filename != "design.png" || !bytes.Equal(data, png) {
						t.Error("multipart upload changed the filename or image")
					}
					w.WriteHeader(http.StatusCreated)
					fmt.Fprintf(w, `{"data":{"id":"%s","name":"design.png"}}`, image)
				case r.Method == http.MethodDelete && r.URL.Path == base+"/"+image:
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Path == base+"/"+request+"/blob":
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"error":"not_found"}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			run := func(args ...string) (string, error) {
				cmd := newPlanningImagesCommand(func() (*cliEnv, error) {
					return &cliEnv{APIURL: server.URL, token: "test-token", SystemID: 105}, nil
				}, owner.name, owner.path)
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				cmd.SetArgs(args)
				err := cmd.Execute()
				return out.String(), err
			}

			if out, err := run("list", owner.id); err != nil || !strings.Contains(out, image) {
				t.Fatalf("list: %v %s", err, out)
			}
			file := filepath.Join(t.TempDir(), "design.png")
			if _, err := run("download", owner.id, request, "-o", file); err == nil {
				t.Fatal("accepted a missing image")
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatal("failed download created a file")
			}
			if out, err := run("download", owner.id, image, "-o", file); err != nil {
				t.Fatalf("download: %v %s", err, out)
			}
			if data, err := os.ReadFile(file); err != nil || !bytes.Equal(png, data) {
				t.Fatalf("saved wrong bytes: %v", err)
			}
			if _, err := run("download", owner.id, image, "-o", file); err == nil {
				t.Fatal("overwrote an existing file")
			}
			if out, err := run("upload", owner.id, file, "--request-id", request); err != nil || !strings.Contains(out, image) {
				t.Fatalf("upload: %v %s", err, out)
			}
			if out, err := run("remove", owner.id, image); err != nil {
				t.Fatalf("remove: %v %s", err, out)
			}
		})
	}
}

func TestPlanningImageRequestsRefuseRedirects(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/images" {
					t.Error("image request followed a redirect")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Location", "/other")
				w.WriteHeader(http.StatusTemporaryRedirect)
			}))
			defer server.Close()
			env := &cliEnv{APIURL: server.URL, token: "test-token"}
			var body []byte
			if method == http.MethodPost {
				body = []byte("synthetic upload")
			}
			_, err := planningImageRequest(env, method, "/images", body, "application/json", 1<<20, http.StatusOK)
			if err == nil || !strings.Contains(err.Error(), "307") {
				t.Fatalf("expected a redirect refusal, got %v", err)
			}
		})
	}
}

func TestPlanningImageRemoveRequiresAcknowledgement(t *testing.T) {
	for _, response := range []struct {
		name string
		code int
		body string
		want string
	}{
		{"wrong success", 200, "<html>Sign in</html>", "HTTP 200"},
		{"lifecycle refusal", 409, `{"error":"invalid_lifecycle_state","message":"Only a Proposed Epic's planning images can change."}`, "invalid_lifecycle_state — Only a Proposed Epic's planning images can change. (ref test-reference)"},
		{"removed", 204, "", ""},
	} {
		t.Run(response.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-request-id", "test-reference")
				w.WriteHeader(response.code)
				if response.body != "" {
					fmt.Fprint(w, response.body)
				}
			}))
			defer server.Close()
			cmd := newPlanningImagesCommand(func() (*cliEnv, error) {
				return &cliEnv{APIURL: server.URL, token: "test-token", SystemID: 105}, nil
			}, "Epic", "/api/work/epics/")
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"remove", "477", "12345678-1111-4444-8888-abcdef000002"})
			err := cmd.Execute()
			if response.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), response.want) {
				t.Fatalf("expected %q, got %v", response.want, err)
			}
		})
	}
}

func TestPlanningImageUploadRateLimitRetriesIdenticalRequest(t *testing.T) {
	originalSleep := limitSleep
	t.Cleanup(func() { limitSleep = originalSleep })
	const request = "12345678-1111-4444-8888-abcdef000003"
	file := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, exhaust := range []bool{false, true} {
		var waits []time.Duration
		limitSleep = func(wait time.Duration) { waits = append(waits, wait) }
		body, contentType, err := planningImageUpload(file, request)
		if err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			call := requests.Add(1)
			data, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(data, body) || r.Header.Get("Content-Type") != contentType {
				t.Error("retry did not send the same multipart request")
			}
			if exhaust || call == 1 {
				w.Header().Set("Retry-After", "30")
				http.Error(w, "gateway limit", http.StatusTooManyRequests)
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"data":{"id":"fixture"}}`)
		}))
		env := &cliEnv{APIURL: server.URL, token: "test-token"}
		data, err := planningImageRequest(env, http.MethodPost, "/images", body, contentType, 1<<20, http.StatusCreated)
		server.Close()
		calls := requests.Load()

		if exhaust {
			var limited serverLimitingError
			if !errors.As(err, &limited) || calls != 5 || len(waits) != 4 {
				t.Fatalf("expected typed rate-limit error after 5 calls and 4 waits, got %v (%d calls, %v waits)", err, calls, waits)
			}
		} else if err != nil || !bytes.Contains(data, []byte("fixture")) || calls != 2 || len(waits) != 1 {
			t.Fatalf("expected successful retry, got %q, %v (%d calls, %v waits)", data, err, calls, waits)
		}
		for _, wait := range waits {
			if wait != 30*time.Second {
				t.Fatalf("did not honor Retry-After: got %s", wait)
			}
		}
	}
}
