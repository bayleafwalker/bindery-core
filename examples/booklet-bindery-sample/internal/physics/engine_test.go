package physics

import (
	"testing"

	enginev1 "github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1"
)

func TestEngine_QueuedCommandsAppliedOnTick(t *testing.T) {
	e := New(Config{MaxCommandsPerTick: 1})

	worldID := "world-1"

	// Enqueue two spawns.
	if _, err := e.EnqueueCommand(worldID, &enginev1.Command{
		CommandId: "c1",
		ActorId:   "a1",
		Payload:   &enginev1.Command_SpawnEntity{SpawnEntity: &enginev1.SpawnEntityCommand{EntityId: "e1"}},
	}, false); err != nil {
		t.Fatalf("enqueue c1: %v", err)
	}
	if _, err := e.EnqueueCommand(worldID, &enginev1.Command{
		CommandId: "c2",
		ActorId:   "a1",
		Payload:   &enginev1.Command_SpawnEntity{SpawnEntity: &enginev1.SpawnEntityCommand{EntityId: "e2"}},
	}, false); err != nil {
		t.Fatalf("enqueue c2: %v", err)
	}

	// Tick once: only 1 command should apply.
	if _, _, err := e.Tick(worldID, 0, 0); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	snap, err := e.Snapshot(worldID, nil, nil, true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if got := len(snap.Entities); got != 1 {
		t.Fatalf("expected 1 entity after first tick, got %d", got)
	}

	// Tick again: second command should apply.
	if _, _, err := e.Tick(worldID, 0, 0); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	snap, err = e.Snapshot(worldID, nil, nil, true)
	if err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}
	if got := len(snap.Entities); got != 2 {
		t.Fatalf("expected 2 entities after second tick, got %d", got)
	}
}

func TestEngine_MoveUpdatesTransform(t *testing.T) {
	e := New(Config{MaxCommandsPerTick: 10})
	worldID := "world-1"

	if _, err := e.EnqueueCommand(worldID, &enginev1.Command{
		CommandId: "spawn",
		ActorId:   "a1",
		Payload:   &enginev1.Command_SpawnEntity{SpawnEntity: &enginev1.SpawnEntityCommand{EntityId: "e1"}},
	}, false); err != nil {
		t.Fatalf("enqueue spawn: %v", err)
	}
	if _, _, err := e.Tick(worldID, 0, 0); err != nil {
		t.Fatalf("tick spawn: %v", err)
	}

	if _, err := e.EnqueueCommand(worldID, &enginev1.Command{
		CommandId: "move",
		ActorId:   "a1",
		Payload: &enginev1.Command_Move{Move: &enginev1.MoveCommand{
			EntityId: "e1",
			Position: &enginev1.Vec3{X: 1, Y: 2, Z: 3},
		}},
	}, false); err != nil {
		t.Fatalf("enqueue move: %v", err)
	}
	if _, _, err := e.Tick(worldID, 0, 0); err != nil {
		t.Fatalf("tick move: %v", err)
	}

	snap, err := e.Snapshot(worldID, nil, nil, true)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(snap.Entities))
	}
	var gotPos *enginev1.Vec3
	for _, c := range snap.Entities[0].Components {
		if c.GetTransform() != nil {
			gotPos = c.GetTransform().Position
			break
		}
	}
	if gotPos == nil || gotPos.X != 1 || gotPos.Y != 2 || gotPos.Z != 3 {
		t.Fatalf("expected position (1,2,3), got %+v", gotPos)
	}
}
