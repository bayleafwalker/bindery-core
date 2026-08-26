package externalruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
)

const stateSnapshotVersion = "bindery.externalruntime.state/v1"

// StateStore is the single-writer durability boundary for the reference
// control plane. The file implementation is intentionally not a multi-replica
// coordination mechanism.
type StateStore interface {
	Load() (serviceSnapshot, error)
	Save(serviceSnapshot) error
}

type storedIdentity struct {
	Public        PublicIdentity `json:"public"`
	TokenVerifier []byte         `json:"token_verifier"`
}

type storedSession struct {
	Public            PublicSession   `json:"public"`
	JoinVerifier      []byte          `json:"join_verifier"`
	ExpiresAt         time.Time       `json:"expires_at"`
	CreatorID         string          `json:"creator_id"`
	ExecutionID       string          `json:"execution_id"`
	PlacementID       string          `json:"placement_id,omitempty"`
	PlacementIntent   PlacementIntent `json:"placement_intent"`
	EnrollmentIDs     []string        `json:"enrollment_ids"`
	CreateRequestHash string          `json:"create_request_hash"`
}

type storedEnrollment struct {
	Public            PublicEnrollment  `json:"public"`
	SessionID         string            `json:"session_id"`
	ClientInstanceID  string            `json:"client_instance_id"`
	LeaseVerifier     []byte            `json:"lease_verifier"`
	TransportVerifier []byte            `json:"transport_verifier"`
	ExpiresAt         time.Time         `json:"expires_at"`
	ReportIDs         map[string]string `json:"report_ids"`
	RequestHash       string            `json:"request_hash"`
}

type storedIdentityReplay struct {
	RequestHash string         `json:"request_hash"`
	Public      PublicIdentity `json:"public"`
}

type storedSessionReplay struct {
	RequestHash string        `json:"request_hash"`
	Public      PublicSession `json:"public"`
	ExpiresAt   time.Time     `json:"expires_at"`
}

type storedEnrollmentReplay struct {
	RequestHash string           `json:"request_hash"`
	Public      PublicEnrollment `json:"public"`
	ExpiresAt   time.Time        `json:"expires_at"`
}

type storedEvidenceReplay struct {
	RequestHash string                 `json:"request_hash"`
	Public      evidencev1.EvidenceSet `json:"public"`
}

type serviceSnapshot struct {
	SchemaVersion         string                            `json:"schema_version"`
	Identities            map[string]storedIdentity         `json:"identities"`
	Handles               map[string]string                 `json:"handles"`
	Sessions              map[string]storedSession          `json:"sessions"`
	Enrollments           map[string]storedEnrollment       `json:"enrollments"`
	Placements            map[string]PublicPlacement        `json:"placements"`
	Executions            map[string]PublicExecution        `json:"executions"`
	EvidenceSets          map[string]evidencev1.EvidenceSet `json:"evidence_sets"`
	IdentityIdempotency   map[string]storedIdentityReplay   `json:"identity_idempotency"`
	SessionIdempotency    map[string]storedSessionReplay    `json:"session_idempotency"`
	EnrollmentIdempotency map[string]storedEnrollmentReplay `json:"enrollment_idempotency"`
	EvidenceIdempotency   map[string]storedEvidenceReplay   `json:"evidence_idempotency"`
}

type FileStateStore struct {
	path string
}

