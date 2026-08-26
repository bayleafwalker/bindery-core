package externalruntime

import (
	"encoding/json"
	"testing"
)

func TestRedactionOracleRejectsSecretFieldsAndFixtures(t *testing.T) {
	if err := ScanPublicOutput([]byte(`{"account_id":"public","handle":"player-one"}`), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []string{
		`{"account_token":"fixture-token"}`,
		`{"transport_credential":"fixture-transport"}`,
		`{"upload_url":"https://internal.invalid/upload"}`,
		`{"source_ip":"192.0.2.10"}`,
		`{"authorization":"Bearer fixture-token"}`,
	} {
		if err := ScanPublicOutput([]byte(sample), "fixture-token"); err == nil {
			t.Fatalf("redaction oracle accepted %s", sample)
		}
	}
	if err := ScanStructuredLog([]byte(`{"event":"request","authorization":"Bearer fixture-token"}`), "fixture-token"); err == nil {
		t.Fatal("log oracle accepted authorization header")
	}
}

func TestRedactionOracleAllowsPublishedRelayPlacementEndpoint(t *testing.T) {
	public, err := json.Marshal(PublicSession{Placement: &PublicPlacement{
		SchemaVersion:     SchemaVersion,
		PlacementID:       "0198c2c3-4d5e-7f60-8123-456789abcdea",
		SessionID:         "0198c2c3-4d5e-7f60-8123-456789abcdeb",
		Region:            "eu-north",
		RelayProviderID:   "bindery-native",
		RelayAllocationID: "0198c2c3-4d5e-7f60-8123-456789abcdef",
		RelayEndpoint:     "127.0.0.1:40000",
		PolicyVersion:     "relay-placement/v1",
		Allocator:         fixtureAllocatorIdentity(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ScanPublicOutput(public); err != nil {
		t.Fatalf("published relay placement was rejected: %v; bytes=%s", err, public)
	}
	if err := ScanPublicOutput([]byte(`{"source_endpoint":"127.0.0.1:40000"}`)); err == nil {
		t.Fatal("unscoped endpoint field was accepted")
	}
}
