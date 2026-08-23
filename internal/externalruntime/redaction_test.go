package externalruntime

import "testing"

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
