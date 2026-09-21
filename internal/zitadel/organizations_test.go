package zitadel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// REQ-CROSS-335 — the CLI lists the signed-in person's workspaces from
// ZITADEL's authenticated-user API (ListMyProjectOrgs), home first, and
// reports what it could not read rather than hiding it.

// orgListServer stands in for the issuer's organization list. It answers
// whatever status and body it is given and records the request it saw.
type orgListServer struct {
	srv    *httptest.Server
	status int
	body   string

	auth        string
	contentType string
	request     map[string]any
	path        string
}

func newOrgListServer(t *testing.T, status int, body string) *orgListServer {
	t.Helper()
	o := &orgListServer{status: status, body: body}
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.path = r.URL.Path
		o.auth = r.Header.Get("Authorization")
		o.contentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &o.request)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(o.status)
		_, _ = io.WriteString(w, o.body)
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *orgListServer) profile() Profile {
	return Profile{Issuer: o.srv.URL, ClientID: "test-client", APIURL: "https://cloud.example.test"}
}

const threeOrgsOutOfOrder = `{"details":{"totalResult":"3"},"result":[
 {"id":"org-zeta","name":"Zeta Works","primaryDomain":"zeta.example","state":"ORG_STATE_ACTIVE"},
 {"id":"org-acme","name":"acme corp","primary_domain":"acme.example","state":"ORG_STATE_ACTIVE"},
 {"id":"org-home","name":"Home Org","primaryDomain":"home.example","state":"ORG_STATE_ACTIVE"}]}`

func TestListOrganizationsAsksTheIssuerWithTheSignInsToken(t *testing.T) {
	server := newOrgListServer(t, http.StatusOK, threeOrgsOutOfOrder)

	list, err := ListOrganizations(context.Background(), server.profile(), "AT-1")
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if server.path != "/auth/v1/global/projectorgs/_search" {
		t.Fatalf("path = %q, want ZITADEL's ListMyProjectOrgs", server.path)
	}
	if server.auth != "Bearer AT-1" {
		t.Fatalf("Authorization = %q, want the sign-in's token as bearer", server.auth)
	}
	if !strings.HasPrefix(server.contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", server.contentType)
	}
	query, _ := server.request["query"].(map[string]any)
	if limit, _ := query["limit"].(float64); limit <= 0 {
		t.Fatalf("request = %v, want a bounded page (query.limit)", server.request)
	}
	if len(list.Organizations) != 3 || list.Truncated {
		t.Fatalf("list = %+v, want three organizations and no truncation", list)
	}
	// Both spellings of the domain field are read.
	for _, org := range list.Organizations {
		if org.PrimaryDomain == "" {
			t.Fatalf("organization %q lost its domain: %+v", org.ID, org)
		}
	}
}

func TestSortOrganizationsPutsHomeFirstThenByName(t *testing.T) {
	orgs := []Organization{
		{ID: "org-zeta", Name: "Zeta Works"},
		{ID: "org-acme", Name: "acme corp"},
		{ID: "org-home", Name: "Home Org"},
		{ID: "org-beta", Name: "Beta"},
	}
	got := SortOrganizations(orgs, "org-home")
	want := []string{"org-home", "org-acme", "org-beta", "org-zeta"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order = %v, want home first then case-insensitive name: %v", ids(got), want)
		}
	}
}

// A token without the resource-owner claim names no home (EPIC-CLI-009
// U3): the sort is by name alone and nothing is marked.
func TestSortOrganizationsWithoutAHomeSortsByNameAlone(t *testing.T) {
	orgs := []Organization{{ID: "b", Name: "Bravo"}, {ID: "a", Name: "alpha"}}
	got := SortOrganizations(orgs, "")
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("order = %v, want alpha then Bravo", ids(got))
	}
}

func ids(orgs []Organization) []string {
	out := make([]string, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, o.ID)
	}
	return out
}

func TestListOrganizationsReportsATotalBeyondThePage(t *testing.T) {
	server := newOrgListServer(t, http.StatusOK, `{"details":{"totalResult":"250"},"result":[{"id":"org-1","name":"One","primaryDomain":"one.example"}]}`)

	list, err := ListOrganizations(context.Background(), server.profile(), "AT-1")
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if !list.Truncated || list.Total != 250 || len(list.Organizations) != 1 {
		t.Fatalf("list = %+v, want the page returned and truncation reported with the total", list)
	}
}

func TestListOrganizationsTreatsA401AsASignInToRepeat(t *testing.T) {
	server := newOrgListServer(t, http.StatusUnauthorized, `{"message":"invalid token"}`)

	_, err := ListOrganizations(context.Background(), server.profile(), "AT-expired")
	if !errors.Is(err, ErrSignInRequired) {
		t.Fatalf("err = %v, want ErrSignInRequired", err)
	}
	if err == nil || !strings.Contains(err.Error(), "sign in again") {
		t.Fatalf("err = %v, want it to say the sign-in must be repeated", err)
	}
}

func TestListOrganizationsCarriesTheIssuersWordsOnAnotherRefusal(t *testing.T) {
	server := newOrgListServer(t, http.StatusBadGateway, `{"message":"upstream exploded"}`)

	_, err := ListOrganizations(context.Background(), server.profile(), "AT-1")
	if err == nil || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("err = %v, want the status and the issuer's words", err)
	}
	if errors.Is(err, ErrSignInRequired) {
		t.Fatal("a 502 is not a reason to sign in again")
	}
}

func TestListOrganizationsNamesAnUnexpectedBody(t *testing.T) {
	server := newOrgListServer(t, http.StatusOK, `<html>not json</html>`)

	_, err := ListOrganizations(context.Background(), server.profile(), "AT-1")
	if err == nil || !strings.Contains(err.Error(), "not the expected JSON") {
		t.Fatalf("err = %v, want the body named as unexpected", err)
	}
}
