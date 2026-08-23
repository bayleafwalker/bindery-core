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
