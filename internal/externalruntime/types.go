package externalruntime

import (
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
)

const SchemaVersion = "1.0.0"

type IdentityStatus string

const (
	IdentityActive    IdentityStatus = "active"
	IdentitySuspended IdentityStatus = "suspended"
)

type SessionPhase string

const (
	SessionCreated   SessionPhase = "created"
	SessionAdmitting SessionPhase = "admitting"
	SessionReady     SessionPhase = "ready"
	SessionRunning   SessionPhase = "running"
	SessionEnded     SessionPhase = "ended"
	SessionFailed    SessionPhase = "failed"
	SessionExpired   SessionPhase = "expired"
	SessionPublished SessionPhase = "published"
)

type ClientClass string

const (
	ClientPlayer   ClientClass = "player"
	ClientObserver ClientClass = "observer"
)

type EnrollmentPhase string

const (
	EnrollmentIssued     EnrollmentPhase = "issued"
	EnrollmentRegistered EnrollmentPhase = "registered"
	EnrollmentReady      EnrollmentPhase = "ready"
	EnrollmentActive     EnrollmentPhase = "active"
	EnrollmentDeparted   EnrollmentPhase = "departed"
	EnrollmentLost       EnrollmentPhase = "lost"
	EnrollmentExpired    EnrollmentPhase = "expired"
)

type Compatibility struct {
	GameFamily     string `json:"game_family"`
	GameVersion    string `json:"game_version"`
	GameHash       string `json:"game_hash"`
	AdapterID      string `json:"adapter_id"`
	AdapterVersion string `json:"adapter_version"`
	ModID          string `json:"mod_id"`
	ModHash        string `json:"mod_hash"`
	MapID          string `json:"map_id"`
	MapHash        string `json:"map_hash"`
}

type ParticipantPolicy struct {
	RequiredPlayers  int `json:"required_players"`
	MaximumPlayers   int `json:"maximum_players"`
	MaximumObservers int `json:"maximum_observers"`
}

type PlacementIntent struct {
	AllowedRegions []string `json:"allowed_regions"`
	LatencyP95MS   int      `json:"latency_p95_ms"`
}

// PlacementAllocator is the coordinator-owned seam between session creation
// and the relay allocation authority. The client supplies only an intent;
// allocation identity, endpoint, provider, and policy come from this seam.
type PlacementAllocator func(PlacementIntent) (PublicPlacement, error)

type CapturePolicy struct {
	SemanticEvents    bool `json:"semantic_events"`
	PostMatchDump     bool `json:"post_match_dump"`
	ObserverPreferred bool `json:"observer_preferred"`
}

// ImplementationIdentity makes a behaviorally significant component
// resolvable after the process and checkout that executed it are gone.
type ImplementationIdentity struct {
	Implementation string `json:"implementation"`
	Repository     string `json:"repository"`
	Revision       string `json:"revision"`
	ConfigDigest   string `json:"config_digest"`
}

type PublicPlacement struct {
	SchemaVersion     string                 `json:"schema_version"`
	PlacementID       string                 `json:"placement_id"`
	SessionID         string                 `json:"session_id"`
	Region            string                 `json:"region"`
	RelayProviderID   string                 `json:"relay_provider_id"`
	RelayAllocationID string                 `json:"relay_allocation_id"`
	RelayEndpoint     string                 `json:"relay_endpoint"`
	PolicyVersion     string                 `json:"policy_version"`
	DecisionSummary   string                 `json:"decision_summary,omitempty"`
	Allocator         ImplementationIdentity `json:"allocator"`
	CreatedAt         time.Time              `json:"created_at"`
}

type ExecutionPhase string

const (
	ExecutionPrepared ExecutionPhase = "prepared"
	ExecutionRunning  ExecutionPhase = "running"
	ExecutionEnded    ExecutionPhase = "ended"
	ExecutionFailed   ExecutionPhase = "failed"
	ExecutionExpired  ExecutionPhase = "expired"
)

// PublicExecution is deliberately distinct from a session. A session carries
// admission intent and participants; an execution is the externally run thing
// to which observations and evidence sets refer.
type PublicExecution struct {
	SchemaVersion  string         `json:"schema_version"`
	ExecutionID    string         `json:"execution_id"`
	SessionID      string         `json:"session_id"`
	PlacementID    string         `json:"placement_id,omitempty"`
	Phase          ExecutionPhase `json:"phase"`
	CreatedAt      time.Time      `json:"created_at"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	EndedAt        *time.Time     `json:"ended_at,omitempty"`
	EvidenceSetIDs []string       `json:"evidence_set_ids,omitempty"`
}

type PublicTransition struct {
	TransitionID string    `json:"transition_id"`
	From         *string   `json:"from"`
	To           string    `json:"to"`
	OccurredAt   time.Time `json:"occurred_at"`
	Source       string    `json:"source"`
	Reason       string    `json:"reason"`
}

type PublicIdentity struct {
	SchemaVersion           string         `json:"schema_version"`
	AccountID               string         `json:"account_id"`
	Handle                  string         `json:"handle"`
	DisplayName             string         `json:"display_name,omitempty"`
	ClaimedAt               time.Time      `json:"claimed_at"`
	UpdatedAt               time.Time      `json:"updated_at,omitempty"`
	Status                  IdentityStatus `json:"status"`
	Metadata                map[string]any `json:"metadata,omitempty"`
	PublicDataNoticeVersion string         `json:"public_data_notice_version"`
}

type PublicEnrollment struct {
	ClientID       string          `json:"client_id"`
	AccountID      string          `json:"account_id"`
	ClientClass    ClientClass     `json:"client_class"`
	Phase          EnrollmentPhase `json:"phase"`
	AdapterID      string          `json:"adapter_id"`
	AdapterVersion string          `json:"adapter_version"`
	EnrolledAt     time.Time       `json:"enrolled_at"`
	DepartedAt     *time.Time      `json:"departed_at"`
}

type PublicCapture struct {
	CaptureID        string    `json:"capture_id"`
	SessionID        string    `json:"session_id"`
	ExecutionID      string    `json:"execution_id"`
	ProducerClientID string    `json:"producer_client_id"`
	ProducerClass    string    `json:"producer_class"`
	CaptureMethod    string    `json:"capture_method"`
	AdapterID        string    `json:"adapter_id"`
	AdapterVersion   string    `json:"adapter_version"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
}

