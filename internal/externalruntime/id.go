package externalruntime

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"
)

// newUUIDv7 creates a RFC 9562 UUID version 7 using millisecond Unix time and
// cryptographically random remainder bits. The service does not accept IDs
// supplied by clients for security-sensitive records.
func newUUIDv7(now time.Time) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ms := uint64(now.UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	_ = binary.BigEndian // documents the UUID byte order used above
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(b[0:4]), binary.BigEndian.Uint16(b[4:6]),
		binary.BigEndian.Uint16(b[6:8]), binary.BigEndian.Uint16(b[8:10]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15])), nil
}
