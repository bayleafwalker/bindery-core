package externalruntime

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

// replayNormalizer is a second version of the shipped gap normalizer, used to
// prove that a new version derives a new dataset rather than replacing one.
type replayNormalizer struct{}

func (replayNormalizer) ID() string      { return "bindery.capture-gap" }
func (replayNormalizer) Version() string { return "2" }

func (n replayNormalizer) Normalize(input capture.Input) ([]capture.DerivedEvent, error) {
	derived, err := capture.Normalizer(capture.NormalizerV1()).Normalize(input)
	if err != nil {
		return nil, err
	}
	for index := range derived {
		derived[index].Derivation.NormalizerVersion = "2"
		derived[index].Event.CaptureMethod = "normalizer/bindery.capture-gap@2"
	}
	return derived, nil
}

func init() { capture.Register(replayNormalizer{}) }

func gappedClosedCapture(t *testing.T, service *Service, name string) (captureFixture, string) {
	t.Helper()
	fixture := newCaptureFixture(t, service, name)
	mustIngest(t, service, fixture.playerA, 0, 1)
	mustIngest(t, service, fixture.playerA, 3, 4)
	if _, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, CaptureCloseRequest{
		FinalSequence: 4, ObservedGaps: [][2]uint64{{2, 2}}, LocalDrops: 1, EndReason: "client-exit",
	}); err != nil {
		t.Fatal(err)
	}
	return fixture, fixture.playerA.capture
}

