package openttd

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// This file implements OpenTTD's admin network protocol from its published
// specification (docs/admin_network.md and src/network/core/tcp_admin.h in the
// OpenTTD tree). Nothing here is negotiated with the game's authors and nothing
// in the game was changed to accommodate Bindery: the protocol is what a
// third-party server-management tool would have to speak, and the packets below
// are the ones the game chooses to publish.
//
// The wire format is a uint16 little-endian length that counts itself and the
// one-byte packet type, followed by the payload. Strings are NUL-terminated
// UTF-8; booleans are a single byte.

// Packets an admin application sends.
const (
	adminJoin            = 0
	adminQuit            = 1
	adminUpdateFrequency = 2
	adminPoll            = 3
	adminRcon            = 5
)

// Packets the server sends. Every server packet is numbered from 100.
const (
	serverFull           = 100
	serverBanned         = 101
	serverError          = 102
	serverProtocol       = 103
	serverWelcome        = 104
	serverNewGame        = 105
	serverShutdown       = 106
	serverDate           = 107
	serverClientJoin     = 108
	serverClientInfo     = 109
	serverClientUpdate   = 110
	serverClientQuit     = 111
	serverClientError    = 112
	serverCompanyNew     = 113
	serverCompanyInfo    = 114
	serverCompanyUpdate  = 115
	serverCompanyRemove  = 116
	serverCompanyEconomy = 117
	serverCompanyStats   = 118
	serverChat           = 119
	serverRcon           = 120
	serverConsole        = 121
	serverCmdNames       = 122
	serverRconEnd        = 125
	serverPong           = 126
	serverCmdLogging     = 127
)

// AdminUpdateType values, as the game numbers them.
const (
	UpdateDate       uint16 = 0
	UpdateClientInfo uint16 = 1
	UpdateCompanyNew uint16 = 2
	UpdateChat       uint16 = 5
	UpdateConsole    uint16 = 6
	UpdateCmdLogging uint16 = 8
)

// AdminUpdateFrequency is a bitset in the protocol, not an enum.
const (
	FrequencyPoll      uint16 = 1 << 0
	FrequencyDaily     uint16 = 1 << 1
	FrequencyAutomatic uint16 = 1 << 6
)

const maxPacketSize = 32767

// Admin is one admin-network connection. Two of them observing the same server
// is what this runtime has instead of two clients that each simulate.
type Admin struct {
	name   string
	conn   net.Conn
	reader *bufio.Reader
}

// Welcome is what the server says about the game once an admin is authorised.
// It is the game's own identity claim, and it is the only content identity this
// runtime has: an OpenTTD map is generated from a seed on the server, so there
// is nothing shipped for participants to hash and agree on.
type Welcome struct {
	ServerName  string
	Revision    string
	Dedicated   bool
	Seed        uint32
	Landscape   uint8
	StartDate   uint32
	MapWidth    uint16
	MapHeight   uint16
	AdminAPIVer uint8
}

// DialAdmin opens an admin connection and authenticates with a password.
func DialAdmin(name, address, password string) (*Admin, Welcome, error) {
	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return nil, Welcome{}, fmt.Errorf("dial admin port: %w", err)
	}
	admin := &Admin{name: name, conn: conn, reader: bufio.NewReader(conn)}
	welcome, err := admin.join(password)
	if err != nil {
		conn.Close()
		return nil, Welcome{}, err
	}
	return admin, welcome, nil
}

// Name is the admin application name this connection announced. It is the
// producer identity from the game's point of view.
func (a *Admin) Name() string { return a.name }

func (a *Admin) join(password string) (Welcome, error) {
	// The insecure JOIN is used deliberately: the alternative is an X25519 key
	// exchange, and re-implementing a handshake is not what is under test here.
	// The server refuses it unless allow_insecure_admin_login is set, which is
	// a server-side opt-in this runtime's own config makes over loopback only.
	body := encodeString(password)
	body = append(body, encodeString(a.name)...)
	body = append(body, encodeString(AdapterVersion)...)
	if err := a.send(adminJoin, body); err != nil {
		return Welcome{}, err
	}

	var welcome Welcome
	var gotProtocol bool
	for {
		kind, payload, err := a.Receive(15 * time.Second)
		if err != nil {
			return Welcome{}, err
		}
		switch kind {
		case serverProtocol:
			reader := newPayload(payload)
			welcome.AdminAPIVer = reader.uint8()
			gotProtocol = true
		case serverWelcome:
			reader := newPayload(payload)
			welcome.ServerName = reader.string()
			welcome.Revision = reader.string()
			welcome.Dedicated = reader.bool()
			reader.string() // used to be the map name; the game sends it empty
			welcome.Seed = reader.uint32()
			welcome.Landscape = reader.uint8()
			welcome.StartDate = reader.uint32()
			welcome.MapWidth = reader.uint16()
			welcome.MapHeight = reader.uint16()
			if !gotProtocol {
				return Welcome{}, errors.New("welcome arrived before protocol")
			}
			return welcome, reader.err
		case serverFull, serverBanned:
			return Welcome{}, fmt.Errorf("admin connection refused (packet %d)", kind)
		case serverError:
			return Welcome{}, fmt.Errorf("admin join refused with network error %d", newPayload(payload).uint8())
		}
	}
}

