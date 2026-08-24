package externalruntime

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDTOFixtureSeparatesPublicAndAuthenticatedFields(t *testing.T) {
	fixture := fixtureCreateIdentityResponse()
	if fixture.AccountToken == "" {
		t.Fatal("fixture must carry authenticated account-token material")
	}
	public, err := json.Marshal(fixture.PublicIdentity)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(public)
	if strings.Contains(encoded, fixture.AccountToken) {
		t.Fatal("public DTO fixture contains authenticated account-token material")
	}
	if fixture.PublicIdentity.AccountID != "acct-fixture" {
		t.Fatalf("account id = %q, want acct-fixture", fixture.PublicIdentity.AccountID)
	}
	if fixture.PublicIdentity.Handle != "player-fixture" {
		t.Fatalf("handle = %q, want player-fixture", fixture.PublicIdentity.Handle)
	}
}

func TestIdentityFixtureStoresVerifierWithoutBearerToken(t *testing.T) {
	fixture := fixtureIdentityRecord()
	if len(fixture.tokenVerifier) != 32 {
		t.Fatalf("token verifier length = %d, want 32", len(fixture.tokenVerifier))
	}
	public, err := json.Marshal(fixture.PublicIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "account-token-fixture") {
		t.Fatal("identity fixture public DTO contains bearer token material")
	}
	if string(fixture.tokenVerifier) == "account-token-fixture" {
		t.Fatal("identity fixture stored bearer token instead of a verifier")
	}
}
