package openttd

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// An Observation is one thing the game told an admin application happened. The
// vocabulary is OpenTTD's, not Bindery's: nothing in the control plane knows
// these names, which is the property ERH-007 is testing.
type Observation struct {
	Kind string
	// GameTick is filled only for command packets, which carry the frame the
	// command executed on. Everything else in this protocol is announced
	// without a tick, so the field is null -- the shape core has to tolerate
	// from any runtime that is not organised around a tick.
	GameTick *uint64
	Payload  json.RawMessage
}

// Observer is one admin connection recording what the server tells it. Two of
// these on the same server are two independent producers in Bindery's sense:
// separate processes' worth of state, separate sockets, separate decoders, and
// neither can see what the other received.
type Observer struct {
	admin *Admin

	mutex        sync.Mutex
	observations []Observation
	failure      error
	done         chan struct{}
	startAfter   func(Observation) bool
	armed        bool
	stopWhen     func(Observation) bool
}

// NewObserver takes ownership of an authenticated admin connection.
func NewObserver(admin *Admin) *Observer {
	return &Observer{admin: admin, done: make(chan struct{})}
}

// Subscribe registers for the event-driven updates this runtime observes.
//
// Time-driven updates are deliberately absent. Registering ADMIN_UPDATE_DATE
// on a daily frequency would make each observer's event count depend on when
// its socket happened to be registered, which is a property of the observer
// rather than of the game.
func (o *Observer) Subscribe() error {
	for _, subscription := range []struct {
		updateType uint16
		frequency  uint16
	}{
		{UpdateClientInfo, FrequencyAutomatic},
		{UpdateCompanyNew, FrequencyAutomatic},
		{UpdateChat, FrequencyAutomatic},
		{UpdateConsole, FrequencyAutomatic},
		{UpdateCmdLogging, FrequencyAutomatic},
	} {
		if err := o.admin.Subscribe(subscription.updateType, subscription.frequency); err != nil {
			return fmt.Errorf("subscribe to update %d: %w", subscription.updateType, err)
		}
	}
	return nil
}

// Start records observations between two facts in the game's own history.
//
// Recording begins after startAfter matches -- the matching observation is
// discarded, because it is the marker rather than part of the run -- and ends
// with the observation stopWhen matches, which is kept. Bounding the recording
// by game facts rather than by wall-clock time is what lets two observers that
// connected at different moments record the same interval; an observer that
// simply recorded from its own connection onwards would also record its own
// arrival, and its own arrival is exactly what the other observer cannot see.
// A nil startAfter records from the connection onwards.
func (o *Observer) Start(startAfter, stopWhen func(Observation) bool) {
	o.startAfter = startAfter
	o.armed = startAfter == nil
	o.stopWhen = stopWhen
	go o.run()
}

func (o *Observer) run() {
	defer close(o.done)
	for {
		kind, body, err := o.admin.Receive(120 * time.Second)
		if err != nil {
			o.mutex.Lock()
			o.failure = err
			o.mutex.Unlock()
			return
		}
		observation, ok, err := decode(kind, body)
		if err != nil {
			o.mutex.Lock()
			o.failure = err
			o.mutex.Unlock()
			return
		}
		if !ok {
			continue
		}
		if !o.armed {
			if o.startAfter(observation) {
				o.armed = true
			}
			continue
		}
		o.mutex.Lock()
		o.observations = append(o.observations, observation)
		o.mutex.Unlock()
		if o.stopWhen != nil && o.stopWhen(observation) {
			return
		}
	}
}

// Wait blocks until the observer stopped, and reports what it recorded.
func (o *Observer) Wait(within time.Duration) ([]Observation, error) {
	select {
	case <-o.done:
	case <-time.After(within):
		return o.Snapshot(), fmt.Errorf("observer %s did not reach its stopping observation within %s", o.admin.Name(), within)
	}
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if o.failure != nil {
		return o.observations, fmt.Errorf("observer %s: %w", o.admin.Name(), o.failure)
	}
	return o.observations, nil
}

// Snapshot copies what has been recorded so far.
func (o *Observer) Snapshot() []Observation {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return append([]Observation(nil), o.observations...)
}