func NewFileStateStore(path string) (*FileStateStore, error) {
	if path == "" {
		return nil, errors.New("state path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve state path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	return &FileStateStore{path: absolute}, nil
}

func (s *FileStateStore) Load() (serviceSnapshot, error) {
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyServiceSnapshot(), nil
	}
	if err != nil {
		return serviceSnapshot{}, fmt.Errorf("inspect state file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return serviceSnapshot{}, errors.New("state path must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return serviceSnapshot{}, fmt.Errorf("state file permissions %04o expose private verifier material", info.Mode().Perm())
	}
	file, err := os.Open(s.path)
	if err != nil {
		return serviceSnapshot{}, fmt.Errorf("open state file: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<20))
	decoder.DisallowUnknownFields()
	var snapshot serviceSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return serviceSnapshot{}, fmt.Errorf("decode state snapshot: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return serviceSnapshot{}, errors.New("state file contains more than one JSON value")
	}
	return snapshot, nil
}

func (s *FileStateStore) Save(snapshot serviceSnapshot) error {
	if info, err := os.Lstat(s.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("state path must be a regular file, not a symlink")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect state file: %w", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode state snapshot: %w", err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(s.path)
	temporary, err := os.CreateTemp(directory, ".bindery-state-*")
	if err != nil {
		return fmt.Errorf("create state transaction: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect state transaction: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("write state transaction: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync state transaction: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close state transaction: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("commit state transaction: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open state directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync state directory: %w", err)
	}
	return nil
}

func OpenPersistentService(allocator PlacementAllocator, store StateStore) (*Service, error) {
	if store == nil {
		return nil, errors.New("persistent service requires a state store")
	}
	service := NewServiceWithPlacementAllocator(allocator)
	service.stateStore = store
	snapshot, err := store.Load()
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.restoreSnapshotLocked(snapshot); err != nil {
		return nil, fmt.Errorf("restore control state: %w", err)
	}
	return service, nil
}

func emptyServiceSnapshot() serviceSnapshot {
	return serviceSnapshot{
		SchemaVersion:         stateSnapshotVersion,
		Identities:            make(map[string]storedIdentity),
		Handles:               make(map[string]string),
		Sessions:              make(map[string]storedSession),
		Enrollments:           make(map[string]storedEnrollment),
		Placements:            make(map[string]PublicPlacement),
		Executions:            make(map[string]PublicExecution),
		EvidenceSets:          make(map[string]evidencev1.EvidenceSet),
		IdentityIdempotency:   make(map[string]storedIdentityReplay),
		SessionIdempotency:    make(map[string]storedSessionReplay),
		EnrollmentIdempotency: make(map[string]storedEnrollmentReplay),
		EvidenceIdempotency:   make(map[string]storedEvidenceReplay),
	}
}

func (s *Service) snapshotLocked() serviceSnapshot {
	snapshot := emptyServiceSnapshot()
	for id, identity := range s.identities {
		snapshot.Identities[id] = storedIdentity{Public: clonePublicIdentity(identity.PublicIdentity), TokenVerifier: append([]byte(nil), identity.tokenVerifier...)}
	}
	for handle, id := range s.handles {
		snapshot.Handles[handle] = id
	}
	for id, session := range s.sessions {
		enrollmentIDs := make([]string, 0, len(session.enrollments))
		for enrollmentID := range session.enrollments {
			enrollmentIDs = append(enrollmentIDs, enrollmentID)
		}
		sort.Strings(enrollmentIDs)
		snapshot.Sessions[id] = storedSession{
			Public:            clonePublicSession(session.PublicSession),
			JoinVerifier:      append([]byte(nil), session.joinVerifier...),
			ExpiresAt:         session.expiresAt,
			CreatorID:         session.creatorID,
			ExecutionID:       session.executionID,
			PlacementID:       session.placementID,
			PlacementIntent:   PlacementIntent{AllowedRegions: append([]string(nil), session.placementIntent.AllowedRegions...), LatencyP95MS: session.placementIntent.LatencyP95MS},
			EnrollmentIDs:     enrollmentIDs,
			CreateRequestHash: session.createRequestHash,
		}
	}
	for id, enrollment := range s.enrollments {
		reportIDs := make(map[string]string, len(enrollment.reportIDs))
		for key, value := range enrollment.reportIDs {
			reportIDs[key] = value
		}
		snapshot.Enrollments[id] = storedEnrollment{
			Public:            enrollment.PublicEnrollment,
			SessionID:         enrollment.sessionID,
			ClientInstanceID:  enrollment.clientInstanceID,
			LeaseVerifier:     append([]byte(nil), enrollment.leaseVerifier...),
			TransportVerifier: append([]byte(nil), enrollment.transportVerifier...),
			ExpiresAt:         enrollment.expiresAt,
			ReportIDs:         reportIDs,
			RequestHash:       enrollment.requestHash,
		}
	}
	for id, placement := range s.placements {
		snapshot.Placements[id] = placement
	}
	for id, execution := range s.executions {
		snapshot.Executions[id] = clonePublicExecution(execution)
	}
	for id, set := range s.evidenceSets {
		snapshot.EvidenceSets[id] = cloneEvidenceSet(set)
	}
	for key, replay := range s.identityIdempotency {
		snapshot.IdentityIdempotency[key] = storedIdentityReplay{RequestHash: replay.RequestHash, Public: clonePublicIdentity(replay.Public)}
	}
	for key, replay := range s.sessionIdempotency {
		snapshot.SessionIdempotency[key] = storedSessionReplay{RequestHash: replay.RequestHash, Public: clonePublicSession(replay.Public), ExpiresAt: replay.ExpiresAt}
	}
	for key, replay := range s.enrollmentIdempotency {
		snapshot.EnrollmentIdempotency[key] = storedEnrollmentReplay{RequestHash: replay.RequestHash, Public: replay.Public, ExpiresAt: replay.ExpiresAt}
	}
	for key, replay := range s.evidenceIdempotency {
		snapshot.EvidenceIdempotency[key] = storedEvidenceReplay{RequestHash: replay.RequestHash, Public: cloneEvidenceSet(replay.Public)}
	}
	return snapshot
}

func (s *Service) restoreSnapshotLocked(snapshot serviceSnapshot) error {
	if snapshot.SchemaVersion != stateSnapshotVersion {
		return fmt.Errorf("unsupported state schema %q", snapshot.SchemaVersion)
	}
	identities := make(map[string]*identityRecord, len(snapshot.Identities))
	for id, stored := range snapshot.Identities {
		if id == "" || stored.Public.AccountID != id || len(stored.TokenVerifier) != 32 {
			return fmt.Errorf("identity %q is invalid", id)
		}
		identities[id] = &identityRecord{PublicIdentity: clonePublicIdentity(stored.Public), tokenVerifier: append([]byte(nil), stored.TokenVerifier...)}
	}
	handles := make(map[string]string, len(snapshot.Handles))
	for handle, id := range snapshot.Handles {
		identity, ok := identities[id]
		if !ok || identity.Handle != handle {
			return fmt.Errorf("handle %q does not resolve to its identity", handle)
		}
		handles[handle] = id
	}
	placements := make(map[string]PublicPlacement, len(snapshot.Placements))
	for id, placement := range snapshot.Placements {
		if placement.PlacementID != id {
			return fmt.Errorf("placement %q has a different identity", id)
		}
		if err := validatePublicPlacement(placement); err != nil {
			return fmt.Errorf("placement %q: %w", id, err)
		}
		placements[id] = placement
	}
	executions := make(map[string]PublicExecution, len(snapshot.Executions))
	for id, execution := range snapshot.Executions {
		if execution.ExecutionID != id || execution.SessionID == "" {
			return fmt.Errorf("execution %q is invalid", id)
		}
		if execution.PlacementID != "" {
			if _, ok := placements[execution.PlacementID]; !ok {
				return fmt.Errorf("execution %q has dangling placement %q", id, execution.PlacementID)
			}
		}
		executions[id] = clonePublicExecution(execution)
	}
	enrollments := make(map[string]*enrollmentRecord, len(snapshot.Enrollments))
	for id, stored := range snapshot.Enrollments {
		if stored.Public.ClientID != id || stored.SessionID == "" || len(stored.LeaseVerifier) != 32 || len(stored.TransportVerifier) != 32 {
			return fmt.Errorf("enrollment %q is invalid", id)
		}
		reportIDs := make(map[string]string, len(stored.ReportIDs))
		for key, value := range stored.ReportIDs {
			reportIDs[key] = value
		}
		enrollments[id] = &enrollmentRecord{
			PublicEnrollment: stored.Public,
			sessionID:        stored.SessionID, clientInstanceID: stored.ClientInstanceID,
			leaseVerifier: append([]byte(nil), stored.LeaseVerifier...), transportVerifier: append([]byte(nil), stored.TransportVerifier...),
			expiresAt: stored.ExpiresAt, reportIDs: reportIDs, requestHash: stored.RequestHash,
		}
	}
	sessions := make(map[string]*sessionRecord, len(snapshot.Sessions))
	for id, stored := range snapshot.Sessions {
		if stored.Public.SessionID != id || stored.Public.ExecutionID != stored.ExecutionID {
			return fmt.Errorf("session %q is invalid", id)
		}
		if _, ok := identities[stored.CreatorID]; !ok {
			return fmt.Errorf("session %q has dangling creator %q", id, stored.CreatorID)
		}
		execution, ok := executions[stored.ExecutionID]
		if !ok || execution.SessionID != id {
			return fmt.Errorf("session %q has dangling execution %q", id, stored.ExecutionID)
		}
		if stored.PlacementID != "" {
			placement, ok := placements[stored.PlacementID]
			if !ok || placement.SessionID != id || stored.Public.PlacementID != stored.PlacementID {
				return fmt.Errorf("session %q has dangling placement %q", id, stored.PlacementID)
			}
		}
		record := &sessionRecord{
			PublicSession: clonePublicSession(stored.Public),
			joinVerifier:  append([]byte(nil), stored.JoinVerifier...), expiresAt: stored.ExpiresAt,
			creatorID: stored.CreatorID, executionID: stored.ExecutionID, placementID: stored.PlacementID,
			placementIntent: PlacementIntent{AllowedRegions: append([]string(nil), stored.PlacementIntent.AllowedRegions...), LatencyP95MS: stored.PlacementIntent.LatencyP95MS},
			enrollments:     make(map[string]*enrollmentRecord), createRequestHash: stored.CreateRequestHash,
		}
		for _, enrollmentID := range stored.EnrollmentIDs {
			enrollment, ok := enrollments[enrollmentID]
			if !ok || enrollment.sessionID != id {
				return fmt.Errorf("session %q has dangling enrollment %q", id, enrollmentID)
			}
			record.enrollments[enrollmentID] = enrollment
		}
		sessions[id] = record
	}
	for id, enrollment := range enrollments {
		if _, ok := sessions[enrollment.sessionID]; !ok {
			return fmt.Errorf("enrollment %q has dangling session %q", id, enrollment.sessionID)
		}
	}
	evidenceSets := make(map[string]evidencev1.EvidenceSet, len(snapshot.EvidenceSets))
	for id, set := range snapshot.EvidenceSets {
		if set.EvidenceSetID != id {
			return fmt.Errorf("evidence set %q has a different identity", id)
		}
		if _, ok := executions[set.ExecutionID]; !ok {
			return fmt.Errorf("evidence set %q has dangling execution %q", id, set.ExecutionID)
		}
		evidenceSets[id] = cloneEvidenceSet(set)
	}
	for executionID, execution := range executions {
		for _, evidenceSetID := range execution.EvidenceSetIDs {
			set, ok := evidenceSets[evidenceSetID]
			if !ok || set.ExecutionID != executionID {
				return fmt.Errorf("execution %q has dangling evidence set %q", executionID, evidenceSetID)
			}
		}
	}

	s.identities, s.handles, s.sessions, s.enrollments = identities, handles, sessions, enrollments
	s.placements, s.executions, s.evidenceSets = placements, executions, evidenceSets
	s.identityIdempotency = make(map[string]identityCreateReplay, len(snapshot.IdentityIdempotency))
	for key, replay := range snapshot.IdentityIdempotency {
		s.identityIdempotency[key] = identityCreateReplay{RequestHash: replay.RequestHash, Public: clonePublicIdentity(replay.Public)}
	}
	s.sessionIdempotency = make(map[string]sessionCreateReplay, len(snapshot.SessionIdempotency))
	for key, replay := range snapshot.SessionIdempotency {
		s.sessionIdempotency[key] = sessionCreateReplay{RequestHash: replay.RequestHash, Public: clonePublicSession(replay.Public), ExpiresAt: replay.ExpiresAt}
	}
	s.enrollmentIdempotency = make(map[string]enrollmentCreateReplay, len(snapshot.EnrollmentIdempotency))
	for key, replay := range snapshot.EnrollmentIdempotency {
		s.enrollmentIdempotency[key] = enrollmentCreateReplay{RequestHash: replay.RequestHash, Public: replay.Public, ExpiresAt: replay.ExpiresAt}
	}
	s.evidenceIdempotency = make(map[string]evidenceCreateReplay, len(snapshot.EvidenceIdempotency))
	for key, replay := range snapshot.EvidenceIdempotency {
		s.evidenceIdempotency[key] = evidenceCreateReplay{RequestHash: replay.RequestHash, Public: cloneEvidenceSet(replay.Public)}
	}
	for _, session := range s.sessions {
		s.refreshPublicEnrollmentsLocked(session)
	}
	return nil
}

func (s *Service) commitLocked(before serviceSnapshot) error {
	if s.stateStore == nil {
		return nil
	}
	if err := s.stateStore.Save(s.snapshotLocked()); err != nil {
		if restoreErr := s.restoreSnapshotLocked(before); restoreErr != nil {
			return domainError("STATE_INTEGRITY_ERROR", "control-state persistence failed and the in-memory rollback could not be verified")
		}
		return domainError("STATE_PERSISTENCE_FAILED", "control-state persistence failed; the mutation was rolled back")
	}
	return nil
}
