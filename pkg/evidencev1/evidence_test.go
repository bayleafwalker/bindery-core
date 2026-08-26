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
			{ObserverID: "client-b", ExecutionID: "execution-ra2-vertical-slice", StreamID: "telemetry-b", EventCount: 6651},
			{ObserverID: "client-a", ExecutionID: "execution-ra2-vertical-slice", StreamID: "telemetry-a", EventCount: 6651},
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
			{ObserverID: "a", ExecutionID: "execution-1", StreamID: "a-stream", EventCount: 6651},
			{ObserverID: "b", ExecutionID: "execution-1", StreamID: "b-stream", EventCount: 6650},
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
			{ObserverID: "same", ExecutionID: "execution-1", StreamID: "stream-1", EventCount: 10},
			{ObserverID: "same", ExecutionID: "execution-1", StreamID: "stream-2", EventCount: 10},
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
			{ObserverID: "a", ExecutionID: "execution-1", StreamID: "stream-a"},
			{ObserverID: "b", ExecutionID: "execution-1", StreamID: "stream-b"},
		},
	})
	if !errors.Is(err, ErrUnsupportedMethod) {
		t.Fatalf("error = %v, want unsupported method", err)
	}
}
