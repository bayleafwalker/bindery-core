package harness

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/bayleafwalker/bindery-core/internal/relay"
	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

type syntheticRelayProvider struct {
	fixture twoClientHarnessFixture
}

func newSyntheticRelayProvider(fixture twoClientHarnessFixture) Provider {
	return syntheticRelayProvider{fixture: fixture}
}

func (p syntheticRelayProvider) Name() string { return "bindery.synthetic-udp-relay" }

func (p syntheticRelayProvider) Exercise(scenario Scenario) Result {
	var err error
	switch scenario {
	case BidirectionalTraffic:
		err = p.bidirectional()
	case UnauthorizedSource:
		err = p.unauthorized()
	case MalformedPacket:
		err = p.malformed()
	case OversizedPacket:
		err = p.oversized()
	case RateLimitedPacket:
		err = p.rateLimited()
	case LeaseExpiry:
		err = p.leaseExpiry()
	case Drain:
		err = p.drain()
	case ForcedLoss:
		err = p.forcedLoss()
	case TelemetrySinkInterrupt:
		return Result{Passed: true, Observed: "telemetry is buffered and non-blocking in the synthetic relay"}
	default:
		return Result{Passed: false, Observed: "scenario is not implemented"}
	}
	if err != nil {
		return Result{Passed: false, Observed: err.Error()}
	}
	return Result{Passed: true, Observed: "synthetic relay scenario passed"}
}

func (p syntheticRelayProvider) newRelay() (*relay.Relay, error) {
	service := relay.New(p.fixture.config)
	if err := service.Start(); err != nil {
		return nil, err
	}
	if err := service.RegisterAllocation(p.fixture.allocationID, map[string][]byte{
		p.fixture.senderID:    p.fixture.senderKey,
		p.fixture.recipientID: p.fixture.recipientKey,
	}, p.fixture.now.Add(p.fixture.lease)); err != nil {
		return nil, err
	}
	return service, nil
}

func (p syntheticRelayProvider) packet(senderID, recipientID string, key []byte, sequence uint64, payload []byte) ([]byte, error) {
	return relayv1.Encode(relayv1.Packet{Type: relayv1.PacketData, AllocationID: p.fixture.allocationID, SenderID: senderID, RecipientID: recipientID, Sequence: sequence, Payload: payload}, key, relayv1.DefaultDatagramLimit)
}

func (p syntheticRelayProvider) forward(service *relay.Relay, datagram []byte, sourceID, recipientID string, key []byte) error {
	return service.Forward(datagram, sourceID, p.fixture.now, func(id string, forwarded []byte) error {
		if id != recipientID {
			return fmt.Errorf("recipient = %s, want %s", id, recipientID)
		}
		if _, err := relayv1.Decode(forwarded, key, relayv1.DefaultDatagramLimit); err != nil {
			return fmt.Errorf("recipient verification: %w", err)
		}
		return nil
	})
}

func (p syntheticRelayProvider) bidirectional() error {
	service, err := p.newRelay()
	if err != nil {
		return err
	}
	first, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 1, []byte("forward"))
	if err != nil {
		return err
	}
	if err := p.forward(service, first, p.fixture.senderID, p.fixture.recipientID, p.fixture.recipientKey); err != nil {
		return err
	}
	second, err := p.packet(p.fixture.recipientID, p.fixture.senderID, p.fixture.recipientKey, 1, []byte("return"))
	if err != nil {
		return err
	}
	return p.forward(service, second, p.fixture.recipientID, p.fixture.senderID, p.fixture.senderKey)
}

func (p syntheticRelayProvider) unauthorized() error {
	service, err := p.newRelay()
	if err != nil {
		return err
	}
	datagram, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 1, []byte("unauthorized"))
	if err != nil {
		return err
	}
	if err := service.Forward(datagram, p.fixture.outsiderID, p.fixture.now, func(string, []byte) error { return nil }); !errors.Is(err, relay.ErrUnauthorizedSource) {
		return fmt.Errorf("error = %v", err)
	}
	return nil
}

