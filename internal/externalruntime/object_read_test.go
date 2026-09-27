package externalruntime

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObjectsAreServedByContentHashWithTheirMediaType(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	fixture := newCaptureFixture(t, service, "object-read")
	replay := []byte("replay bytes \x00\x01\x02")
	manifest, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, "application/x-ra2-replay", replay)
	if err != nil {
		t.Fatal(err)
	}

	// Unauthenticated, like every other public read.
	response := serve(handler, httptest.NewRequest(http.MethodGet, "/v1/objects/"+manifest.ContentHash, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("object read status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Body.String() != string(replay) {
		t.Fatalf("object body = %q, want %q", response.Body.String(), replay)
	}
	header := response.Header()
	if got := header.Get("Content-Type"); got != "application/x-ra2-replay" {
		t.Fatalf("content type = %q", got)
	}
	if got := header.Get("ETag"); got != `"`+manifest.ContentHash+`"` {
		t.Fatalf("etag = %q", got)
	}
	if got := header.Get("Cache-Control"); !strings.Contains(got, "immutable") || !strings.Contains(got, "public") {
		t.Fatalf("cache control = %q", got)
	}
	if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("producer-typed bytes were served without nosniff and a sandbox: %v", header)
	}
	if got := header.Get("Content-Length"); got != "16" {
		t.Fatalf("content length = %q", got)
	}

	conditional := httptest.NewRequest(http.MethodGet, "/v1/objects/"+manifest.ContentHash, nil)
	conditional.Header.Set("If-None-Match", `W/"`+manifest.ContentHash+`"`)
	notModified := serve(handler, conditional)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional read = %d %q", notModified.Code, notModified.Body.String())
	}
}

func TestObjectReadRejectsMalformedAndUnknownHashes(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	fixture := newCaptureFixture(t, service, "object-miss")
	receipt := mustIngest(t, service, fixture.playerA, 0, 1)

	for _, tc := range []struct {
		target string
		status int
		code   string
	}{
		{"/v1/objects/not-a-hash", http.StatusBadRequest, "OBJECT_HASH_INVALID"},
		{"/v1/objects/sha256:ABC", http.StatusBadRequest, "OBJECT_HASH_INVALID"},
		{"/v1/objects/" + testHashA, http.StatusNotFound, "OBJECT_NOT_FOUND"},
		// A raw batch is content-addressed in the same store but is read
		// through the event endpoints, not served as an object.
		{"/v1/objects/" + receipt.RawObjectHash, http.StatusNotFound, "OBJECT_NOT_FOUND"},
	} {
		response := serve(handler, httptest.NewRequest(http.MethodGet, tc.target, nil))
		if response.Code != tc.status || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%s: status = %d, body = %s", tc.target, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: error was not the JSON error schema", tc.target)
		}
	}
	if response := serve(handler, httptest.NewRequest(http.MethodPost, "/v1/objects/"+testHashA, nil)); response.Code != http.StatusNotFound {
		t.Fatalf("object write through the read path status = %d", response.Code)
	}
}

func TestObjectMediaTypeAcrossCapturesIsTheFirstRecorded(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	fixture := newCaptureFixture(t, service, "object-order")
	data := []byte("the same bytes, twice")

	if _, err := service.StoreCaptureObject(fixture.playerB.lease, fixture.playerB.capture, "application/x-second", data); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	manifest, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, "application/x-first", data)
	if err != nil {
		t.Fatal(err)
	}
	object, err := service.GetObject(manifest.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if object.MediaType != "application/x-second" {
		t.Fatalf("media type = %q, want the earliest recorded", object.MediaType)
	}

	// At an identical receive time the lower capture id wins.
	tied := NewService()
	tied.clock = func() time.Time { return now }
	tiedFixture := newCaptureFixture(t, tied, "object-tie")
	first, second := tiedFixture.playerA, tiedFixture.playerB
	if second.capture < first.capture {
		first, second = second, first
	}
	if _, err := tied.StoreCaptureObject(second.lease, second.capture, "application/x-higher", data); err != nil {
		t.Fatal(err)
	}
	if _, err := tied.StoreCaptureObject(first.lease, first.capture, "application/x-lower", data); err != nil {
		t.Fatal(err)
	}
	object, err = tied.GetObject(manifest.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if object.MediaType != "application/x-lower" {
		t.Fatalf("tied media type = %q, want the lower capture id's", object.MediaType)
	}
}

func TestObjectsAreServedAfterARestartAndCorruptionIsRefused(t *testing.T) {
	directory := t.TempDir()
	service := openPersistentCaptureService(t, directory)
	fixture := newCaptureFixture(t, service, "object-durable")
	manifest, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, DecisionTraceMediaType, []byte("{\"tick\":1}\n"))
	if err != nil {
		t.Fatal(err)
	}

	reopened := openPersistentCaptureService(t, directory)
	handler := NewHandler(reopened)
	response := serve(handler, httptest.NewRequest(http.MethodGet, "/v1/objects/"+manifest.ContentHash, nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != DecisionTraceMediaType || response.Body.String() != "{\"tick\":1}\n" {
		t.Fatalf("restored object read = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}

	// Same length, different bytes: restore's size check passes, and the read
	// must still refuse to serve bytes under a hash they no longer have.
	var objectPath string
	if err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.Contains(path, strings.TrimPrefix(manifest.ContentHash, "sha256:")) {
			objectPath = path
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if objectPath == "" {
		t.Fatal("object file not found on disk")
	}
	if err := os.Chmod(objectPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("{\"tick\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(objectPath, 0o400); err != nil {
		t.Fatal(err)
	}
	corrupt := serve(handler, httptest.NewRequest(http.MethodGet, "/v1/objects/"+manifest.ContentHash, nil))
	if corrupt.Code != http.StatusInternalServerError || !strings.Contains(corrupt.Body.String(), "OBJECT_UNREADABLE") {
		t.Fatalf("corrupt object read = %d %s", corrupt.Code, corrupt.Body.String())
	}
}

func TestDecisionTraceMediaTypeIsAcceptedByTheObjectLane(t *testing.T) {
	if !mediaTypePattern.MatchString(DecisionTraceMediaType) {
		t.Fatalf("%q does not match the object lane's media type pattern", DecisionTraceMediaType)
	}
	service := NewService()
	fixture := newCaptureFixture(t, service, "decision-trace")
	// Core does not parse a trace: a body that is not NDJSON is stored as is.
	manifest, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, DecisionTraceMediaType, []byte("not json at all"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.MediaType != DecisionTraceMediaType {
		t.Fatalf("manifest media type = %q", manifest.MediaType)
	}
}