// Close ends the admin connection, which also ends the recording goroutine.
func (o *Observer) Close() { _ = o.admin.Close() }

// decode turns one admin packet into an observation. Packets that are protocol
// mechanics rather than game facts are dropped, as the protocol's own
// documentation asks unknown packets to be.
func decode(kind byte, body []byte) (Observation, bool, error) {
	reader := newPayload(body)
	var (
		name    string
		fields  map[string]any
		gameTic *uint64
	)
	switch kind {
	case serverClientJoin:
		name = "openttd.client-joined"
		fields = map[string]any{"client_id": reader.uint32()}
	case serverClientInfo:
		clientID := reader.uint32()
		// The client's network address is read and deliberately discarded: it
		// is the one field in this protocol that identifies a human's machine,
		// and an evidence record does not need it to be evidence.
		reader.string()
		clientName := reader.string()
		reader.uint8() // used to be language
		joinDate := reader.uint32()
		company := reader.uint8()
		name = "openttd.client-info"
		fields = map[string]any{"client_id": clientID, "client_name": clientName, "join_date": joinDate, "company": company}
	case serverClientUpdate:
		name = "openttd.client-updated"
		fields = map[string]any{"client_id": reader.uint32(), "client_name": reader.string(), "company": reader.uint8()}
	case serverClientQuit:
		name = "openttd.client-quit"
		fields = map[string]any{"client_id": reader.uint32()}
	case serverClientError:
		name = "openttd.client-error"
		fields = map[string]any{"client_id": reader.uint32(), "error": reader.uint8()}
	case serverCompanyNew:
		name = "openttd.company-new"
		fields = map[string]any{"company_id": reader.uint8()}
	case serverCompanyInfo:
		companyID := reader.uint8()
		companyName := reader.string()
		manager := reader.string()
		colour := reader.uint8()
		protected := reader.bool()
		inaugurated := reader.uint32()
		isAI := reader.bool()
		name = "openttd.company-info"
		fields = map[string]any{
			"company_id": companyID, "company_name": companyName, "manager": manager,
			"colour": colour, "protected": protected, "inaugurated_year": inaugurated, "is_ai": isAI,
		}
	case serverCompanyUpdate:
		name = "openttd.company-updated"
		fields = map[string]any{
			"company_id": reader.uint8(), "company_name": reader.string(), "manager": reader.string(),
			"colour": reader.uint8(), "protected": reader.bool(), "quarters_of_bankruptcy": reader.uint8(),
		}
	case serverCompanyRemove:
		name = "openttd.company-removed"
		fields = map[string]any{"company_id": reader.uint8(), "reason": reader.uint8()}
	case serverChat:
		name = "openttd.chat"
		fields = map[string]any{
			"action": reader.uint8(), "destination_type": reader.uint8(),
			"client_id": reader.uint32(), "message": reader.string(),
		}
	case serverConsole:
		name = "openttd.console"
		fields = map[string]any{"origin": reader.string(), "text": reader.string()}
	case serverCmdLogging:
		clientID := reader.uint32()
		company := reader.uint8()
		commandID := reader.uint16()
		// The command's encoded parameters are variable-length and explicitly
		// unstable across game versions, so their length is recorded and their
		// content is not.
		remaining := len(body) - reader.offset
		if remaining < 4 {
			return Observation{}, false, fmt.Errorf("command packet has %d bytes left, want at least a frame", remaining)
		}
		parameterBytes := remaining - 4
		reader.take(parameterBytes)
		frame := uint64(reader.uint32())
		gameTic = &frame
		name = "openttd.command"
		fields = map[string]any{
			"client_id": clientID, "company": company, "command_id": commandID,
			"parameter_bytes": parameterBytes,
		}
	case serverNewGame:
		name = "openttd.new-game"
		fields = map[string]any{}
	case serverShutdown:
		name = "openttd.server-shutdown"
		fields = map[string]any{}
	default:
		return Observation{}, false, nil
	}
	if reader.err != nil {
		return Observation{}, false, fmt.Errorf("decode %s: %w", name, reader.err)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return Observation{}, false, err
	}
	return Observation{Kind: name, GameTick: gameTic, Payload: encoded}, true, nil
}
