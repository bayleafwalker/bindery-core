package externalruntime

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHeavyObjectsAreContentAddressedAndPubliclyManifested(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "objects")
	dump := []byte("stats.dmp binary contents")

	manifest, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, "application/octet-stream", dump)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Bytes != int64(len(dump)) || manifest.CaptureID != fixture.playerA.capture {
		t.Fatalf("manifest = %+v", manifest)
	}
	if !strings.HasPrefix(manifest.ContentHash, "sha256:") {
		t.Fatalf("content hash = %q", manifest.ContentHash)
	}

	record, err := service.GetCapture(fixture.playerA.capture)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Objects) != 1 || record.Objects[0] != manifest.ContentHash {
		t.Fatalf("capture objects = %v", record.Objects)
	}
	// Heavy artifacts must not enter the semantic event sequence space.
	if record.Completeness.EventCount != 0 {
		t.Fatalf("an object was counted as an observation: %+v", record.Completeness)
	}

	replay, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, "application/octet-stream", dump)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ContentHash != manifest.ContentHash || len(service.captures[fixture.playerA.capture].Index) != 1 {
		t.Fatal("re-uploading identical bytes created a second object")
	}
	if _, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, "application/x-replay", dump); !hasCode(err, "SEQUENCE_CONFLICT") {
		t.Fatal("the same bytes were accepted under two media types")
	}
}

func TestObjectManifestNamesNoStorageLocation(t *testing.T) {
	// The manifest is published. It says what the artifact is and what it
	// hashes to, and nothing about where the broker keeps it.
	service := NewService()
	fixture := newCaptureFixture(t, service, "object-redaction")
	manifest, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, "application/octet-stream", []byte("dump"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := ScanPublicOutput(encoded, fixture.playerA.lease, fixture.playerA.transport, "objects/"); err != nil {
		t.Fatalf("object manifest leaked material: %v (%s)", err, encoded)
	}
	if strings.Contains(string(encoded), "private") {
		t.Fatalf("object manifest named a private location: %s", encoded)
	}
}

func TestObjectLaneRefusesBadMediaTypesEmptyBodiesAndForeignLeases(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "object-authz")
	for _, mediaType := range []string{"", "octet-stream", "application/", "APPLICATION/JSON"} {
		if _, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, mediaType, []byte("x")); !hasCode(err, "MEDIA_TYPE_INVALID") {
			t.Fatalf("media type %q error = %v", mediaType, err)
		}
	}
	if _, err := service.StoreCaptureObject(fixture.playerA.lease, fixture.playerA.capture, "application/octet-stream", nil); !hasCode(err, "OBJECT_EMPTY") {
		t.Fatal("an empty object body was accepted")
	}
	if _, err := service.StoreCaptureObject(fixture.playerB.lease, fixture.playerA.capture, "application/octet-stream", []byte("x")); !hasCode(err, "TOKEN_INVALID") {
		t.Fatal("a foreign lease uploaded to another client's capture")
	}
}

func TestObjectUploadOverHTTPTakesBytesDirectly(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	fixture := newCaptureFixture(t, service, "http-object")

	request := httptestRequest(http.MethodPost, "/v1/captures/"+fixture.playerA.capture+"/objects", "post-match dump bytes")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Authorization", "Bearer "+fixture.playerA.lease)
	response := serve(handler, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("object upload status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := ScanPublicOutput(response.Body.Bytes(), fixture.playerA.lease, fixture.playerA.transport); err != nil {
		t.Fatalf("object response leaked material: %v", err)
	}
	var manifest PublicObjectManifest
	if err := json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.MediaType != "application/octet-stream" {
		t.Fatalf("media type = %q", manifest.MediaType)
	}
}
