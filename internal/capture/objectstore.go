package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

type PublicObjectManifest struct {
	ContentHash string `json:"content_hash"`
	MediaType   string `json:"media_type"`
	Bytes       int    `json:"bytes"`
	CaptureID   string `json:"capture_id"`
	ProducerID  string `json:"producer_id"`
	PrivateKey  string `json:"-"`
}

type ObjectStore struct {
	mu        sync.RWMutex
	objects   map[string][]byte
	manifests map[string]PublicObjectManifest
}

func NewObjectStore() *ObjectStore {
	return &ObjectStore{objects: make(map[string][]byte), manifests: make(map[string]PublicObjectManifest)}
}

func (s *ObjectStore) Put(captureID, producerID, mediaType string, data []byte) (PublicObjectManifest, error) {
	if len(data) == 0 || mediaType == "" {
		return PublicObjectManifest{}, errors.New("object content and media type are required")
	}
	hash := sha256.Sum256(data)
	contentHash := "sha256:" + hex.EncodeToString(hash[:])
	manifest := PublicObjectManifest{ContentHash: contentHash, MediaType: mediaType, Bytes: len(data), CaptureID: captureID, ProducerID: producerID, PrivateKey: "private/" + contentHash}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[contentHash]; !exists {
		s.objects[contentHash] = append([]byte(nil), data...)
	}
	s.manifests[contentHash] = manifest
	return manifest, nil
}

func (s *ObjectStore) Get(contentHash string) ([]byte, PublicObjectManifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.objects[contentHash]
	if !ok {
		return nil, PublicObjectManifest{}, errors.New("object not found")
	}
	return append([]byte(nil), data...), s.manifests[contentHash], nil
}
