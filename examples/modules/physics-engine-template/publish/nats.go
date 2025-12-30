package publish

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
)

type natsPublisher struct {
	nc *nats.Conn
}

func NewNATSPublisher(ctx context.Context, url string) (Publisher, error) {
	_ = ctx
	if url == "" {
		// Template default.
		url = nats.DefaultURL
	}

	// TODO(security): Configure credentials, TLS, and reconnect/backoff.
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("connect nats %s: %w", url, err)
	}

	return &natsPublisher{nc: nc}, nil
}

func (p *natsPublisher) Publish(ctx context.Context, subject string, payload []byte) error {
	_ = ctx
	// TODO(observability): Add tracing headers and structured logging.
	return p.nc.Publish(subject, payload)
}

func (p *natsPublisher) Close() error {
	if p.nc != nil {
		p.nc.Close()
	}
	return nil
}
