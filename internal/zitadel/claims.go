package zitadel

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Claims are the values the CLI reads from a token to decide about a
// workspace (REQ-CROSS-335): the issuer, the home organization ZITADEL names
// in the resource-owner claim, and the organization the platform claim says
// the token acts in.
type Claims struct {
	Issuer             string
	HomeOrganizationID string
	OrganizationID     string
	// Subject, Email and ExpiresAt are the identity and lifetime claims
	// `auth status` and the expiry check read (REQ-CROSS-388/389).
	Subject   string
	Email     string
	ExpiresAt time.Time
}

// The platform claim is keyed `urn:modernpath:token:v2`; the version is part
// of the key (zitadel-actions/docs/token-format.md §3).
//
// TokenClaims reads Claims out of a JWT payload without verifying anything,
// reporting false when the value is not a readable JWT. No signature check
// happens here on purpose: this is a local read whose only use is to refuse
// storing a credential that names another workspace than the one chosen —
// the parties that must not trust an unverified token, the edge and core,
// both validate it in full.
func TokenClaims(token string) (Claims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, false
	}
	var raw struct {
		Issuer   string  `json:"iss"`
		Subject  string  `json:"sub"`
		Email    string  `json:"email"`
		Exp      float64 `json:"exp"`
		Home     string  `json:"urn:zitadel:iam:user:resourceowner:id"`
		Platform struct {
			OrgID string `json:"org_id"`
		} `json:"urn:modernpath:token:v2"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Claims{}, false
	}
	claims := Claims{
		Issuer:             raw.Issuer,
		HomeOrganizationID: raw.Home,
		OrganizationID:     raw.Platform.OrgID,
		Subject:            raw.Subject,
		Email:              raw.Email,
	}
	// A missing exp stays zero: "no lifetime known" is not "expired now".
	if raw.Exp > 0 {
		claims.ExpiresAt = time.Unix(int64(raw.Exp), 0).UTC()
	}
	return claims, true
}
