package capture

import (
	"testing"
	"time"
)

func TestCoordinatorFrozenCaptureFixtureCorpus(t *testing.T) {
	batch := fixtureCaptureBatch("capture-fixture", "player-fixture", 2, 3)
	if batch.CaptureID != "capture-fixture" || batch.ProducerClientID != "player-fixture" {
		t.Fatalf("batch identity = %+v", batch)
	}
	if batch.FirstSequence != 2 || batch.LastSequence != 3 || len(batch.Events) != 2 {
		t.Fatalf("batch range = %+v", batch)
	}
	if batch.Events[0].EventID != "capture-fixture-player-fixture-event-2" || batch.Events[1].EventID != "capture-fixture-player-fixture-event-3" {
		t.Fatalf("event ids = %q, %q", batch.Events[0].EventID, batch.Events[1].EventID)
	}
	wantReceived := time.Date(2026, 8, 24, 12, 0, 2, 0, time.UTC)
	if !batch.Events[0].ReceivedAt.Equal(wantReceived) {
		t.Fatalf("received at = %s, want %s", batch.Events[0].ReceivedAt, wantReceived)
	}
	close := fixtureCaptureClose(3)
	if close.FinalSequence != 3 || close.LocalDrops != 1 || close.EndReason != "client-exit" {
		t.Fatalf("close fixture = %+v", close)
	}
}
