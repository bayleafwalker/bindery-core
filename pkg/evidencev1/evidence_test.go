package evidencev1

import (
	"errors"
	"testing"
	"time"
)

func TestExactCountReconcilesIndependentRA2Streams(t *testing.T) {
	set, err := Reconcile(ReconcileRequest{
		ExecutionID: "execution-ra2-vertical-slice",
		Method:      MethodExactCount,
		CreatedAt:   time.Date(2026, 8, 25, 19, 0, 0, 0, time.UTC),
		Observations: []ObservationSummary{
			{ObserverID: "client-b", ExecutionID: "execution-ra2-vertical-slice", StreamID: "telemetry-b", EventCount: 6651, Source: SourceClientReported},
			{ObserverID: "client-a", ExecutionID: "execution-ra2-vertical-slice", StreamID: "telemetry-a", EventCount: 6651, Source: SourceClientReported},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if set.Reconciliation.Outcome != OutcomeConsistent {
		t.Fatalf("outcome = %s, want consistent", set.Reconciliation.Outcome)
	}
	if len(set.Reconciliation.DistinctCounts) != 1 || set.Reconciliation.DistinctCounts[0] != 6651 {
		t.Fatalf("distinct counts = %v", set.Reconciliation.DistinctCounts)
	}
	if set.Observations[0].ObserverID != "client-a" || set.EvidenceSetID == "" {
		t.Fatalf("evidence set is not canonical: %+v", set)
	}
}

func TestExactCountRetainsDisagreement(t *testing.T) {
	set, err := Reconcile(ReconcileRequest{
		ExecutionID: "execution-1",
		Method:      MethodExactCount,
		CreatedAt:   time.Now(),
		Observations: []ObservationSummary{
			{ObserverID: "a", ExecutionID: "execution-1", StreamID: "a-stream", EventCount: 6651, Source: SourceClientReported},
			{ObserverID: "b", ExecutionID: "execution-1", StreamID: "b-stream", EventCount: 6650, Source: SourceClientReported},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if set.Reconciliation.Outcome != OutcomeInconsistent || len(set.Observations) != 2 {
		t.Fatalf("disagreement was not preserved: %+v", set)
	}
}

func TestReconciliationRequiresIndependentObservers(t *testing.T) {
	_, err := Reconcile(ReconcileRequest{
		ExecutionID: "execution-1",
		Method:      MethodExactCount,
		CreatedAt:   time.Now(),
		Observations: []ObservationSummary{
			{ObserverID: "same", ExecutionID: "execution-1", StreamID: "stream-1", EventCount: 10, Source: SourceClientReported},
			{ObserverID: "same", ExecutionID: "execution-1", StreamID: "stream-2", EventCount: 10, Source: SourceClientReported},
		},
	})
	if err == nil {
		t.Fatal("one observer with two streams was treated as independent evidence")
	}
}

func TestUnimplementedPoliciesRemainExplicit(t *testing.T) {
	_, err := Reconcile(ReconcileRequest{
		ExecutionID: "execution-1",
		Method:      MethodSemanticEquivalent,
		CreatedAt:   time.Now(),
		Observations: []ObservationSummary{
			{ObserverID: "a", ExecutionID: "execution-1", StreamID: "stream-a", Source: SourceClientReported},
			{ObserverID: "b", ExecutionID: "execution-1", StreamID: "stream-b", Source: SourceClientReported},
		},
	})
	if !errors.Is(err, ErrUnsupportedMethod) {
		t.Fatalf("error = %v, want unsupported method", err)
	}
}

const (
	hashA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	hashD = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

func orderedHashRequest(observations ...ObservationSummary) ReconcileRequest {
	return ReconcileRequest{ExecutionID: "execution-1", Method: MethodOrderedHash, CreatedAt: time.Now(), Observations: observations}
}

// Two producers' streams always differ in ordered_hash, because it covers who
// produced them. What they witnessed is compared through observed_hash.
func TestOrderedHashComparesWhatWasObservedNotWhoObservedIt(t *testing.T) {
	set, err := Reconcile(orderedHashRequest(
		ObservationSummary{ObserverID: "a", ExecutionID: "execution-1", StreamID: "a-stream", EventCount: 8, OrderedHash: hashA, ObservedHash: hashC, Source: SourceBrokerDerived},
		ObservationSummary{ObserverID: "b", ExecutionID: "execution-1", StreamID: "b-stream", EventCount: 8, OrderedHash: hashB, ObservedHash: hashC, Source: SourceBrokerDerived},
	))
	if err != nil {
		t.Fatal(err)
	}
	if set.Reconciliation.Outcome != OutcomeConsistent {
		t.Fatalf("outcome = %s, want consistent", set.Reconciliation.Outcome)
	}
	if len(set.Reconciliation.DistinctHashes) != 1 || set.Reconciliation.DistinctHashes[0] != hashC {
		t.Fatalf("distinct hashes = %v, want the observed hash", set.Reconciliation.DistinctHashes)
	}
	if set.Observations[0].OrderedHash != hashA || set.Observations[1].OrderedHash != hashB {
		t.Fatalf("stream identities were not retained: %+v", set.Observations)
	}
}

func TestOrderedHashRetainsObservedDisagreement(t *testing.T) {
	set, err := Reconcile(orderedHashRequest(
		ObservationSummary{ObserverID: "a", ExecutionID: "execution-1", StreamID: "a-stream", EventCount: 8, OrderedHash: hashA, ObservedHash: hashC, Source: SourceBrokerDerived},
		ObservationSummary{ObserverID: "b", ExecutionID: "execution-1", StreamID: "b-stream", EventCount: 8, OrderedHash: hashB, ObservedHash: hashD, Source: SourceBrokerDerived},
	))
	if err != nil {
		t.Fatal(err)
	}
	if set.Reconciliation.Outcome != OutcomeInconsistent || len(set.Reconciliation.DistinctHashes) != 2 {
		t.Fatalf("disagreement was not preserved: %+v", set.Reconciliation)
	}
}

// A client-reported summary may carry only ordered_hash, whose meaning the
// client chose. Those are still compared as given.
func TestOrderedHashWithoutObservedHashesComparesStreamHashes(t *testing.T) {
	set, err := Reconcile(orderedHashRequest(
		ObservationSummary{ObserverID: "a", ExecutionID: "execution-1", StreamID: "a-stream", OrderedHash: hashA, Source: SourceClientReported},
		ObservationSummary{ObserverID: "b", ExecutionID: "execution-1", StreamID: "b-stream", OrderedHash: hashA, Source: SourceClientReported},
	))
	if err != nil {
		t.Fatal(err)
	}
	if set.Reconciliation.Outcome != OutcomeConsistent || set.Reconciliation.DistinctHashes[0] != hashA {
		t.Fatalf("reconciliation = %+v", set.Reconciliation)
	}
}

// A producer-independent digest and a stream identity are different things;
// comparing one with the other would report disagreement that is not there.
func TestOrderedHashRefusesToMixObservedAndStreamHashes(t *testing.T) {
	_, err := Reconcile(orderedHashRequest(
		ObservationSummary{ObserverID: "a", ExecutionID: "execution-1", StreamID: "a-stream", OrderedHash: hashA, ObservedHash: hashC, Source: SourceBrokerDerived},
		ObservationSummary{ObserverID: "b", ExecutionID: "execution-1", StreamID: "b-stream", OrderedHash: hashB, Source: SourceClientReported},
	))
	if !errors.Is(err, ErrMixedHashKinds) {
		t.Fatalf("error = %v, want mixed hash kinds", err)
	}
}

func TestOrderedHashRejectsMalformedObservedHash(t *testing.T) {
	_, err := Reconcile(orderedHashRequest(
		ObservationSummary{ObserverID: "a", ExecutionID: "execution-1", StreamID: "a-stream", OrderedHash: hashA, ObservedHash: "sha256:nope", Source: SourceBrokerDerived},
		ObservationSummary{ObserverID: "b", ExecutionID: "execution-1", StreamID: "b-stream", OrderedHash: hashB, ObservedHash: "sha256:nope", Source: SourceBrokerDerived},
	))
	if err == nil {
		t.Fatal("a malformed observed hash was accepted")
	}
}
