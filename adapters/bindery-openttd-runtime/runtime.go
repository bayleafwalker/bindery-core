package openttd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

// Runtime drives Bindery's published contracts on behalf of an OpenTTD server
// it does not control. Every identifier it supplies is something the game or
// the operating system can produce; nothing is invented to satisfy a validator.
type Runtime struct {
	client  *Client
	account string

	// GameHash is the sha256 of the OpenTTD executable that is actually
	// running. It is the only content identity in this runtime that anyone can
	// check: the map is generated on the server from a seed, so there is no
	// shipped content for participants to agree on.
	GameHash    string
	GameVersion string

	SessionID   string
	ExecutionID string
	JoinToken   string

	Observers []Enrolled
	Players   []Enrolled
}

// Enrolled is one participant the control plane knows about.
type Enrolled struct {
	Name      string
	ClientID  string
	Lease     string
	CaptureID string
}

// NewRuntime hashes the game binary, which is what this runtime can honestly
// claim as its content identity.
func NewRuntime(client *Client, binary string) (*Runtime, error) {
	file, err := os.Open(binary)
	if err != nil {
		return nil, fmt.Errorf("open game binary: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return nil, fmt.Errorf("hash game binary: %w", err)
	}
	return &Runtime{
		client:      client,
		GameHash:    "sha256:" + hex.EncodeToString(digest.Sum(nil)),
		GameVersion: DefaultGameVersion,
	}, nil
}

func (r *Runtime) ClaimIdentity(handle string) error {
	var response IdentityResponse
	if err := r.client.Do("POST", "/v1/identities", map[string]any{"handle": handle}, 201, &response,
		Options{IdempotencyKey: "openttd-identity-" + handle}); err != nil {
		return err
	}
	r.account = response.AccountToken
	return nil
}

// CreateSession declares what the game reported about itself.
//
// There is no mod and no map. OpenTTD generates its world on the server from a
// seed, and the seed identifies a generator input rather than content anyone
// can verify they hold, so naming it as a map hash would be exactly the
// invention the ERH-007 run removed from core. The seed and the map's shape
// travel as ordinary session context instead.
func (r *Runtime) CreateSession(welcome Welcome, players, observers int) error {
	if welcome.Revision != "" {
		r.GameVersion = welcome.Revision
	}
	request := map[string]any{
		"compatibility": map[string]any{
			"game_family": GameFamily, "game_version": r.GameVersion, "game_hash": r.GameHash,
			"adapter_id": AdapterID, "adapter_version": AdapterVersion,
		},
		"participant_policy": map[string]any{
			"required_players": players, "maximum_players": players, "maximum_observers": observers,
		},
		"placement": map[string]any{"allowed_regions": []string{"eu-north"}, "latency_p95_ms": 100},
		"capture":   map[string]any{"semantic_events": true, "post_match_dump": false},
	}
	var response SessionResponse
	if err := r.client.Do("POST", "/v1/sessions", request, 201, &response,
		Options{Bearer: r.account, IdempotencyKey: "openttd-session"}); err != nil {
		return err
	}
	r.SessionID = response.PublicSession.SessionID
	r.ExecutionID = response.PublicSession.ExecutionID
	r.JoinToken = response.SessionJoinCredential
	if response.PublicSession.Compatibility.ModID != "" || response.PublicSession.Compatibility.MapID != "" {
		return fmt.Errorf("the control plane invented mod/map identity this runtime never supplied")
	}
	if response.PublicSession.Compatibility.GameVersion != r.GameVersion {
		return fmt.Errorf("session records game_version %q, the server reported %q",
			response.PublicSession.Compatibility.GameVersion, r.GameVersion)
	}
	return nil
}

// EnrollObserver seats one admin-network application. Observer is the honest
// class: an admin connection watches the game, it does not play it.
func (r *Runtime) EnrollObserver(name string) error {
	response, err := r.enroll(name, "observer", "openttd-admin-protocol")
	if err != nil {
		return err
	}
	if len(response.CaptureStreamOffers) != 1 {
		return fmt.Errorf("observer %s received %d capture offers, want 1", name, len(response.CaptureStreamOffers))
	}
	r.Observers = append(r.Observers, Enrolled{
		Name:      name,
		ClientID:  response.PublicEnrollment.ClientID,
		Lease:     response.ClientLeaseToken,
		CaptureID: response.CaptureStreamOffers[0].CaptureID,
	})
	return nil
}

// EnrollPlayer seats a human's game client. In OpenTTD a client is told what
// happened by the server, so it witnesses nothing independently and produces
// no observations -- and the control plane hands it a capture stream anyway.
func (r *Runtime) EnrollPlayer(name string) error {
	response, err := r.enroll(name, "player", "")
	if err != nil {
		return err
	}
	seat := Enrolled{Name: name, ClientID: response.PublicEnrollment.ClientID, Lease: response.ClientLeaseToken}
	if len(response.CaptureStreamOffers) > 0 {
		seat.CaptureID = response.CaptureStreamOffers[0].CaptureID
	}
	r.Players = append(r.Players, seat)
	return nil
}

// EnrollExpectingRefusal offers a client that differs from the session only in
// which build of the game it runs, and reports the code the control plane
// refused it with.
func (r *Runtime) EnrollExpectingRefusal(instance, gameHash string) (string, error) {
	status, body, err := r.client.Call("POST", "/v1/sessions/"+r.SessionID+"/enrollments", map[string]any{
		"client_instance_id": instance,
		"client_class":       "player",
		"adapter":            map[string]any{"id": AdapterID, "version": AdapterVersion},
		"compatibility":      map[string]any{"game_hash": gameHash},
	}, Options{Bearer: r.account, IdempotencyKey: "openttd-enroll-" + instance, JoinCredential: r.JoinToken})
	if err != nil {
		return "", err
	}
	if status >= 200 && status < 300 {
		return "", fmt.Errorf("a client running a different build of the game was accepted (status %d)", status)
	}
	var failure Failure
	if err := json.Unmarshal(body, &failure); err != nil {
		return "", fmt.Errorf("status %d: %s", status, string(body))
	}
	return failure.Code, nil
}

func (r *Runtime) enroll(instance, class, captureMethod string) (EnrollmentResponse, error) {
	request := map[string]any{
		"client_instance_id": instance,
		"client_class":       class,
		"adapter":            map[string]any{"id": AdapterID, "version": AdapterVersion},
		"compatibility":      map[string]any{"game_hash": r.GameHash},
	}
	if captureMethod != "" {
		request["capture_method"] = captureMethod
	}
	var response EnrollmentResponse
	err := r.client.Do("POST", "/v1/sessions/"+r.SessionID+"/enrollments", request, 201, &response,
		Options{Bearer: r.account, IdempotencyKey: "openttd-enroll-" + instance, JoinCredential: r.JoinToken})
	return response, err
}

// EventID is derived from what was observed and nothing else, so two admin
// applications that saw the same fact mint the same identifier for it. That is
// the strongest form of agreement this runtime can offer, and the ordered-hash
// finding survives it.
func EventID(sequence int, observation Observation) string {
	digest := sha256.New()
	fmt.Fprintf(digest, "%d\n%s\n", sequence, observation.Kind)
	if observation.GameTick != nil {
		digest.Write([]byte(strconv.FormatUint(*observation.GameTick, 10)))
	}
	digest.Write([]byte{'\n'})
	digest.Write(observation.Payload)
	return "openttd-" + hex.EncodeToString(digest.Sum(nil))[:32]
}

// Publish files one observer's recording on its own capture stream.
func (r *Runtime) Publish(observer Enrolled, observations []Observation, batchSize int) (int, error) {
	inputs := make([]EventInput, 0, len(observations))
	for index, observation := range observations {
		inputs = append(inputs, EventInput{
			EventID:  EventID(index, observation),
			Sequence: uint64(index),
			// Null for everything except command packets: OpenTTD announces
			// most of what it announces without a tick, and inventing one would
			// be a lie about when it happened.
			GameTick:       observation.GameTick,
			EventType:      observation.Kind,
			PayloadVersion: "1",
			Payload:        observation.Payload,
		})
	}
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := BatchRequest{
			FirstSequence: inputs[start].Sequence,
			LastSequence:  inputs[end-1].Sequence,
			Events:        inputs[start:end],
		}
		var receipt Receipt
		if err := r.client.Do("POST", "/v1/captures/"+observer.CaptureID+"/batches", batch, 200, &receipt,
			Options{Bearer: observer.Lease, IdempotencyKey: fmt.Sprintf("openttd-%s-batch-%d", observer.Name, start)}); err != nil {
			return 0, err
		}
		if receipt.AcknowledgedThrough != int64(end-1) {
			return 0, fmt.Errorf("acknowledged_through = %d after a batch ending at %d", receipt.AcknowledgedThrough, end-1)
		}
	}
	return len(inputs), nil
}

