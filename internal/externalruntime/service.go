package externalruntime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
)

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,31}$`)
var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type DomainError struct {
	Code    string
	Message string
}

func (e *DomainError) Error() string { return e.Code + ": " + e.Message }

func domainError(code, message string) error { return &DomainError{Code: code, Message: message} }

type identityCreateReplay struct {
	RequestHash string
	Public      PublicIdentity
}

type sessionCreateReplay struct {
	RequestHash string
	Public      PublicSession
	ExpiresAt   time.Time
}

type enrollmentCreateReplay struct {
	RequestHash string
	Public      PublicEnrollment
	ExpiresAt   time.Time
}

type evidenceCreateReplay struct {
	RequestHash string
	Public      evidencev1.EvidenceSet
}

type Service struct {
	mu sync.RWMutex

	clock              func() time.Time
	placementAllocator PlacementAllocator
	stateStore          StateStore

	identities  map[string]*identityRecord
	handles     map[string]string
	sessions    map[string]*sessionRecord
	enrollments map[string]*enrollmentRecord
	placements  map[string]PublicPlacement
	executions  map[string]PublicExecution
	evidenceSets map[string]evidencev1.EvidenceSet

	identityIdempotency   map[string]identityCreateReplay
	sessionIdempotency    map[string]sessionCreateReplay
	enrollmentIdempotency map[string]enrollmentCreateReplay
	evidenceIdempotency   map[string]evidenceCreateReplay
}

func NewService() *Service {
	return NewServiceWithPlacementAllocator(nil)
}

// NewServiceWithPlacementAllocator wires the service to the external
// allocation authority without making the client request authoritative for
// relay identity or endpoint selection.
func NewServiceWithPlacementAllocator(allocator PlacementAllocator) *Service {
	return &Service{
		clock:                 time.Now,
		placementAllocator:    allocator,
		identities:            make(map[string]*identityRecord),
		handles:               make(map[string]string),
		sessions:              make(map[string]*sessionRecord),
		enrollments:           make(map[string]*enrollmentRecord),
		placements:            make(map[string]PublicPlacement),
		executions:            make(map[string]PublicExecution),
		evidenceSets:          make(map[string]evidencev1.EvidenceSet),
		identityIdempotency:   make(map[string]identityCreateReplay),
		sessionIdempotency:    make(map[string]sessionCreateReplay),
		enrollmentIdempotency: make(map[string]enrollmentCreateReplay),
		evidenceIdempotency:   make(map[string]evidenceCreateReplay),
	}
}

func (s *Service) now() time.Time { return s.clock().UTC() }

func (s *Service) CreateIdentity(req CreateIdentityRequest, idempotencyKey string) (CreateIdentityResponse, error) {
	if idempotencyKey == "" {
		return CreateIdentityResponse{}, domainError("IDEMPOTENCY_KEY_REQUIRED", "an idempotency key is required")
	}
	req.Handle = strings.ToLower(req.Handle)
	if !handlePattern.MatchString(req.Handle) {
		return CreateIdentityResponse{}, domainError("HANDLE_INVALID", "handle must match [a-z0-9][a-z0-9-]{2,31}")
	}
	if len(req.DisplayName) > 80 {
		return CreateIdentityResponse{}, domainError("DISPLAY_NAME_INVALID", "display name exceeds 80 characters")
	}
	metadata, err := validateMetadata(req.Metadata)
	if err != nil {
		return CreateIdentityResponse{}, err
	}
	req.Metadata = metadata
	hash := requestHash(req)

	s.mu.Lock()
	defer s.mu.Unlock()
	if replay, ok := s.identityIdempotency[idempotencyKey]; ok {
		if replay.RequestHash != hash {
			return CreateIdentityResponse{}, domainError("IDEMPOTENCY_CONFLICT", "idempotency key was reused with a different request")
		}
		return CreateIdentityResponse{PublicIdentity: clonePublicIdentity(replay.Public), Recovery: "none"}, nil
	}
	if _, exists := s.handles[req.Handle]; exists {
		return CreateIdentityResponse{}, domainError("HANDLE_TAKEN", "handle is already claimed")
	}
	before := s.snapshotLocked()
	now := s.now()
	accountID, err := newUUIDv7(now)
	if err != nil {
		return CreateIdentityResponse{}, fmt.Errorf("create account id: %w", err)
	}
	token, verifier, err := newCredential()
	if err != nil {
		return CreateIdentityResponse{}, fmt.Errorf("create account token: %w", err)
	}
	public := PublicIdentity{
		SchemaVersion: SchemaVersion, AccountID: accountID, Handle: req.Handle,
		DisplayName: req.DisplayName, ClaimedAt: now, UpdatedAt: now,
		Status: IdentityActive, Metadata: metadata,
		PublicDataNoticeVersion: "1.0",
	}
	s.identities[accountID] = &identityRecord{PublicIdentity: public, tokenVerifier: verifier}
	s.handles[req.Handle] = accountID
	response := CreateIdentityResponse{PublicIdentity: public, AccountToken: token, Recovery: "none"}
	s.identityIdempotency[idempotencyKey] = identityCreateReplay{RequestHash: hash, Public: public}
	if err := s.commitLocked(before); err != nil {
		return CreateIdentityResponse{}, err
	}
	return response, nil
}

func (s *Service) GetIdentity(accountID string) (PublicIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	identity, ok := s.identities[accountID]
	if !ok {
		return PublicIdentity{}, domainError("IDENTITY_NOT_FOUND", "identity was not found")
	}
	return clonePublicIdentity(identity.PublicIdentity), nil
}

func (s *Service) CreateSession(accountToken, idempotencyKey string, req CreateSessionRequest) (CreateSessionResponse, error) {
	if idempotencyKey == "" {
		return CreateSessionResponse{}, domainError("IDEMPOTENCY_KEY_REQUIRED", "an idempotency key is required")
	}
	req.Placement.AllowedRegions = append([]string(nil), req.Placement.AllowedRegions...)
	if err := validateSessionRequest(req); err != nil {
		return CreateSessionResponse{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	accountID, err := s.authenticateAccountLocked(accountToken)
	if err != nil {
		return CreateSessionResponse{}, err
	}
	hash := requestHash(req)
	replayKey := accountID + ":" + idempotencyKey
	if replay, ok := s.sessionIdempotency[replayKey]; ok {
		if replay.RequestHash != hash {
			return CreateSessionResponse{}, domainError("IDEMPOTENCY_CONFLICT", "idempotency key was reused with a different request")
		}
		return CreateSessionResponse{PublicSession: clonePublicSession(replay.Public), ExpiresAt: replay.ExpiresAt}, nil
	}
	before := s.snapshotLocked()
	now := s.now()
	sessionID, err := newUUIDv7(now)
	if err != nil {
		return CreateSessionResponse{}, fmt.Errorf("create session id: %w", err)
	}
	executionID, err := newUUIDv7(now)
	if err != nil {
		return CreateSessionResponse{}, fmt.Errorf("create execution id: %w", err)
	}
	placement, err := resolvePlacement(s.placementAllocator, req.Placement, sessionID, now)
	if err != nil {
		return CreateSessionResponse{}, err
	}
	join, joinVerifier, err := newCredential()
	if err != nil {
		return CreateSessionResponse{}, fmt.Errorf("create join credential: %w", err)
	}
	public := PublicSession{
		SchemaVersion: SchemaVersion, SessionID: sessionID,
		ExecutionID: executionID,
		CreatedByAccountID: accountID, CreatedAt: now, UpdatedAt: now,
		Phase: SessionCreated, Compatibility: req.Compatibility,
		ParticipantPolicy: req.ParticipantPolicy, CapturePolicy: req.Capture,
		Placement:   placement,
		Enrollments: []PublicEnrollment{}, Transitions: []PublicTransition{},
		PublicDataNoticeVersion: "1.0",
	}
	if placement != nil {
		public.PlacementID = placement.PlacementID
	}
	appendTransition(&public, nil, string(SessionCreated), "match-broker", "session-created", now)
	record := &sessionRecord{
		PublicSession: public, joinVerifier: joinVerifier,
		expiresAt: now.Add(15 * time.Minute), creatorID: accountID,
		executionID: executionID, placementID: public.PlacementID,
		placementIntent: req.Placement, enrollments: make(map[string]*enrollmentRecord),
		createRequestHash: hash,
	}
	s.sessions[sessionID] = record
	if placement != nil {
		s.placements[placement.PlacementID] = *placement
	}
	s.executions[executionID] = PublicExecution{
		SchemaVersion: SchemaVersion,
		ExecutionID:   executionID,
		SessionID:     sessionID,
		PlacementID:   public.PlacementID,
		Phase:         ExecutionPrepared,
		CreatedAt:     now,
	}
	response := CreateSessionResponse{PublicSession: clonePublicSession(public), SessionJoinCredential: join, ExpiresAt: record.expiresAt}
	s.sessionIdempotency[replayKey] = sessionCreateReplay{RequestHash: hash, Public: public, ExpiresAt: record.expiresAt}
	if err := s.commitLocked(before); err != nil {
		return CreateSessionResponse{}, err
	}
	return response, nil
}

func (s *Service) GetSession(sessionID string) (PublicSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return PublicSession{}, err
	}
	session, ok := s.sessions[sessionID]
	if !ok {
		return PublicSession{}, domainError("SESSION_NOT_FOUND", "session was not found")
	}
	s.refreshPublicEnrollmentsLocked(session)
	return clonePublicSession(session.PublicSession), nil
}

func (s *Service) GetEnrollment(clientID string) (PublicEnrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return PublicEnrollment{}, err
	}
	enrollment, ok := s.enrollments[clientID]
	if !ok {
		return PublicEnrollment{}, domainError("ENROLLMENT_NOT_FOUND", "enrollment was not found")
	}
	return enrollment.PublicEnrollment, nil
}

func (s *Service) GetPlacement(placementID string) (PublicPlacement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	placement, ok := s.placements[placementID]
	if !ok {
		return PublicPlacement{}, domainError("PLACEMENT_NOT_FOUND", "placement was not found")
	}
	return placement, nil
}

func (s *Service) GetExecution(executionID string) (PublicExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	execution, ok := s.executions[executionID]
	if !ok {
		return PublicExecution{}, domainError("EXECUTION_NOT_FOUND", "execution was not found")
	}
	return clonePublicExecution(execution), nil
}

func (s *Service) GetEvidenceSet(evidenceSetID string) (evidencev1.EvidenceSet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set, ok := s.evidenceSets[evidenceSetID]
	if !ok {
		return evidencev1.EvidenceSet{}, domainError("EVIDENCE_SET_NOT_FOUND", "evidence set was not found")
	}
	return cloneEvidenceSet(set), nil
}

func (s *Service) CreateEvidenceSet(accountToken, executionID, idempotencyKey string, req ReconcileEvidenceRequest) (evidencev1.EvidenceSet, error) {
	if idempotencyKey == "" {
		return evidencev1.EvidenceSet{}, domainError("IDEMPOTENCY_KEY_REQUIRED", "an idempotency key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return evidencev1.EvidenceSet{}, err
	}
	accountID, err := s.authenticateAccountLocked(accountToken)
	if err != nil {
		return evidencev1.EvidenceSet{}, err
	}
	execution, ok := s.executions[executionID]
	if !ok {
		return evidencev1.EvidenceSet{}, domainError("EXECUTION_NOT_FOUND", "execution was not found")
	}
	session, ok := s.sessions[execution.SessionID]
	if !ok {
		return evidencev1.EvidenceSet{}, domainError("STATE_INTEGRITY_ERROR", "execution does not resolve to a session")
	}
	if session.creatorID != accountID {
		return evidencev1.EvidenceSet{}, domainError("TOKEN_INVALID", "only the session creator may reconcile execution evidence")
	}
	hash := requestHash(req)
	replayKey := accountID + ":" + executionID + ":" + idempotencyKey
	if replay, exists := s.evidenceIdempotency[replayKey]; exists {
		if replay.RequestHash != hash {
			return evidencev1.EvidenceSet{}, domainError("IDEMPOTENCY_CONFLICT", "idempotency key was reused with a different request")
		}
		return cloneEvidenceSet(replay.Public), nil
	}
	for _, observation := range req.Observations {
		enrollment, exists := session.enrollments[observation.ObserverID]
		if !exists || enrollment.sessionID != session.SessionID {
			return evidencev1.EvidenceSet{}, domainError("OBSERVER_NOT_ENROLLED", "every observer must be enrolled in the execution session")
		}
	}
	set, err := evidencev1.Reconcile(evidencev1.ReconcileRequest{
		ExecutionID:  executionID,
		Method:       req.Method,
		Observations: req.Observations,
		CreatedAt:    s.now(),
	})
	if err != nil {
		return evidencev1.EvidenceSet{}, domainError("RECONCILIATION_INVALID", err.Error())
	}
	before := s.snapshotLocked()
	s.evidenceSets[set.EvidenceSetID] = cloneEvidenceSet(set)
	execution.EvidenceSetIDs = appendUnique(execution.EvidenceSetIDs, set.EvidenceSetID)
	s.executions[executionID] = execution
	s.evidenceIdempotency[replayKey] = evidenceCreateReplay{RequestHash: hash, Public: cloneEvidenceSet(set)}
	if err := s.commitLocked(before); err != nil {
		return evidencev1.EvidenceSet{}, err
	}
	return set, nil
}

func (s *Service) Enroll(accountToken, sessionJoinCredential, sessionID, idempotencyKey string, req EnrollmentRequest) (EnrollmentCreateResponse, error) {
	if idempotencyKey == "" {
		return EnrollmentCreateResponse{}, domainError("IDEMPOTENCY_KEY_REQUIRED", "an idempotency key is required")
	}
	if req.ClientClass != ClientPlayer && req.ClientClass != ClientObserver {
		return EnrollmentCreateResponse{}, domainError("CLIENT_CLASS_UNSUPPORTED", "client class must be player or observer")
	}
	if req.ClientInstanceID == "" || len(req.ClientInstanceID) > 128 {
		return EnrollmentCreateResponse{}, domainError("CLIENT_INSTANCE_INVALID", "client instance id is required")
	}
	if req.Adapter.ID == "" || req.Adapter.Version == "" {
		return EnrollmentCreateResponse{}, domainError("ADAPTER_INVALID", "adapter id and version are required")
	}
	if !hashPattern.MatchString(req.Compatibility.GameHash) || !hashPattern.MatchString(req.Compatibility.ModHash) || !hashPattern.MatchString(req.Compatibility.MapHash) {
		return EnrollmentCreateResponse{}, domainError("COMPATIBILITY_MISMATCH", "client compatibility hashes must be sha256 values")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return EnrollmentCreateResponse{}, err
	}
	accountID, err := s.authenticateAccountLocked(accountToken)
	if err != nil {
		return EnrollmentCreateResponse{}, err
	}
	session, ok := s.sessions[sessionID]
	if !ok {
		return EnrollmentCreateResponse{}, domainError("SESSION_NOT_FOUND", "session was not found")
	}
	if session.Phase != SessionCreated && session.Phase != SessionAdmitting && session.Phase != SessionReady {
		return EnrollmentCreateResponse{}, domainError("SESSION_NOT_ADMITTING", "session phase does not accept enrollments")
	}
	if !verifyCredential(sessionJoinCredential, session.joinVerifier) {
		return EnrollmentCreateResponse{}, domainError("JOIN_CREDENTIAL_INVALID", "session join credential is invalid or expired")
	}
	if req.Adapter.ID != session.Compatibility.AdapterID || req.Adapter.Version != session.Compatibility.AdapterVersion || req.Compatibility.GameHash != session.Compatibility.GameHash || req.Compatibility.ModHash != session.Compatibility.ModHash || req.Compatibility.MapHash != session.Compatibility.MapHash {
		return EnrollmentCreateResponse{}, domainError("COMPATIBILITY_MISMATCH", "client adapter, mod, or map does not match the session")
	}
	players, observers := 0, 0
	for _, enrollment := range session.enrollments {
		if enrollment.Phase == EnrollmentDeparted || enrollment.Phase == EnrollmentLost || enrollment.Phase == EnrollmentExpired {
			continue
		}
		if enrollment.ClientClass == ClientPlayer {
			players++
		} else {
			observers++
		}
	}
	if req.ClientClass == ClientPlayer && players >= session.ParticipantPolicy.MaximumPlayers {
		return EnrollmentCreateResponse{}, domainError("PLAYER_CAPACITY_EXCEEDED", "session has reached its player limit")
	}
	if req.ClientClass == ClientObserver && observers >= session.ParticipantPolicy.MaximumObservers {
		return EnrollmentCreateResponse{}, domainError("OBSERVER_CAPACITY_EXCEEDED", "session has reached its observer limit")
	}
	hash := requestHash(req)
	replayKey := sessionID + ":" + accountID + ":" + req.ClientInstanceID
	if replay, ok := s.enrollmentIdempotency[replayKey]; ok {
		if replay.RequestHash != hash {
			return EnrollmentCreateResponse{}, domainError("IDEMPOTENCY_CONFLICT", "enrollment key was reused with a different request")
		}
		return EnrollmentCreateResponse{PublicEnrollment: replay.Public, ExpiresAt: replay.ExpiresAt}, nil
	}
	before := s.snapshotLocked()
	now := s.now()
	clientID, err := newUUIDv7(now)
	if err != nil {
		return EnrollmentCreateResponse{}, fmt.Errorf("create client id: %w", err)
	}
	lease, leaseVerifier, err := newCredential()
	if err != nil {
		return EnrollmentCreateResponse{}, fmt.Errorf("create client lease: %w", err)
	}
	transport, transportVerifier, err := newCredential()
	if err != nil {
		return EnrollmentCreateResponse{}, fmt.Errorf("create transport credential: %w", err)
	}
	public := PublicEnrollment{ClientID: clientID, AccountID: accountID, ClientClass: req.ClientClass, Phase: EnrollmentRegistered, AdapterID: req.Adapter.ID, AdapterVersion: req.Adapter.Version, EnrolledAt: now}
	enrollment := &enrollmentRecord{PublicEnrollment: public, sessionID: sessionID, clientInstanceID: req.ClientInstanceID, leaseVerifier: leaseVerifier, transportVerifier: transportVerifier, expiresAt: now.Add(2 * time.Minute), reportIDs: make(map[string]string), requestHash: hash}
	session.enrollments[clientID] = enrollment
	s.enrollments[clientID] = enrollment
	if session.Phase == SessionCreated {
		transitionSession(session, SessionAdmitting, "match-broker", "first-client-enrolled", now)
	}
	s.refreshPublicEnrollmentsLocked(session)
	response := EnrollmentCreateResponse{PublicEnrollment: public, ClientLeaseToken: lease, TransportCredential: transport, ExpiresAt: enrollment.expiresAt}
	s.enrollmentIdempotency[replayKey] = enrollmentCreateReplay{RequestHash: hash, Public: public, ExpiresAt: enrollment.expiresAt}
	if err := s.commitLocked(before); err != nil {
		return EnrollmentCreateResponse{}, err
	}
	return response, nil
}

func (s *Service) Report(clientLease, clientID, idempotencyKey string, req LifecycleReportRequest) (LifecycleReportResponse, error) {
	if idempotencyKey == "" || req.ReportID == "" {
		return LifecycleReportResponse{}, domainError("IDEMPOTENCY_KEY_REQUIRED", "idempotency key and report_id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return LifecycleReportResponse{}, err
	}
	enrollment, ok := s.enrollments[clientID]
	if !ok {
		return LifecycleReportResponse{}, domainError("ENROLLMENT_NOT_FOUND", "enrollment was not found")
	}
	if !verifyCredential(clientLease, enrollment.leaseVerifier) {
		return LifecycleReportResponse{}, domainError("TOKEN_INVALID", "client lease token is invalid")
	}
	hash := requestHash(req)
	if previous, ok := enrollment.reportIDs[idempotencyKey]; ok {
		if previous != hash {
			return LifecycleReportResponse{}, domainError("IDEMPOTENCY_CONFLICT", "report idempotency key was reused with a different request")
		}
		session := s.sessions[enrollment.sessionID]
		s.refreshPublicEnrollmentsLocked(session)
		return LifecycleReportResponse{PublicSession: clonePublicSession(session.PublicSession), PublicEnrollment: enrollment.PublicEnrollment}, nil
	}
	if enrollment.expiresAt.Before(s.now()) {
		return LifecycleReportResponse{}, domainError("LEASE_EXPIRED", "client lease has expired")
	}
	session := s.sessions[enrollment.sessionID]
	before := s.snapshotLocked()
	now := s.now()
	if err := applyReport(session, enrollment, req, now); err != nil {
		return LifecycleReportResponse{}, err
	}
	s.syncExecutionLocked(session, now)
	enrollment.reportIDs[idempotencyKey] = hash
	s.refreshPublicEnrollmentsLocked(session)
	if err := s.commitLocked(before); err != nil {
		return LifecycleReportResponse{}, err
	}
	return LifecycleReportResponse{PublicSession: clonePublicSession(session.PublicSession), PublicEnrollment: enrollment.PublicEnrollment}, nil
}

func (s *Service) Heartbeat(clientLease, clientID string) (HeartbeatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return HeartbeatResponse{}, err
	}
	enrollment, ok := s.enrollments[clientID]
	if !ok {
		return HeartbeatResponse{}, domainError("ENROLLMENT_NOT_FOUND", "enrollment was not found")
	}
	if !verifyCredential(clientLease, enrollment.leaseVerifier) {
		return HeartbeatResponse{}, domainError("TOKEN_INVALID", "client lease token is invalid")
	}
	now := s.now()
	if enrollment.expiresAt.Before(now) || enrollment.Phase == EnrollmentDeparted || enrollment.Phase == EnrollmentLost || enrollment.Phase == EnrollmentExpired {
		return HeartbeatResponse{}, domainError("LEASE_EXPIRED", "client lease has expired")
	}
	before := s.snapshotLocked()
	enrollment.expiresAt = now.Add(2 * time.Minute)
	if err := s.commitLocked(before); err != nil {
		return HeartbeatResponse{}, err
	}
	return HeartbeatResponse{ClientID: clientID, ExpiresAt: enrollment.expiresAt}, nil
}

func (s *Service) Converge(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.convergeAndPersistLocked(now.UTC())
}

func (s *Service) convergeAndPersistLocked(now time.Time) error {
	before := s.snapshotLocked()
	if !s.convergeLocked(now) {
		return nil
	}
	return s.commitLocked(before)
}

func (s *Service) convergeLocked(now time.Time) bool {
	changed := false
	for _, session := range s.sessions {
		if (session.Phase == SessionCreated || session.Phase == SessionAdmitting || session.Phase == SessionReady) && session.expiresAt.Before(now) {
			transitionSession(session, SessionExpired, "lease-converger", "session-admission-expired", now)
			changed = true
		}
		for _, enrollment := range session.enrollments {
			if enrollment.expiresAt.After(now) || enrollment.Phase == EnrollmentDeparted || enrollment.Phase == EnrollmentLost || enrollment.Phase == EnrollmentExpired {
				continue
			}
			from := enrollment.Phase
			if from == EnrollmentIssued || from == EnrollmentRegistered || from == EnrollmentReady {
				enrollment.Phase = EnrollmentExpired
			} else {
				enrollment.Phase = EnrollmentLost
			}
			if from != enrollment.Phase {
				transitionEnrollment(enrollment, from, enrollment.Phase, "lease-converger", "client-lease-expired", now)
				changed = true
			}
			if enrollment.ClientClass == ClientPlayer && session.Phase == SessionRunning {
				transitionSession(session, SessionFailed, "lease-converger", "required-player-lease-expired", now)
				changed = true
			}
		}
		s.refreshPublicEnrollmentsLocked(session)
		s.syncExecutionLocked(session, now)
	}
	return changed
}

func (s *Service) syncExecutionLocked(session *sessionRecord, now time.Time) {
	execution, ok := s.executions[session.executionID]
	if !ok {
		return
	}
	switch session.Phase {
	case SessionRunning:
		execution.Phase = ExecutionRunning
		if execution.StartedAt == nil {
			execution.StartedAt = timePtr(now)
		}
	case SessionEnded, SessionPublished:
		execution.Phase = ExecutionEnded
		if execution.EndedAt == nil {
			execution.EndedAt = timePtr(now)
		}
	case SessionFailed:
		execution.Phase = ExecutionFailed
		if execution.EndedAt == nil {
			execution.EndedAt = timePtr(now)
		}
	case SessionExpired:
		execution.Phase = ExecutionExpired
		if execution.EndedAt == nil {
			execution.EndedAt = timePtr(now)
		}
	}
	s.executions[session.executionID] = execution
}

func (s *Service) authenticateAccountLocked(token string) (string, error) {
	for id, identity := range s.identities {
		if verifyCredential(token, identity.tokenVerifier) {
			if identity.Status != IdentityActive {
				return "", domainError("IDENTITY_SUSPENDED", "identity is suspended")
			}
			return id, nil
		}
	}
	return "", domainError("TOKEN_INVALID", "account token is invalid")
}

func (s *Service) refreshPublicEnrollmentsLocked(session *sessionRecord) {
	ids := make([]string, 0, len(session.enrollments))
	for id := range session.enrollments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	public := make([]PublicEnrollment, 0, len(ids))
	for _, id := range ids {
		public = append(public, session.enrollments[id].PublicEnrollment)
	}
	session.Enrollments = public
}

func validateSessionRequest(req CreateSessionRequest) error {
	if req.Compatibility.GameFamily == "" || req.Compatibility.GameVersion == "" || req.Compatibility.AdapterID == "" || req.Compatibility.AdapterVersion == "" || req.Compatibility.ModID == "" || req.Compatibility.MapID == "" {
		return domainError("COMPATIBILITY_INVALID", "game, adapter, mod, and map identifiers are required")
	}
	if !hashPattern.MatchString(req.Compatibility.GameHash) || !hashPattern.MatchString(req.Compatibility.ModHash) || !hashPattern.MatchString(req.Compatibility.MapHash) {
		return domainError("COMPATIBILITY_INVALID", "game_hash, mod_hash, and map_hash must be sha256 values")
	}
	if req.ParticipantPolicy.RequiredPlayers < 2 || req.ParticipantPolicy.MaximumPlayers < req.ParticipantPolicy.RequiredPlayers || req.ParticipantPolicy.MaximumPlayers > 8 || req.ParticipantPolicy.MaximumObservers < 0 || req.ParticipantPolicy.MaximumObservers > 8 {
		return domainError("PARTICIPANT_POLICY_INVALID", "participant limits are invalid")
	}
	if req.Placement.LatencyP95MS < 0 || len(req.Placement.AllowedRegions) == 0 {
		return domainError("PLACEMENT_INVALID", "at least one allowed region and a non-negative latency target are required")
	}
	return nil
}

func validateMetadata(metadata map[string]any) (map[string]any, error) {
	if len(metadata) > 32 {
		return nil, domainError("METADATA_INVALID", "metadata has too many properties")
	}
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if len(key) == 0 || len(key) > 64 {
			return nil, domainError("METADATA_INVALID", "metadata key is invalid")
		}
		switch typed := value.(type) {
		case nil, string, bool, float64:
			result[key] = typed
		default:
			return nil, domainError("METADATA_INVALID", "metadata values must be scalar")
		}
	}
	return result, nil
}

func requestHash(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", hash[:])
}

func clonePublicIdentity(value PublicIdentity) PublicIdentity {
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}

func cloneMetadata(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func clonePublicSession(value PublicSession) PublicSession {
	value.Enrollments = append([]PublicEnrollment(nil), value.Enrollments...)
	value.CaptureIDs = append([]string(nil), value.CaptureIDs...)
	value.Transitions = append([]PublicTransition(nil), value.Transitions...)
	if value.Placement != nil {
		placement := *value.Placement
		placementCopy := placement
		value.Placement = &placementCopy
	}
	return value
}

func clonePublicExecution(value PublicExecution) PublicExecution {
	value.EvidenceSetIDs = append([]string(nil), value.EvidenceSetIDs...)
	if value.StartedAt != nil {
		started := *value.StartedAt
		value.StartedAt = &started
	}
	if value.EndedAt != nil {
		ended := *value.EndedAt
		value.EndedAt = &ended
	}
	return value
}

func cloneEvidenceSet(value evidencev1.EvidenceSet) evidencev1.EvidenceSet {
	value.Observations = append([]evidencev1.ObservationSummary(nil), value.Observations...)
	value.Reconciliation.DistinctCounts = append([]uint64(nil), value.Reconciliation.DistinctCounts...)
	value.Reconciliation.DistinctHashes = append([]string(nil), value.Reconciliation.DistinctHashes...)
	return value
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendTransition(session *PublicSession, from *SessionPhase, to, source, reason string, now time.Time) {
	transitionID, _ := newUUIDv7(now)
	var fromValue *string
	if from != nil {
		value := string(*from)
		fromValue = &value
	}
	session.Transitions = append(session.Transitions, PublicTransition{TransitionID: transitionID, From: fromValue, To: to, OccurredAt: now, Source: source, Reason: reason})
}

func transitionSession(session *sessionRecord, to SessionPhase, source, reason string, now time.Time) {
	if session.Phase == to {
		return
	}
	from := session.Phase
	session.Phase = to
	session.UpdatedAt = now
	appendTransition(&session.PublicSession, &from, string(to), source, reason, now)
}

func transitionEnrollment(enrollment *enrollmentRecord, from, to EnrollmentPhase, source, reason string, now time.Time) {
	if from == to {
		return
	}
	enrollment.Phase = to
	if to == EnrollmentDeparted {
		enrollment.DepartedAt = timePtr(now)
	}
}

func applyReport(session *sessionRecord, enrollment *enrollmentRecord, req LifecycleReportRequest, now time.Time) error {
	switch req.Kind {
	case "ready":
		if enrollment.Phase != EnrollmentRegistered {
			return domainError("INVALID_TRANSITION", "enrollment is not registered")
		}
		transitionEnrollment(enrollment, enrollment.Phase, EnrollmentReady, "client-report", reportReason(req, "client-ready"), now)
		if readyPlayers(session) >= session.ParticipantPolicy.RequiredPlayers {
			transitionSession(session, SessionReady, "match-broker", "required-players-ready", now)
		}
	case "started":
		if enrollment.Phase != EnrollmentReady {
			return domainError("INVALID_TRANSITION", "enrollment is not ready")
		}
		transitionEnrollment(enrollment, enrollment.Phase, EnrollmentActive, "client-report", reportReason(req, "game-started"), now)
		if session.Phase == SessionReady {
			transitionSession(session, SessionRunning, "client-consensus", "required-players-reported-game-start", now)
		}
	case "exited":
		if enrollment.Phase != EnrollmentActive && enrollment.Phase != EnrollmentReady {
			return domainError("INVALID_TRANSITION", "enrollment is not active")
		}
		transitionEnrollment(enrollment, enrollment.Phase, EnrollmentDeparted, "client-report", reportReason(req, "clean-exit"), now)
		if activePlayers(session) == 0 && session.Phase == SessionRunning {
			transitionSession(session, SessionEnded, "match-broker", "all-players-departed", now)
		}
	case "failed":
		if enrollment.Phase == EnrollmentDeparted || enrollment.Phase == EnrollmentExpired {
			return domainError("INVALID_TRANSITION", "enrollment is terminal")
		}
		transitionEnrollment(enrollment, enrollment.Phase, EnrollmentLost, "client-report", reportReason(req, "client-failed"), now)
		if enrollment.ClientClass == ClientPlayer && session.Phase != SessionEnded && session.Phase != SessionPublished {
			transitionSession(session, SessionFailed, "client-report", reportReason(req, "player-failed"), now)
		}
	case "capture_degraded":
		// Capture degradation is recorded by W3. It never mutates the player
		// lifecycle in W0, especially for an observer.
	default:
		return domainError("REPORT_KIND_INVALID", "report kind is unsupported")
	}
	return nil
}

func reportReason(req LifecycleReportRequest, fallback string) string {
	if req.Reason == "" {
		return fallback
	}
	if len(req.Reason) > 256 {
		return req.Reason[:256]
	}
	return req.Reason
}

func readyPlayers(session *sessionRecord) int {
	count := 0
	for _, enrollment := range session.enrollments {
		if enrollment.ClientClass == ClientPlayer && (enrollment.Phase == EnrollmentReady || enrollment.Phase == EnrollmentActive) {
			count++
		}
	}
	return count
}
func activePlayers(session *sessionRecord) int {
	count := 0
	for _, enrollment := range session.enrollments {
		if enrollment.ClientClass == ClientPlayer && enrollment.Phase == EnrollmentActive {
			count++
		}
	}
	return count
}
func timePtr(value time.Time) *time.Time { return &value }
