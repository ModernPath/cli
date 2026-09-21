package zitadel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Organization is one workspace the signed-in person may work in — a ZITADEL
// organization holding a grant for the platform project (REQ-CROSS-335).
type Organization struct {
	ID            string
	Name          string
	PrimaryDomain string
}

// OrganizationList is one page of the person's workspaces.
type OrganizationList struct {
	Organizations []Organization
	// Truncated says the issuer holds more than the page read; Total is what
	// it reported. The command tells the person; this package only reports.
	Truncated bool
	Total     int
}

// ErrSignInRequired says the issuer refused the token, so the sign-in must be
// repeated before the list can be read.
var ErrSignInRequired = errors.New("the identity provider rejected the token: sign in again")

// projectOrgsPath is ZITADEL's authenticated-user API for the organizations
// the calling user holds a grant in on the calling client's project — the
// same call the auth Agent's organization switcher makes. ZITADEL answers it
// by user and project, never narrowed by the token's roles:id scope, and
// accepts the token only when it carries ZITADEL's own audience
// (zitadelProjectAudienceScope among the login scopes).
const projectOrgsPath = "/auth/v1/global/projectorgs/_search"

// organizationListLimit is the one page read. A person granted in more
// workspaces than this is not a case the product has; a reported total
// beyond the page is returned as Truncated, never hidden.
const organizationListLimit = 100

// organizationListTimeout bounds the list request; a stalled issuer fails the
// list rather than the sign-in.
const organizationListTimeout = 15 * time.Second

// ListOrganizations asks the profile's issuer, with the sign-in's own access
// token as bearer, which workspaces the person may work in. A 401 is
// ErrSignInRequired; any other refusal carries the status and the issuer's
// words; a body that is not the expected JSON is named as such. No retry.
func ListOrganizations(ctx context.Context, profile Profile, accessToken string) (OrganizationList, error) {
	ctx, cancel := context.WithTimeout(ctx, organizationListTimeout)
	defer cancel()

	body := fmt.Sprintf(`{"query":{"limit":%d}}`, organizationListLimit)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(profile.Issuer, "/")+projectOrgsPath, strings.NewReader(body))
	if err != nil {
		return OrganizationList{}, fmt.Errorf("could not build the workspace list request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return OrganizationList{}, fmt.Errorf("could not reach the identity provider for the workspace list: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return OrganizationList{}, fmt.Errorf("could not read the workspace list: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return OrganizationList{}, ErrSignInRequired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OrganizationList{}, fmt.Errorf("the identity provider answered %d to the workspace list: %s", resp.StatusCode, issuerWords(raw))
	}

	// ListMyProjectOrgs answers {details: {totalResult}, result: [{id, name,
	// primaryDomain, state, …}]}; totalResult is a JSON string, and the domain
	// arrives in either spelling depending on the transcoder.
	var answer struct {
		Details struct {
			TotalResult string `json:"totalResult"`
		} `json:"details"`
		Result []struct {
			ID                 string `json:"id"`
			Name               string `json:"name"`
			PrimaryDomain      string `json:"primaryDomain"`
			PrimaryDomainSnake string `json:"primary_domain"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return OrganizationList{}, fmt.Errorf("the workspace list is not the expected JSON: %w", err)
	}

	list := OrganizationList{Organizations: make([]Organization, 0, len(answer.Result))}
	for _, org := range answer.Result {
		if org.ID == "" {
			continue
		}
		domain := org.PrimaryDomain
		if domain == "" {
			domain = org.PrimaryDomainSnake
		}
		list.Organizations = append(list.Organizations, Organization{ID: org.ID, Name: org.Name, PrimaryDomain: domain})
	}
	if total, err := strconv.Atoi(answer.Details.TotalResult); err == nil {
		list.Total = total
		list.Truncated = total > len(answer.Result)
	}
	return list, nil
}

// issuerWords keeps the issuer's own explanation short enough for one line.
func issuerWords(raw []byte) string {
	words := strings.TrimSpace(string(raw))
	if len(words) > 200 {
		words = words[:200] + "…"
	}
	return words
}

// SortOrganizations orders the list for the person: home first, then by
// name (case-insensitive), then id. With no home known — a token without
// the resource-owner claim — the order is by name alone.
func SortOrganizations(orgs []Organization, homeID string) []Organization {
	sorted := append([]Organization(nil), orgs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if homeID != "" && (sorted[i].ID == homeID || sorted[j].ID == homeID) {
			return sorted[i].ID == homeID && sorted[j].ID != homeID
		}
		if a, b := strings.ToLower(sorted[i].Name), strings.ToLower(sorted[j].Name); a != b {
			return a < b
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}