func TestNormalizationPublishesDerivedFactsWithProvenance(t *testing.T) {
	service := NewService()
	fixture, sourceID := gappedClosedCapture(t, service, "normalize")

	derived, err := service.NormalizeCapture(fixture.identity.AccountToken, sourceID, NormalizeRequest{NormalizerID: "bindery.capture-gap", NormalizerVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if derived.ProducerClass != ProducerNormalizer || derived.DerivedFromCaptureID != sourceID {
		t.Fatalf("derived capture = %+v", derived)
	}
	// The derivation stays attributed to the client whose observations it came
	// from, so the referential graph does not sprout a synthetic participant.
	if derived.ProducerClientID != fixture.playerA.id {
		t.Fatalf("derived producer = %q", derived.ProducerClientID)
	}
	if derived.Normalizer == nil || derived.Normalizer.Version != "1" {
		t.Fatalf("derived normalizer = %+v", derived.Normalizer)
	}

	page, err := service.ReadCaptureEvents(derived.CaptureID, "", EventKindDerived, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("derived events = %d, want one gap", len(page.Events))
	}
	event := page.Events[0]
	if event.EventType != capture.EventCaptureGap {
		t.Fatalf("derived event type = %q", event.EventType)
	}
	if event.Derivation == nil || event.Derivation.NormalizerID != "bindery.capture-gap" || len(event.Derivation.SourceEventIDs) == 0 {
		t.Fatalf("derived event has no provenance: %+v", event.Derivation)
	}
	var payload struct {
		FirstMissing uint64 `json:"first_missing_sequence"`
		LastMissing  uint64 `json:"last_missing_sequence"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.FirstMissing != 2 || payload.LastMissing != 2 {
		t.Fatalf("derived gap = %+v", payload)
	}

	// The raw stream is untouched and still reads as observations.
	raw, err := service.ReadCaptureEvents(sourceID, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Events) != 4 {
		t.Fatalf("raw events after normalization = %d", len(raw.Events))
	}
	for _, observation := range raw.Events {
		if observation.Derivation != nil {
			t.Fatal("an observation came back carrying a derivation")
		}
	}
	source, err := service.GetCapture(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Completeness.DerivationIDs) != 1 || source.Completeness.DerivationIDs[0] != derived.CaptureID {
		t.Fatalf("source manifest does not link its derivation: %+v", source.Completeness.DerivationIDs)
	}
}

func TestReplayingTheSameVersionIsANoOpAndANewVersionIsAdditive(t *testing.T) {
	service := NewService()
	fixture, sourceID := gappedClosedCapture(t, service, "replay-normalize")
	request := NormalizeRequest{NormalizerID: "bindery.capture-gap", NormalizerVersion: "1"}

	first, err := service.NormalizeCapture(fixture.identity.AccountToken, sourceID, request)
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.NormalizeCapture(fixture.identity.AccountToken, sourceID, request)
	if err != nil {
		t.Fatal(err)
	}
	if again.CaptureID != first.CaptureID {
		t.Fatal("replaying the same normalizer version created a second dataset")
	}

	second, err := service.NormalizeCapture(fixture.identity.AccountToken, sourceID, NormalizeRequest{NormalizerID: "bindery.capture-gap", NormalizerVersion: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if second.CaptureID == first.CaptureID {
		t.Fatal("a new normalizer version overwrote the old derivation")
	}
	// Both derivations coexist, and so does the raw stream they came from.
	if _, err := service.GetCapture(first.CaptureID); err != nil {
		t.Fatalf("the v1 derivation was lost: %v", err)
	}
	source, err := service.GetCapture(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Completeness.DerivationIDs) != 2 {
		t.Fatalf("source derivations = %v", source.Completeness.DerivationIDs)
	}
}

func TestDerivationsAreNotIndependentObservations(t *testing.T) {
	// Reconciling a stream against a function of itself would manufacture
	// agreement, which is the opposite of what evidence is for.
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	owner := mustIdentity(t, service, "derived-evidence")
	created, err := service.CreateSession(owner.AccountToken, "derived-evidence-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "derived-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "derived-b", ClientPlayer)
	ingestRange(t, service, a, 0, 1, 10)
	ingestRange(t, service, a, 3, 3, 10)
	if _, err := service.CloseCapture(a.lease, a.capture, CaptureCloseRequest{FinalSequence: 3, ObservedGaps: [][2]uint64{{2, 2}}, EndReason: "client-exit"}); err != nil {
		t.Fatal(err)
	}
	ingestRange(t, service, b, 0, 3, 10)
	if _, err := service.CloseCapture(b.lease, b.capture, CaptureCloseRequest{FinalSequence: 3, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}

	derived, err := service.NormalizeCapture(owner.AccountToken, a.capture, NormalizeRequest{NormalizerID: "bindery.capture-gap", NormalizerVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "derived-compare", ReconcileEvidenceRequest{
		Method: "exact-count", CaptureIDs: []string{a.capture, derived.CaptureID},
	}); !hasCode(err, "CAPTURE_IS_DERIVED") {
		t.Fatalf("comparing a stream against its own derivation error = %v", err)
	}
}

func TestNormalizationIsRefusedForOpenStreamsUnknownVersionsAndNonCreators(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "normalize-refusals")
	mustIngest(t, service, fixture.playerA, 0, 1)

	if _, err := service.NormalizeCapture(fixture.identity.AccountToken, fixture.playerA.capture, NormalizeRequest{NormalizerID: "bindery.capture-gap", NormalizerVersion: "1"}); !hasCode(err, "CAPTURE_NOT_CLOSED") {
		t.Fatalf("open stream error = %v", err)
	}
	if _, err := service.NormalizeCapture(fixture.identity.AccountToken, fixture.playerA.capture, NormalizeRequest{NormalizerID: "bindery.capture-gap", NormalizerVersion: "99"}); !hasCode(err, "NORMALIZER_UNKNOWN") {
		t.Fatalf("unknown version error = %v", err)
	}
	outsider := mustIdentity(t, service, "normalize-outsider")
	if _, err := service.NormalizeCapture(outsider.AccountToken, fixture.playerA.capture, NormalizeRequest{NormalizerID: "bindery.capture-gap", NormalizerVersion: "1"}); !hasCode(err, "TOKEN_INVALID") {
		t.Fatalf("non-creator error = %v", err)
	}
}

func TestNormalizeOverHTTP(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	fixture, sourceID := gappedClosedCapture(t, service, "http-normalize")

	request := httptestRequest(http.MethodPost, "/v1/captures/"+sourceID+":normalize", `{"normalizer_id":"bindery.capture-gap","normalizer_version":"1"}`)
	request.Header.Set("Authorization", "Bearer "+fixture.identity.AccountToken)
	response := serve(handler, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("normalize status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := ScanPublicOutput(response.Body.Bytes(), fixture.identity.AccountToken, fixture.playerA.lease); err != nil {
		t.Fatalf("derived capture leaked material: %v", err)
	}

	bad := httptestRequest(http.MethodGet, "/v1/captures/"+sourceID+"/events?kind=sideways", "")
	if code := serve(handler, bad).Code; code != http.StatusBadRequest {
		t.Fatalf("unknown event kind status = %d", code)
	}
}
