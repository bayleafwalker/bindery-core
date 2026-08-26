package relay

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

type State string

const (
	Starting    State = "Starting"
	Accepting   State = "Accepting"
	Draining    State = "Draining"
	Empty       State = "Empty"
	Terminating State = "Terminating"
	Failed      State = "Failed"
)

var (
	ErrNotAccepting       = errors.New("relay is not accepting new allocations")
	ErrRelayUnavailable   = errors.New("relay is unavailable")
	ErrAllocationNotFound = errors.New("relay allocation was not found")
	ErrUnauthorizedSource = errors.New("source is not registered for allocation")
	ErrRecipientNotFound  = errors.New("recipient is not registered for allocation")
	ErrReplay             = errors.New("packet sequence was already observed")
	ErrRateLimited        = errors.New("packet or byte rate limit exceeded")
	ErrLeaseExpired       = errors.New("relay allocation lease expired")
	ErrNoRelayCapacity    = errors.New("no relay satisfies placement and capacity policy")
)

type Config struct {
	DatagramLimit    int
	PacketsPerSecond int
	BytesPerSecond   int
	TelemetryBuffer  int
}

func (c Config) withDefaults() Config {
	if c.DatagramLimit <= 0 {
		c.DatagramLimit = relayv1.DefaultDatagramLimit
	}
	if c.PacketsPerSecond <= 0 {
		c.PacketsPerSecond = 1000
	}
	if c.BytesPerSecond <= 0 {
		c.BytesPerSecond = 1 << 20
	}
	if c.TelemetryBuffer <= 0 {
		c.TelemetryBuffer = 256
	}
	return c
}

type TelemetryEvent struct {
	Kind         string
	AllocationID string
	SenderID     string
	RecipientID  string
	Bytes        int
	OccurredAt   time.Time
	Reason       string
}

type MetricsSnapshot struct {
	PacketsForwarded uint64
	BytesForwarded   uint64
	PacketsDropped   uint64
	Unauthorized     uint64
	Malformed        uint64
	Oversized        uint64
	RateLimited      uint64
	ReplayRejected   uint64
	LeaseExpired     uint64
	DeliveryFailed   uint64
}

type Metrics struct {
	packetsForwarded atomic.Uint64
	bytesForwarded   atomic.Uint64
	packetsDropped   atomic.Uint64
	unauthorized     atomic.Uint64
	malformed        atomic.Uint64
	oversized        atomic.Uint64
	rateLimited      atomic.Uint64
	replayRejected   atomic.Uint64
	leaseExpired     atomic.Uint64
	deliveryFailed   atomic.Uint64
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{PacketsForwarded: m.packetsForwarded.Load(), BytesForwarded: m.bytesForwarded.Load(), PacketsDropped: m.packetsDropped.Load(), Unauthorized: m.unauthorized.Load(), Malformed: m.malformed.Load(), Oversized: m.oversized.Load(), RateLimited: m.rateLimited.Load(), ReplayRejected: m.replayRejected.Load(), LeaseExpired: m.leaseExpired.Load(), DeliveryFailed: m.deliveryFailed.Load()}
}

type client struct {
	key        []byte
	window     relayv1.ReplayWindow
	packetRate tokenBucket
	byteRate   tokenBucket
}

type allocation struct {
	id         string
	leaseUntil time.Time
	clients    map[string]*client
}

type Relay struct {
	mu          sync.RWMutex
	state       State
	failure     string
	config      Config
	allocations map[string]*allocation
	metrics     Metrics
	telemetry   chan TelemetryEvent
}

func New(config Config) *Relay {
	config = config.withDefaults()
	return &Relay{state: Starting, config: config, allocations: make(map[string]*allocation), telemetry: make(chan TelemetryEvent, config.TelemetryBuffer)}
}

func (r *Relay) State() State                     { r.mu.RLock(); defer r.mu.RUnlock(); return r.state }
func (r *Relay) FailureReason() string            { r.mu.RLock(); defer r.mu.RUnlock(); return r.failure }
func (r *Relay) Metrics() MetricsSnapshot         { return r.metrics.Snapshot() }
func (r *Relay) Telemetry() <-chan TelemetryEvent { return r.telemetry }

