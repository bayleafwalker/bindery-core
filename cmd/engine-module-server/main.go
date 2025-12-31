package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"google.golang.org/grpc"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
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

	// gRPC Options
	var opts []grpc.ServerOption
	if s := os.Getenv("BINDERY_GRPC_INITIAL_WINDOW_SIZE"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			opts = append(opts, grpc.InitialWindowSize(int32(v)))
		}
	}
	if s := os.Getenv("BINDERY_GRPC_INITIAL_CONN_WINDOW_SIZE"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			opts = append(opts, grpc.InitialConnWindowSize(int32(v)))
		}
	}

	grpcServer := grpc.NewServer(opts...)
	enginev1.RegisterEngineModuleServer(grpcServer, &server{})

	// UDS Listener
	udsDir := os.Getenv("BINDERY_UDS_DIR")
	moduleName := os.Getenv("BINDERY_MODULE_NAME")
	if udsDir != "" && moduleName != "" {
		socketPath := filepath.Join(udsDir, moduleName+".sock")
		_ = os.Remove(socketPath)
		udsLis, err := net.Listen("unix", socketPath)
		if err != nil {
			fmt.Printf("Failed to listen on UDS %s: %v\n", socketPath, err)
		} else {
			fmt.Printf("Listening on UDS %s\n", socketPath)
			go func() {
				if err := grpcServer.Serve(udsLis); err != nil {
					fmt.Printf("UDS serve error: %v\n", err)
				}
			}()
		}
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(fmt.Errorf("listen %s: %w", listenAddr, err))
	}
	fmt.Printf("Listening on TCP %s\n", listenAddr)

	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Errorf("grpc serve: %w", err))
	}
}
