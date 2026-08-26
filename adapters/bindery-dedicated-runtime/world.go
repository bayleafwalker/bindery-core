// Package dedicated is a second external runtime for Bindery, written to test
// the control plane's abstractions rather than to be a game.
//
// It differs from the RA2 adapter in the way ERH-007 asks a second runtime to
// differ. ADR-001 says clients own the simulation: in Red Alert 2 every client
// runs the same lockstep world, so every client is an independent observer and
// the relay carries opaque peer traffic. Here the server owns the simulation.
// Clients submit intents and receive state; they observe nothing the server did
// not tell them, so the server is the only honest producer of observations.
//
// The world is generated from a seed rather than shipped as content, so there
// is no mod and no map to identify or hash.
package dedicated

import (
	"encoding/json"
	"fmt"
)

// World is a deterministic tick-based simulation. Determinism is what makes it
// useful here: the same seed and tick count produce the same events, so a
// restart drill can assert that what survived is what was produced.
type World struct {
	seed    uint64
	depots  int
	tick    uint64
	convoys []convoy
}

type convoy struct {
	id       int
	position int
	cargo    uint64
}

// Event is one thing the server decided happened. It is the runtime's own
// vocabulary; nothing in Bindery core knows these names.
type Event struct {
	Type    string
	Tick    uint64
	Payload json.RawMessage
}

func NewWorld(seed uint64, depots, convoys int) *World {
	world := &World{seed: seed, depots: depots}
	for index := 0; index < convoys; index++ {
		world.convoys = append(world.convoys, convoy{id: index, position: index % depots})
	}
	return world
}

// Tick advances the world one step and returns what the server observed. A
// client cannot produce these: it does not run the simulation.
func (w *World) Tick() ([]Event, error) {
	w.tick++
	events := make([]Event, 0, len(w.convoys))
	for index := range w.convoys {
		unit := &w.convoys[index]
		// A cheap deterministic mix. The point is reproducibility, not realism.
		w.seed = w.seed*6364136223846793005 + 1442695040888963407
		delivered := (w.seed >> 33) % 7
		unit.cargo += delivered
		unit.position = (unit.position + 1) % w.depots
		payload, err := json.Marshal(map[string]any{
			"convoy":    unit.id,
			"depot":     unit.position,
			"delivered": delivered,
			"cargo":     unit.cargo,
		})
		if err != nil {
			return nil, fmt.Errorf("encode convoy event: %w", err)
		}
		events = append(events, Event{Type: "dedicated.convoy-arrived", Tick: w.tick, Payload: payload})
	}
	return events, nil
}

// Run advances the world and flattens every tick's observations in order.
func (w *World) Run(ticks int) ([]Event, error) {
	var all []Event
	for step := 0; step < ticks; step++ {
		events, err := w.Tick()
		if err != nil {
			return nil, err
		}
		all = append(all, events...)
	}
	return all, nil
}
