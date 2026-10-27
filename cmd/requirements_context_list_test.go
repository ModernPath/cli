package cmd

// Epic 340 (G1), test case cb517e6b: `modernpath requirements contexts`
// reads GET /api/v1/systems/:system_id/requirements/contexts for the bound
// System and prints one line per code, starting with the code, with its name,
// its number of requirements and its other names; then a line starting
// "Unclassified" with how many requirements have no context. --json prints the
// route's data object. In a workspace bound to no System it stops before
// sending a request, with an error that names the bound System, and a refusal
// exits non-zero with the server's reason.
//
// After the first slice the command sends its request and prints nothing, so
// these assertions fail on the output and the errors.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const requirementContextsReply = `{"data":{"contexts":[
  {"code":"BILLING","name":"Billing","other_names":["Invoices"],"requirement_count":3},
  {"code":"ORDERS","name":"ORDERS","other_names":[],"requirement_count":1}],
 "without_context":2}}`

// requirementContextsServer answers the reachability probe and records every
// other request as "<method> <path>".
func requirementContextsServer(t *testing.T, status int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

// outputLine returns the first output line that starts with prefix and its
// index, or -1.
func outputLine(out, prefix string) (string, int) {
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return line, i
		}
	}
	return "", -1
}

func TestRequirementsContextsPrintsEachCodeThenUnclassified(t *testing.T) {
	server, seen := requirementContextsServer(t, http.StatusOK, requirementContextsReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "requirements", "contexts")
	if err != nil {
		t.Fatalf("requirements contexts: %v\n%s", err, out)
	}
	if want := []string{"GET /api/v1/systems/1/requirements/contexts"}; !reflect.DeepEqual(*seen, want) {
		t.Fatalf("requests = %v; want %v", *seen, want)
	}

	billing, billingAt := outputLine(out, "BILLING")
	if billingAt < 0 {
		t.Fatalf("no line starts with the code BILLING:\n%s", out)
	}
	for _, want := range []string{"Billing", "Invoices"} {
		if !strings.Contains(billing, want) {
			t.Errorf("the BILLING line lacks %q: %q", want, billing)
		}
	}
	if !regexp.MustCompile(`\b3\b`).MatchString(billing) {
		t.Errorf("the BILLING line lacks its 3 requirements: %q", billing)
	}
	orders, ordersAt := outputLine(out, "ORDERS")
	if ordersAt < 0 || !regexp.MustCompile(`\b1\b`).MatchString(orders) {
		t.Errorf("no ORDERS line with its 1 requirement:\n%s", out)
	}
	unclassified, unclassifiedAt := outputLine(out, "Unclassified")
	if unclassifiedAt < 0 || !regexp.MustCompile(`\b2\b`).MatchString(unclassified) {
		t.Fatalf("no line labelled Unclassified with the 2 requirements without a context:\n%s", out)
	}
	if unclassifiedAt < billingAt || unclassifiedAt < ordersAt {
		t.Errorf("the Unclassified line comes before a code's line:\n%s", out)
	}
}

func TestRequirementsContextsJSONPrintsTheRoutesData(t *testing.T) {
	server, _ := requirementContextsServer(t, http.StatusOK, requirementContextsReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "requirements", "contexts", "--json")
	if err != nil {
		t.Fatalf("requirements contexts --json: %v\n%s", err, out)
	}
	var reply struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(requirementContextsReply), &reply); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json did not print a JSON object: %v\n%q", err, out)
	}
	if !reflect.DeepEqual(got, reply.Data) {
		t.Fatalf("--json printed %#v; want the route's data %#v", got, reply.Data)
	}
}

func TestRequirementsContextsRefusesAWorkspaceBoundToNoSystem(t *testing.T) {
	server, seen := requirementContextsServer(t, http.StatusOK, requirementContextsReply)
	cobraWorkspace(t, server, 0)

	_, err := runRoot(t, "requirements", "contexts")
	if err == nil || !strings.Contains(err.Error(), "bound System") {
		t.Fatalf("error = %v; want a refusal that names the bound System", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("a workspace bound to no System reached the server: %v", *seen)
	}
}

func TestRequirementsContextsReportsTheServersRefusal(t *testing.T) {
	server, _ := requirementContextsServer(t, http.StatusNotFound, `{"error":"not_found"}`)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "requirements", "contexts")
	if err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("error = %v; want a non-zero exit with the server's reason not_found\n%s", err, out)
	}
}