// MarkRun connects an admin application and immediately leaves. The game
// announces both on its console, which every already-subscribed observer sees,
// so a connect-and-leave is a fact in the game's history that observers can
// agree to start from without any of them having to trust a clock.
func MarkRun(name, address, password string) error {
	admin, _, err := DialAdmin(name, address, password)
	if err != nil {
		return err
	}
	return admin.Quit()
}

// RunMarker matches the console line the game prints when the named admin
// application connects.
func RunMarker(name string) func(Observation) bool {
	needle := "'" + name + "' (" + AdapterVersion + ") has connected"
	return func(observation Observation) bool {
		if observation.Kind != "openttd.console" {
			return false
		}
		var fields struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(observation.Payload, &fields); err != nil {
			return false
		}
		return strings.Contains(fields.Text, needle)
	}
}

// Subscribe asks for an update type at a frequency. The server confirms
// nothing: an unsupported pair simply disconnects the admin.
func (a *Admin) Subscribe(updateType, frequency uint16) error {
	body := binary.LittleEndian.AppendUint16(nil, updateType)
	body = binary.LittleEndian.AppendUint16(body, frequency)
	return a.send(adminUpdateFrequency, body)
}

// Poll asks for one update immediately. The update type is a uint8 here and a
// uint16 in Subscribe; that asymmetry is the protocol's, and the game's own
// documentation calls it a legacy gotcha.
func (a *Admin) Poll(updateType uint8, extra uint32) error {
	body := []byte{updateType}
	body = binary.LittleEndian.AppendUint32(body, extra)
	return a.send(adminPoll, body)
}

// Rcon runs a console command on the server as the admin.
func (a *Admin) Rcon(command string) error {
	return a.send(adminRcon, encodeString(command))
}

// Quit says goodbye, which the protocol asks for but does not require.
func (a *Admin) Quit() error {
	err := a.send(adminQuit, nil)
	closeErr := a.conn.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Close drops the connection without a goodbye.
func (a *Admin) Close() error { return a.conn.Close() }

// Receive reads one packet. A deadline of zero blocks indefinitely.
func (a *Admin) Receive(timeout time.Duration) (kind byte, payload []byte, err error) {
	if timeout > 0 {
		if err := a.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return 0, nil, err
		}
	}
	var header [3]byte
	if _, err := io.ReadFull(a.reader, header[:]); err != nil {
		return 0, nil, err
	}
	size := int(binary.LittleEndian.Uint16(header[:2]))
	if size < 3 || size > maxPacketSize {
		return 0, nil, fmt.Errorf("admin packet claims %d bytes", size)
	}
	body := make([]byte, size-3)
	if _, err := io.ReadFull(a.reader, body); err != nil {
		return 0, nil, err
	}
	return header[2], body, nil
}

func (a *Admin) send(kind byte, body []byte) error {
	size := len(body) + 3
	if size > maxPacketSize {
		return fmt.Errorf("admin packet of %d bytes exceeds the protocol maximum", size)
	}
	packet := binary.LittleEndian.AppendUint16(make([]byte, 0, size), uint16(size))
	packet = append(packet, kind)
	packet = append(packet, body...)
	if err := a.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, err := a.conn.Write(packet)
	return err
}

func encodeString(value string) []byte {
	// The game reads NUL-terminated strings; an embedded NUL would truncate.
	return append([]byte(strings.ReplaceAll(value, "\x00", "")), 0)
}

// payload is a cursor over one packet's body. It records the first error and
// then yields zero values, so a decoder can read fields in order and check once.
type payload struct {
	data   []byte
	offset int
	err    error
}

func newPayload(data []byte) *payload { return &payload{data: data} }

func (p *payload) take(count int) []byte {
	if p.err != nil {
		return nil
	}
	if p.offset+count > len(p.data) {
		p.err = fmt.Errorf("admin packet truncated: wanted %d bytes at offset %d of %d", count, p.offset, len(p.data))
		return nil
	}
	slice := p.data[p.offset : p.offset+count]
	p.offset += count
	return slice
}

func (p *payload) uint8() uint8 {
	slice := p.take(1)
	if slice == nil {
		return 0
	}
	return slice[0]
}

func (p *payload) bool() bool { return p.uint8() != 0 }

func (p *payload) uint16() uint16 {
	slice := p.take(2)
	if slice == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(slice)
}

func (p *payload) uint32() uint32 {
	slice := p.take(4)
	if slice == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(slice)
}

func (p *payload) string() string {
	if p.err != nil {
		return ""
	}
	end := p.offset
	for end < len(p.data) && p.data[end] != 0 {
		end++
	}
	if end == len(p.data) {
		p.err = fmt.Errorf("admin packet ended inside a string at offset %d", p.offset)
		return ""
	}
	value := string(p.data[p.offset:end])
	p.offset = end + 1
	return value
}
