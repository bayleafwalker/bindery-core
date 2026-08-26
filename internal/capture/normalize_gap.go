package capture

import (
	"encoding/json"
	"fmt"
)

// captureGapNormalizer publishes what the broker independently knows about a
// stream's completeness as first-class events on the public schema.
//
// It is deliberately the only normalizer core ships. Everything else a real
// game's telemetry means -- what a unit is, what a resource is, what winning
// is -- belongs to an adapter, and putting it here would make the platform's
// abstraction claim untestable by making core know about one game. Gaps are
// different: they are a property of the transport and the capture, which is
// exactly what this repository owns.
type captureGapNormalizer struct{}

func init() { Register(captureGapNormalizer{}) }

func (captureGapNormalizer) ID() string      { return "bindery.capture-gap" }
func (captureGapNormalizer) Version() string { return "1" }

func (n captureGapNormalizer) Normalize(input Input) ([]DerivedEvent, error) {
	if !input.Closed {
		return nil, fmt.Errorf("%w: capture-gap normalization needs a closed stream", ErrBatchInvalid)
	}
	bySequence := make(map[uint64]RawEvent, len(input.Events))
	for _, event := range input.Events {
		bySequence[event.Sequence] = event
	}
	derived := make([]DerivedEvent, 0, len(input.MissingRanges))
	for index, gap := range input.MissingRanges {
		payload, err := json.Marshal(struct {
			FirstMissing uint64 `json:"first_missing_sequence"`
			LastMissing  uint64 `json:"last_missing_sequence"`
			EventCount   uint64 `json:"missing_event_count"`
			EndReason    string `json:"end_reason,omitempty"`
		}{gap[0], gap[1], gap[1] - gap[0] + 1, input.EndReason})
		if err != nil {
			return nil, err
		}
		// The gap is bracketed by whatever did arrive on either side of it.
		// Those are the observations the claim actually rests on.
		sources := make([]string, 0, 2)
		if gap[0] > 0 {
			if before, ok := bySequence[gap[0]-1]; ok {
				sources = append(sources, before.EventID)
			}
		}
		if after, ok := bySequence[gap[1]+1]; ok {
			sources = append(sources, after.EventID)
		}
		if len(sources) == 0 {
			// A gap with nothing on either side is still a fact, but it is a
			// fact about the stream rather than about any observation, so the
			// derivation names the stream's first surviving event if there is
			// one at all.
			for _, event := range input.Events {
				sources = append(sources, event.EventID)
				break
			}
		}
		if len(sources) == 0 {
			return nil, fmt.Errorf("%w: a derivation must reference at least one source observation", ErrBatchInvalid)
		}
		template := input.Events[0]
		derived = append(derived, DerivedEvent{
			Event: RawEvent{
				EventID:          fmt.Sprintf("%s-gap-%016d", template.CaptureID, index),
				SessionID:        template.SessionID,
				ExecutionID:      template.ExecutionID,
				CaptureID:        template.CaptureID,
				ProducerClientID: template.ProducerClientID,
				ProducerClass:    "normalizer",
				CaptureMethod:    "normalizer/" + n.ID() + "@" + n.Version(),
				AdapterID:        template.AdapterID,
				AdapterVersion:   template.AdapterVersion,
				Sequence:         uint64(index),
				ReceivedAt:       template.ReceivedAt,
				EventType:        EventCaptureGap,
				PayloadVersion:   "1.0.0",
				Payload:          payload,
			},
			Derivation: Derivation{NormalizerID: n.ID(), NormalizerVersion: n.Version(), SourceEventIDs: sources},
		})
	}
	return derived, nil
}

// NormalizerV1 exposes the shipped gap normalizer for callers that want to
// compose with it rather than re-derive its behaviour.
func NormalizerV1() Normalizer { return captureGapNormalizer{} }
