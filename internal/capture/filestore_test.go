package capture

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestObjectStore(t *testing.T) (*FileObjectStore, string) {
	t.Helper()
	directory := t.TempDir()
	store, err := NewFileObjectStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	return store, directory
}

func TestObjectSurvivesReopenAndVerifiesOnRead(t *testing.T) {
	store, directory := newTestObjectStore(t)
	payload := []byte(`{"batch":"one"}`)
	contentHash, err := store.Put(payload)
	if err != nil {
		t.Fatal(err)
	}
	if contentHash != DigestOf(payload) {
		t.Fatalf("put hash = %s, want %s", contentHash, DigestOf(payload))
	}

	reopened, err := NewFileObjectStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Has(contentHash) {
		t.Fatal("object did not survive reopen")
	}
	read, err := reopened.Get(contentHash)
	if err != nil {
		t.Fatal(err)
	}
	if string(read) != string(payload) {
		t.Fatalf("object round trip = %s", read)
	}
	size, err := reopened.Size(contentHash)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("object size = %d, want %d", size, len(payload))
	}
}

func TestIdenticalPutDoesNotRewriteTheObject(t *testing.T) {
	store, _ := newTestObjectStore(t)
	payload := []byte(`{"batch":"two"}`)
	contentHash, err := store.Put(payload)
	if err != nil {
		t.Fatal(err)
	}
	path := objectPath(t, store, contentHash)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0o600 {
		t.Fatalf("object permissions = %04o, want 0600", before.Mode().Perm())
	}
	if _, err := store.Put(payload); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an identical put rewrote the object instead of leaving it alone")
	}
}

func TestCorruptedObjectIsReportedAsCorruptNotMissing(t *testing.T) {
	// "I have never seen this" and "I have this and it is wrong" must not
	// collapse into one answer: only the second one means evidence was lost.
	store, _ := newTestObjectStore(t)
	contentHash, err := store.Put([]byte(`{"batch":"three"}`))
	if err != nil {
		t.Fatal(err)
	}
	path := objectPath(t, store, contentHash)
	if err := os.WriteFile(path, []byte(`{"batch":"tampered"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(contentHash); !errors.Is(err, ErrObjectCorrupt) {
		t.Fatalf("tampered object error = %v, want ErrObjectCorrupt", err)
	}
	missing := "sha256:" + strings.Repeat("0", 64)
	if _, err := store.Get(missing); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("absent object error = %v, want ErrObjectNotFound", err)
	}
}

func TestSweepRemovesUncommittedTransactionsOnly(t *testing.T) {
	store, _ := newTestObjectStore(t)
	contentHash, err := store.Put([]byte(`{"batch":"four"}`))
	if err != nil {
		t.Fatal(err)
	}
	shard := filepath.Dir(objectPath(t, store, contentHash))
	orphan := filepath.Join(shard, temporaryPrefix+"crashed")
	if err := os.WriteFile(orphan, []byte("half a batch"), 0o600); err != nil {
		t.Fatal(err)
	}
	swept, err := store.SweepTemps()
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want 1", swept)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("crashed transaction survived the sweep")
	}
	if !store.Has(contentHash) {
		t.Fatal("the sweep removed a committed object")
	}
}

func TestSymlinkedAndWidenedObjectPathsAreRefused(t *testing.T) {
	store, _ := newTestObjectStore(t)
	payload := []byte(`{"batch":"five"}`)
	contentHash := DigestOf(payload)
	path := objectPath(t, store, contentHash)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "planted")
	if err := os.WriteFile(elsewhere, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(contentHash); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked object error = %v", err)
	}
	if _, err := store.Put(payload); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("put over a symlink error = %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(contentHash); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("world-readable object error = %v", err)
	}
}

func TestMalformedContentHashCannotEscapeTheObjectRoot(t *testing.T) {
	store, _ := newTestObjectStore(t)
	for _, candidate := range []string{"../../etc/passwd", "sha256:../../etc/passwd", "", "sha256:NOTHEX", "deadbeef"} {
		if store.Has(candidate) {
			t.Fatalf("%q was accepted as a content hash", candidate)
		}
		if _, err := store.Get(candidate); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("%q error = %v", candidate, err)
		}
	}
}

func objectPath(t *testing.T, store *FileObjectStore, contentHash string) string {
	t.Helper()
	digest := strings.TrimPrefix(contentHash, HashPrefix)
	return filepath.Join(store.Root(), digest[:2], digest)
}
