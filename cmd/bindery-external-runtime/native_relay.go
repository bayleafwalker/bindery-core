package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
	"github.com/bayleafwalker/bindery-core/internal/relay"
	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

// nativeRelay runs Bindery's own relay inside the control plane process and
// closes the loop that env-configured deployments leave open: a session
// created here allocates on this relay, and the clients that enroll into that
// session are admitted to it as they arrive.
//
// Running it in-process is what makes the transport key transferable at all.
// Enrollment mints the key, hands it to the client once and keeps only a
// sha256 verifier, so the key can be given to the relay only on the enrollment
// call itself. Across a process boundary that hand-off needs an authenticated,
// encrypted admin channel; in-process it is a function call.
type nativeRelay struct {
	service *relay.Relay
	logger  *slog.Logger
	lease   time.Duration

	// endpoints maps a client id to the address it was last seen sending
	// authenticated traffic from. The control plane never learns where a
	// client will be, so the address cannot be configured ahead of time the
	// way cmd/bindery-udp-relay does; it is learned from the traffic itself.
	mu        sync.RWMutex
	endpoints map[string]*net.UDPAddr
}

// errUnknownEndpoint marks a drop caused by a recipient that has not yet
// spoken. It is distinguished from every other delivery failure because it is
// the expected state at the start of a match, not a fault.
var errUnknownEndpoint = errors.New("recipient endpoint has not been learned yet")

// deliveryError wraps a failure raised by the delivery callback, which is the
// only part of forwarding that runs after the relay has authenticated the
// sender. Endpoint learning keys off that distinction: an address may be
// trusted once the packet from it verified against the sender's key, and never
// before, or a forged header would let anyone rebind a victim's endpoint.
type deliveryError struct{ err error }

func (e deliveryError) Error() string { return e.err.Error() }
func (e deliveryError) Unwrap() error { return e.err }

func newNativeRelay(config allocatorConfig, logger *slog.Logger) *nativeRelay {
	relayConfig := relay.Config{
		DatagramLimit:    envInt("BINDERY_RELAY_DATAGRAM_LIMIT", relayv1.DefaultDatagramLimit),
		PacketsPerSecond: envInt("BINDERY_RELAY_PACKETS_PER_SECOND", 1000),
		BytesPerSecond:   envInt("BINDERY_RELAY_BYTES_PER_SECOND", 1<<20),
	}
	return &nativeRelay{
		service:   relay.New(relayConfig),
		logger:    logger,
		lease:     time.Duration(envInt("BINDERY_RELAY_LEASE_SECONDS", 86400)) * time.Second,
		endpoints: make(map[string]*net.UDPAddr),
	}
}

func (n *nativeRelay) start() error { return n.service.Start() }

// admitter is the externalruntime seam implementation. Lease policy lives here
// rather than in the control plane, which knows when a client arrived but not
// how long this deployment intends to carry it.
func (n *nativeRelay) admitter() externalruntime.RelayAdmitter {
	return func(admission externalruntime.RelayAdmission) error {
		err := n.service.AdmitClient(
			admission.RelayAllocationID,
			admission.ClientID,
			admission.TransportKey,
			admission.AdmittedAt.Add(n.lease),
		)
		if err != nil {
			n.logger.Error("relay admission failed",
				"allocation_id", admission.RelayAllocationID,
				"client_id", admission.ClientID,
				"error", err)
			return err
		}
		n.logger.Info("relay admitted client",
			"allocation_id", admission.RelayAllocationID,
			"client_id", admission.ClientID,
			"client_class", string(admission.ClientClass),
			"session_id", admission.SessionID)
		return nil
	}
}

// serve runs the datagram loop. It returns only when the socket closes, so
// callers run it on its own goroutine.
func (n *nativeRelay) serve(connection *net.UDPConn, limit int) {
	buffer := make([]byte, 64*1024)
	for {
		length, source, readErr := connection.ReadFromUDP(buffer)
		if readErr != nil {
			var netErr net.Error
			if errors.As(readErr, &netErr) && netErr.Timeout() {
				continue
			}
			if errors.Is(readErr, net.ErrClosed) {
				return
			}
			n.logger.Error("udp read failed", "error", readErr)
			return
		}
		datagram := append([]byte(nil), buffer[:length]...)
		header, peekErr := relayv1.Peek(datagram, limit)
		if peekErr != nil {
			continue
		}
		err := n.service.Forward(datagram, header.SenderID, time.Now().UTC(), func(recipientID string, output []byte) error {
			endpoint, ok := n.endpointFor(recipientID)
			if !ok {
				return deliveryError{err: errUnknownEndpoint}
			}
			if _, writeErr := connection.WriteToUDP(output, endpoint); writeErr != nil {
				return deliveryError{err: writeErr}
			}
			return nil
		})
		// Learn only from packets the relay authenticated. err == nil proves
		// that; so does a delivery failure, which can only be reached after
		// the sender's key verified. Every other error is pre-authentication.
		var delivery deliveryError
		if err == nil || errors.As(err, &delivery) {
			n.learn(header.SenderID, source)
		}
	}
}

func (n *nativeRelay) endpointFor(clientID string) (*net.UDPAddr, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	endpoint, ok := n.endpoints[clientID]
	return endpoint, ok
}

func (n *nativeRelay) learn(clientID string, source *net.UDPAddr) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if existing, ok := n.endpoints[clientID]; ok && existing.String() == source.String() {
		return
	}
	n.endpoints[clientID] = source
	n.logger.Info("relay learned client endpoint", "client_id", clientID, "endpoint", source.String())
}

// listenAddress resolves the address to bind. BINDERY_RELAY_ADDR wins when it
// is set; otherwise the relay binds every interface on the port the placement
// record advertises, so what clients are told and what is listening cannot
// drift apart by default.
func listenAddress(endpoint string) (*net.UDPAddr, error) {
	if override := strings.TrimSpace(os.Getenv("BINDERY_RELAY_ADDR")); override != "" {
		return net.ResolveUDPAddr("udp", override)
	}
	_, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, fmt.Errorf("relay endpoint %q is not host:port: %w", endpoint, err)
	}
	return net.ResolveUDPAddr("udp", ":"+port)
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
