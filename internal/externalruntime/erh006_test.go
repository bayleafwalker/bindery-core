package externalruntime

import (
	"path/filepath"
	"testing"

	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
	"github.com/bayleafwalker/bindery-core/pkg/gatev1"
)

// The tests here cover roadmap item ERH-006's second acceptance criterion:
// "Exact-count reconciliation records the two client streams without
// adapter-owned adjudication." Performing the RA2 run itself needs two real
// game clients and is not something a test can claim to have done; what these
// establish is that the control plane no longer takes the adapters' word for
// the counts.

func TestReconciliationDerivesCountsFromPersistedObservations(t *testing.T) {
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	owner := mustIdentity(t, service, "derive-owner")
	created, err := service.CreateSession(owner.AccountToken, "derive-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "derive-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "derive-b", ClientPlayer)
	closeMatchingStreams(t, service, a, b, 32)

	result, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "derive-evidence", ReconcileEvidenceRequest{
		Method: evidencev1.MethodExactCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reconciliation.Outcome != evidencev1.OutcomeConsistent {
		t.Fatalf("outcome = %s", result.Reconciliation.Outcome)
	}
	if len(result.Observations) != 2 {
		t.Fatalf("observations = %+v", result.Observations)
	}
	streams := map[string]evidencev1.ObservationSummary{}
	for _, observation := range result.Observations {
		if observation.Source != evidencev1.SourceBrokerDerived {
			t.Fatalf("observation source = %q", observation.Source)
		}
		if observation.EventCount != 32 || observation.OrderedHash == "" {
			t.Fatalf("observation = %+v", observation)
		}
		streams[observation.StreamID] = observation
	}
	if _, ok := streams[a.capture]; !ok {
		t.Fatal("player A's capture is not among the compared streams")
	}
	if _, ok := streams[b.capture]; !ok {
		t.Fatal("player B's capture is not among the compared streams")
	}
	// The two producers observed the same execution and their streams hash
	// alike only because the fixture makes them identical; what matters is
	// that the hash came from the stored events, not from the clients.
	if streams[a.capture].ObserverID != a.id || streams[b.capture].ObserverID != b.id {
		t.Fatal("streams are not attributed to their producers")
	}
}

func TestDisagreeingStreamsStayAttributableAndInconsistent(t *testing.T) {
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	owner := mustIdentity(t, service, "disagree-owner")
	created, err := service.CreateSession(owner.AccountToken, "disagree-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "disagree-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "disagree-b", ClientPlayer)
	ingestRange(t, service, a, 0, 9, 10)
	ingestRange(t, service, b, 0, 7, 10)
	if _, err := service.CloseCapture(a.lease, a.capture, CaptureCloseRequest{FinalSequence: 9, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CloseCapture(b.lease, b.capture, CaptureCloseRequest{FinalSequence: 7, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}

	result, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "disagree-evidence", ReconcileEvidenceRequest{
		Method: evidencev1.MethodExactCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reconciliation.Outcome != evidencev1.OutcomeInconsistent {
		t.Fatalf("outcome = %s, want inconsistent", result.Reconciliation.Outcome)
	}
	// Neither account is discarded and neither is promoted.
	if len(result.Observations) != 2 || len(result.Reconciliation.DistinctCounts) != 2 {
		t.Fatalf("disagreement was not retained: %+v", result)
	}
}

func TestAnIncompleteStreamIsExcludedByTheGateAndNamedInTheResult(t *testing.T) {
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	owner := mustIdentity(t, service, "excluded-owner")
	created, err := service.CreateSession(owner.AccountToken, "excluded-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "excluded-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "excluded-b", ClientPlayer)
	ingestRange(t, service, a, 0, 9, 10)
	if _, err := service.CloseCapture(a.lease, a.capture, CaptureCloseRequest{FinalSequence: 9, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}
	// B never closes its stream, so there is no complete second account.
	ingestRange(t, service, b, 0, 9, 10)

	_, err = service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "excluded-evidence", ReconcileEvidenceRequest{
		Method: evidencev1.MethodExactCount,
	})
	if !hasCode(err, "RECONCILIATION_INVALID") {
		t.Fatalf("reconciliation with one admitted stream error = %v", err)
	}

	// Once B closes, both are admitted.
	if _, err := service.CloseCapture(b.lease, b.capture, CaptureCloseRequest{FinalSequence: 9, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "excluded-evidence-2", ReconcileEvidenceRequest{
		Method: evidencev1.MethodExactCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range result.GateResults {
		if gate.Status != string(gatev1.StatusPass) {
			t.Fatalf("gate result = %+v", gate)
		}
	}
}

func TestClientReportedCountsSurviveWhereThereIsNoCapturePlane(t *testing.T) {
	// Executions with no captured streams keep the older path: there is
	// nothing for the broker to derive from, and refusing the request would
	// remove a capability rather than tighten one.
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	owner := mustIdentity(t, service, "legacy-owner")
	request := testSessionRequest()
	request.Capture = CapturePolicy{}
	created, err := service.CreateSession(owner.AccountToken, "legacy-session", request)
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "legacy-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "legacy-b", ClientPlayer)

	result, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "legacy-evidence", ReconcileEvidenceRequest{
		Method: evidencev1.MethodExactCount,
		Observations: []evidencev1.ObservationSummary{
			{ObserverID: a.id, ExecutionID: created.PublicSession.ExecutionID, StreamID: "telemetry-a", EventCount: 6651, Source: evidencev1.SourceClientReported},
			{ObserverID: b.id, ExecutionID: created.PublicSession.ExecutionID, StreamID: "telemetry-b", EventCount: 6651, Source: evidencev1.SourceClientReported},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reconciliation.Outcome != evidencev1.OutcomeConsistent || len(result.GateResults) != 0 {
		t.Fatalf("legacy path result = %+v", result)
	}
	for _, observation := range result.Observations {
		if observation.Source != evidencev1.SourceClientReported {
			t.Fatalf("legacy observation source = %q", observation.Source)
		}
	}
}

func TestDerivedEvidenceAndItsGateResultsSurviveARestartTwice(t *testing.T) {
	// Run the whole drill twice against the same state directory: once to
	// prove it survives a restart, and again to prove the restart itself left
	// nothing that a second one trips over.
	directory := t.TempDir()
	service := openPersistentCaptureService(t, directory)
	owner := mustIdentity(t, service, "drill-owner")
	created, err := service.CreateSession(owner.AccountToken, "drill-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "drill-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "drill-b", ClientPlayer)
	if _, err := service.StoreCaptureObject(a.lease, a.capture, "application/octet-stream", []byte("stats.dmp")); err != nil {
		t.Fatal(err)
	}
	closeMatchingStreams(t, service, a, b, 64)
	original, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "drill-evidence", ReconcileEvidenceRequest{
		Method: evidencev1.MethodExactCount,
	})
	if err != nil {
		t.Fatal(err)
	}

	for pass := 0; pass < 2; pass++ {
		reopened := openPersistentCaptureService(t, directory)
		restored, err := reopened.GetEvidenceSet(original.EvidenceSetID)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if restored.Reconciliation.Outcome != evidencev1.OutcomeConsistent || len(restored.GateResults) != 2 {
			t.Fatalf("pass %d: restored = %+v", pass, restored)
		}
		record, err := reopened.GetCapture(a.capture)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if len(record.Objects) != 1 || record.Completeness.EventCount != 64 {
			t.Fatalf("pass %d: capture = %+v", pass, record)
		}
		if _, err := reopened.ReadCaptureEvents(a.capture, "", "", 1000); err != nil {
			t.Fatalf("pass %d: events unreadable after restart: %v", pass, err)
		}
	}

	if _, err := NewFileStateStore(filepath.Join(directory, "control-state.json")); err != nil {
		t.Fatal(err)
	}
}
