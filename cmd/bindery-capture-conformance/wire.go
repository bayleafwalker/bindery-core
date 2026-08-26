package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The DTOs below are declared here rather than imported from
// internal/externalruntime for the same reason canon.go re-implements the
// canonical encoding: a producer that shares the broker's Go types proves that
// the types are self-consistent, not that the published contract is
// implementable. Only the fields this producer actually uses are declared.

type event struct {
	EventID        string          `json:"event_id"`
	Sequence       uint64          `json:"sequence"`
	GameTick       *uint64         `json:"game_tick,omitempty"`
	ProducerTime   *time.Time      `json:"producer_time,omitempty"`
	EventType      string          `json:"event_type"`
	PayloadVersion string          `json:"payload_version,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

type ingestBatch struct {
	FirstSequence  uint64  `json:"first_sequence"`
	LastSequence   uint64  `json:"last_sequence"`
	ProducerDigest string  `json:"producer_digest,omitempty"`
	Events         []event `json:"events"`
}

type captureReceipt struct {
	CaptureID   string `json:"capture_id"`
	ExecutionID string `json:"execution_id"`
	// AcknowledgedThrough is the highest contiguous sequence held from zero,
	// and is -1 when sequence zero is still missing. It is not a count.
	AcknowledgedThrough int64       `json:"acknowledged_through"`
	MissingRanges       [][2]uint64 `json:"missing_ranges"`
	RawObjectHash       string      `json:"raw_object_hash"`
	Duplicate           bool        `json:"duplicate"`
}

type captureOffer struct {
	CaptureID      string `json:"capture_id"`
	ProducerClass  string `json:"producer_class"`
	CaptureMethod  string `json:"capture_method"`
	MaxBatchBytes  int64  `json:"max_batch_bytes"`
	MaxBatchEvents int    `json:"max_batch_events"`
	MaxObjectBytes int64  `json:"max_object_bytes"`
}

type identityResponse struct {
	AccountToken string `json:"account_token"`
}

type sessionResponse struct {
	PublicSession struct {
		SessionID   string `json:"session_id"`
		ExecutionID string `json:"execution_id"`
		Phase       string `json:"phase"`
	} `json:"public_session"`
	SessionJoinCredential string `json:"session_join_credential"`
}

type enrollmentResponse struct {
	PublicEnrollment struct {
		ClientID       string `json:"client_id"`
		ClientClass    string `json:"client_class"`
		AdapterID      string `json:"adapter_id"`
		AdapterVersion string `json:"adapter_version"`
	} `json:"public_enrollment"`
	ClientLeaseToken    string         `json:"client_lease_token"`
	CaptureStreamOffers []captureOffer `json:"capture_stream_offers"`
}

type publicCapture struct {
	CaptureID    string   `json:"capture_id"`
	Status       string   `json:"status"`
	Objects      []string `json:"objects"`
	Completeness *struct {
		ExpectedThrough *uint64     `json:"expected_through"`
		ObservedRanges  [][2]uint64 `json:"observed_ranges"`
		MissingRanges   [][2]uint64 `json:"missing_ranges"`
		EventCount      uint64      `json:"event_count"`
		LocalDrops      uint64      `json:"local_drops"`
		RawObjectHashes []string    `json:"raw_object_hashes"`
		Closed          bool        `json:"closed"`
		EndReason       string      `json:"end_reason"`
		DerivationIDs   []string    `json:"derivation_ids"`
	} `json:"completeness"`
	DerivedFromCaptureID string `json:"derived_from_capture_id"`
	Normalizer           *struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	} `json:"normalizer"`
}

type objectManifest struct {
	ContentHash string `json:"content_hash"`
	Bytes       int64  `json:"bytes"`
	MediaType   string `json:"media_type"`
}

type eventPage struct {
	Events []struct {
		EventID  string `json:"event_id"`
		Sequence uint64 `json:"sequence"`
	} `json:"events"`
	NextCursor string `json:"next_cursor"`
}

// client is a plain HTTP caller. Everything it does crosses a socket; that is
// the property this whole program exists to establish.
type client struct {
	baseURL string
	http    *http.Client
}

func newClient(baseURL string) *client {
	return &client{baseURL: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

type callOptions struct {
	bearer         string
	idempotencyKey string
	joinCredential string
	contentType    string
	rawBody        []byte
}

// do performs one request and returns the raw status and body.
func (c *client) do(method, path string, body any, options callOptions) (int, []byte, error) {
	var reader io.Reader
	contentType := options.contentType
	switch {
	case options.rawBody != nil:
		reader = bytes.NewReader(options.rawBody)
	case body != nil:
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
		if contentType == "" {
			contentType = "application/json"
		}
	}
	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if options.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+options.bearer)
	}
	if options.idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", options.idempotencyKey)
	}
	if options.joinCredential != "" {
		request.Header.Set("X-Session-Join-Credential", options.joinCredential)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	return response.StatusCode, payload, nil
}

// call performs one request and decodes into out. wantStatus is checked before
// decoding so a contract violation is reported as itself rather than as a
// confusing decode error.
func (c *client) call(method, path string, body any, wantStatus int, out any, options callOptions) error {
	status, payload, err := c.do(method, path, body, options)
	if err != nil {
		return err
	}
	if status != wantStatus {
		return fmt.Errorf("%s %s: status %d, want %d: %s", method, path, status, wantStatus, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

// callExpectingRejection is for negative controls. It requires a 4xx and
// returns the status and the error code the contract published, so a step can
// assert that the broker refused for the stated reason rather than by accident.
func (c *client) callExpectingRejection(method, path string, body any, options callOptions) (int, string, error) {
	status, payload, err := c.do(method, path, body, options)
	if err != nil {
		return 0, "", err
	}
	if status < 400 || status > 499 {
		return status, "", fmt.Errorf("%s %s: status %d, want a 4xx rejection: %s", method, path, status, strings.TrimSpace(string(payload)))
	}
	var failure struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(payload, &failure); err != nil {
		return status, "", fmt.Errorf("%s %s: decode error body: %w", method, path, err)
	}
	return status, failure.Code, nil
}