type PublicSession struct {
	SchemaVersion           string             `json:"schema_version"`
	SessionID               string             `json:"session_id"`
	ExecutionID             string             `json:"execution_id"`
	PlacementID             string             `json:"placement_id,omitempty"`
	CreatedByAccountID      string             `json:"created_by_account_id"`
	CreatedAt               time.Time          `json:"created_at"`
	UpdatedAt               time.Time          `json:"updated_at,omitempty"`
	Phase                   SessionPhase       `json:"phase"`
	Compatibility           Compatibility      `json:"compatibility"`
	ParticipantPolicy       ParticipantPolicy  `json:"participant_policy"`
	CapturePolicy           CapturePolicy      `json:"capture_policy"`
	Placement               *PublicPlacement   `json:"placement,omitempty"`
	Enrollments             []PublicEnrollment `json:"enrollments"`
	CaptureIDs              []string           `json:"capture_ids,omitempty"`
	Transitions             []PublicTransition `json:"transitions"`
	PublicDataNoticeVersion string             `json:"public_data_notice_version"`
}

type CreateIdentityRequest struct {
	Handle      string         `json:"handle"`
	DisplayName string         `json:"display_name,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type CreateIdentityResponse struct {
	PublicIdentity PublicIdentity `json:"public_identity"`
	AccountToken   string         `json:"account_token"`
	Recovery       string         `json:"recovery"`
}

type CreateSessionRequest struct {
	Compatibility     Compatibility     `json:"compatibility"`
	ParticipantPolicy ParticipantPolicy `json:"participant_policy"`
	Placement         PlacementIntent   `json:"placement"`
	Capture           CapturePolicy     `json:"capture"`
}

type CreateSessionResponse struct {
	PublicSession         PublicSession `json:"public_session"`
	SessionJoinCredential string        `json:"session_join_credential"`
	ExpiresAt             time.Time     `json:"expires_at"`
}

type EnrollmentRequest struct {
	ClientInstanceID string        `json:"client_instance_id"`
	ClientClass      ClientClass   `json:"client_class"`
	Adapter          AdapterRef    `json:"adapter"`
	Compatibility    ClientHashes  `json:"compatibility"`
	RegionProbes     []RegionProbe `json:"region_probes,omitempty"`
}

type AdapterRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ClientHashes struct {
	GameHash string `json:"game_hash"`
	ModHash  string `json:"mod_hash"`
	MapHash  string `json:"map_hash"`
}

type RegionProbe struct {
	Region string `json:"region"`
	RTTMS  int    `json:"rtt_ms"`
}

type EnrollmentCreateResponse struct {
	PublicEnrollment    PublicEnrollment `json:"public_enrollment"`
	ClientLeaseToken    string           `json:"client_lease_token"`
	TransportCredential string           `json:"transport_credential"`
	ExpiresAt           time.Time        `json:"expires_at"`
}

type LifecycleReportRequest struct {
	ReportID string `json:"report_id"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason,omitempty"`
}

type LifecycleReportResponse struct {
	PublicSession    PublicSession    `json:"public_session"`
	PublicEnrollment PublicEnrollment `json:"public_enrollment"`
}

type HeartbeatResponse struct {
	ClientID  string    `json:"client_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ReconcileEvidenceRequest struct {
	Method       evidencev1.Method               `json:"method"`
	Observations []evidencev1.ObservationSummary `json:"observations"`
}

type ErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type identityRecord struct {
	PublicIdentity
	tokenVerifier []byte
}

type sessionRecord struct {
	PublicSession
	joinVerifier      []byte
	expiresAt         time.Time
	creatorID         string
	executionID       string
	placementID       string
	placementIntent   PlacementIntent
	enrollments       map[string]*enrollmentRecord
	createRequestHash string
}

type enrollmentRecord struct {
	PublicEnrollment
	sessionID         string
	clientInstanceID  string
	leaseVerifier     []byte
	transportVerifier []byte
	expiresAt         time.Time
	reportIDs         map[string]string
	requestHash       string
}
