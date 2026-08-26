package capture

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// FileObjectStore is the durable half of the capture plane. Capture volume is
// unbounded relative to control-plane state -- the reference run recorded 6,651
// events per participant -- so event bytes must not ride in the control
// snapshot, which is rewritten in full on every mutation.
//
// Everything here is content-addressed and immutable: batch bodies, heavy
// capture artifacts, and the per-capture batch indexes themselves. One
// mechanism, not three. The snapshot stores hashes; this stores bytes.
//
// The write discipline is deliberately identical to FileStateStore.Save:
// temp file in the destination directory, chmod, write, fsync, close, rename,
// fsync the directory. A crash therefore never leaves a partial file at a
// content path -- only a temp file, which SweepTemps removes.
type FileObjectStore struct {
	root string
}

const (
	objectDirectoryName = "objects"
	temporaryPrefix     = ".bindery-object-"
	// MaxObjectBytes bounds a single stored object. The heavy-artifact lane
	// (post-match dumps, replays) is what needs the headroom; batch bodies are
	// capped far lower at the HTTP boundary.
	MaxObjectBytes = 64 << 20
)

var (
	// ErrObjectNotFound is returned for a hash the store has never held. It is
	// deliberately distinct from a corruption error: "I have never seen this"
	// and "I have this and it is wrong" are different facts.
	ErrObjectNotFound = errors.New("capture object not found")
	// ErrObjectCorrupt means the bytes on disk do not hash to the name they
	// are filed under.
	ErrObjectCorrupt = errors.New("capture object does not match its content hash")

	contentHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// NewFileObjectStore roots the object store beside the control snapshot. The
// caller passes the state directory, not the state file.
func NewFileObjectStore(stateDirectory string) (*FileObjectStore, error) {
	if stateDirectory == "" {
		return nil, errors.New("object store requires a state directory")
	}
	absolute, err := filepath.Abs(stateDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve object root: %w", err)
	}
	root := filepath.Join(absolute, objectDirectoryName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create object root: %w", err)
	}
	return &FileObjectStore{root: root}, nil
}

// Root exposes the object directory for diagnostics and tests.
func (s *FileObjectStore) Root() string { return s.root }

func (s *FileObjectStore) path(contentHash string) (string, error) {
	if !contentHashPattern.MatchString(contentHash) {
		return "", fmt.Errorf("%w: %q is not a sha256 content hash", ErrObjectNotFound, contentHash)
	}
	digest := strings.TrimPrefix(contentHash, HashPrefix)
	return filepath.Join(s.root, digest[:2], digest), nil
}

// Put stores bytes under their own sha256 and returns the content hash. It is
// idempotent by construction: identical bytes produce the same path, and an
// existing object is left exactly as it is rather than rewritten.
func (s *FileObjectStore) Put(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("refusing to store an empty capture object")
	}
	if len(data) > MaxObjectBytes {
		return "", fmt.Errorf("capture object of %d bytes exceeds the %d byte limit", len(data), MaxObjectBytes)
	}
	contentHash := formatDigest(sha256.Sum256(data))
	destination, err := s.path(contentHash)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create object shard: %w", err)
	}
	if present, err := s.regularFileAt(destination); err != nil {
		return "", err
	} else if present {
		return contentHash, nil
	}
	temporary, err := os.CreateTemp(directory, temporaryPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("create object transaction: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", fmt.Errorf("protect object transaction: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write object transaction: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", fmt.Errorf("sync object transaction: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close object transaction: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", fmt.Errorf("commit object transaction: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return "", err
	}
	return contentHash, nil
}

// Has reports whether the store holds an object, without reading it. Startup
// integrity uses this: re-hashing every object on every boot is O(total bytes)
// for a check whose consequences only arrive at evidence-derivation time.
func (s *FileObjectStore) Has(contentHash string) bool {
	destination, err := s.path(contentHash)
	if err != nil {
		return false
	}
	present, err := s.regularFileAt(destination)
	return err == nil && present
}

// Size returns the stored byte length, used by the cheap startup check.
func (s *FileObjectStore) Size(contentHash string) (int64, error) {
	destination, err := s.path(contentHash)
	if err != nil {
		return 0, err
	}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("%w: %s", ErrObjectNotFound, contentHash)
	}
	if err != nil {
		return 0, fmt.Errorf("inspect object: %w", err)
	}
	if err := checkObjectMode(info); err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Get reads an object and verifies it against the hash it is filed under. This
// is where corruption is caught, because this is where it has a consequence.
func (s *FileObjectStore) Get(contentHash string) ([]byte, error) {
	destination, err := s.path(contentHash)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, contentHash)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect object: %w", err)
	}
	if err := checkObjectMode(info); err != nil {
		return nil, err
	}
	file, err := os.Open(destination)
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxObjectBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	if formatDigest(sha256.Sum256(data)) != contentHash {
		return nil, fmt.Errorf("%w: %s", ErrObjectCorrupt, contentHash)
	}
	return data, nil
}

// SweepTemps removes uncommitted write transactions left by a crash. It is
// safe because the control plane is single-writer by construction: no other
// process is mid-rename. Orphaned *objects* are never swept -- deleting
// content-addressed evidence on a heuristic is how evidence gets lost.
func (s *FileObjectStore) SweepTemps() (int, error) {
	shards, err := os.ReadDir(s.root)
	if err != nil {
		return 0, fmt.Errorf("read object root: %w", err)
	}
	swept := 0
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		directory := filepath.Join(s.root, shard.Name())
		entries, err := os.ReadDir(directory)
		if err != nil {
			return swept, fmt.Errorf("read object shard: %w", err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), temporaryPrefix) {
				continue
			}
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				return swept, fmt.Errorf("sweep object transaction: %w", err)
			}
			swept++
		}
	}
	return swept, nil
}

func (s *FileObjectStore) regularFileAt(destination string) (bool, error) {
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect object: %w", err)
	}
	if err := checkObjectMode(info); err != nil {
		return false, err
	}
	return true, nil
}

func checkObjectMode(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("capture object path must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("capture object permissions %04o are wider than the state boundary allows", info.Mode().Perm())
	}
	return nil
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open object directory: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync object directory: %w", err)
	}
	return nil
}

// DigestOf is the content hash a Put of these bytes would produce, without
// writing anything. Callers use it to decide whether a write is needed at all.
func DigestOf(data []byte) string { return formatDigest(sha256.Sum256(data)) }

// MemoryObjectStore is the non-persistent counterpart, used by a Service with
// no state store behind it. It enforces the same content-addressing so a test
// that passes here is testing the same identity rules production uses.
type MemoryObjectStore struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func NewMemoryObjectStore() *MemoryObjectStore {
	return &MemoryObjectStore{objects: make(map[string][]byte)}
}

func (s *MemoryObjectStore) Put(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("refusing to store an empty capture object")
	}
	if len(data) > MaxObjectBytes {
		return "", fmt.Errorf("capture object of %d bytes exceeds the %d byte limit", len(data), MaxObjectBytes)
	}
	contentHash := DigestOf(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[contentHash]; !exists {
		s.objects[contentHash] = append([]byte(nil), data...)
	}
	return contentHash, nil
}

func (s *MemoryObjectStore) Get(contentHash string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.objects[contentHash]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, contentHash)
	}
	return append([]byte(nil), data...), nil
}

func (s *MemoryObjectStore) Has(contentHash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.objects[contentHash]
	return ok
}

func (s *MemoryObjectStore) Size(contentHash string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.objects[contentHash]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrObjectNotFound, contentHash)
	}
	return int64(len(data)), nil
}