// CloseCapture ends a stream that produced something.
func (r *Runtime) CloseCapture(participant Enrolled, finalSequence uint64, reason string) (Capture, error) {
	var record Capture
	err := r.client.Do("POST", "/v1/captures/"+participant.CaptureID+":close", map[string]any{
		"final_sequence": finalSequence, "local_drops": 0, "end_reason": reason,
	}, 200, &record, Options{Bearer: participant.Lease})
	return record, err
}

// ClosePlayerCaptures ends the streams the game's own clients were given and
// never used. final_sequence is unsigned, so the smallest claim available is
// that sequence 0 exists; a client that honestly saw nothing has no way to say
// so, which is the finding this repeats against a game nobody here wrote.
func (r *Runtime) ClosePlayerCaptures() ([]Capture, error) {
	records := make([]Capture, 0, len(r.Players))
	for _, seat := range r.Players {
		if seat.CaptureID == "" {
			continue
		}
		record, err := r.CloseCapture(seat, 0, "the game's clients observe nothing independently")
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (r *Runtime) GetCapture(captureID string) (Capture, error) {
	var record Capture
	err := r.client.Do("GET", "/v1/captures/"+captureID, nil, 200, &record, Options{})
	return record, err
}

// Reconcile publishes an evidence set over whatever streams passed the gate.
func (r *Runtime) Reconcile(method, idempotencyKey string) (EvidenceSet, error) {
	var result EvidenceSet
	err := r.client.Do("POST", "/v1/executions/"+r.ExecutionID+"/evidence-sets",
		map[string]any{"method": method}, 201, &result,
		Options{Bearer: r.account, IdempotencyKey: idempotencyKey})
	return result, err
}

// ReconcileRaw returns the status and body as they came, for the cases where
// the interesting answer is a refusal or an unwelcome outcome.
func (r *Runtime) ReconcileRaw(method, idempotencyKey string) (int, EvidenceSet, Failure, error) {
	status, body, err := r.client.Call("POST", "/v1/executions/"+r.ExecutionID+"/evidence-sets",
		map[string]any{"method": method},
		Options{Bearer: r.account, IdempotencyKey: idempotencyKey})
	if err != nil {
		return 0, EvidenceSet{}, Failure{}, err
	}
	var evidence EvidenceSet
	var failure Failure
	if status >= 200 && status < 300 {
		if err := json.Unmarshal(body, &evidence); err != nil {
			return status, EvidenceSet{}, Failure{}, err
		}
		return status, evidence, Failure{}, nil
	}
	if err := json.Unmarshal(body, &failure); err != nil {
		return status, EvidenceSet{}, Failure{}, fmt.Errorf("status %d: %s", status, string(body))
	}
	return status, EvidenceSet{}, failure, nil
}
