package dedicated

import (
	"fmt"
)

const (
	AdapterID      = "bindery.dedicated-adapter"
	AdapterVersion = "0.1.0"
	GameFamily     = "bindery.dedicated"
	GameVersion    = "1.0.0"
	// The server binary's identity is the only content there is to pin. There
	// is no mod and no map: the world comes from a seed, so nothing is shipped
	// for participants to agree on beyond the server itself.
	GameHash = "sha256:4d6564696361746564526f6f74000000000000000000000000000000deadbeef"
)

// Server is the dedicated runtime: one process that owns the simulation and
// enrolls itself as the session's sole observation producer.
type Server struct {
	client  *Client
	world   *World
	account string

	SessionID    string
	ExecutionID  string
	JoinToken    string
	ClientID     string
	Lease        string
	CaptureID    string
	ReplicaID    string
	ReplicaLease string
	ReplicaCap   string
	PlayerLeases []PlayerClient
}

// PlayerClient is an enrolled human seat. In this runtime a player observes
// nothing independently, so it holds a capture stream it will never write to.
// That is not a bug in the adapter; see the ERH-007 assessment.
type PlayerClient struct {
	ClientID  string
	Lease     string
	CaptureID string
}

func NewServer(client *Client, world *World) *Server {
	return &Server{client: client, world: world}
}

func (s *Server) ClaimIdentity(handle string) error {
	var response IdentityResponse
	if err := s.client.Do("POST", "/v1/identities", map[string]any{"handle": handle}, 201, &response,
		Options{IdempotencyKey: "dedicated-identity-" + handle}); err != nil {
		return err
	}
	s.account = response.AccountToken
	return nil
}

func (s *Server) AccountToken() string { return s.account }

// CreateSession declares the runtime's compatibility without a mod or a map.
// Until 2026-08-26 the control plane refused this, and the only way through was
// to invent identifiers and hashes for content that does not exist.
func (s *Server) CreateSession(players, observers int) error {
	request := map[string]any{
		"compatibility": map[string]any{
			"game_family": GameFamily, "game_version": GameVersion, "game_hash": GameHash,
			"adapter_id": AdapterID, "adapter_version": AdapterVersion,
		},
		"participant_policy": map[string]any{
			"required_players": players, "maximum_players": players, "maximum_observers": observers,
		},
		"placement": map[string]any{"allowed_regions": []string{"eu-north"}, "latency_p95_ms": 100},
		"capture":   map[string]any{"semantic_events": true, "post_match_dump": false},
	}
	var response SessionResponse
	if err := s.client.Do("POST", "/v1/sessions", request, 201, &response,
		Options{Bearer: s.account, IdempotencyKey: "dedicated-session"}); err != nil {
		return err
	}
	s.SessionID = response.PublicSession.SessionID
	s.ExecutionID = response.PublicSession.ExecutionID
	s.JoinToken = response.SessionJoinCredential
	if response.PublicSession.Compatibility.ModID != "" || response.PublicSession.Compatibility.MapID != "" {
		return fmt.Errorf("the control plane invented mod/map identity this runtime never supplied")
	}
	return nil
}

// EnrollServer joins the simulation owner as an observer. Observer is the
// honest class: the server watches the world it computes, it does not occupy a
// player seat.
func (s *Server) EnrollServer() error {
	response, err := s.enroll("dedicated-server", "observer", "server-authoritative")
	if err != nil {
		return err
	}
	s.ClientID = response.PublicEnrollment.ClientID
	s.Lease = response.ClientLeaseToken
	if len(response.CaptureStreamOffers) != 1 {
		return fmt.Errorf("server received %d capture offers, want 1", len(response.CaptureStreamOffers))
	}
	s.CaptureID = response.CaptureStreamOffers[0].CaptureID
	return nil
}

// EnrollReplica joins a hot standby that runs the same deterministic world
// from the same seed. It is the server-authoritative answer to "two
// independent observers": not two players who each simulate, but two server
// instances that must agree, which is the divergence a transport sim actually
// needs to detect.
func (s *Server) EnrollReplica() error {
	response, err := s.enroll("dedicated-replica", "observer", "server-authoritative-replica")
	if err != nil {
		return err
	}
	s.ReplicaID = response.PublicEnrollment.ClientID
	s.ReplicaLease = response.ClientLeaseToken
	if len(response.CaptureStreamOffers) != 1 {
		return fmt.Errorf("replica received %d capture offers, want 1", len(response.CaptureStreamOffers))
	}
	s.ReplicaCap = response.CaptureStreamOffers[0].CaptureID
	return nil
}

// EnrollPlayers seats the humans. Each is handed a capture stream because the
// control plane mints one per client, which this runtime has no use for.
func (s *Server) EnrollPlayers(count int) error {
	for index := 0; index < count; index++ {
		response, err := s.enroll(fmt.Sprintf("dedicated-player-%d", index), "player", "")
		if err != nil {
			return fmt.Errorf("enroll player %d: %w", index, err)
		}
		seat := PlayerClient{ClientID: response.PublicEnrollment.ClientID, Lease: response.ClientLeaseToken}
		if len(response.CaptureStreamOffers) > 0 {
			seat.CaptureID = response.CaptureStreamOffers[0].CaptureID
		}
		s.PlayerLeases = append(s.PlayerLeases, seat)
	}
	return nil
}

