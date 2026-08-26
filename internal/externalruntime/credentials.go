package externalruntime

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
)

var errRandom = errors.New("secure random source unavailable")

func newCredential() (string, []byte, error) {
	plainBytes := make([]byte, 32)
	if _, err := rand.Read(plainBytes); err != nil {
		return "", nil, errors.Join(errRandom, err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(plainBytes)
	// The bearer presented on the wire is the base64url text. Hash that exact
	// value so storage never needs to retain or reconstruct the raw token bytes.
	hash := sha256.Sum256([]byte(encoded))
	return encoded, hash[:], nil
}

func verifyCredential(plain string, verifier []byte) bool {
	hash := sha256.Sum256([]byte(plain))
	var stored [sha256.Size]byte
	copy(stored[:], verifier)
	lengthEqual := subtle.ConstantTimeEq(int32(len(verifier)), int32(len(stored)))
	return subtle.ConstantTimeCompare(hash[:], stored[:]) == 1 && lengthEqual == 1
}