func (r *Relay) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Starting {
		return fmt.Errorf("start from %s: %w", r.state, ErrRelayUnavailable)
	}
	r.state = Accepting
	return nil
}

func (r *Relay) BeginDrain() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Accepting {
		return fmt.Errorf("drain from %s: %w", r.state, ErrRelayUnavailable)
	}
	r.state = Draining
	if len(r.allocations) == 0 {
		r.state = Empty
	}
	return nil
}

func (r *Relay) MarkTerminating() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Empty {
		return fmt.Errorf("terminate from %s: relay must be empty", r.state)
	}
	r.state = Terminating
	return nil
}

func (r *Relay) Fail(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failure = reason
	r.state = Failed
	r.emitLocked(TelemetryEvent{Kind: "relay.failed", Reason: reason, OccurredAt: time.Now().UTC()})
}

func (r *Relay) RegisterAllocation(allocationID string, clients map[string][]byte, leaseUntil time.Time) error {
	if allocationID == "" || len(clients) == 0 {
		return errors.New("allocation and clients are required")
	}
	if _, err := relayv1.PeekMustUUID(allocationID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Accepting {
		return ErrNotAccepting
	}
	if leaseUntil.IsZero() {
		return errors.New("allocation lease is required")
	}
	registered := make(map[string]*client, len(clients))
	for id, key := range clients {
		admitted, err := r.newClientLocked(id, key)
		if err != nil {
			return err
		}
		registered[id] = admitted
	}
	r.allocations[allocationID] = &allocation{id: allocationID, leaseUntil: leaseUntil.UTC(), clients: registered}
	return nil
}

// AdmitClient adds one client to an allocation, creating the allocation on
// first admission. A control plane learns its clients one enrollment at a
// time, so it cannot present the whole client set up front the way a
// statically configured relay can; RegisterAllocation replaces the client set
// and would evict everyone already admitted.
//
// Re-admitting an existing client replaces its key and resets its replay
// window and rate buckets, which is what a client that re-enrolled needs.
// leaseUntil only ever extends an existing lease: an allocation must not be
// shortened by a later arrival.
func (r *Relay) AdmitClient(allocationID, clientID string, key []byte, leaseUntil time.Time) error {
	if allocationID == "" || clientID == "" {
		return errors.New("allocation and client are required")
	}
	if _, err := relayv1.PeekMustUUID(allocationID); err != nil {
		return err
	}
	if leaseUntil.IsZero() {
		return errors.New("allocation lease is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Accepting {
		return ErrNotAccepting
	}
	admitted, err := r.newClientLocked(clientID, key)
	if err != nil {
		return err
	}
	existing, ok := r.allocations[allocationID]
	if !ok {
		r.allocations[allocationID] = &allocation{id: allocationID, leaseUntil: leaseUntil.UTC(), clients: map[string]*client{clientID: admitted}}
		return nil
	}
	existing.clients[clientID] = admitted
	if leaseUntil.UTC().After(existing.leaseUntil) {
		existing.leaseUntil = leaseUntil.UTC()
	}
	return nil
}

func (r *Relay) newClientLocked(id string, key []byte) (*client, error) {
	if _, err := relayv1.PeekMustUUID(id); err != nil {
		return nil, fmt.Errorf("client %s: %w", id, err)
	}
	if len(key) != relayv1.TransportKeyBytes {
		return nil, relayv1.ErrInvalidKey
	}
	return &client{key: append([]byte(nil), key...), packetRate: newTokenBucket(r.config.PacketsPerSecond), byteRate: newTokenBucket(r.config.BytesPerSecond)}, nil
}

func (r *Relay) CloseAllocation(allocationID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.allocations[allocationID]; !ok {
		return ErrAllocationNotFound
	}
	delete(r.allocations, allocationID)
	if r.state == Draining && len(r.allocations) == 0 {
		r.state = Empty
	}
	r.emitLocked(TelemetryEvent{Kind: "allocation.closed", AllocationID: allocationID, OccurredAt: now.UTC()})
	return nil
}

// Forward authenticates the source, enforces allocation-local limits and
// re-signs the opaque payload for the recipient. The delivery callback is the
// only network operation supplied by a UDP adapter; it must not call broker or
// telemetry services synchronously.
func (r *Relay) Forward(datagram []byte, sourceID string, now time.Time, deliver func(recipientID string, datagram []byte) error) error {
	if deliver == nil {
		return errors.New("delivery callback is required")
	}
	r.mu.RLock()
	state := r.state
	config := r.config
	if state != Accepting && state != Draining {
		r.mu.RUnlock()
		r.metrics.packetsDropped.Add(1)
		return ErrRelayUnavailable
	}
	header, err := relayv1.Peek(datagram, config.DatagramLimit)
	if err != nil {
		r.mu.RUnlock()
		r.countDecodeError(err)
		return err
	}
	allocation, ok := r.allocations[header.AllocationID]
	if !ok {
		r.mu.RUnlock()
		r.metrics.unauthorized.Add(1)
		r.metrics.packetsDropped.Add(1)
		return ErrAllocationNotFound
	}
	if allocation.leaseUntil.Before(now) {
		r.mu.RUnlock()
		r.metrics.leaseExpired.Add(1)
		r.metrics.packetsDropped.Add(1)
		return ErrLeaseExpired
	}
	source, ok := allocation.clients[sourceID]
	if !ok || sourceID != header.SenderID {
		r.mu.RUnlock()
		r.metrics.unauthorized.Add(1)
		r.metrics.packetsDropped.Add(1)
		return ErrUnauthorizedSource
	}
	recipient, ok := allocation.clients[header.RecipientID]
	if !ok {
		r.mu.RUnlock()
		r.metrics.unauthorized.Add(1)
		r.metrics.packetsDropped.Add(1)
		return ErrRecipientNotFound
	}
	packet, err := relayv1.Decode(datagram, source.key, config.DatagramLimit)
	if err != nil {
		r.mu.RUnlock()
		r.countDecodeError(err)
		return err
	}
	if !source.window.Accept(packet.Sequence) {
		r.mu.RUnlock()
		r.metrics.replayRejected.Add(1)
		r.metrics.packetsDropped.Add(1)
		return ErrReplay
	}
	if !source.packetRate.Allow(now, 1) || !source.byteRate.Allow(now, len(datagram)) {
		r.mu.RUnlock()
		r.metrics.rateLimited.Add(1)
		r.metrics.packetsDropped.Add(1)
		return ErrRateLimited
	}
	r.mu.RUnlock()
	forwarded, err := relayv1.Encode(packet, recipient.key, config.DatagramLimit)
	if err != nil {
		r.metrics.packetsDropped.Add(1)
		return err
	}
	if err := deliver(header.RecipientID, forwarded); err != nil {
		r.metrics.deliveryFailed.Add(1)
		r.metrics.packetsDropped.Add(1)
		r.emit(TelemetryEvent{Kind: "packet.delivery-failed", AllocationID: header.AllocationID, SenderID: header.SenderID, RecipientID: header.RecipientID, Bytes: len(datagram), OccurredAt: now.UTC(), Reason: err.Error()})
		return err
	}
	r.metrics.packetsForwarded.Add(1)
	r.metrics.bytesForwarded.Add(uint64(len(datagram)))
	r.emit(TelemetryEvent{Kind: "packet.forwarded", AllocationID: header.AllocationID, SenderID: header.SenderID, RecipientID: header.RecipientID, Bytes: len(datagram), OccurredAt: now.UTC()})
	return nil
}

func (r *Relay) countDecodeError(err error) {
	r.metrics.packetsDropped.Add(1)
	if errors.Is(err, relayv1.ErrOversized) {
		r.metrics.oversized.Add(1)
	} else {
		r.metrics.malformed.Add(1)
	}
}

func (r *Relay) emit(event TelemetryEvent) {
	select {
	case r.telemetry <- event:
	default:
	}
}
func (r *Relay) emitLocked(event TelemetryEvent) {
	select {
	case r.telemetry <- event:
	default:
	}
}

type tokenBucket struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
}

func newTokenBucket(rate int) tokenBucket {
	return tokenBucket{rate: float64(rate), capacity: float64(rate), tokens: float64(rate)}
}

func (b *tokenBucket) Allow(now time.Time, amount int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last.IsZero() {
		b.last = now
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}
	if float64(amount) > b.tokens {
		return false
	}
	b.tokens -= float64(amount)
	return true
}
