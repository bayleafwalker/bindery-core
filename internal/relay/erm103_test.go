package relay

import (
	"errors"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

func TestERM103AllocationLeaseRetainsPlacementDecision(t *testing.T) {
	fixture := fixtureAllocationLease()
	decision, err := ChoosePlacement(fixture.request, fixture.candidates)
	if err != nil {
		t.Fatal(err)
	}
	if decision.ProviderID != fixture.expectedProviderID || decision.RelayID != fixture.expectedRelayID || decision.Region != fixture.expectedRegion {
		t.Fatalf("placement = %+v", decision)
	}
	if decision.PolicyVersion != PlacementPolicyVersion {
		t.Fatalf("policy version = %q", decision.PolicyVersion)
	}
	if len(decision.Inputs) != len(fixture.candidates) || decision.Inputs[0].ParticipantRTTMS["player-a"] != 30 {
		t.Fatalf("decision inputs = %+v", decision.Inputs)
	}

	service := New(Config{PacketsPerSecond: 100, BytesPerSecond: 100_000})
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterAllocation(fixture.allocationID, map[string][]byte{fixture.senderID: fixture.senderKey, fixture.recipientID: fixture.recipientKey}, fixture.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	datagram, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: fixture.allocationID, SenderID: fixture.senderID, RecipientID: fixture.recipientID, Sequence: 1, Payload: []byte("lease")}, fixture.senderKey, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Forward(datagram, fixture.senderID, fixture.now.Add(2*time.Minute), func(string, []byte) error { return nil }); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("expired allocation error = %v", err)
	}
	if service.Metrics().LeaseExpired != 1 {
		t.Fatalf("metrics = %+v", service.Metrics())
	}
}
