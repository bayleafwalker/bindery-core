package relay

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

// TestAdmitClientAccumulatesClients is the property RegisterAllocation cannot
// provide. A control plane admits one client per enrollment, so the second
// admission must not evict the first the way replacing the client set does.
func TestAdmitClientAccumulatesClients(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	senderKey := bytes.Repeat([]byte{0x11}, relayv1.TransportKeyBytes)
	recipientKey := bytes.Repeat([]byte{0x22}, relayv1.TransportKeyBytes)
	relay := New(Config{PacketsPerSecond: 100, BytesPerSecond: 100_000})
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	if err := relay.AdmitClient(relayAllocation, relaySender, senderKey, now.Add(time.Minute)); err != nil {
		t.Fatalf("admit sender: %v", err)
	}
	if err := relay.AdmitClient(relayAllocation, relayRecipient, recipientKey, now.Add(time.Minute)); err != nil {
		t.Fatalf("admit recipient: %v", err)
	}
	datagram, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: relayAllocation, SenderID: relaySender, RecipientID: relayRecipient, Sequence: 1, Payload: []byte("opaque")}, senderKey, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	delivered := false
	if err := relay.Forward(datagram, relaySender, now, func(id string, forwarded []byte) error {
		delivered = true
		if id != relayRecipient {
			t.Fatalf("recipient = %s, want %s", id, relayRecipient)
		}
		if _, err := relayv1.Decode(forwarded, recipientKey, relayv1.DefaultDatagramLimit); err != nil {
			t.Fatalf("recipient could not verify: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("forward after two separate admissions: %v", err)
	}
	if !delivered {
		t.Fatal("packet was not delivered")
	}
}

// A lease is a property of the allocation, so a client arriving with a shorter
// one must not cut short a match the relay already agreed to carry.
func TestAdmitClientOnlyExtendsTheLease(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{0x11}, relayv1.TransportKeyBytes)
	relay := New(Config{PacketsPerSecond: 100, BytesPerSecond: 100_000})
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	if err := relay.AdmitClient(relayAllocation, relaySender, key, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := relay.AdmitClient(relayAllocation, relayRecipient, key, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	datagram, err := relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: relayAllocation, SenderID: relaySender, RecipientID: relayRecipient, Sequence: 1, Payload: []byte("opaque")}, key, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	// Past the short lease, inside the long one.
	err = relay.Forward(datagram, relaySender, now.Add(30*time.Minute), func(string, []byte) error { return nil })
	if errors.Is(err, ErrLeaseExpired) {
		t.Fatal("a later, shorter admission shortened the allocation lease")
	}
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
}

func TestAdmitClientRefusesInvalidInput(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{0x11}, relayv1.TransportKeyBytes)
	relay := New(Config{})
	if err := relay.AdmitClient(relayAllocation, relaySender, key, now.Add(time.Minute)); !errors.Is(err, ErrNotAccepting) {
		t.Fatalf("admission before start = %v, want ErrNotAccepting", err)
	}
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	if err := relay.AdmitClient("not-a-uuid", relaySender, key, now.Add(time.Minute)); err == nil {
		t.Fatal("expected a non-UUID allocation to be refused")
	}
	if err := relay.AdmitClient(relayAllocation, "not-a-uuid", key, now.Add(time.Minute)); err == nil {
		t.Fatal("expected a non-UUID client to be refused")
	}
	if err := relay.AdmitClient(relayAllocation, relaySender, key[:8], now.Add(time.Minute)); !errors.Is(err, relayv1.ErrInvalidKey) {
		t.Fatal("expected a short key to be refused")
	}
	if err := relay.AdmitClient(relayAllocation, relaySender, key, time.Time{}); err == nil {
		t.Fatal("expected a missing lease to be refused")
	}
}
