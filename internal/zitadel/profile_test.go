package zitadel

import "testing"

// REQ-CROSS-235 clause 2: --sso alone selects prod, --sso --test selects
// test — and the two profiles must never mix issuer/client/API values.
func TestSelectProfilePicksTheRightIssuerClientAPITriple(t *testing.T) {
	prod := SelectProfile(false)
	if prod != ProdProfile {
		t.Fatalf("SelectProfile(false) = %+v, want the baked-in ProdProfile %+v", prod, ProdProfile)
	}

	test := SelectProfile(true)
	if test != TestProfile {
		t.Fatalf("SelectProfile(true) = %+v, want the baked-in TestProfile %+v", test, TestProfile)
	}

	if prod.Issuer == test.Issuer || prod.ClientID == test.ClientID || prod.APIURL == test.APIURL {
		t.Fatalf("prod and test profiles must never share a value: prod=%+v test=%+v", prod, test)
	}
}

func TestProfilesAreFixedNotOperatorSupplied(t *testing.T) {
	// D4 (USER:2026-08-24): the registered client IDs are literal, compiled-in
	// constants — never read from a flag, env var, or config file.
	if ProdProfile.Issuer != "https://id.modernpath.ai" {
		t.Fatalf("prod issuer changed: %q", ProdProfile.Issuer)
	}
	if ProdProfile.ClientID != "387656061974216719" {
		t.Fatalf("prod client ID changed: %q", ProdProfile.ClientID)
	}
	if ProdProfile.APIURL != "https://api.modernpath.ai" {
		t.Fatalf("prod API URL changed: %q", ProdProfile.APIURL)
	}
	if TestProfile.Issuer != "https://id.test-plat.modernpath.ai" {
		t.Fatalf("test issuer changed: %q", TestProfile.Issuer)
	}
	if TestProfile.ClientID != "387656000301170778" {
		t.Fatalf("test client ID changed: %q", TestProfile.ClientID)
	}
	if TestProfile.APIURL != "https://api.workload.test-plat.modernpath.ai" {
		t.Fatalf("test API URL changed: %q", TestProfile.APIURL)
	}
}
