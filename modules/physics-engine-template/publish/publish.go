package publish

import "context"

// Publisher is the minimal event-publishing seam.
//
// TODO(platform): Replace this with the platform's canonical event-bus client
// (including tracing headers, ordering keys, schema registry integration, etc.).
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
	Close() error
}
