package capture

import (
	"fmt"
	"time"
)

func fixtureCaptureBatch(captureID, producerID string, first, last uint64) Batch {
	events := make([]RawEvent, 0, last-first+1)
	for sequence := first; sequence <= last; sequence++ {
			events = append(events, RawEvent{
			EventID:          fmt.Sprintf("%s-%s-event-%d", captureID, producerID, sequence),
			SessionID:        "session-fixture",
			ExecutionID:      "execution-fixture",
			CaptureID:        captureID,
			ProducerClientID: producerID,
			ProducerClass:    "player",
			CaptureMethod:    "fixture",
			AdapterID:        "bindery.ra2-adapter",
			AdapterVersion:   "0.1.0",
			Sequence:         sequence,
			ReceivedAt:       time.Date(2026, 8, 24, 12, 0, int(sequence), 0, time.UTC),
			EventType:        "game.event",
			PayloadVersion:   "1.0",
			Payload:          []byte(fmt.Sprintf(`{"sequence":%d}`, sequence)),
		})
	}
	return Batch{CaptureID: captureID, ExecutionID: "execution-fixture", ProducerClientID: producerID, FirstSequence: first, LastSequence: last, Events: events}
}

func fixtureCaptureClose(finalSequence uint64) StreamClose {
	return StreamClose{ExecutionID: "execution-fixture", FinalSequence: finalSequence, ObservedGaps: [][2]uint64{{2, 2}}, LocalDrops: 1, EndReason: "client-exit", ClosedAt: time.Date(2026, 8, 24, 12, 1, 0, 0, time.UTC)}
}