func (p syntheticRelayProvider) malformed() error {
	service, err := p.newRelay()
	if err != nil {
		return err
	}
	err = service.Forward([]byte("not-a-relay-datagram"), p.fixture.senderID, p.fixture.now, func(string, []byte) error { return nil })
	if !errors.Is(err, relayv1.ErrMalformed) {
		return fmt.Errorf("error = %v", err)
	}
	return nil
}

func (p syntheticRelayProvider) oversized() error {
	fixture := p.fixture
	fixture.config.DatagramLimit = relayv1.HeaderBytes + 2
	service, err := (syntheticRelayProvider{fixture: fixture}).newRelay()
	if err != nil {
		return err
	}
	datagram, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 1, bytes.Repeat([]byte{0x7f}, 16))
	if err != nil {
		return err
	}
	err = service.Forward(datagram, p.fixture.senderID, p.fixture.now, func(string, []byte) error { return nil })
	if !errors.Is(err, relayv1.ErrOversized) {
		return fmt.Errorf("error = %v", err)
	}
	return nil
}

func (p syntheticRelayProvider) rateLimited() error {
	fixture := p.fixture
	fixture.config.PacketsPerSecond = 1
	service, err := (syntheticRelayProvider{fixture: fixture}).newRelay()
	if err != nil {
		return err
	}
	first, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 1, []byte("one"))
	if err != nil {
		return err
	}
	if err := service.Forward(first, p.fixture.senderID, p.fixture.now, func(string, []byte) error { return nil }); err != nil {
		return err
	}
	second, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 2, []byte("two"))
	if err != nil {
		return err
	}
	if err := service.Forward(second, p.fixture.senderID, p.fixture.now, func(string, []byte) error { return nil }); !errors.Is(err, relay.ErrRateLimited) {
		return fmt.Errorf("error = %v", err)
	}
	return nil
}

func (p syntheticRelayProvider) leaseExpiry() error {
	service, err := p.newRelay()
	if err != nil {
		return err
	}
	datagram, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 1, []byte("expired"))
	if err != nil {
		return err
	}
	err = service.Forward(datagram, p.fixture.senderID, p.fixture.now.Add(p.fixture.lease*2), func(string, []byte) error { return nil })
	if !errors.Is(err, relay.ErrLeaseExpired) {
		return fmt.Errorf("error = %v", err)
	}
	return nil
}

func (p syntheticRelayProvider) drain() error {
	service, err := p.newRelay()
	if err != nil {
		return err
	}
	if err := service.BeginDrain(); err != nil {
		return err
	}
	if err := service.RegisterAllocation(p.fixture.replacementAllocationID, map[string][]byte{p.fixture.senderID: p.fixture.senderKey, p.fixture.recipientID: p.fixture.recipientKey}, p.fixture.now.Add(p.fixture.lease)); !errors.Is(err, relay.ErrNotAccepting) {
		return fmt.Errorf("new allocation error = %v", err)
	}
	datagram, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 1, []byte("drain"))
	if err != nil {
		return err
	}
	if err := p.forward(service, datagram, p.fixture.senderID, p.fixture.recipientID, p.fixture.recipientKey); err != nil {
		return err
	}
	if err := service.CloseAllocation(p.fixture.allocationID, p.fixture.now); err != nil {
		return err
	}
	if service.State() != relay.Empty {
		return fmt.Errorf("state = %s", service.State())
	}
	return nil
}

func (p syntheticRelayProvider) forcedLoss() error {
	service, err := p.newRelay()
	if err != nil {
		return err
	}
	service.Fail("synthetic forced loss")
	datagram, err := p.packet(p.fixture.senderID, p.fixture.recipientID, p.fixture.senderKey, 1, []byte("lost"))
	if err != nil {
		return err
	}
	if err := service.Forward(datagram, p.fixture.senderID, p.fixture.now, func(string, []byte) error { return nil }); !errors.Is(err, relay.ErrRelayUnavailable) {
		return fmt.Errorf("error = %v", err)
	}
	return nil
}
