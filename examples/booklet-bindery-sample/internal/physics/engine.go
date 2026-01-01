package physics

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
)

type Config struct {
	// MaxCommandsPerTick bounds how many queued commands are applied per tick step.
	// If <= 0, a safe default is used.
	MaxCommandsPerTick int
}

type Engine struct {
	mu                 sync.Mutex
	worlds             map[string]*world
	maxCommandsPerTick int
}

func New(cfg Config) *Engine {
	maxCommandsPerTick := cfg.MaxCommandsPerTick
	if maxCommandsPerTick <= 0 {
		maxCommandsPerTick = 16
	}
	return &Engine{
		worlds:             make(map[string]*world),
		maxCommandsPerTick: maxCommandsPerTick,
	}
}

func (e *Engine) InitializeWorld(worldID string) (int64, error) {
	worldID = normalizeID(worldID)
	if worldID == "" {
		return 0, errors.New("worldID is empty")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	w := newWorld(e.maxCommandsPerTick)
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
	w := newWorld(e.maxCommandsPerTick)
	e.worlds[worldID] = w
	return w
}

type world struct {
	mu                 sync.Mutex
	tick               int64
	entities           map[string]*enginev1.Entity
	queue              []*enginev1.Command
	seenCommandIDs     map[string]struct{}
	nextGeneratedID    int64
	maxCommandsPerTick int
}

func newWorld(maxCommandsPerTick int) *world {
	if maxCommandsPerTick <= 0 {
		maxCommandsPerTick = 16
	}
	return &world{
		entities:           make(map[string]*enginev1.Entity),
		queue:              nil,
		seenCommandIDs:     make(map[string]struct{}),
		nextGeneratedID:    1,
		maxCommandsPerTick: maxCommandsPerTick,
	}
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

	entities := make([]*enginev1.Entity, 0, len(w.entities))
	for id, e := range w.entities {
		if len(allow) > 0 {
			if _, ok := allow[id]; !ok {
				continue
			}
		}
		entities = append(entities, cloneEntity(e, includeComponents))
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
		w.entities[id] = &enginev1.Entity{
			EntityId: id,
			Type:     "demo",
			Components: []*enginev1.Component{
				{
					Type: "transform",
					Payload: &enginev1.Component_Transform{
						Transform: &enginev1.TransformComponent{
							Position:      &enginev1.Vec3{X: 0, Y: 0, Z: 0},
							RotationEuler: &enginev1.Vec3{X: 0, Y: 0, Z: 0},
							Scale:         &enginev1.Vec3{X: 1, Y: 1, Z: 1},
						},
					},
				},
			},
			Metadata: map[string]string{
				"spawnedBy": normalizeID(cmd.GetActorId()),
			},
		}
		return nil
	case *enginev1.Command_Move:
		entityID := normalizeID(p.Move.GetEntityId())
		e, ok := w.entities[entityID]
		if !ok {
			return fmt.Errorf("entity %q not found", entityID)
		}
		pos := p.Move.GetPosition()
		if pos == nil {
			return errors.New("move.position is nil")
		}
		setEntityPosition(e, pos)
		return nil
	case *enginev1.Command_DespawnEntity:
		entityID := normalizeID(p.DespawnEntity.GetEntityId())
		if _, ok := w.entities[entityID]; !ok {
			return fmt.Errorf("entity %q not found", entityID)
		}
		delete(w.entities, entityID)
		return nil
	case *enginev1.Command_Opaque:
		return nil
	default:
		return errors.New("command payload is missing or unknown")
	}
}

func (w *world) generateEntityIDLocked() string {
	id := w.nextGeneratedID
	w.nextGeneratedID++
	return fmt.Sprintf("e-%d", id)
}

func setEntityPosition(e *enginev1.Entity, pos *enginev1.Vec3) {
	if e == nil {
		return
	}
	// Find existing transform component.
	for _, c := range e.Components {
		if c == nil {
			continue
		}
		if t := c.GetTransform(); t != nil {
			if t.Position == nil {
				t.Position = &enginev1.Vec3{}
			}
			t.Position.X = pos.X
			t.Position.Y = pos.Y
			t.Position.Z = pos.Z
			return
		}
	}
	// No transform found; add one.
	e.Components = append(e.Components, &enginev1.Component{
		Type: "transform",
		Payload: &enginev1.Component_Transform{
			Transform: &enginev1.TransformComponent{
				Position:      &enginev1.Vec3{X: pos.X, Y: pos.Y, Z: pos.Z},
				RotationEuler: &enginev1.Vec3{X: 0, Y: 0, Z: 0},
				Scale:         &enginev1.Vec3{X: 1, Y: 1, Z: 1},
			},
		},
	})
}

func cloneEntity(e *enginev1.Entity, includeComponents bool) *enginev1.Entity {
	if e == nil {
		return nil
	}
	out := &enginev1.Entity{
		EntityId: e.GetEntityId(),
		Type:     e.GetType(),
		Metadata: cloneStringMap(e.GetMetadata()),
	}
	if includeComponents {
		out.Components = cloneComponents(e.GetComponents())
	}
	return out
}

func cloneComponents(in []*enginev1.Component) []*enginev1.Component {
	if len(in) == 0 {
		return nil
	}
	out := make([]*enginev1.Component, 0, len(in))
	for _, c := range in {
		out = append(out, cloneComponent(c))
	}
	return out
}

func cloneComponent(c *enginev1.Component) *enginev1.Component {
	if c == nil {
		return nil
	}
	out := &enginev1.Component{Type: c.GetType()}
	switch p := c.GetPayload().(type) {
	case *enginev1.Component_Transform:
		out.Payload = &enginev1.Component_Transform{Transform: cloneTransform(p.Transform)}
	case *enginev1.Component_Health:
		if p.Health != nil {
			out.Payload = &enginev1.Component_Health{Health: &enginev1.HealthComponent{Current: p.Health.Current, Max: p.Health.Max}}
		}
	case *enginev1.Component_Opaque:
		b := make([]byte, len(p.Opaque))
		copy(b, p.Opaque)
		out.Payload = &enginev1.Component_Opaque{Opaque: b}
	}
	return out
}

func cloneTransform(t *enginev1.TransformComponent) *enginev1.TransformComponent {
	if t == nil {
		return nil
	}
	return &enginev1.TransformComponent{
		Position:      cloneVec3(t.Position),
		RotationEuler: cloneVec3(t.RotationEuler),
		Scale:         cloneVec3(t.Scale),
	}
}

func cloneVec3(v *enginev1.Vec3) *enginev1.Vec3 {
	if v == nil {
		return nil
	}
	return &enginev1.Vec3{X: v.X, Y: v.Y, Z: v.Z}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
