package physics

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
)

type Config struct {
	// MaxCommandsPerTick bounds how many queued commands are applied per tick step.
	// If <= 0, a safe default is used.
	MaxCommandsPerTick int

	// SampleGame enables the built-in sample "two planets / ships" simulation.
	// When disabled, the engine behaves like a minimal generic entity store.
	SampleGame SampleGameConfig
}

type SampleGameConfig struct {
	Enabled bool

	PlanetDistance    int64 // center-to-center distance
	PlanetRadius      int64
	ShipOrbitRadius   int64
	ShipsPerTeam      int
	ShipMaxHP         int32
	ShipSpeedPerTick  int64
	FireRange         int64
	FireDamage        int32
	FireCooldownTicks int64
}

type Engine struct {
	mu                 sync.Mutex
	worlds             map[string]*world
	maxCommandsPerTick int
	sampleGame         SampleGameConfig
}

func New(cfg Config) *Engine {
	maxCommandsPerTick := cfg.MaxCommandsPerTick
	if maxCommandsPerTick <= 0 {
		maxCommandsPerTick = 16
	}

	sg := normalizeSampleGame(cfg.SampleGame)

	return &Engine{
		worlds:             make(map[string]*world),
		maxCommandsPerTick: maxCommandsPerTick,
		sampleGame:         sg,
	}
}

func (e *Engine) InitializeWorld(worldID string) (int64, error) {
	worldID = normalizeID(worldID)
	if worldID == "" {
		return 0, errors.New("worldID is empty")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	w := newWorld(e.maxCommandsPerTick, e.sampleGame)
	e.worlds[worldID] = w
	return 0, nil
}

func (e *Engine) EnqueueCommand(worldID string, cmd *enginev1.Command, dryRun bool) (int64, error) {
	worldID = normalizeID(worldID)
	if worldID == "" {
		return 0, errors.New("worldID is empty")
	}
	if cmd == nil {
		return 0, errors.New("command is nil")
	}
	if normalizeID(cmd.GetCommandId()) == "" {
		return 0, errors.New("command.command_id is empty")
	}

	w := e.getOrCreateWorld(worldID)
	return w.enqueue(cmd, dryRun)
}

func (e *Engine) Tick(worldID string, expectedCurrentTick, targetTick int64) (int64, []*enginev1.Event, error) {
	worldID = normalizeID(worldID)
	if worldID == "" {
		return 0, nil, errors.New("worldID is empty")
	}

	w := e.getOrCreateWorld(worldID)
	return w.step(expectedCurrentTick, targetTick)
}

// TickAll advances all known worlds by one step (used for demo auto-ticking).
func (e *Engine) TickAll() map[string]int64 {
	e.mu.Lock()
	ids := make([]string, 0, len(e.worlds))
	for id := range e.worlds {
		ids = append(ids, id)
	}
	e.mu.Unlock()

	out := make(map[string]int64, len(ids))
	for _, id := range ids {
		newTick, _, err := e.Tick(id, 0, 0)
		if err == nil {
			out[id] = newTick
		}
	}
	return out
}

func (e *Engine) Snapshot(worldID string, atTick *int64, entityIDs []string, includeComponents bool) (*enginev1.WorldState, error) {
	worldID = normalizeID(worldID)
	if worldID == "" {
		return nil, errors.New("worldID is empty")
	}

	w := e.getOrCreateWorld(worldID)
	return w.snapshot(worldID, atTick, entityIDs, includeComponents)
}

func (e *Engine) getOrCreateWorld(worldID string) *world {
	e.mu.Lock()
	defer e.mu.Unlock()
	if w, ok := e.worlds[worldID]; ok {
		return w
	}
	w := newWorld(e.maxCommandsPerTick, e.sampleGame)
	e.worlds[worldID] = w
	return w
}

type world struct {
	mu                 sync.Mutex
	tick               int64
	entities           map[string]*entityState
	queue              []*enginev1.Command
	seenCommandIDs     map[string]struct{}
	nextGeneratedID    int64
	maxCommandsPerTick int
	sampleGame         SampleGameConfig
}

type vec3 struct {
	X int64
	Y int64
	Z int64
}

type entityKind string

const (
	entityKindDemo   entityKind = "demo"
	entityKindPlanet entityKind = "planet"
	entityKindShip   entityKind = "ship"
)

type entityState struct {
	id       string
	kind     entityKind
	team     string
	radius   int64
	pos      vec3
	vel      vec3
	hp       int32
	maxHP    int32
	cooldown int64
}

func newWorld(maxCommandsPerTick int, sampleGame SampleGameConfig) *world {
	if maxCommandsPerTick <= 0 {
		maxCommandsPerTick = 16
	}
	w := &world{
		entities:           make(map[string]*entityState),
		queue:              nil,
		seenCommandIDs:     make(map[string]struct{}),
		nextGeneratedID:    1,
		maxCommandsPerTick: maxCommandsPerTick,
		sampleGame:         sampleGame,
	}
	if w.sampleGame.Enabled {
		w.resetSampleGameLocked()
	}
	return w
}

func (w *world) enqueue(cmd *enginev1.Command, dryRun bool) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := normalizeID(cmd.GetCommandId())
	if _, ok := w.seenCommandIDs[id]; ok {
		// Idempotent accept.
		return w.tick, nil
	}

	if err := validateCommandLocked(w, cmd); err != nil {
		return w.tick, err
	}
	if dryRun {
		return w.tick, nil
	}

	w.seenCommandIDs[id] = struct{}{}
	w.queue = append(w.queue, cmd)
	// Commands are applied on the next tick step.
	return w.tick + 1, nil
}