func (s *Server) enroll(instance, class, captureMethod string) (EnrollmentResponse, error) {
	request := map[string]any{
		"client_instance_id": instance,
		"client_class":       class,
		"adapter":            map[string]any{"id": AdapterID, "version": AdapterVersion},
		// No mod or map hash: this client loaded neither.
		"compatibility": map[string]any{"game_hash": GameHash},
	}
	if captureMethod != "" {
		request["capture_method"] = captureMethod
	}
	var response EnrollmentResponse
	err := s.client.Do("POST", "/v1/sessions/"+s.SessionID+"/enrollments", request, 201, &response,
		Options{Bearer: s.account, IdempotencyKey: "dedicated-enroll-" + instance, JoinCredential: s.JoinToken})
	return response, err
}

// Simulate runs the world and pushes what the server observed, in batches, to
// the server's own capture stream.
func (s *Server) Simulate(ticks, batchSize int) (int, error) {
	return s.simulateInto(s.world, s.CaptureID, s.Lease, "primary", ticks, batchSize)
}

// SimulateReplica runs a second world from the same seed and files what it
// observed against the replica's own stream.
func (s *Server) SimulateReplica(world *World, ticks, batchSize int) (int, error) {
	return s.simulateInto(world, s.ReplicaCap, s.ReplicaLease, "replica", ticks, batchSize)
}

func (s *Server) simulateInto(world *World, captureID, lease, label string, ticks, batchSize int) (int, error) {
	events, err := world.Run(ticks)
	if err != nil {
		return 0, err
	}
	inputs := make([]EventInput, 0, len(events))
	for index, event := range events {
		tick := event.Tick
		inputs = append(inputs, EventInput{
			EventID:  fmt.Sprintf("dedicated-%08d", index),
			Sequence: uint64(index),
			// GameTick is a core field this runtime happens to be able to fill.
			// A runtime without a tick has no honest value for it; see the
			// ERH-007 assessment on why it cannot be removed.
			GameTick:       &tick,
			EventType:      event.Type,
			PayloadVersion: "1",
			Payload:        event.Payload,
		})
	}
	for start := 0; start < len(inputs); start += batchSize {
		end := start + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := BatchRequest{FirstSequence: inputs[start].Sequence, LastSequence: inputs[end-1].Sequence, Events: inputs[start:end]}
		var receipt Receipt
		if err := s.client.Do("POST", "/v1/captures/"+captureID+"/batches", batch, 200, &receipt,
			Options{Bearer: lease, IdempotencyKey: fmt.Sprintf("dedicated-%s-batch-%d", label, start)}); err != nil {
			return 0, err
		}
		if receipt.AcknowledgedThrough != int64(end-1) {
			return 0, fmt.Errorf("acknowledged_through = %d after batch ending at %d", receipt.AcknowledgedThrough, end-1)
		}
	}
	return len(inputs), nil
}

func (s *Server) CloseServerCapture(finalSequence uint64) (Capture, error) {
	return s.closeStream(s.CaptureID, s.Lease, finalSequence)
}

func (s *Server) CloseReplicaCapture(finalSequence uint64) (Capture, error) {
	return s.closeStream(s.ReplicaCap, s.ReplicaLease, finalSequence)
}

func (s *Server) closeStream(captureID, lease string, finalSequence uint64) (Capture, error) {
	var record Capture
	err := s.client.Do("POST", "/v1/captures/"+captureID+":close", map[string]any{
		"final_sequence": finalSequence, "local_drops": 0, "end_reason": "simulation complete",
	}, 200, &record, Options{Bearer: lease})
	return record, err
}

// ReconcileExpectingRefusal is used to pin a contract refusal as a finding.
func (s *Server) ReconcileExpectingRefusal(method, idempotencyKey string) (string, error) {
	var failure struct {
		Code string `json:"code"`
	}
	err := s.client.Do("POST", "/v1/executions/"+s.ExecutionID+"/evidence-sets",
		map[string]any{"method": method}, 400, &failure,
		Options{Bearer: s.account, IdempotencyKey: idempotencyKey})
	return failure.Code, err
}

// ClosePlayerCaptures ends the streams the players never wrote to. There is no
// way to say "this producer legitimately observed nothing": final_sequence is
// an unsigned sequence number, so zero claims that sequence 0 exists. The
// stream therefore closes with a gap it does not really have.
func (s *Server) ClosePlayerCaptures() ([]Capture, error) {
	records := make([]Capture, 0, len(s.PlayerLeases))
	for _, seat := range s.PlayerLeases {
		if seat.CaptureID == "" {
			continue
		}
		var record Capture
		if err := s.client.Do("POST", "/v1/captures/"+seat.CaptureID+":close", map[string]any{
			"final_sequence": 0, "local_drops": 0, "end_reason": "client produced no observations",
		}, 200, &record, Options{Bearer: seat.Lease}); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *Server) GetCapture(captureID string) (Capture, error) {
	var record Capture
	err := s.client.Do("GET", "/v1/captures/"+captureID, nil, 200, &record, Options{})
	return record, err
}

func (s *Server) ReconcileEvidence(method, idempotencyKey string) (EvidenceSet, error) {
	var result EvidenceSet
	err := s.client.Do("POST", "/v1/executions/"+s.ExecutionID+"/evidence-sets",
		map[string]any{"method": method}, 201, &result,
		Options{Bearer: s.account, IdempotencyKey: idempotencyKey})
	return result, err
}
