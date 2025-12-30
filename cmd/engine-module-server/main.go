package main

import (
	"context"
	"flag"
	"fmt"
	"net"

	"google.golang.org/grpc"

	enginev1 "github.com/anvil-platform/anvil/proto/game/engine/v1"
)

type server struct {
	enginev1.UnimplementedEngineModuleServer
}

func (s *server) InitializeWorld(ctx context.Context, req *enginev1.InitializeWorldRequest) (*enginev1.InitializeWorldResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.InitializeWorldResponse{
			Result: &enginev1.InitializeWorldResponse_Error{
				Error: &enginev1.Error{
					Code:    enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT,
					Message: "request is nil",
				},
			},
		}, nil
	}
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
				Error: &enginev1.Error{
					Code:    enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT,
					Message: "request is nil",
				},
			},
		}, nil
	}
	return &enginev1.ApplyCommandResponse{
		Result: &enginev1.ApplyCommandResponse_Ok{
			Ok: &enginev1.ApplyCommandOk{
				AppliedTick: 0,
				Events:      nil,
			},
		},
	}, nil
}

func (s *server) Tick(ctx context.Context, req *enginev1.TickRequest) (*enginev1.TickResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.TickResponse{
			Result: &enginev1.TickResponse_Error{
				Error: &enginev1.Error{
					Code:    enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT,
					Message: "request is nil",
				},
			},
		}, nil
	}
	return &enginev1.TickResponse{
		Result: &enginev1.TickResponse_Ok{
			Ok: &enginev1.TickOk{
				NewTick: 0,
				Events:  nil,
			},
		},
	}, nil
}

func (s *server) GetStateSnapshot(ctx context.Context, req *enginev1.GetStateSnapshotRequest) (*enginev1.GetStateSnapshotResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.GetStateSnapshotResponse{
			Result: &enginev1.GetStateSnapshotResponse_Error{
				Error: &enginev1.Error{
					Code:    enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT,
					Message: "request is nil",
				},
			},
		}, nil
	}

	// Minimal placeholder snapshot: no persisted state.
	worldID := req.GetWorldId()
	if worldID == "" {
		worldID = "unknown"
	}

	return &enginev1.GetStateSnapshotResponse{
		Result: &enginev1.GetStateSnapshotResponse_Ok{
			Ok: &enginev1.GetStateSnapshotOk{
				WorldState: &enginev1.WorldState{
					WorldId:  worldID,
					Tick:     0,
					Entities: nil,
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
	flag.StringVar(&listenAddr, "listen", ":50051", "address to listen on")
	flag.Parse()

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(fmt.Errorf("listen %s: %w", listenAddr, err))
	}

	grpcServer := grpc.NewServer()
	enginev1.RegisterEngineModuleServer(grpcServer, &server{})

	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Errorf("grpc serve: %w", err))
	}
}
