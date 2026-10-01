// Package evidencev1 defines provenance-preserving observations and the
// smallest generic reconciliation policies used by external runtimes.
package evidencev1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Method string

const (
	MethodExactCount         Method = "exact-count"
	MethodOrderedHash        Method = "ordered-hash"
	MethodSemanticEquivalent Method = "semantic-equivalence"
	MethodQuorum             Method = "quorum"
	MethodDomainSpecific     Method = "domain-specific"
)

type Outcome string

const (
	OutcomeConsistent   Outcome = "consistent"
	OutcomeInconsistent Outcome = "inconsistent"
)

// Observation is one attributable event. Capture stores may retain these in
// full; reconciliation normally consumes bounded stream summaries instead.
type Observation struct {
	ObservationID string          `json:"observation_id"`
	ObserverID    string          `json:"observer_id"`
	ExecutionID   string          `json:"execution_id"`
	StreamID      string          `json:"stream_id"`
	Sequence      uint64          `json:"sequence"`
	ObservedAt    time.Time       `json:"observed_at"`
	Event         json.RawMessage `json:"event"`
}

// Source records who counted.
//
// A summary the producer supplied is a claim about itself; one the broker
// computed from persisted observations is not. An evidence set that cannot
// say which it holds cannot support any statement about independence, so the
// field is required rather than optional -- including in the evidence set's
// content-derived identity.
type Source string

const (
	SourceClientReported Source = "client-reported"
	SourceBrokerDerived  Source = "broker-derived"
)

// ObservationSummary is a compact claim about one independently produced
// stream. It never becomes truth merely because reconciliation accepts it.
type ObservationSummary struct {
	ObserverID  string `json:"observer_id"`
	ExecutionID string `json:"execution_id"`
	StreamID    string `json:"stream_id"`
	EventCount  uint64 `json:"event_count"`
	// OrderedHash identifies one producer's stream. It covers who produced
	// the stream, so two producers never share one.
	OrderedHash string `json:"ordered_hash,omitempty"`
	// ObservedHash covers only what the stream witnessed, in order, and is
	// what ordered-hash reconciliation compares between producers.
	ObservedHash string `json:"observed_hash,omitempty"`
	Source       Source `json:"source"`
}

type Reconciliation struct {
	Method            Method   `json:"method"`
	Outcome           Outcome  `json:"outcome"`
	ComparedObservers int      `json:"compared_observers"`
	DistinctCounts    []uint64 `json:"distinct_counts,omitempty"`
	DistinctHashes    []string `json:"distinct_hashes,omitempty"`
}

type EvidenceSet struct {
	SchemaVersion  string               `json:"schema_version"`
	EvidenceSetID  string               `json:"evidence_set_id"`
	ExecutionID    string               `json:"execution_id"`
	Observations   []ObservationSummary `json:"observations"`
	Reconciliation Reconciliation       `json:"reconciliation"`
	CreatedAt      time.Time            `json:"created_at"`
}

type ReconcileRequest struct {
	ExecutionID  string
	Method       Method
	Observations []ObservationSummary
	CreatedAt    time.Time
}

// ErrSourceUnknown rejects a summary that does not say how it was produced.
var ErrSourceUnknown = errors.New("observation summary does not record how it was produced")

var ErrUnsupportedMethod = errors.New("reconciliation method is not implemented")

// ErrMixedHashKinds rejects an ordered-hash reconciliation in which some
// summaries carry an observed hash and others only a stream hash. The two are
// different digests, and comparing them would report disagreement that is not
// there.
var ErrMixedHashKinds = errors.New("ordered-hash reconciliation cannot compare observed hashes with stream hashes")

