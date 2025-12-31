package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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
				Metadata:    map[string]string{"placeholder": "true"},
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
	flag.StringVar(&listenAddr, "listen", ":50051", "address to listen on")
	flag.Parse()

	grpcServer := grpc.NewServer()
	enginev1.RegisterEngineModuleServer(grpcServer, &server{})

	physicsTarget := strings.TrimSpace(os.Getenv("BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT"))
	worldID := strings.TrimSpace(os.Getenv("BINDERY_DEMO_WORLD_ID"))
	if worldID == "" {
		worldID = "world-001"
	}
	if physicsTarget != "" {
		go probePhysics(physicsTarget, worldID)
	} else {
		fmt.Printf("No physics dependency injected (BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT is empty)\n")
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

func probePhysics(target, worldID string) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			cancel()
			fmt.Printf("Physics dial failed (%s): %v\n", target, err)
			time.Sleep(2 * time.Second)
			continue
		}

		c := enginev1.NewEngineModuleClient(conn)
		resp, err := c.GetStateSnapshot(ctx, &enginev1.GetStateSnapshotRequest{
			WorldId:   worldID,
			RequestId: fmt.Sprintf("demo-%d", time.Now().UnixNano()),
			Selector:  &enginev1.GetStateSnapshotRequest_Latest{Latest: &enginev1.SnapshotLatest{}},
		})
		cancel()
		_ = conn.Close()

		if err != nil {
			fmt.Printf("Physics GetStateSnapshot failed: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		switch r := resp.GetResult().(type) {
		case *enginev1.GetStateSnapshotResponse_Ok:
			state := r.Ok.GetWorldState()
			fmt.Printf("Physics snapshot ok: world=%s tick=%d entities=%d\n", state.GetWorldId(), state.GetTick(), len(state.GetEntities()))
		case *enginev1.GetStateSnapshotResponse_Error:
			fmt.Printf("Physics snapshot error: code=%s message=%q\n", r.Error.GetCode().String(), r.Error.GetMessage())
		default:
			fmt.Printf("Physics snapshot: unknown result\n")
		}

		time.Sleep(5 * time.Second)
	}
}
