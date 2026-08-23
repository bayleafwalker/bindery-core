package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
	"github.com/bayleafwalker/bindery-sample-game/internal/physics"
)

type server struct {
	enginev1.UnimplementedEngineModuleServer
	engine *physics.Engine
}

func (s *server) InitializeWorld(ctx context.Context, req *enginev1.InitializeWorldRequest) (*enginev1.InitializeWorldResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.InitializeWorldResponse{Result: &enginev1.InitializeWorldResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "request is nil")}}, nil
	}
	if strings.TrimSpace(req.GetWorldId()) == "" {
		return &enginev1.InitializeWorldResponse{Result: &enginev1.InitializeWorldResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "world_id is empty")}}, nil
	}

	initialTick, err := s.engine.InitializeWorld(req.GetWorldId())
	if err != nil {
		return &enginev1.InitializeWorldResponse{Result: &enginev1.InitializeWorldResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INTERNAL, err.Error())}}, nil
	}

	return &enginev1.InitializeWorldResponse{
		Result: &enginev1.InitializeWorldResponse_Ok{
			Ok: &enginev1.InitializeWorldOk{
				InitialTick: initialTick,
				Metadata:    map[string]string{"demo": "true"},
			},
		},
	}, nil
}

func (s *server) ApplyCommand(ctx context.Context, req *enginev1.ApplyCommandRequest) (*enginev1.ApplyCommandResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.ApplyCommandResponse{Result: &enginev1.ApplyCommandResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "request is nil")}}, nil
	}
	if strings.TrimSpace(req.GetWorldId()) == "" {
		return &enginev1.ApplyCommandResponse{Result: &enginev1.ApplyCommandResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "world_id is empty")}}, nil
	}
	if req.Command == nil {
		return &enginev1.ApplyCommandResponse{Result: &enginev1.ApplyCommandResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "command is nil")}}, nil
	}

	appliedTick, err := s.engine.EnqueueCommand(req.GetWorldId(), req.Command, req.GetDryRun())
	if err != nil {
		return &enginev1.ApplyCommandResponse{Result: &enginev1.ApplyCommandResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_FAILED_PRECONDITION, err.Error())}}, nil
	}

	return &enginev1.ApplyCommandResponse{
		Result: &enginev1.ApplyCommandResponse_Ok{
			Ok: &enginev1.ApplyCommandOk{
				AppliedTick: appliedTick,
			},
		},
	}, nil
}

func (s *server) Tick(ctx context.Context, req *enginev1.TickRequest) (*enginev1.TickResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.TickResponse{Result: &enginev1.TickResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "request is nil")}}, nil
	}
	if strings.TrimSpace(req.GetWorldId()) == "" {
		return &enginev1.TickResponse{Result: &enginev1.TickResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "world_id is empty")}}, nil
	}

	newTick, events, err := s.engine.Tick(req.GetWorldId(), req.GetExpectedCurrentTick(), req.GetTargetTick())
	if err != nil {
		return &enginev1.TickResponse{Result: &enginev1.TickResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_CONFLICT, err.Error())}}, nil
	}

	return &enginev1.TickResponse{
		Result: &enginev1.TickResponse_Ok{
			Ok: &enginev1.TickOk{
				NewTick: newTick,
				Events:  events,
			},
		},
	}, nil
}

func (s *server) GetStateSnapshot(ctx context.Context, req *enginev1.GetStateSnapshotRequest) (*enginev1.GetStateSnapshotResponse, error) {
	_ = ctx
	if req == nil {
		return &enginev1.GetStateSnapshotResponse{Result: &enginev1.GetStateSnapshotResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "request is nil")}}, nil
	}
	if strings.TrimSpace(req.GetWorldId()) == "" {
		return &enginev1.GetStateSnapshotResponse{Result: &enginev1.GetStateSnapshotResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_INVALID_ARGUMENT, "world_id is empty")}}, nil
	}

	var atTick *int64
	switch s := req.GetSelector().(type) {
	case *enginev1.GetStateSnapshotRequest_AtTick:
		t := s.AtTick.GetTick()
		atTick = &t
	}

	ws, err := s.engine.Snapshot(req.GetWorldId(), atTick, req.GetEntityIds(), req.GetIncludeComponents())
	if err != nil {
		return &enginev1.GetStateSnapshotResponse{Result: &enginev1.GetStateSnapshotResponse_Error{Error: errStatus(enginev1.StatusCode_STATUS_CODE_FAILED_PRECONDITION, err.Error())}}, nil
	}

	return &enginev1.GetStateSnapshotResponse{
		Result: &enginev1.GetStateSnapshotResponse_Ok{
			Ok: &enginev1.GetStateSnapshotOk{
				WorldState: ws,
				Metadata:   map[string]string{"demo": "true"},
			},
		},
	}, nil
}

func errStatus(code enginev1.StatusCode, message string) *enginev1.Error {
	return &enginev1.Error{
		Code:    code,
		Message: message,
	}
}

func main() {
	var listenAddr string
	flag.StringVar(&listenAddr, "listen", ":50051", "address to listen on")
	flag.Parse()

	maxPerTick := envInt("BINDERY_SAMPLE_MAX_COMMANDS_PER_TICK", 256)
	tickInterval := time.Duration(envInt("BINDERY_SAMPLE_TICK_INTERVAL_MS", 200)) * time.Millisecond
	autoTick := envBool("BINDERY_SAMPLE_AUTOTICK", true)

	sampleGameEnabled := envBool("BINDERY_SAMPLE_GAME_ENABLED", true)
	sampleCfg := physics.SampleGameConfig{
		Enabled:           sampleGameEnabled,
		PlanetDistance:    envInt64("BINDERY_SAMPLE_PLANET_DISTANCE", 200),
		PlanetRadius:      envInt64("BINDERY_SAMPLE_PLANET_RADIUS", 20),
		ShipOrbitRadius:   envInt64("BINDERY_SAMPLE_SHIP_ORBIT_RADIUS", 35),
		ShipsPerTeam:      envInt("BINDERY_SAMPLE_SHIPS_PER_TEAM", 8),
		ShipMaxHP:         int32(envInt("BINDERY_SAMPLE_SHIP_MAX_HP", 60)),
		ShipSpeedPerTick:  envInt64("BINDERY_SAMPLE_SHIP_SPEED_PER_TICK", 2),
		FireRange:         envInt64("BINDERY_SAMPLE_FIRE_RANGE", 18),
		FireDamage:        int32(envInt("BINDERY_SAMPLE_FIRE_DAMAGE", 1)),
		FireCooldownTicks: envInt64("BINDERY_SAMPLE_FIRE_COOLDOWN_TICKS", 10),
	}

	eng := physics.New(physics.Config{
		MaxCommandsPerTick: maxPerTick,
		SampleGame:         sampleCfg,
	})

	if autoTick {
		go func() {
			t := time.NewTicker(tickInterval)
			defer t.Stop()
			for range t.C {
				_ = eng.TickAll()
			}
		}()
	}

	grpcServer := grpc.NewServer()
	enginev1.RegisterEngineModuleServer(grpcServer, &server{engine: eng})

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(fmt.Errorf("listen %s: %w", listenAddr, err))
	}
	fmt.Printf("demo-physics: listen=%s autotick=%t tickInterval=%s maxCommandsPerTick=%d sampleGame=%t shipsPerTeam=%d\n",
		listenAddr,
		autoTick,
		tickInterval,
		maxPerTick,
		sampleGameEnabled,
		sampleCfg.ShipsPerTeam,
	)

	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Errorf("grpc serve: %w", err))
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

func envBool(name string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return def
	}
}

func envInt64(name string, def int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}
