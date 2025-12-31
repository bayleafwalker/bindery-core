package main

// Local run (template smoke test):
//
// Terminal A (start NATS):
//   docker run --rm -p 4222:4222 nats:2
//
// Terminal B (run the module):
//   cd examples/modules/physics-engine-template
//   NATS_URL=nats://127.0.0.1:4222 go run . --listen :50051
//
// Terminal C (call an RPC using the repo's generic client):
//   cd /projects/dev/bindery
//   go run ./cmd/engine-module-client --target 127.0.0.1:50051 --world world-1
//
// Notes:
// - This module publishes placeholder payloads to the subject "physics.state.v1".
// - Real platforms should inject the resolved messaging.bus endpoint via bindings/secrets.

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
	"github.com/bayleafwalker/bindery-core/modules/physics-engine-template/publish"
)

type server struct {
	enginev1.UnimplementedEngineModuleServer
	publisher publish.Publisher
}

func (s *server) InitializeWorld(ctx context.Context, req *enginev1.InitializeWorldRequest) (*enginev1.InitializeWorldResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.InitializeWorldResponse{
			Result: &enginev1.InitializeWorldResponse_Error{
				Error: &enginev1.Error{Code: enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, Message: "request is nil"},
			},
		}, nil
	}

	// TODO(physics): Initialize/restore physics world state for the requested world_id.
	// - Initialize spatial structures
	// - Load persisted snapshot/checkpoint if applicable
	// - Validate config keys

	return &enginev1.InitializeWorldResponse{
		Result: &enginev1.InitializeWorldResponse_Ok{
			Ok: &enginev1.InitializeWorldOk{
				InitialTick: 0,
				Metadata: map[string]string{
					"placeholder": "true",
				},
			},
		},
	}, nil
}

func (s *server) ApplyCommand(ctx context.Context, req *enginev1.ApplyCommandRequest) (*enginev1.ApplyCommandResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.ApplyCommandResponse{
			Result: &enginev1.ApplyCommandResponse_Error{
				Error: &enginev1.Error{Code: enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, Message: "request is nil"},
			},
		}, nil
	}

	// TODO(physics): Interpret physics-relevant commands.
	// Common patterns:
	// - Treat interaction engine as the producer of game intents, and physics as a validator/integrator.
	// - Validate entity existence and constraints.
	// - Buffer command effects for the next Tick.

	return &enginev1.ApplyCommandResponse{
		Result: &enginev1.ApplyCommandResponse_Ok{
			Ok: &enginev1.ApplyCommandOk{AppliedTick: 0},
		},
	}, nil
}

func (s *server) Tick(ctx context.Context, req *enginev1.TickRequest) (*enginev1.TickResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.TickResponse{
			Result: &enginev1.TickResponse_Error{
				Error: &enginev1.Error{Code: enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, Message: "request is nil"},
			},
		}, nil
	}

	// TODO(physics): Advance simulation.
	// - Obtain time via required capability `time.source` (not wired in this template)
	// - Integrate physics for delta_millis
	// - Produce deterministic events/deltas
	// - Publish to event bus (NATS in this template)

	if s.publisher != nil {
		// TODO(physics): Replace placeholder subject/payload with a real protobuf event.
		_ = s.publisher.Publish(ctx, "physics.state.v1", []byte("TODO"))
	}

	return &enginev1.TickResponse{
		Result: &enginev1.TickResponse_Ok{
			Ok: &enginev1.TickOk{NewTick: 0},
		},
	}, nil
}

func (s *server) GetStateSnapshot(ctx context.Context, req *enginev1.GetStateSnapshotRequest) (*enginev1.GetStateSnapshotResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.GetStateSnapshotResponse{
			Result: &enginev1.GetStateSnapshotResponse_Error{
				Error: &enginev1.Error{Code: enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, Message: "request is nil"},
			},
		}, nil
	}

	// TODO(physics): Return a snapshot of physics-relevant world state.
	// - Support latest vs at_tick selectors
	// - Optionally filter entities
	// - Consider compression and/or delta encoding

	worldID := req.GetWorldId()
	if worldID == "" {
		worldID = "unknown"
	}

	return &enginev1.GetStateSnapshotResponse{
		Result: &enginev1.GetStateSnapshotResponse_Ok{
			Ok: &enginev1.GetStateSnapshotOk{
				WorldState: &enginev1.WorldState{
					WorldId: worldID,
					Tick:    0,
					Metadata: map[string]string{
						"placeholder": "true",
					},
				},
				Metadata: map[string]string{
					"placeholder": "true",
				},
			},
		},
	}, nil
}

func main() {
	var listenAddr string
	flag.StringVar(&listenAddr, "listen", ":50051", "gRPC listen address")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// NATS is the chosen event bus for this template.
	// TODO(platform): In the real platform, the resolver/bindings would provide
	// connection details (e.g., NATS URL/creds) via config/secret injection.
	natsURL := os.Getenv("NATS_URL")
	pub, err := publish.NewNATSPublisher(ctx, natsURL)
	if err != nil {
		// For a template, it's reasonable to start without NATS; publishing becomes a no-op.
		pub = nil
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(fmt.Errorf("listen %s: %w", listenAddr, err))
	}

	grpcServer := grpc.NewServer()
	enginev1.RegisterEngineModuleServer(grpcServer, &server{publisher: pub})

	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Errorf("grpc serve: %w", err))
	}
}
