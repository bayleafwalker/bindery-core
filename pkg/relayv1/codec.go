package relayv1

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	Version              byte = 1
	HeaderBytes               = 98
	DefaultDatagramLimit      = 1400
	TransportKeyBytes         = 32
)

var magic = [4]byte{'B', 'R', 'L', 'Y'}

type PacketType byte

const (
	PacketData      PacketType = 1
	PacketRegister  PacketType = 2
	PacketHeartbeat PacketType = 3
)

type Packet struct {
	Type         PacketType
	AllocationID string
	SenderID     string
	RecipientID  string
	Sequence     uint64
	Payload      []byte
}

type Header struct {
	Type         PacketType
	AllocationID string
	SenderID     string
	RecipientID  string
	Sequence     uint64
	PayloadBytes int
}

var (
	ErrMalformed      = errors.New("malformed relay datagram")
	ErrUnsupported    = errors.New("unsupported relay packet")
	ErrOversized      = errors.New("relay datagram exceeds configured limit")
	ErrInvalidKey     = errors.New("transport key must be 32 bytes")
	ErrAuthentication = errors.New("relay datagram authentication failed")
)

func Encode(packet Packet, transportKey []byte, datagramLimit int) ([]byte, error) {
	if len(transportKey) != TransportKeyBytes {
		return nil, ErrInvalidKey
	}
	if packet.Type != PacketData && packet.Type != PacketRegister && packet.Type != PacketHeartbeat {
		return nil, ErrUnsupported
	}
	if len(packet.Payload) > 0xffff {
		return nil, ErrOversized
	}
	allocation, err := parseUUID(packet.AllocationID)
	if err != nil {
		return nil, fmt.Errorf("allocation id: %w", err)
	}
	sender, err := parseUUID(packet.SenderID)
	if err != nil {
		return nil, fmt.Errorf("sender id: %w", err)
	}
	recipient, err := parseUUID(packet.RecipientID)
	if err != nil {
		return nil, fmt.Errorf("recipient id: %w", err)
	}
	if datagramLimit <= 0 {
		datagramLimit = DefaultDatagramLimit
	}
	if HeaderBytes+len(packet.Payload) > datagramLimit {
		return nil, ErrOversized
	}
	data := make([]byte, HeaderBytes+len(packet.Payload))
	copy(data[0:4], magic[:])
	data[4] = Version
	data[5] = byte(packet.Type)
	copy(data[8:24], allocation[:])
	copy(data[24:40], sender[:])
	copy(data[40:56], recipient[:])
	binary.BigEndian.PutUint64(data[56:64], packet.Sequence)
	binary.BigEndian.PutUint16(data[64:66], uint16(len(packet.Payload)))
	copy(data[HeaderBytes:], packet.Payload)
	mac := hmac.New(sha256.New, transportKey)
	_, _ = mac.Write(data[:66])
	_, _ = mac.Write(data[HeaderBytes:])
	copy(data[66:98], mac.Sum(nil))
	return data, nil
}

func Peek(data []byte, datagramLimit int) (Header, error) {
	if datagramLimit <= 0 {
		datagramLimit = DefaultDatagramLimit
	}
	if len(data) < HeaderBytes || len(data) > datagramLimit {
		if len(data) > datagramLimit {
			return Header{}, ErrOversized
		}
		return Header{}, ErrMalformed
	}
	if string(data[0:4]) != string(magic[:]) || data[4] != Version {
		return Header{}, ErrMalformed
	}
	if data[6] != 0 || data[7] != 0 {
		return Header{}, ErrMalformed
	}
	packetType := PacketType(data[5])
	if packetType != PacketData && packetType != PacketRegister && packetType != PacketHeartbeat {
		return Header{}, ErrUnsupported
	}
	payloadBytes := int(binary.BigEndian.Uint16(data[64:66]))
	if HeaderBytes+payloadBytes != len(data) {
		return Header{}, ErrMalformed
	}
	allocation, err := formatUUID(data[8:24])
	if err != nil {
		return Header{}, err
	}
	sender, err := formatUUID(data[24:40])
	if err != nil {
		return Header{}, err
	}
	recipient, err := formatUUID(data[40:56])
	if err != nil {
		return Header{}, err
	}
	return Header{Type: packetType, AllocationID: allocation, SenderID: sender, RecipientID: recipient, Sequence: binary.BigEndian.Uint64(data[56:64]), PayloadBytes: payloadBytes}, nil
}

func Decode(data, transportKey []byte, datagramLimit int) (Packet, error) {
	if len(transportKey) != TransportKeyBytes {
		return Packet{}, ErrInvalidKey
	}
	header, err := Peek(data, datagramLimit)
	if err != nil {
		return Packet{}, err
	}
	mac := hmac.New(sha256.New, transportKey)
	_, _ = mac.Write(data[:66])
	_, _ = mac.Write(data[HeaderBytes:])
	if !hmac.Equal(mac.Sum(nil), data[66:98]) {
		return Packet{}, ErrAuthentication
	}
	payload := append([]byte(nil), data[HeaderBytes:]...)
	return Packet{Type: header.Type, AllocationID: header.AllocationID, SenderID: header.SenderID, RecipientID: header.RecipientID, Sequence: header.Sequence, Payload: payload}, nil
}

func parseUUID(value string) ([16]byte, error) {
	var result [16]byte
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return result, errors.New("must be canonical UUID")
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 {
		return result, errors.New("must be canonical UUID")
	}
	copy(result[:], decoded)
	return result, nil
}

func formatUUID(value []byte) (string, error) {
	if len(value) != 16 {
		return "", ErrMalformed
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(value[0:4]), hex.EncodeToString(value[4:6]), hex.EncodeToString(value[6:8]), hex.EncodeToString(value[8:10]), hex.EncodeToString(value[10:16])), nil
}
