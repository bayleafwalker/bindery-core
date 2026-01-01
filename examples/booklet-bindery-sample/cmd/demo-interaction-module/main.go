package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
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
	_ = req
	return &enginev1.InitializeWorldResponse{
		Result: &enginev1.InitializeWorldResponse_Ok{
			Ok: &enginev1.InitializeWorldOk{InitialTick: 0, Metadata: map[string]string{"demo": "true"}},
		},
	}, nil
}

func (s *server) ApplyCommand(ctx context.Context, req *enginev1.ApplyCommandRequest) (*enginev1.ApplyCommandResponse, error) {
	_ = ctx
	_ = req
	return &enginev1.ApplyCommandResponse{
		Result: &enginev1.ApplyCommandResponse_Ok{
			Ok: &enginev1.ApplyCommandOk{AppliedTick: 0},
		},
	}, nil
}

func (s *server) Tick(ctx context.Context, req *enginev1.TickRequest) (*enginev1.TickResponse, error) {
	_ = ctx
	_ = req
	return &enginev1.TickResponse{
		Result: &enginev1.TickResponse_Ok{
			Ok: &enginev1.TickOk{NewTick: 0},
		},
	}, nil
}

func (s *server) GetStateSnapshot(ctx context.Context, req *enginev1.GetStateSnapshotRequest) (*enginev1.GetStateSnapshotResponse, error) {
	_ = ctx
	_ = req
	return &enginev1.GetStateSnapshotResponse{
		Result: &enginev1.GetStateSnapshotResponse_Ok{
			Ok: &enginev1.GetStateSnapshotOk{
				WorldState: &enginev1.WorldState{WorldId: "demo", Tick: 0, Metadata: map[string]string{"demo": "true"}},
				Metadata:   map[string]string{"demo": "true"},
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
	actorID := strings.TrimSpace(os.Getenv("BINDERY_DEMO_ACTOR_ID"))
	if actorID == "" {
		actorID = "demo-actor"
	}

	commandInterval := time.Duration(envInt("BINDERY_DEMO_COMMAND_INTERVAL_MS", 75)) * time.Millisecond
	snapshotInterval := time.Duration(envInt("BINDERY_DEMO_SNAPSHOT_INTERVAL_MS", 500)) * time.Millisecond

	if physicsTarget != "" {
		go runClientLoop(physicsTarget, worldID, actorID, commandInterval, snapshotInterval)
	} else {
		fmt.Printf("No physics dependency injected (BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT is empty)\n")
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(fmt.Errorf("listen %s: %w", listenAddr, err))
	}
	fmt.Printf("demo-interaction: listen=%s world=%s actor=%s physics=%s\n", listenAddr, worldID, actorID, physicsTarget)

	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Errorf("grpc serve: %w", err))
	}
}

func runClientLoop(target, worldID, actorID string, commandInterval, snapshotInterval time.Duration) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	entityID := fmt.Sprintf("entity-%s", actorID)

	var spawned bool
	var lastSnapshot time.Time
	var smokePrinted bool

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			cancel()
			fmt.Printf("physics dial failed (%s): %v\n", target, err)
			time.Sleep(1 * time.Second)
			continue
		}
		c := enginev1.NewEngineModuleClient(conn)

		now := time.Now()
		if !spawned {
			reqID := fmt.Sprintf("spawn-%d", now.UnixNano())
			cmd := &enginev1.Command{
				CommandId:          reqID,
				ActorId:            actorID,
				IssuedAtUnixMillis: now.UnixMilli(),
				Payload:            &enginev1.Command_SpawnEntity{SpawnEntity: &enginev1.SpawnEntityCommand{EntityId: entityID}},
			}
			resp, err := c.ApplyCommand(ctx, &enginev1.ApplyCommandRequest{
				WorldId:   worldID,
				RequestId: reqID,
				Command:   cmd,
			})
			if err != nil {
				cancel()
				_ = conn.Close()
				fmt.Printf("spawn ApplyCommand failed: %v\n", err)
				time.Sleep(1 * time.Second)
				continue
			}
			if resp.GetError() != nil {
				cancel()
				_ = conn.Close()
				fmt.Printf("spawn rejected: code=%s message=%q\n", resp.GetError().GetCode().String(), resp.GetError().GetMessage())
				time.Sleep(1 * time.Second)
				continue
			}
			spawned = true
		} else {
			reqID := fmt.Sprintf("move-%d", now.UnixNano())
			cmd := &enginev1.Command{
				CommandId:          reqID,
				ActorId:            actorID,
				IssuedAtUnixMillis: now.UnixMilli(),
				Payload: &enginev1.Command_Move{Move: &enginev1.MoveCommand{
					EntityId: entityID,
					Position: &enginev1.Vec3{
						X: float64(rng.Intn(10)),
						Y: float64(rng.Intn(10)),
						Z: 0,
					},
				}},
			}
			resp, err := c.ApplyCommand(ctx, &enginev1.ApplyCommandRequest{
				WorldId:   worldID,
				RequestId: reqID,
				Command:   cmd,
			})
			if err != nil {
				cancel()
				_ = conn.Close()
				fmt.Printf("move ApplyCommand failed: %v\n", err)
				time.Sleep(1 * time.Second)
				continue
			}
			if resp.GetError() != nil {
				fmt.Printf("move rejected: code=%s message=%q\n", resp.GetError().GetCode().String(), resp.GetError().GetMessage())
			}
		}

		if lastSnapshot.IsZero() || time.Since(lastSnapshot) >= snapshotInterval {
			lastSnapshot = time.Now()
			snap, err := c.GetStateSnapshot(ctx, &enginev1.GetStateSnapshotRequest{
				WorldId:   worldID,
				RequestId: fmt.Sprintf("snapshot-%d", time.Now().UnixNano()),
				Selector:  &enginev1.GetStateSnapshotRequest_Latest{Latest: &enginev1.SnapshotLatest{}},
			})
			if err != nil {
				fmt.Printf("GetStateSnapshot failed: %v\n", err)
			} else if snap.GetOk() != nil {
				state := snap.GetOk().GetWorldState()
				fmt.Printf("physics snapshot: world=%s tick=%d entities=%d\n", state.GetWorldId(), state.GetTick(), len(state.GetEntities()))
				if !smokePrinted && state.GetTick() > 0 && len(state.GetEntities()) > 0 {
					fmt.Printf("BINDERY_SMOKE_OK world=%s tick=%d entities=%d\n", state.GetWorldId(), state.GetTick(), len(state.GetEntities()))
					smokePrinted = true
				}
			} else if snap.GetError() != nil {
				fmt.Printf("GetStateSnapshot error: code=%s message=%q\n", snap.GetError().GetCode().String(), snap.GetError().GetMessage())
			}
		}

		cancel()
		_ = conn.Close()

		time.Sleep(commandInterval)
	}
}

func envInt(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}
