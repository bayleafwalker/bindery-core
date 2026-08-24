package harness

import (
	"bytes"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/relay"
	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

type twoClientHarnessFixture struct {
	config                  relay.Config
	now                     time.Time
	lease                   time.Duration
	allocationID            string
	replacementAllocationID string
	senderID                string
	recipientID             string
	outsiderID              string
	senderKey               []byte
	recipientKey            []byte
}

func fixtureTwoClientHarness() twoClientHarnessFixture {
	return twoClientHarnessFixture{
		config:                  relay.Config{PacketsPerSecond: 100, BytesPerSecond: 100_000},
		now:                     time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		lease:                   time.Minute,
		allocationID:            "0198c2c3-4d5e-7f60-8123-456789abcdef",
		replacementAllocationID: "0198c2c3-4d5e-7f63-8123-456789abcdef",
		senderID:                "0198c2c3-4d5e-7f61-8123-456789abcdef",
		recipientID:             "0198c2c3-4d5e-7f62-8123-456789abcdef",
		outsiderID:              "0198c2c3-4d5e-7f64-8123-456789abcdef",
		senderKey:               bytes.Repeat([]byte{0x11}, relayv1.TransportKeyBytes),
		recipientKey:            bytes.Repeat([]byte{0x22}, relayv1.TransportKeyBytes),
	}
}
