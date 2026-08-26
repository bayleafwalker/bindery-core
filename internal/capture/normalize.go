package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Normalization is additive and versioned. A raw observation is never edited,
// never reinterpreted in place, and never replaced by its derivation; running
// a newer normalizer produces a new derived dataset alongside the old one.
//
// This is what lets contradictory accounts coexist. Two producers that
// disagree yield two raw streams and two derivations, all four attributable.
// Nothing here picks a winner, because nothing here is in a position to.

// The generic public event types normalizers emit.
const (
	EventMatchLifecycle  = "game.match.lifecycle"
	EventResourceChanged = "game.player.resource-changed"
	EventEntityBuilt     = "game.entity.constructed"
	EventEntityDestroyed = "game.entity.destroyed"
	EventActionObserved  = "game.player.action-observed"
	EventCaptureGap      = "game.capture.gap"
)

var ErrNormalizerUnknown = errors.New("normalizer is not registered")

// Derivation links a derived fact back to the observations it came from. A
// derived event without one would be indistinguishable from an observation,
// which is precisely the confusion the evidence model exists to prevent.
type Derivation struct {
	NormalizerID      string   `json:"normalizer_id"`
	NormalizerVersion string   `json:"normalizer_version"`
	SourceEventIDs    []string `json:"source_event_ids"`
}

// DerivedEvent is a normalized fact plus its provenance. It is encoded
// separately from RawEvent rather than by extending it: the raw canonical
// encoding is frozen and content-addressed, and derived data is a different
// kind of artifact that should not be able to masquerade as an observation.
type DerivedEvent struct {
	Event      RawEvent   `json:"event"`
	Derivation Derivation `json:"derivation"`
}

// Input is everything a normalizer is allowed to see about a closed stream.
type Input struct {
	Events        []RawEvent
	MissingRanges [][2]uint64
	Closed        bool
	EndReason     string
}

// Normalizer turns raw observations into the generic public schema. It is
// given the whole closed stream rather than one event at a time, because some
// derived facts -- a gap, for one -- are properties of the stream and not of
// any event in it.
type Normalizer interface {
	ID() string
	Version() string
	Normalize(Input) ([]DerivedEvent, error)
}

var normalizers = map[string]Normalizer{}

// Register makes a normalizer available by id. Re-registering the same id
// panics rather than shadowing: two normalizers answering to one name would
// make a recorded normalizer_id ambiguous, and the whole point of recording it
// is that it is not.
func Register(normalizer Normalizer) {
	key := normalizer.ID() + "@" + normalizer.Version()
	if _, exists := normalizers[key]; exists {
		panic("duplicate normalizer registration: " + key)
	}
	normalizers[key] = normalizer
}

// Lookup resolves an exact id and version. There is no "latest": a derivation
// that cannot name the version that produced it is not reproducible.
func Lookup(id, version string) (Normalizer, error) {
	normalizer, ok := normalizers[id+"@"+version]
	if !ok {
		return nil, fmt.Errorf("%w: %s@%s", ErrNormalizerUnknown, id, version)
	}
	return normalizer, nil
}

// RegisteredNormalizers lists what is available, in a stable order.
func RegisteredNormalizers() []Normalizer {
	keys := make([]string, 0, len(normalizers))
	for key := range normalizers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	listed := make([]Normalizer, 0, len(keys))
	for _, key := range keys {
		listed = append(listed, normalizers[key])
	}
	return listed
}

// CanonicalDerivedBatchBytes encodes derived events for the object store,
// reusing the raw canonical form for the event itself so that a derived event
// and the observation it mirrors hash their shared fields identically.
func CanonicalDerivedBatchBytes(events []DerivedEvent) ([]byte, error) {
	type canonicalDerived struct {
		Event      canonicalEvent `json:"event"`
		Derivation Derivation     `json:"derivation"`
	}
	encoded := make([]canonicalDerived, 0, len(events))
	for _, derived := range events {
		canonical, err := canonicalize(derived.Event)
		if err != nil {
			return nil, err
		}
		sources := append([]string(nil), derived.Derivation.SourceEventIDs...)
		sort.Strings(sources)
		encoded = append(encoded, canonicalDerived{Event: canonical, Derivation: Derivation{
			NormalizerID:      derived.Derivation.NormalizerID,
			NormalizerVersion: derived.Derivation.NormalizerVersion,
			SourceEventIDs:    sources,
		}})
	}
	body, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: derived batch is not encodable", ErrBatchInvalid)
	}
	return body, nil
}

// DecodeCanonicalDerivedBatch reverses CanonicalDerivedBatchBytes.
func DecodeCanonicalDerivedBatch(encoded []byte) ([]DerivedEvent, error) {
	type canonicalDerived struct {
		Event      json.RawMessage `json:"event"`
		Derivation Derivation      `json:"derivation"`
	}
	var wire []canonicalDerived
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("%w: persisted derivation is not canonical", ErrBatchInvalid)
	}
	events := make([]DerivedEvent, 0, len(wire))
	for _, entry := range wire {
		single, err := DecodeCanonicalBatch(append(append([]byte("["), entry.Event...), ']'))
		if err != nil {
			return nil, err
		}
		if len(single) != 1 {
			return nil, fmt.Errorf("%w: derived entry does not hold exactly one event", ErrBatchInvalid)
		}
		events = append(events, DerivedEvent{Event: single[0], Derivation: entry.Derivation})
	}
	return events, nil
}
