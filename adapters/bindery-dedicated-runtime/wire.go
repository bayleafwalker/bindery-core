package dedicated

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The DTOs and the client here are the adapter's own. An adapter that imported
// Bindery's Go types would be testing that the types agree with themselves;
// ERH-007 is asking whether the published contract survives a second runtime,
// so this speaks the contract and nothing else.

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

type Options struct {
	Bearer         string
	IdempotencyKey string
	JoinCredential string
}

func (c *Client) Do(method, path string, body any, wantStatus int, out any, options Options) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if options.Bearer != "" {
		request.Header.Set("Authorization", "Bearer "+options.Bearer)
	}
	if options.IdempotencyKey != "" {
		request.Header.Set("Idempotency-Key", options.IdempotencyKey)
	}
	if options.JoinCredential != "" {
		request.Header.Set("X-Session-Join-Credential", options.JoinCredential)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("%s %s: status %d, want %d: %s", method, path, response.StatusCode, wantStatus, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(payload, out)
}

type IdentityResponse struct {
	AccountToken string `json:"account_token"`
}

type SessionResponse struct {
	PublicSession struct {
		SessionID     string `json:"session_id"`
		ExecutionID   string `json:"execution_id"`
		Phase         string `json:"phase"`
		Compatibility struct {
			GameFamily string `json:"game_family"`
			ModID      string `json:"mod_id"`
			MapID      string `json:"map_id"`
			ModHash    string `json:"mod_hash"`
			MapHash    string `json:"map_hash"`
		} `json:"compatibility"`
	} `json:"public_session"`
	SessionJoinCredential string `json:"session_join_credential"`
}

type CaptureOffer struct {
	CaptureID     string `json:"capture_id"`
	ProducerClass string `json:"producer_class"`
	CaptureMethod string `json:"capture_method"`
}

type EnrollmentResponse struct {
	PublicEnrollment struct {
		ClientID       string `json:"client_id"`
		AdapterID      string `json:"adapter_id"`
		AdapterVersion string `json:"adapter_version"`
	} `json:"public_enrollment"`
	ClientLeaseToken    string         `json:"client_lease_token"`
	CaptureStreamOffers []CaptureOffer `json:"capture_stream_offers"`
}

type EventInput struct {
	EventID        string          `json:"event_id"`
	Sequence       uint64          `json:"sequence"`
	GameTick       *uint64         `json:"game_tick,omitempty"`
	EventType      string          `json:"event_type"`
	PayloadVersion string          `json:"payload_version,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

type BatchRequest struct {
	FirstSequence uint64       `json:"first_sequence"`
	LastSequence  uint64       `json:"last_sequence"`
	Events        []EventInput `json:"events"`
}

type Receipt struct {
	AcknowledgedThrough int64 `json:"acknowledged_through"`
}

type Capture struct {
	CaptureID    string `json:"capture_id"`
	Status       string `json:"status"`
	Completeness *struct {
		EventCount    uint64      `json:"event_count"`
		MissingRanges [][2]uint64 `json:"missing_ranges"`
		Closed        bool        `json:"closed"`
	} `json:"completeness"`
}

type GateResult struct {
	GateID             string `json:"gate_id"`
	CaptureID          string `json:"capture_id"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	CalibrationValid   bool   `json:"calibration_valid"`
	ImplementationHash string `json:"implementation_hash"`
}

type EvidenceSet struct {
	EvidenceSetID  string `json:"evidence_set_id"`
	Reconciliation struct {
		Method            string `json:"method"`
		Outcome           string `json:"outcome"`
		ComparedObservers int    `json:"compared_observers"`
	} `json:"reconciliation"`
	Observations []struct {
		StreamID    string `json:"stream_id"`
		ObserverID  string `json:"observer_id"`
		EventCount  uint64 `json:"event_count"`
		OrderedHash string `json:"ordered_hash"`
		Source      string `json:"source"`
	} `json:"observations"`
	GateResults []GateResult `json:"gate_results"`
}
