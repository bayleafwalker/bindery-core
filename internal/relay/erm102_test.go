package relay

import (
	"errors"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

func TestERM102BoundedRelayEnforcesOpaqueSourceSizeAndRateLimits(t *testing.T) {
	fixture := fixtureBoundedRelay()
	service := New(fixture.config)
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterAllocation(fixture.allocationID, map[string][]byte{fixture.senderID: fixture.senderKey, fixture.recipientID: fixture.recipientKey}, fixture.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	valid, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: fixture.allocationID, SenderID: fixture.senderID, RecipientID: fixture.recipientID, Sequence: 1, Payload: []byte("ok")}, fixture.senderKey, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	var delivered []byte
	if err := service.Forward(valid, fixture.senderID, fixture.now, func(_ string, datagram []byte) error {
		delivered = append([]byte(nil), datagram...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := relayv1.Decode(delivered, fixture.recipientKey, relayv1.DefaultDatagramLimit); err != nil {
		t.Fatalf("forwarded datagram did not verify with recipient key: %v", err)
	}

	if err := service.Forward(valid, fixture.recipientID, fixture.now, func(string, []byte) error { return nil }); !errors.Is(err, ErrUnauthorizedSource) {
		t.Fatalf("unauthorized source error = %v", err)
	}
	overlarge, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: fixture.allocationID, SenderID: fixture.senderID, RecipientID: fixture.recipientID, Sequence: 2, Payload: []byte("bad")}, fixture.senderKey, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Forward(overlarge, fixture.senderID, fixture.now, func(string, []byte) error { return nil }); !errors.Is(err, relayv1.ErrOversized) {
		t.Fatalf("oversized datagram error = %v", err)
	}
	if err := service.Forward(valid, fixture.senderID, fixture.now, func(string, []byte) error { return nil }); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limit error = %v", err)
	}

	metrics := service.Metrics()
	if metrics.PacketsForwarded != 1 || metrics.Unauthorized != 1 || metrics.Oversized != 1 || metrics.RateLimited != 1 || metrics.PacketsDropped != 3 {
		t.Fatalf("metrics = %+v", metrics)
	}
}
