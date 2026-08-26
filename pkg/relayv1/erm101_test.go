package relayv1

import (
	"bytes"
	"testing"
)

func TestERM101DatagramEnvelopeIdentifiesEndpointsWithoutDecodingPayload(t *testing.T) {
	for _, fixture := range fixtureDatagramCases() {
		t.Run(string(fixture.packet.Type), func(t *testing.T) {
			datagram, err := Encode(fixture.packet, fixture.key, DefaultDatagramLimit)
			if err != nil {
				t.Fatal(err)
			}
			if len(datagram) != HeaderBytes+len(fixture.packet.Payload) {
				t.Fatalf("datagram length = %d, want %d", len(datagram), HeaderBytes+len(fixture.packet.Payload))
			}
			header, err := Peek(datagram, DefaultDatagramLimit)
			if err != nil {
				t.Fatal(err)
			}
			if header.Type != fixture.packet.Type || header.AllocationID != fixture.packet.AllocationID || header.SenderID != fixture.packet.SenderID || header.RecipientID != fixture.packet.RecipientID || header.Sequence != fixture.packet.Sequence || header.PayloadBytes != len(fixture.packet.Payload) {
				t.Fatalf("header = %+v, want packet identity and payload length", header)
			}
			if bytes.Contains(datagram[:HeaderBytes], fixture.packet.Payload) {
				t.Fatal("opaque payload appeared in the fixed header")
			}
			decoded, err := Decode(datagram, fixture.key, DefaultDatagramLimit)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Type != fixture.packet.Type || decoded.AllocationID != fixture.packet.AllocationID || decoded.SenderID != fixture.packet.SenderID || decoded.RecipientID != fixture.packet.RecipientID || decoded.Sequence != fixture.packet.Sequence || !bytes.Equal(decoded.Payload, fixture.packet.Payload) {
				t.Fatalf("decoded = %+v, want opaque packet round trip", decoded)
			}
		})
	}
}