func (w *world) step(expectedCurrentTick, targetTick int64) (int64, []*enginev1.Event, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if expectedCurrentTick != 0 && expectedCurrentTick != w.tick {
		return w.tick, nil, fmt.Errorf("expected_current_tick=%d does not match current_tick=%d", expectedCurrentTick, w.tick)
	}

	steps := int64(1)
	if targetTick > 0 && targetTick > w.tick {
		steps = targetTick - w.tick
		if steps > 1000 {
			steps = 1000
		}
	}

	var events []*enginev1.Event
	for i := int64(0); i < steps; i++ {
		w.tick++
		events = append(events, w.applyQueuedCommandsLocked(w.tick)...)
		if w.sampleGame.Enabled {
			events = append(events, w.stepSampleGameLocked(w.tick)...)
		}
	}
	return w.tick, events, nil
}

func (w *world) applyQueuedCommandsLocked(tick int64) []*enginev1.Event {
	max := w.maxCommandsPerTick
	if max <= 0 {
		max = len(w.queue)
	}
	if max > len(w.queue) {
		max = len(w.queue)
	}

	var events []*enginev1.Event
	for i := 0; i < max; i++ {
		cmd := w.queue[0]
		w.queue = w.queue[1:]

		if err := applyCommandLocked(w, cmd); err != nil {
			events = append(events, &enginev1.Event{
				Type: "physics.command.error",
				Tick: tick,
				Payload: &enginev1.Event_Opaque{
					Opaque: []byte(err.Error()),
				},
			})
			continue
		}

		b, _ := json.Marshal(map[string]any{
			"commandId": cmd.GetCommandId(),
			"actorId":   cmd.GetActorId(),
			"kind":      commandKind(cmd),
			"tick":      tick,
		})
		events = append(events, &enginev1.Event{
			Type: "physics.command.applied",
			Tick: tick,
			Payload: &enginev1.Event_Opaque{
				Opaque: b,
			},
		})
	}
	return events
}

