package externalruntime

import "time"

func fixtureCreateIdentityResponse() CreateIdentityResponse {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return CreateIdentityResponse{
		PublicIdentity: PublicIdentity{
			SchemaVersion:           SchemaVersion,
			AccountID:               "acct-fixture",
			Handle:                  "player-fixture",
			ClaimedAt:               now,
			Status:                  IdentityActive,
			PublicDataNoticeVersion: "1.0",
		},
		AccountToken: "account-token-fixture",
		Recovery:     "none",
	}
}
