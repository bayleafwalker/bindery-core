package relayv1

import (
	"bytes"
	"testing"
)

const allocationID = "0198c2c3-4d5e-7f60-8123-456789abcdef"
const senderID = "0198c2c3-4d5e-7f61-8123-456789abcdef"
const recipientID = "0198c2c3-4d5e-7f62-8123-456789abcdef"

func TestCodecRoundTripAndOpaquePayload(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, TransportKeyBytes)
	payload := []byte{0, 1, 2, 0xff, 0x00}
	datagram, err := Encode(Packet{Type: PacketData, AllocationID: allocationID, SenderID: senderID, RecipientID: recipientID, Sequence: 7, Payload: payload}, key, DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(datagram, key, DefaultDatagramLimit)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.AllocationID != allocationID || decoded.SenderID != senderID || decoded.RecipientID != recipientID || decoded.Sequence != 7 || !bytes.Equal(decoded.Payload, payload) {
		t.Fatalf("decoded packet differs: %+v", decoded)
	}
	if _, err := Decode(datagram, bytes.Repeat([]byte{0x43}, TransportKeyBytes), DefaultDatagramLimit); err != ErrAuthentication {
		t.Fatalf("wrong key error = %v, want authentication failure", err)
	}
	oversized, err := Encode(Packet{Type: PacketData, AllocationID: allocationID, SenderID: senderID, RecipientID: recipientID, Payload: bytes.Repeat([]byte{1}, DefaultDatagramLimit)}, key, DefaultDatagramLimit)
	if err != ErrOversized || oversized != nil {
		t.Fatalf("oversized encode = %v, %v", oversized, err)
	}
}

func TestReplayWindowAllowsReorderingButRejectsDuplicates(t *testing.T) {
	var window ReplayWindow
	for _, sequence := range []uint64{10, 8, 9, 12, 11} {
		if !window.Accept(sequence) {
			t.Fatalf("sequence %d was rejected", sequence)
		}
	}
	for _, sequence := range []uint64{10, 11, 12, 8} {
		if window.Accept(sequence) {
			t.Fatalf("duplicate sequence %d was accepted", sequence)
		}
	}
	if !window.Accept(80) {
		t.Fatal("new sequence was rejected")
	}
	if window.Accept(1) {
		t.Fatal("old sequence was accepted")
	}
}