func (w *world) snapshot(worldID string, atTick *int64, entityIDs []string, includeComponents bool) (*enginev1.WorldState, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if atTick != nil && *atTick != w.tick {
		return nil, fmt.Errorf("snapshot at tick=%d not available (current tick=%d)", *atTick, w.tick)
	}

	allow := make(map[string]struct{}, len(entityIDs))
	for _, id := range entityIDs {
		id = normalizeID(id)
		if id != "" {
			allow[id] = struct{}{}
		}
	}

	ids := make([]string, 0, len(w.entities))
	for id := range w.entities {
		if len(allow) > 0 {
			if _, ok := allow[id]; !ok {
				continue
			}
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	entities := make([]*enginev1.Entity, 0, len(ids))
	for _, id := range ids {
		entities = append(entities, w.entities[id].toProto(includeComponents))
	}

	return &enginev1.WorldState{
		WorldId:  worldID,
		Tick:     w.tick,
		Entities: entities,
		Metadata: map[string]string{
			"generatedAtUnixMillis": fmt.Sprintf("%d", time.Now().UnixMilli()),
		},
	}, nil
}

func validateCommandLocked(w *world, cmd *enginev1.Command) error {
	switch cmd.GetPayload().(type) {
	case *enginev1.Command_SpawnEntity:
		// ok
	case *enginev1.Command_Move:
		if normalizeID(cmd.GetMove().GetEntityId()) == "" {
			return errors.New("move.entity_id is empty")
		}
	case *enginev1.Command_DespawnEntity:
		if normalizeID(cmd.GetDespawnEntity().GetEntityId()) == "" {
			return errors.New("despawn_entity.entity_id is empty")
		}
	case *enginev1.Command_Opaque:
		// ok
	default:
		return errors.New("command payload is missing or unknown")
	}
	_ = w
	return nil
}

func applyCommandLocked(w *world, cmd *enginev1.Command) error {
	switch p := cmd.GetPayload().(type) {
	case *enginev1.Command_SpawnEntity:
		id := normalizeID(p.SpawnEntity.GetEntityId())
		if id == "" {
			id = w.generateEntityIDLocked()
		}
		if _, ok := w.entities[id]; ok {
			return fmt.Errorf("entity %q already exists", id)
		}
		state := &entityState{id: id, kind: entityKindDemo}
		if comps := p.SpawnEntity.GetComponents(); len(comps) > 0 {
			state.applySpawnComponents(comps)
		}
		w.entities[id] = state
		return nil
	case *enginev1.Command_Move:
		entityID := normalizeID(p.Move.GetEntityId())
		e, ok := w.entities[entityID]
		if !ok {
			return fmt.Errorf("entity %q not found", entityID)
		}
		pos := p.Move.GetPosition()
		if pos != nil {
			e.pos = vec3{X: int64(pos.X), Y: int64(pos.Y), Z: int64(pos.Z)}
		}
		if v := p.Move.GetVelocity(); v != nil {
			e.vel = vec3{X: int64(v.X), Y: int64(v.Y), Z: int64(v.Z)}
		}
		return nil
	case *enginev1.Command_DespawnEntity:
		entityID := normalizeID(p.DespawnEntity.GetEntityId())
		if _, ok := w.entities[entityID]; !ok {
			return fmt.Errorf("entity %q not found", entityID)
		}
		delete(w.entities, entityID)
		return nil
	case *enginev1.Command_Opaque:
		return w.applyOpaqueLocked(cmd, p.Opaque)
	default:
		return errors.New("command payload is missing or unknown")
	}
}

func (w *world) generateEntityIDLocked() string {
	id := w.nextGeneratedID
	w.nextGeneratedID++
	return fmt.Sprintf("e-%d", id)
}

func (e *entityState) applySpawnComponents(components []*enginev1.Component) {
	for _, c := range components {
		if c == nil {
			continue
		}
		switch p := c.GetPayload().(type) {
		case *enginev1.Component_Transform:
			if p.Transform != nil && p.Transform.Position != nil {
				e.pos = vec3{X: int64(p.Transform.Position.X), Y: int64(p.Transform.Position.Y), Z: int64(p.Transform.Position.Z)}
			}
		case *enginev1.Component_Health:
			if p.Health != nil {
				e.hp = p.Health.Current
				e.maxHP = p.Health.Max
				if e.maxHP == 0 {
					e.maxHP = e.hp
				}
				if e.maxHP < e.hp {
					e.maxHP = e.hp
				}
				if e.kind == entityKindDemo {
					e.kind = entityKindShip
				}
			}
		case *enginev1.Component_Opaque:
			raw := string(p.Opaque)
			switch normalizeID(c.GetType()) {
			case "bindery.sample.kind":
				switch normalizeID(raw) {
				case "planet":
					e.kind = entityKindPlanet
				case "ship":
					e.kind = entityKindShip
				}
			case "bindery.sample.team":
				e.team = normalizeID(raw)
			case "bindery.sample.radius":
				if v, err := strconv.ParseInt(normalizeID(raw), 10, 64); err == nil {
					e.radius = v
				}
			}
		}
	}
}

func (e *entityState) toProto(includeComponents bool) *enginev1.Entity {
	if e == nil {
		return nil
	}

	meta := map[string]string{
		"kind": string(e.kind),
	}
	if e.team != "" {
		meta["team"] = e.team
	}
	if e.radius != 0 {
		meta["radius"] = fmt.Sprintf("%d", e.radius)
	}
	if e.vel != (vec3{}) {
		meta["vx"] = fmt.Sprintf("%d", e.vel.X)
		meta["vy"] = fmt.Sprintf("%d", e.vel.Y)
		meta["vz"] = fmt.Sprintf("%d", e.vel.Z)
	}
	if e.cooldown != 0 {
		meta["cooldown"] = fmt.Sprintf("%d", e.cooldown)
	}

	out := &enginev1.Entity{
		EntityId:   e.id,
		Type:       string(e.kind),
		Metadata:   meta,
		Components: nil,
	}
	if !includeComponents {
		return out
	}

	out.Components = append(out.Components, &enginev1.Component{
		Type: "transform",
		Payload: &enginev1.Component_Transform{
			Transform: &enginev1.TransformComponent{
				Position:      &enginev1.Vec3{X: float64(e.pos.X), Y: float64(e.pos.Y), Z: float64(e.pos.Z)},
				RotationEuler: &enginev1.Vec3{X: 0, Y: 0, Z: 0},
				Scale:         &enginev1.Vec3{X: 1, Y: 1, Z: 1},
			},
		},
	})
	if e.maxHP > 0 {
		out.Components = append(out.Components, &enginev1.Component{
			Type: "health",
			Payload: &enginev1.Component_Health{
				Health: &enginev1.HealthComponent{
					Current: e.hp,
					Max:     e.maxHP,
				},
			},
		})
	}
	return out
}

type damagePayload struct {
	TargetID string `json:"targetEntityId"`
	Amount   int32  `json:"amount"`
}

func (w *world) applyOpaqueLocked(cmd *enginev1.Command, opaque *enginev1.OpaqueCommand) error {
	if opaque == nil {
		return nil
	}
	switch normalizeID(opaque.GetType()) {
	case "sample.damage":
		var p damagePayload
		if err := json.Unmarshal(opaque.GetData(), &p); err != nil {
			return fmt.Errorf("opaque sample.damage: %w", err)
		}
		targetID := normalizeID(p.TargetID)
		if targetID == "" {
			return errors.New("opaque sample.damage targetEntityId is empty")
		}
		target, ok := w.entities[targetID]
		if !ok {
			return fmt.Errorf("target %q not found", targetID)
		}
		if target.maxHP <= 0 {
			return fmt.Errorf("target %q has no health", targetID)
		}
		if p.Amount <= 0 {
			return errors.New("opaque sample.damage amount must be > 0")
		}
		target.hp -= p.Amount
		if target.hp <= 0 {
			delete(w.entities, targetID)
		}
		_ = cmd
		return nil
	default:
		// Unknown opaque command types are accepted (forward-compatible).
		return nil
	}
}

func (w *world) resetSampleGameLocked() {
	w.entities = make(map[string]*entityState)
	w.queue = nil
	w.seenCommandIDs = make(map[string]struct{})
	w.nextGeneratedID = 1
	w.tick = 0

	cfg := w.sampleGame

	half := cfg.PlanetDistance / 2
	planetRed := &entityState{
		id:     "planet-red",
		kind:   entityKindPlanet,
		team:   "red",
		radius: cfg.PlanetRadius,
		pos:    vec3{X: -half, Y: 0, Z: 0},
	}
	planetBlue := &entityState{
		id:     "planet-blue",
		kind:   entityKindPlanet,
		team:   "blue",
		radius: cfg.PlanetRadius,
		pos:    vec3{X: half, Y: 0, Z: 0},
	}
	w.entities[planetRed.id] = planetRed
	w.entities[planetBlue.id] = planetBlue

	offsets := shipSpawnOffsets(cfg.ShipOrbitRadius, cfg.ShipsPerTeam)
	for i := 0; i < cfg.ShipsPerTeam; i++ {
		off := offsets[i]
		id := fmt.Sprintf("ship-red-%02d", i+1)
		w.entities[id] = &entityState{
			id:    id,
			kind:  entityKindShip,
			team:  "red",
			pos:   vec3{X: planetRed.pos.X + off.X, Y: planetRed.pos.Y + off.Y, Z: 0},
			vel:   vec3{X: cfg.ShipSpeedPerTick, Y: 0, Z: 0},
			hp:    cfg.ShipMaxHP,
			maxHP: cfg.ShipMaxHP,
		}
	}
	for i := 0; i < cfg.ShipsPerTeam; i++ {
		off := offsets[i]
		id := fmt.Sprintf("ship-blue-%02d", i+1)
		w.entities[id] = &entityState{
			id:    id,
			kind:  entityKindShip,
			team:  "blue",
			pos:   vec3{X: planetBlue.pos.X + off.X, Y: planetBlue.pos.Y + off.Y, Z: 0},
			vel:   vec3{X: -cfg.ShipSpeedPerTick, Y: 0, Z: 0},
			hp:    cfg.ShipMaxHP,
			maxHP: cfg.ShipMaxHP,
		}
	}
}

func (w *world) stepSampleGameLocked(tick int64) []*enginev1.Event {
	cfg := w.sampleGame

	shipIDs := make([]string, 0, len(w.entities))
	for id, e := range w.entities {
		if e.kind == entityKindShip && e.maxHP > 0 {
			shipIDs = append(shipIDs, id)
		}
	}
	sort.Strings(shipIDs)

	for _, id := range shipIDs {
		if e := w.entities[id]; e != nil && e.cooldown > 0 {
			e.cooldown--
		}
	}

	type action struct {
		kind   string // "move" or "fire"
		target string
		vel    vec3
	}
	actions := make(map[string]action, len(shipIDs))

	fireRangeSq := cfg.FireRange * cfg.FireRange

	// Decide actions (deterministically).
	for _, shipID := range shipIDs {
		ship := w.entities[shipID]
		if ship == nil {
			continue
		}

		enemyID, enemyDistSq := w.nearestEnemyShipLocked(ship)
		if enemyID != "" && ship.cooldown == 0 && enemyDistSq <= fireRangeSq {
			actions[shipID] = action{kind: "fire", target: enemyID, vel: vec3{}}
			continue
		}
		if enemyID != "" {
			enemy := w.entities[enemyID]
			actions[shipID] = action{kind: "move", vel: stepToward(ship.pos, enemy.pos, cfg.ShipSpeedPerTick)}
		} else {
			actions[shipID] = action{kind: "move", vel: vec3{}}
		}
	}

	// Apply fire simultaneously (aggregate damage by target).
	damageByTarget := make(map[string]int32)
	var events []*enginev1.Event
	for _, shipID := range shipIDs {
		act, ok := actions[shipID]
		if !ok || act.kind != "fire" {
			continue
		}
		ship := w.entities[shipID]
		if ship == nil {
			continue
		}
		target := w.entities[act.target]
		if target == nil {
			continue
		}
		damageByTarget[act.target] += cfg.FireDamage
		ship.cooldown = cfg.FireCooldownTicks
		b, _ := json.Marshal(map[string]any{
			"attacker": shipID,
			"target":   act.target,
			"damage":   cfg.FireDamage,
		})
		events = append(events, &enginev1.Event{
			Type: "sample.fire",
			Tick: tick,
			Payload: &enginev1.Event_Opaque{
				Opaque: b,
			},
		})
	}

	targetIDs := make([]string, 0, len(damageByTarget))
	for id := range damageByTarget {
		targetIDs = append(targetIDs, id)
	}
	sort.Strings(targetIDs)
	for _, targetID := range targetIDs {
		target := w.entities[targetID]
		if target == nil {
			continue
		}
		target.hp -= damageByTarget[targetID]
		if target.hp <= 0 {
			delete(w.entities, targetID)
			b, _ := json.Marshal(map[string]any{
				"entity": targetID,
			})
			events = append(events, &enginev1.Event{
				Type: "sample.died",
				Tick: tick,
				Payload: &enginev1.Event_Opaque{
					Opaque: b,
				},
			})
		}
	}

	// Apply movement.
	for _, shipID := range shipIDs {
		ship := w.entities[shipID]
		if ship == nil {
			continue
		}
		act, ok := actions[shipID]
		if !ok {
			continue
		}
		if act.kind == "fire" {
			ship.vel = vec3{}
			continue
		}
		ship.vel = act.vel
		ship.pos.X += ship.vel.X
		ship.pos.Y += ship.vel.Y
	}

	// Optional: emit a one-time smoke marker once simulation advances.
	if tick == 1 {
		events = append(events, &enginev1.Event{
			Type: "sample.started",
			Tick: tick,
			Payload: &enginev1.Event_Opaque{
				Opaque: []byte("BINDERY_SMOKE_OK"),
			},
		})
	}

	return events
}

func (w *world) nearestEnemyShipLocked(ship *entityState) (string, int64) {
	if ship == nil || ship.kind != entityKindShip {
		return "", 0
	}

	bestID := ""
	var bestDistSq int64

	// Deterministic scan: sort keys.
	ids := make([]string, 0, len(w.entities))
	for id := range w.entities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := w.entities[id]
		if e == nil || e.kind != entityKindShip || e.team == ship.team {
			continue
		}
		dx := e.pos.X - ship.pos.X
		dy := e.pos.Y - ship.pos.Y
		distSq := dx*dx + dy*dy
		if bestID == "" || distSq < bestDistSq || (distSq == bestDistSq && id < bestID) {
			bestID = id
			bestDistSq = distSq
		}
	}
	return bestID, bestDistSq
}

func shipSpawnOffsets(orbit int64, count int) []vec3 {
	if count <= 0 {
		return nil
	}
	if orbit <= 0 {
		orbit = 20
	}
	out := make([]vec3, 0, count)
	for i := 0; i < count; i++ {
		angle := 2 * math.Pi * float64(i) / float64(count)
		dx := int64(math.Round(float64(orbit) * math.Cos(angle)))
		dy := int64(math.Round(float64(orbit) * math.Sin(angle)))
		out = append(out, vec3{X: dx, Y: dy, Z: 0})
	}
	return out
}

func stepToward(from, to vec3, speed int64) vec3 {
	if speed <= 0 {
		return vec3{}
	}
	dx := to.X - from.X
	dy := to.Y - from.Y
	if dx == 0 && dy == 0 {
		return vec3{}
	}
	if abs64(dx) >= abs64(dy) {
		return vec3{X: sign64(dx) * speed, Y: 0, Z: 0}
	}
	return vec3{X: 0, Y: sign64(dy) * speed, Z: 0}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func sign64(v int64) int64 {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}

func normalizeSampleGame(cfg SampleGameConfig) SampleGameConfig {
	if !cfg.Enabled {
		return SampleGameConfig{Enabled: false}
	}
	if cfg.PlanetDistance <= 0 {
		cfg.PlanetDistance = 200
	}
	if cfg.PlanetRadius <= 0 {
		cfg.PlanetRadius = 20
	}
	if cfg.ShipOrbitRadius <= 0 {
		cfg.ShipOrbitRadius = 35
	}
	if cfg.ShipsPerTeam <= 0 {
		cfg.ShipsPerTeam = 8
	}
	if cfg.ShipMaxHP <= 0 {
		cfg.ShipMaxHP = 60
	}
	if cfg.ShipSpeedPerTick <= 0 {
		cfg.ShipSpeedPerTick = 2
	}
	if cfg.FireRange <= 0 {
		cfg.FireRange = 18
	}
	if cfg.FireDamage <= 0 {
		cfg.FireDamage = 1
	}
	if cfg.FireCooldownTicks <= 0 {
		cfg.FireCooldownTicks = 10
	}
	return cfg
}

func normalizeID(s string) string {
	if s == "" {
		return ""
	}
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func commandKind(cmd *enginev1.Command) string {
	switch cmd.GetPayload().(type) {
	case *enginev1.Command_SpawnEntity:
		return "spawn"
	case *enginev1.Command_Move:
		return "move"
	case *enginev1.Command_DespawnEntity:
		return "despawn"
	case *enginev1.Command_Opaque:
		return "opaque"
	default:
		return "unknown"
	}
}
