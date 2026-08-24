package relay

import (
	"errors"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

func TestERM104DrainRejectsNewAllocationsAndCompletesExistingLease(t *testing.T) {
	fixture := fixtureDrainLifecycle()
	service := New(fixture.config)
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterAllocation(fixture.allocationID, map[string][]byte{fixture.senderID: fixture.senderKey, fixture.recipientID: fixture.recipientKey}, fixture.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := service.BeginDrain(); err != nil {
		t.Fatal(err)
	}
	if service.State() != Draining {
		t.Fatalf("state after drain = %s", service.State())
	}
	if err := service.RegisterAllocation(fixture.replacementAllocationID, map[string][]byte{fixture.senderID: fixture.senderKey, fixture.recipientID: fixture.recipientKey}, fixture.now.Add(time.Minute)); !errors.Is(err, ErrNotAccepting) {
		t.Fatalf("new allocation during drain = %v", err)
	}

	datagram, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: fixture.allocationID, SenderID: fixture.senderID, RecipientID: fixture.recipientID, Sequence: 1, Payload: []byte("drain")}, fixture.senderKey, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Forward(datagram, fixture.senderID, fixture.now, func(string, []byte) error { return nil }); err != nil {
		t.Fatalf("existing lease during drain = %v", err)
	}
	if err := service.CloseAllocation(fixture.allocationID, fixture.now); err != nil {
		t.Fatal(err)
	}
	if service.State() != Empty {
		t.Fatalf("state after existing lease closes = %s", service.State())
	}
	if err := service.MarkTerminating(); err != nil {
		t.Fatal(err)
	}
	if service.Metrics().PacketsForwarded != 1 {
		t.Fatalf("metrics = %+v", service.Metrics())
	}
}
