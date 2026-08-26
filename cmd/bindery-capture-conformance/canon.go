package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// This file is a deliberate second implementation of the frozen canonical
// encoding described in internal/capture/canon.go. It does not import that
// package, and it must not: the point of this producer is to show that the
// encoding is reproducible by a program that has only the specification, not
// Bindery's Go types. If the two implementations disagree, the broker rejects
// this producer's digest and the conformance run fails, which is the signal
// worth having.
//
// Field order is the specification. encoding/json emits struct fields in
// declaration order, so the order below is load-bearing and must match the
// broker's canonicalEvent exactly.
type canonicalEvent struct {
	EventID          string          `json:"event_id"`
	SessionID        string          `json:"session_id"`
	ExecutionID      string          `json:"execution_id"`
	CaptureID        string          `json:"capture_id"`
	ProducerClientID string          `json:"producer_client_id"`
	ProducerClass    string          `json:"producer_class"`
	CaptureMethod    string          `json:"capture_method"`
	AdapterID        string          `json:"adapter_id"`
	AdapterVersion   string          `json:"adapter_version"`
	Sequence         uint64          `json:"sequence"`
	GameTick         *uint64         `json:"game_tick"`
	ProducerTime     *string         `json:"producer_time"`
	ReceivedAt       string          `json:"received_at"`
	EventType        string          `json:"event_type"`
	PayloadVersion   string          `json:"payload_version"`
	Payload          json.RawMessage `json:"payload"`
}

// attribution is the part of a canonical event the producer does not choose
// per event: it comes from enrollment and from the capture stream offer.
type attribution struct {
	SessionID        string
	ExecutionID      string
	CaptureID        string
	ProducerClientID string
	ProducerClass    string
	CaptureMethod    string
	AdapterID        string
	AdapterVersion   string
}

func canonicalTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

// producerDigest hashes the events as the producer sent them. ReceivedAt is
// zero here on purpose: the broker stamps its own receive time, so a digest the
// producer can compute must exclude it. The zero time still encodes, as
// "0001-01-01T00:00:00Z".
func producerDigest(events []event, at attribution) (string, error) {
	canonical := make([]canonicalEvent, 0, len(events))
	for _, source := range events {
		encoded, err := canonicalize(source, at)
		if err != nil {
			return "", err
		}
		canonical = append(canonical, encoded)
	}
	body, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode canonical batch: %w", err)
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func canonicalize(source event, at attribution) (canonicalEvent, error) {
	// Compacted rather than passed through, so that two producers agreeing on
	// the facts cannot disagree on the hash because one of them indented.
	var payload bytes.Buffer
	if err := json.Compact(&payload, source.Payload); err != nil {
		return canonicalEvent{}, fmt.Errorf("event %s: payload is not valid JSON: %w", source.EventID, err)
	}
	canonical := canonicalEvent{
		EventID:          source.EventID,
		SessionID:        at.SessionID,
		ExecutionID:      at.ExecutionID,
		CaptureID:        at.CaptureID,
		ProducerClientID: at.ProducerClientID,
		ProducerClass:    at.ProducerClass,
		CaptureMethod:    at.CaptureMethod,
		AdapterID:        at.AdapterID,
		AdapterVersion:   at.AdapterVersion,
		Sequence:         source.Sequence,
		GameTick:         source.GameTick,
		ReceivedAt:       canonicalTime(time.Time{}),
		EventType:        source.EventType,
		PayloadVersion:   source.PayloadVersion,
		Payload:          json.RawMessage(payload.Bytes()),
	}
	if source.ProducerTime != nil {
		producerTime := canonicalTime(*source.ProducerTime)
		canonical.ProducerTime = &producerTime
	}
	return canonical, nil
}
