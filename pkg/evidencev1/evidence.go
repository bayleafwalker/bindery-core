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

// ObservationSummary is a compact claim about one independently produced
// stream. It never becomes truth merely because reconciliation accepts it.
type ObservationSummary struct {
	ObserverID  string `json:"observer_id"`
	ExecutionID string `json:"execution_id"`
	StreamID    string `json:"stream_id"`
	EventCount  uint64 `json:"event_count"`
	OrderedHash string `json:"ordered_hash,omitempty"`
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

var ErrUnsupportedMethod = errors.New("reconciliation method is not implemented")

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
		for _, observation := range observations {
			if !digestPattern.MatchString(observation.OrderedHash) {
				return EvidenceSet{}, errors.New("ordered-hash reconciliation requires a sha256 digest for every stream")
			}
		}
		reconciliation.DistinctHashes = distinctHashes(observations)
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

func distinctHashes(observations []ObservationSummary) []string {
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		seen[observation.OrderedHash] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for hash := range seen {
		result = append(result, hash)
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
