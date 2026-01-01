package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
)

//go:embed static/*
var embeddedStatic embed.FS

type apiState struct {
	WorldID            string      `json:"worldId"`
	Tick               int64       `json:"tick"`
	Entities           []apiEntity `json:"entities"`
	UpdatedAtUnixMilli int64       `json:"updatedAtUnixMillis"`
	Error              string      `json:"error,omitempty"`
}

type apiEntity struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Team     string  `json:"team,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	HP       int32   `json:"hp,omitempty"`
	MaxHP    int32   `json:"maxHp,omitempty"`
	Radius   int64   `json:"radius,omitempty"`
	Cooldown int64   `json:"cooldown,omitempty"`
}

type stateCache struct {
	mu    sync.RWMutex
	state apiState
}

func main() {
	var listenAddr string
	flag.StringVar(&listenAddr, "listen", ":8080", "HTTP listen address")
	flag.Parse()

	physicsTarget := strings.TrimSpace(os.Getenv("BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT"))

	worldID := strings.TrimSpace(os.Getenv("BINDERY_SAMPLE_WORLD_ID"))
	if worldID == "" {
		worldID = "world-001"
	}

	pollInterval := time.Duration(envInt("BINDERY_SAMPLE_POLL_INTERVAL_MS", 200)) * time.Millisecond

	sub, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		panic(fmt.Errorf("static fs: %w", err))
	}

	cache := &stateCache{}
	if physicsTarget != "" {
		go cache.pollSnapshots(context.Background(), physicsTarget, worldID, pollInterval)
	} else {
		cache.setError(worldID, "no physics endpoint injected (BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT is empty)")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", cache.handleState)
	mux.Handle("/", http.FileServer(http.FS(sub)))

	fmt.Printf("demo-web: listen=%s world=%s physics=%s pollInterval=%s\n", listenAddr, worldID, physicsTarget, pollInterval)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		panic(fmt.Errorf("http serve: %w", err))
	}
}

func (c *stateCache) pollSnapshots(ctx context.Context, target, worldID string, interval time.Duration) {
	backoff := 500 * time.Millisecond
	for {
		conn, err := dial(ctx, target)
		if err != nil {
			c.setError(worldID, fmt.Sprintf("dial failed: %v", err))
			time.Sleep(backoff)
			continue
		}

		client := enginev1.NewEngineModuleClient(conn)
		t := time.NewTicker(interval)
		for {
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return
			case <-t.C:
				snap, err := snapshotOnce(ctx, client, worldID)
				if err != nil {
					c.setError(worldID, err.Error())
					_ = conn.Close()
					t.Stop()
					time.Sleep(backoff)
					goto reconnect
				}
				c.setState(snap)
			}
		}
	reconnect:
		continue
	}
}

func dial(ctx context.Context, target string) (*grpc.ClientConn, error) {
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return grpc.DialContext(dctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func snapshotOnce(ctx context.Context, client enginev1.EngineModuleClient, worldID string) (apiState, error) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	reqID := fmt.Sprintf("web-snapshot-%d", time.Now().UnixNano())
	resp, err := client.GetStateSnapshot(rctx, &enginev1.GetStateSnapshotRequest{
		WorldId:           worldID,
		RequestId:         reqID,
		Selector:          &enginev1.GetStateSnapshotRequest_Latest{Latest: &enginev1.SnapshotLatest{}},
		IncludeComponents: true,
	})
	if err != nil {
		return apiState{WorldID: worldID, UpdatedAtUnixMilli: time.Now().UnixMilli()}, fmt.Errorf("GetStateSnapshot: %w", err)
	}
	if resp.GetError() != nil {
		return apiState{WorldID: worldID, UpdatedAtUnixMilli: time.Now().UnixMilli()}, fmt.Errorf("GetStateSnapshot: %s", resp.GetError().GetMessage())
	}

	ws := resp.GetOk().GetWorldState()
	if ws == nil {
		return apiState{WorldID: worldID, UpdatedAtUnixMilli: time.Now().UnixMilli()}, fmt.Errorf("GetStateSnapshot: missing world_state")
	}
	return stateFromWorld(ws), nil
}

func stateFromWorld(ws *enginev1.WorldState) apiState {
	state := apiState{
		WorldID:            ws.GetWorldId(),
		Tick:               ws.GetTick(),
		UpdatedAtUnixMilli: time.Now().UnixMilli(),
	}

	entities := make([]apiEntity, 0, len(ws.GetEntities()))
	for _, e := range ws.GetEntities() {
		if e == nil {
			continue
		}
		kind := strings.TrimSpace(e.GetType())
		if kind == "" {
			kind = strings.TrimSpace(e.GetMetadata()["kind"])
		}

		var pos *enginev1.Vec3
		var hp *enginev1.HealthComponent
		for _, c := range e.GetComponents() {
			if c == nil {
				continue
			}
			if t := c.GetTransform(); t != nil && t.Position != nil {
				pos = t.Position
			}
			if h := c.GetHealth(); h != nil {
				hp = h
			}
		}
		if pos == nil {
			continue
		}

		ent := apiEntity{
			ID:   e.GetEntityId(),
			Kind: kind,
			Team: strings.TrimSpace(e.GetMetadata()["team"]),
			X:    pos.X,
			Y:    pos.Y,
			Z:    pos.Z,
		}
		if hp != nil {
			ent.HP = hp.Current
			ent.MaxHP = hp.Max
		}
		if raw := strings.TrimSpace(e.GetMetadata()["radius"]); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
				ent.Radius = v
			}
		}
		if raw := strings.TrimSpace(e.GetMetadata()["cooldown"]); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
				ent.Cooldown = v
			}
		}
		entities = append(entities, ent)
	}
	state.Entities = entities
	return state
}

func (c *stateCache) setState(state apiState) {
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
}

func (c *stateCache) setError(worldID, message string) {
	c.mu.Lock()
	c.state = apiState{
		WorldID:            worldID,
		UpdatedAtUnixMilli: time.Now().UnixMilli(),
		Error:              message,
	}
	c.mu.Unlock()
}

func (c *stateCache) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	c.mu.RLock()
	state := c.state
	c.mu.RUnlock()

	b, err := json.Marshal(state)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
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
