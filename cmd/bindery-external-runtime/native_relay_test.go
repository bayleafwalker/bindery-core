package main

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

const (
	testHashA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testHashB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// nativeRelayHarness is a control plane wired to its own relay, listening on a
// kernel-assigned port so the test never races another process for one.
type nativeRelayHarness struct {
	service  *externalruntime.Service
	endpoint *net.UDPAddr
}

func startNativeRelayHarness(t *testing.T) nativeRelayHarness {
	t.Helper()
	t.Setenv("BINDERY_BUILD_REVISION", testBuildRevision)
	t.Setenv("BINDERY_RELAY_PROVIDER", binderyNativeProviderID)
	t.Setenv("BINDERY_RELAY_ENDPOINT", "127.0.0.1:40000")
	t.Setenv("BINDERY_RELAY_ADDR", "127.0.0.1:0")

	config, err := allocatorConfigFromEnv()
	if err != nil {
		t.Fatalf("allocator config: %v", err)
	}
	native := newNativeRelay(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := native.start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	bind, err := listenAddress(config.Endpoint)
	if err != nil {
		t.Fatalf("listen address: %v", err)
	}
	connection, err := net.ListenUDP("udp", bind)
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	t.Cleanup(func() { connection.Close() })

	service := externalruntime.NewServiceWithPlacementAllocator(newRelayAllocator(config))
	service.SetRelayAdmitter(native.admitter())
	go native.serve(connection, relayv1.DefaultDatagramLimit)

	return nativeRelayHarness{service: service, endpoint: connection.LocalAddr().(*net.UDPAddr)}
}

// relayClient is one enrolled client holding the transport key the control
// plane issued it, speaking to the relay over a real socket.
type relayClient struct {
	id           string
	key          []byte
	allocationID string
	socket       *net.UDPConn
	sequence     uint64
}

func enrollClient(t *testing.T, harness nativeRelayHarness, accountToken, join, sessionID, instance, allocationID string) *relayClient {
	t.Helper()
	response, err := harness.service.Enroll(accountToken, join, sessionID, "enroll-"+instance, externalruntime.EnrollmentRequest{
		ClientInstanceID: instance,
		ClientClass:      externalruntime.ClientPlayer,
		Adapter:          externalruntime.AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility:    externalruntime.ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
	})
	if err != nil {
		t.Fatalf("enroll %s: %v", instance, err)
	}
	// The relay signs with the raw bytes behind the credential, not the text.
	key, err := base64.RawURLEncoding.DecodeString(response.TransportCredential)
	if err != nil {
		t.Fatalf("decode transport credential: %v", err)
	}
	socket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("client socket: %v", err)
	}
	t.Cleanup(func() { socket.Close() })
	return &relayClient{id: response.PublicEnrollment.ClientID, key: key, allocationID: allocationID, socket: socket}
}

func (c *relayClient) send(t *testing.T, to *relayClient, target *net.UDPAddr, payload string) {
	t.Helper()
	c.sequence++
	datagram, err := relayv1.Encode(relayv1.Packet{
		Type:         relayv1.PacketData,
		AllocationID: c.allocationID,
		SenderID:     c.id,
		RecipientID:  to.id,
		Sequence:     c.sequence,
		Payload:      []byte(payload),
	}, c.key, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := c.socket.WriteToUDP(datagram, target); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func (c *relayClient) receive(t *testing.T) relayv1.Packet {
	t.Helper()
	if err := c.socket.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	buffer := make([]byte, 64*1024)
	length, _, err := c.socket.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	packet, err := relayv1.Decode(buffer[:length], c.key, relayv1.DefaultDatagramLimit)
	if err != nil {
		t.Fatalf("decode delivered datagram: %v", err)
	}
	return packet
}

// TestNativeRelayCarriesASessionCreatedByTheControlPlane is the whole point of
// the native provider: a session created through the control plane results in
// Bindery's own relay carrying traffic between the clients that enrolled into
// it, using credentials that control plane issued and no configuration handed
// to the relay out of band.
func TestNativeRelayCarriesASessionCreatedByTheControlPlane(t *testing.T) {
	harness := startNativeRelayHarness(t)

	identity, err := harness.service.CreateIdentity(externalruntime.CreateIdentityRequest{Handle: "native-relay-host"}, "create-native-host")
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	created, err := harness.service.CreateSession(identity.AccountToken, "native-relay-session", externalruntime.CreateSessionRequest{
		Compatibility:     externalruntime.Compatibility{GameFamily: "ra2-yr", GameVersion: "1.001", GameHash: testHashA, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", ModID: "vanilla-yr", ModHash: testHashA, MapID: "official:sample", MapHash: testHashB},
		ParticipantPolicy: externalruntime.ParticipantPolicy{RequiredPlayers: 2, MaximumPlayers: 2, MaximumObservers: 1},
		Placement:         externalruntime.PlacementIntent{AllowedRegions: []string{"eu-north"}, LatencyP95MS: 100},
		Capture:           externalruntime.CapturePolicy{SemanticEvents: true},
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	placement := created.PublicSession.Placement
	if placement == nil {
		t.Fatal("session was created without a placement")
	}
	if placement.RelayProviderID != binderyNativeProviderID {
		t.Fatalf("provider = %q, want %q", placement.RelayProviderID, binderyNativeProviderID)
	}

	sessionID := created.PublicSession.SessionID
	a := enrollClient(t, harness, identity.AccountToken, created.SessionJoinCredential, sessionID, "client-a", placement.RelayAllocationID)
	b := enrollClient(t, harness, identity.AccountToken, created.SessionJoinCredential, sessionID, "client-b", placement.RelayAllocationID)

	// The relay learns a client's address from its first authenticated packet,
	// so each direction opens only after that client has spoken once. A's
	// first packet is expected to drop: B's address is not yet known.
	a.send(t, b, harness.endpoint, "priming")

	b.send(t, a, harness.endpoint, "from-b")
	if got := string(a.receive(t).Payload); got != "from-b" {
		t.Fatalf("A received %q, want %q", got, "from-b")
	}

	a.send(t, b, harness.endpoint, "from-a")
	if got := string(b.receive(t).Payload); got != "from-a" {
		t.Fatalf("B received %q, want %q", got, "from-a")
	}
}
