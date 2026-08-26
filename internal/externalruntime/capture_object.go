package externalruntime

import (
	"regexp"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

// mediaTypePattern is a conservative subset of RFC 6838. The media type is
// echoed in a public manifest, so it is validated rather than trusted.
var mediaTypePattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9][a-z0-9.+-]{0,126}$`)

// PublicObjectManifest describes one heavy capture artifact -- a post-match
// dump, a replay, a crash bundle -- without describing where it is stored.
//
// There is no storage-location field, and not merely because ScanPublicOutput
// would reject one. Content addressing removed the need: the object's name is
// its hash, which is already public, so there is no second private identifier
// to leak. An earlier draft of this type carried a `private_key` that had to be
// remembered not to serialize; the design that makes the mistake impossible is
// better than the tag that catches it.
type PublicObjectManifest struct {
	SchemaVersion    string    `json:"schema_version"`
	ContentHash      string    `json:"content_hash"`
	MediaType        string    `json:"media_type"`
	Bytes            int64     `json:"bytes"`
	CaptureID        string    `json:"capture_id"`
	ProducerClientID string    `json:"producer_client_id"`
	CaptureMethod    string    `json:"capture_method"`
	ReceivedAt       time.Time `json:"received_at"`
}

// StoreCaptureObject accepts a heavy artifact on its own lane, keeping
// multi-megabyte dumps out of the hot semantic event path where they would
// share a size limit and a sequence space with ordinary observations.
func (s *Service) StoreCaptureObject(clientLease, captureID, mediaType string, data []byte) (PublicObjectManifest, error) {
	if !mediaTypePattern.MatchString(mediaType) {
		return PublicObjectManifest{}, domainError("MEDIA_TYPE_INVALID", "a concrete media type is required")
	}
	if len(data) == 0 {
		return PublicObjectManifest{}, domainError("OBJECT_EMPTY", "capture object body is empty")
	}
	if len(data) > capture.MaxObjectBytes {
		return PublicObjectManifest{}, domainError("PAYLOAD_TOO_LARGE", "capture object exceeds the negotiated limit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return PublicObjectManifest{}, err
	}
	record, ok := s.captures[captureID]
	if !ok {
		return PublicObjectManifest{}, domainError("CAPTURE_NOT_FOUND", "capture was not found")
	}
	enrollment, ok := s.enrollments[record.ProducerClientID]
	if !ok {
		return PublicObjectManifest{}, domainError("STATE_INTEGRITY_ERROR", "capture producer is not resolvable")
	}
	if !verifyCredential(clientLease, enrollment.leaseVerifier) {
		return PublicObjectManifest{}, domainError("TOKEN_INVALID", "client lease token is invalid")
	}

	now := s.now()
	contentHash := capture.DigestOf(data)
	if existing := record.objectEntry(contentHash); existing != nil {
		if existing.MediaType != mediaType {
			return PublicObjectManifest{}, domainError("SEQUENCE_CONFLICT", "these bytes were already recorded under a different media type")
		}
		return record.objectManifest(*existing), nil
	}

	before := s.snapshotLocked()
	if _, err := s.objects.Put(data); err != nil {
		return PublicObjectManifest{}, domainError("STATE_PERSISTENCE_FAILED", "capture object could not be persisted")
	}
	entry := CaptureIndexEntry{
		ContentHash: contentHash, Kind: CaptureEntryObject,
		MediaType: mediaType, Bytes: int64(len(data)), ReceivedAt: now,
	}
	record.Index = append(record.Index, entry)
	record.Objects = appendUnique(record.Objects, contentHash)
	if err := s.commitLocked(before); err != nil {
		return PublicObjectManifest{}, err
	}
	return s.captures[captureID].objectManifest(entry), nil
}

func (r *captureRecord) objectEntry(contentHash string) *CaptureIndexEntry {
	for index, entry := range r.Index {
		if entry.Kind == CaptureEntryObject && entry.ContentHash == contentHash {
			return &r.Index[index]
		}
	}
	return nil
}

func (r *captureRecord) objectManifest(entry CaptureIndexEntry) PublicObjectManifest {
	return PublicObjectManifest{
		SchemaVersion:    SchemaVersion,
		ContentHash:      entry.ContentHash,
		MediaType:        entry.MediaType,
		Bytes:            entry.Bytes,
		CaptureID:        r.CaptureID,
		ProducerClientID: r.ProducerClientID,
		CaptureMethod:    r.CaptureMethod,
		ReceivedAt:       entry.ReceivedAt,
	}
}