// Reconcile compares independent observation streams without changing or
// discarding any of the claims. exact-count is intentionally policy #1: it
// captures the RA2 vertical slice's two matching 6,651-event accounts without
// pretending that equal counts prove semantic identity.
func Reconcile(request ReconcileRequest) (EvidenceSet, error) {
	if request.ExecutionID == "" || request.CreatedAt.IsZero() {
		return EvidenceSet{}, errors.New("execution id and reconciliation time are required")
	}
	if len(request.Observations) < 2 {
		return EvidenceSet{}, errors.New("at least two independent observations are required")
	}

	observations := append([]ObservationSummary(nil), request.Observations...)
	observers := make(map[string]struct{}, len(observations))
	streams := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		if observation.ObserverID == "" || observation.StreamID == "" || observation.ExecutionID != request.ExecutionID {
			return EvidenceSet{}, errors.New("every observation must name its observer, stream, and requested execution")
		}
		if observation.Source != SourceClientReported && observation.Source != SourceBrokerDerived {
			return EvidenceSet{}, fmt.Errorf("%w: stream %q", ErrSourceUnknown, observation.StreamID)
		}
		if _, duplicate := streams[observation.StreamID]; duplicate {
			return EvidenceSet{}, fmt.Errorf("duplicate observation stream %q", observation.StreamID)
		}
		observers[observation.ObserverID] = struct{}{}
		streams[observation.StreamID] = struct{}{}
	}
	if len(observers) < 2 {
		return EvidenceSet{}, errors.New("reconciliation requires at least two distinct observers")
	}
	sort.Slice(observations, func(i, j int) bool {
		if observations[i].ObserverID == observations[j].ObserverID {
			return observations[i].StreamID < observations[j].StreamID
		}
		return observations[i].ObserverID < observations[j].ObserverID
	})

	reconciliation := Reconciliation{Method: request.Method, ComparedObservers: len(observers)}
	switch request.Method {
	case MethodExactCount:
		reconciliation.DistinctCounts = distinctCounts(observations)
		reconciliation.Outcome = outcome(len(reconciliation.DistinctCounts) == 1)
	case MethodOrderedHash:
		hashes, err := comparedHashes(observations)
		if err != nil {
			return EvidenceSet{}, err
		}
		reconciliation.DistinctHashes = distinctStrings(hashes)
		reconciliation.Outcome = outcome(len(reconciliation.DistinctHashes) == 1)
	case MethodSemanticEquivalent, MethodQuorum, MethodDomainSpecific:
		return EvidenceSet{}, fmt.Errorf("%w: %s", ErrUnsupportedMethod, request.Method)
	default:
		return EvidenceSet{}, fmt.Errorf("unknown reconciliation method %q", request.Method)
	}

	id, err := evidenceSetID(request.ExecutionID, request.Method, observations)
	if err != nil {
		return EvidenceSet{}, err
	}
	return EvidenceSet{
		SchemaVersion:  "1.0.0",
		EvidenceSetID:  id,
		ExecutionID:    request.ExecutionID,
		Observations:   observations,
		Reconciliation: reconciliation,
		CreatedAt:      request.CreatedAt.UTC(),
	}, nil
}

func outcome(consistent bool) Outcome {
	if consistent {
		return OutcomeConsistent
	}
	return OutcomeInconsistent
}

func distinctCounts(observations []ObservationSummary) []uint64 {
	seen := make(map[uint64]struct{}, len(observations))
	for _, observation := range observations {
		seen[observation.EventCount] = struct{}{}
	}
	result := make([]uint64, 0, len(seen))
	for count := range seen {
		result = append(result, count)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// comparedHashes picks the digest ordered-hash reconciliation compares: the
// observed hash when every summary carries one, the stream hash when none
// does. A client-reported stream hash means whatever the client made it mean,
// so it is still compared as given; a mix of the two is refused.
func comparedHashes(observations []ObservationSummary) ([]string, error) {
	observed := 0
	for _, observation := range observations {
		if observation.ObservedHash != "" {
			observed++
		}
	}
	if observed != 0 && observed != len(observations) {
		return nil, ErrMixedHashKinds
	}
	hashes := make([]string, 0, len(observations))
	for _, observation := range observations {
		hash := observation.OrderedHash
		if observed != 0 {
			hash = observation.ObservedHash
		}
		if !digestPattern.MatchString(hash) {
			return nil, errors.New("ordered-hash reconciliation requires a sha256 digest for every stream")
		}
		hashes = append(hashes, hash)
	}
	return hashes, nil
}

func distinctStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func evidenceSetID(executionID string, method Method, observations []ObservationSummary) (string, error) {
	canonical, err := json.Marshal(struct {
		ExecutionID  string               `json:"execution_id"`
		Method       Method               `json:"method"`
		Observations []ObservationSummary `json:"observations"`
	}{executionID, method, observations})
	if err != nil {
		return "", fmt.Errorf("encode evidence identity: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
