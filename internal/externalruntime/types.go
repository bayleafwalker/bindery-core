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

// Compatibility identifies what the participants must agree on to play
// together. Game and adapter identity are universal; mod and map are not.
//
// A runtime whose world is generated rather than shipped, or which has no mod
// concept at all, supplies neither pair, and omitting them is not a degraded
// session -- it is an accurate description of one. Requiring them forced such
// a runtime to invent hashes to satisfy a validator, which is the opposite of
// what a compatibility check is for. The pairs are all-or-nothing: an id
// without its hash names content nobody can verify.
type Compatibility struct {
	GameFamily     string `json:"game_family"`
	GameVersion    string `json:"game_version"`
	GameHash       string `json:"game_hash"`
	AdapterID      string `json:"adapter_id"`
	AdapterVersion string `json:"adapter_version"`
	ModID          string `json:"mod_id,omitempty"`
	ModHash        string `json:"mod_hash,omitempty"`
	MapID          string `json:"map_id,omitempty"`
	MapHash        string `json:"map_hash,omitempty"`
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

// RelayAdmission carries one enrolled client to the relay that the session's
// placement already named. TransportKey is the raw key the client will sign
// datagrams with, and it is passed here because this is the only moment it
// exists: enrollment stores a sha256 verifier and discards the key bytes, so
// an admission that is deferred can never be completed.
//
// The admitter owns lease policy. The control plane knows when a client
// arrived, not how long the deployment intends to carry it.
type RelayAdmission struct {
	SessionID         string
	PlacementID       string
	RelayAllocationID string
	ClientID          string
	ClientClass       ClientClass
	TransportKey      []byte
	AdmittedAt        time.Time
}

// RelayAdmitter is the seam between enrollment and the relay's allocation
// table. It is the counterpart to PlacementAllocator: the allocator decides
// where a session will meet, and the admitter makes that relay willing to
// carry the clients when they arrive. Without it a placement names a tunnel
// that will reject every packet sent to it.
type RelayAdmitter func(RelayAdmission) error

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
	CaptureID        string               `json:"capture_id"`
	SessionID        string               `json:"session_id"`
	ExecutionID      string               `json:"execution_id"`
	ProducerClientID string               `json:"producer_client_id"`
	ProducerClass    string               `json:"producer_class"`
	CaptureMethod    string               `json:"capture_method"`
	AdapterID        string               `json:"adapter_id"`
	AdapterVersion   string               `json:"adapter_version"`
	Status           string               `json:"status"`
	CreatedAt        time.Time            `json:"created_at"`
	ClosedAt         *time.Time           `json:"closed_at,omitempty"`
	Objects          []string             `json:"objects,omitempty"`
	Completeness     *CaptureCompleteness `json:"completeness,omitempty"`
	// DerivedFromCaptureID and Normalizer are set only on derived captures.
	// A derived stream is published as a capture in its own right so that it
	// is readable, attributable, and impossible to mistake for an observation.
	DerivedFromCaptureID string         `json:"derived_from_capture_id,omitempty"`
	Normalizer           *NormalizerRef `json:"normalizer,omitempty"`
}

type NormalizerRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// CaptureCompleteness is the answer to "how much of this stream do we
// actually have", published instead of an unexplained boolean. Observed ranges
// are the broker's own count; expected_through and local_drops are the
// producer's claim at close. Both are retained so a disagreement stays visible.
type CaptureCompleteness struct {
	ExpectedThrough *uint64     `json:"expected_through,omitempty"`
	ObservedRanges  [][2]uint64 `json:"observed_ranges"`
	MissingRanges   [][2]uint64 `json:"missing_ranges"`
	EventCount      uint64      `json:"event_count"`
	LocalDrops      uint64      `json:"local_drops"`
	RawObjectHashes []string    `json:"raw_object_hashes"`
	Closed          bool        `json:"closed"`
	EndReason       string      `json:"end_reason,omitempty"`
	// DerivationIDs names the derived captures produced from this stream, so a
	// reader following the raw record can find every interpretation of it.
	DerivationIDs  []string `json:"derivation_ids,omitempty"`
	SourceCoverage string   `json:"source_coverage,omitempty"`
	ClockQuality   string   `json:"clock_quality,omitempty"`
}

// CaptureStreamOffer is handed to a client at enrollment. It carries limits
// and identity, never a location: a field naming where to send bytes would be
// an operational endpoint in a public DTO, which ScanPublicOutput rejects on
// sight and rightly so. The route is in the contract, not in the response.
type CaptureStreamOffer struct {
	SchemaVersion  string      `json:"schema_version"`
	CaptureID      string      `json:"capture_id"`
	ProducerClass  ClientClass `json:"producer_class"`
	CaptureMethod  string      `json:"capture_method"`
	MaxBatchBytes  int64       `json:"max_batch_bytes"`
	MaxBatchEvents int         `json:"max_batch_events"`
	MaxObjectBytes int64       `json:"max_object_bytes"`
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
	CaptureMethod    string        `json:"capture_method,omitempty"`
	Adapter          AdapterRef    `json:"adapter"`
	Compatibility    ClientHashes  `json:"compatibility"`
	RegionProbes     []RegionProbe `json:"region_probes,omitempty"`
}

type AdapterRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// ClientHashes is a client's account of the content it loaded. It mirrors
// Compatibility: a client of a runtime with no mod or map sends neither, and
// the broker requires exactly the pairs its session declared.
type ClientHashes struct {
	GameHash string `json:"game_hash"`
	ModHash  string `json:"mod_hash,omitempty"`
	MapHash  string `json:"map_hash,omitempty"`
}

type RegionProbe struct {
	Region string `json:"region"`
	RTTMS  int    `json:"rtt_ms"`
}

type EnrollmentCreateResponse struct {
	PublicEnrollment    PublicEnrollment     `json:"public_enrollment"`
	ClientLeaseToken    string               `json:"client_lease_token"`
	TransportCredential string               `json:"transport_credential"`
	ExpiresAt           time.Time            `json:"expires_at"`
	CaptureStreamOffers []CaptureStreamOffer `json:"capture_stream_offers,omitempty"`
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
	Method evidencev1.Method `json:"method"`
	// CaptureIDs selects which captured streams to compare. Empty means every
	// capture on the execution.
	CaptureIDs []string `json:"capture_ids,omitempty"`
	// Observations is the pre-capture-plane path, kept only for executions
	// that have no captured streams at all. Supplying it for an execution that
	// does have them is refused rather than merged: the two are not
	// interchangeable, because only one of them is independent of the client.
	Observations []evidencev1.ObservationSummary `json:"observations,omitempty"`
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
