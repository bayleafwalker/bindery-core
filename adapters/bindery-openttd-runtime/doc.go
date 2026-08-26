// Package openttd runs Bindery's contracts against OpenTTD, a game this
// repository does not control.
//
// It exists because the ERH-007 assessment of 2026-08-26 named its own limit:
// adapters/bindery-dedicated-runtime is a second adapter against a second
// integration mechanism, but it is not a second game, and it was written by
// someone who could read the contracts while writing it. Its findings are a
// lower bound.
//
// OpenTTD removes that caveat in the ways that matter. The game is an
// unmodified third-party binary; it has no idea Bindery exists and offers no
// hook to add one. Its integration surface was designed years before this
// project and cannot be widened to suit it: a command line, and an admin
// network protocol that is what it is. What can be observed is what the game
// chooses to publish, in the shape it chooses to publish it -- which is exactly
// the position a real integrator is in, and exactly what the dedicated runtime
// could not simulate.
//
// The differences from the dedicated runtime are the same ones ERH-007 asks a
// second runtime to have, and then some. The server owns the simulation, so
// clients observe nothing independently; the map is generated from a seed, so
// there is no shipped content to hash; observations arrive over a binary TCP
// protocol rather than being produced in-process; and the producers are admin
// applications that watch the game rather than participants in it.
package openttd

const (
	AdapterID      = "bindery.openttd-adapter"
	AdapterVersion = "0.1.0"
	GameFamily     = "openttd"

	// GameVersion is overwritten with the revision the server reports over the
	// admin network, so a run records the build it actually observed rather
	// than a version this repository asserts.
	DefaultGameVersion = "unknown"
)
