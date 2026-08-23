package relay

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

const relayAllocation = "0198c2c3-4d5e-7f60-8123-456789abcdef"
const relaySender = "0198c2c3-4d5e-7f61-8123-456789abcdef"
const relayRecipient = "0198c2c3-4d5e-7f62-8123-456789abcdef"

func TestRelayAuthenticatesResignsAndDrains(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	senderKey := bytes.Repeat([]byte{0x11}, relayv1.TransportKeyBytes)
	recipientKey := bytes.Repeat([]byte{0x22}, relayv1.TransportKeyBytes)
	relay := New(Config{PacketsPerSecond: 100, BytesPerSecond: 100_000})
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	if err := relay.RegisterAllocation(relayAllocation, map[string][]byte{relaySender: senderKey, relayRecipient: recipientKey}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	source, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: relayAllocation, SenderID: relaySender, RecipientID: relayRecipient, Sequence: 1, Payload: []byte("opaque")}, senderKey, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	var received []byte
	if err := relay.Forward(source, relaySender, now, func(id string, datagram []byte) error {
		if id != relayRecipient {
			t.Fatalf("recipient = %s", id)
		}
		received = datagram
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := relayv1.Decode(received, recipientKey, relayv1.DefaultDatagramLimit); err != nil {
		t.Fatalf("recipient could not verify relay signature: %v", err)
	}
	if _, err := relayv1.Decode(received, senderKey, relayv1.DefaultDatagramLimit); err == nil {
		t.Fatal("recipient packet still verifies with sender key")
	}
	if err := relay.Forward(source, relaySender, now, func(string, []byte) error { return nil }); !errors.Is(err, ErrReplay) {
		t.Fatalf("duplicate packet error = %v", err)
	}
	if err := relay.BeginDrain(); err != nil {
		t.Fatal(err)
	}
	if relay.State() != Draining {
		t.Fatalf("state = %s", relay.State())
	}
	if err := relay.RegisterAllocation("0198c2c3-4d5e-7f63-8123-456789abcdef", map[string][]byte{relaySender: senderKey, relayRecipient: recipientKey}, now.Add(time.Minute)); !errors.Is(err, ErrNotAccepting) {
		t.Fatalf("new allocation during drain = %v", err)
	}
	if err := relay.CloseAllocation(relayAllocation, now); err != nil {
		t.Fatal(err)
	}
	if relay.State() != Empty {
		t.Fatalf("state after closing final allocation = %s", relay.State())
	}
	if err := relay.MarkTerminating(); err != nil {
		t.Fatal(err)
	}
	if relay.State() != Terminating {
		t.Fatalf("state after terminate = %s", relay.State())
	}
	if relay.Metrics().PacketsForwarded != 1 || relay.Metrics().ReplayRejected != 1 {
		t.Fatalf("metrics = %+v", relay.Metrics())
	}
}

func TestRelayRejectsUnauthorizedOversizedAndExpiredTraffic(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{0x31}, relayv1.TransportKeyBytes)
	relay := New(Config{DatagramLimit: 100})
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	if err := relay.RegisterAllocation(relayAllocation, map[string][]byte{relaySender: key, relayRecipient: key}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	packet, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: relayAllocation, SenderID: relaySender, RecipientID: relayRecipient, Sequence: 1, Payload: []byte("x")}, key, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.Forward(packet, relayRecipient, now, func(string, []byte) error { return nil }); !errors.Is(err, ErrUnauthorizedSource) {
		t.Fatalf("unauthorized source error = %v", err)
	}
	if err := relay.Forward(packet, relaySender, now.Add(2*time.Second), func(string, []byte) error { return nil }); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("expired allocation error = %v", err)
	}
	if relay.Metrics().Unauthorized != 1 || relay.Metrics().LeaseExpired != 1 {
		t.Fatalf("metrics = %+v", relay.Metrics())
	}
}
