package externalruntime

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

func admittingService(t *testing.T, admitter RelayAdmitter) *Service {
	t.Helper()
	service := NewServiceWithPlacementAllocator(func(PlacementIntent) (PublicPlacement, error) {
		return *fixtureRelayPlacement(), nil
	})
	service.SetRelayAdmitter(admitter)
	return service
}

// A client whose relay would not admit it must not receive a lease. The
// alternative is a client holding valid control-plane credentials for a relay
// that will silently drop everything it sends.
func TestEnrollmentFailsWhenTheRelayRefusesTheClient(t *testing.T) {
	service := admittingService(t, func(RelayAdmission) error { return errors.New("relay is draining") })
	identity := mustIdentity(t, service, "refused-host")
	created, err := service.CreateSession(identity.AccountToken, "refused-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Enroll(identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "enroll-refused", EnrollmentRequest{
		ClientInstanceID: "refused-client",
		ClientClass:      ClientPlayer,
		Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility:    ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
	})
	if !hasCode(err, "RELAY_ADMISSION_FAILED") {
		t.Fatalf("error = %v, want RELAY_ADMISSION_FAILED", err)
	}
}

// The key handed to the relay must be the one the client will sign with, and
// the placement it is admitted against must be the session's own.
func TestAdmissionCarriesTheClientsOwnKeyAndPlacement(t *testing.T) {
	var seen []RelayAdmission
	service := admittingService(t, func(admission RelayAdmission) error {
		seen = append(seen, admission)
		return nil
	})
	identity := mustIdentity(t, service, "admitting-host")
	created, err := service.CreateSession(identity.AccountToken, "admitting-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	response, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, sessionID, "enroll-admitted", EnrollmentRequest{
		ClientInstanceID: "admitted-client",
		ClientClass:      ClientPlayer,
		Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility:    ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("admissions = %d, want 1", len(seen))
	}
	admission := seen[0]
	if admission.ClientID != response.PublicEnrollment.ClientID {
		t.Fatalf("client = %q, want %q", admission.ClientID, response.PublicEnrollment.ClientID)
	}
	if admission.SessionID != sessionID {
		t.Fatalf("session = %q, want %q", admission.SessionID, sessionID)
	}
	if admission.RelayAllocationID != created.PublicSession.Placement.RelayAllocationID {
		t.Fatalf("allocation = %q, want the session's", admission.RelayAllocationID)
	}
	if len(admission.TransportKey) != relayv1.TransportKeyBytes {
		t.Fatalf("key length = %d, want %d", len(admission.TransportKey), relayv1.TransportKeyBytes)
	}
	expected, err := base64.RawURLEncoding.DecodeString(response.TransportCredential)
	if err != nil {
		t.Fatal(err)
	}
	if string(admission.TransportKey) != string(expected) {
		t.Fatal("the relay was given a key the client will not sign with")
	}
}

// A service with no admitter must still enroll: not every deployment runs the
// native relay, and cncnet-private is configured out of band.
func TestEnrollmentSucceedsWithoutAnAdmitter(t *testing.T) {
	service := NewServiceWithPlacementAllocator(func(PlacementIntent) (PublicPlacement, error) {
		return *fixtureRelayPlacement(), nil
	})
	identity := mustIdentity(t, service, "unadmitted-host")
	created, err := service.CreateSession(identity.AccountToken, "unadmitted-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "enroll-unadmitted", EnrollmentRequest{
		ClientInstanceID: "unadmitted-client",
		ClientClass:      ClientPlayer,
		Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility:    ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
	}); err != nil {
		t.Fatalf("enroll without admitter: %v", err)
	}
}
