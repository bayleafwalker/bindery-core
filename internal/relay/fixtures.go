package relay

import (
	"bytes"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

type boundedRelayFixture struct {
	config       Config
	now          time.Time
	allocationID string
	senderID     string
	recipientID  string
	senderKey    []byte
	recipientKey []byte
}

func fixtureBoundedRelay() boundedRelayFixture {
	return boundedRelayFixture{
		config:       Config{DatagramLimit: relayv1.HeaderBytes + 2, PacketsPerSecond: 1, BytesPerSecond: 1 << 20},
		now:          time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		allocationID: "0198c2c3-4d5e-7f60-8123-456789abcdef",
		senderID:     "0198c2c3-4d5e-7f61-8123-456789abcdef",
		recipientID:  "0198c2c3-4d5e-7f62-8123-456789abcdef",
		senderKey:    bytes.Repeat([]byte{0x11}, relayv1.TransportKeyBytes),
		recipientKey: bytes.Repeat([]byte{0x22}, relayv1.TransportKeyBytes),
	}
}
