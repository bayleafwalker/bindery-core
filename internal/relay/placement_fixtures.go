package relay

import (
	"bytes"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

func fixturePlacementRequest() PlacementRequest { return PlacementRequest{} }

func fixturePlacementCandidates() []Candidate { return nil }

type allocationLeaseFixture struct {
	now                time.Time
	allocationID       string
	senderID           string
	recipientID        string
	senderKey          []byte
	recipientKey       []byte
	request            PlacementRequest
	candidates         []Candidate
	expectedProviderID string
	expectedRelayID    string
	expectedRegion     string
}

func fixtureAllocationLease() allocationLeaseFixture {
	return allocationLeaseFixture{
		now:          time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		allocationID: "0198c2c3-4d5e-7f60-8123-456789abcdef",
		senderID:     "0198c2c3-4d5e-7f61-8123-456789abcdef",
		recipientID:  "0198c2c3-4d5e-7f62-8123-456789abcdef",
		senderKey:    bytes.Repeat([]byte{0x11}, relayv1.TransportKeyBytes),
		recipientKey: bytes.Repeat([]byte{0x22}, relayv1.TransportKeyBytes),
		request: PlacementRequest{
			AllowedRegions: []string{"eu-north"},
			ParticipantIDs: []string{"player-a", "player-b"},
			LatencyP95MS:   100,
		},
		candidates: []Candidate{
			{ProviderID: "native", RelayID: "relay-b", Region: "eu-north", State: Accepting, RemainingPacketRate: 900, RemainingEgressBytesPS: 10_000, ParticipantRTTMS: map[string]int{"player-a": 30, "player-b": 50}},
			{ProviderID: "native", RelayID: "relay-a", Region: "eu-north", State: Accepting, RemainingPacketRate: 900, RemainingEgressBytesPS: 10_000, ParticipantRTTMS: map[string]int{"player-a": 30, "player-b": 50}},
		},
		expectedProviderID: "native",
		expectedRelayID:    "relay-a",
		expectedRegion:     "eu-north",
	}
}
