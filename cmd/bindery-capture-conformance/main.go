// Command bindery-capture-conformance drives the capture plane end to end as
// an independent producer, over HTTP, from outside the broker's process.
//
// It exists because every capture proof in this repository until now called
// the service or its http.Handler directly. Those tests establish that the
// broker is internally consistent; none of them establishes that the published
// contract is implementable by a program that was not compiled against it, or
// that anything survives a real socket. This program imports no Bindery
// package: it re-declares the wire DTOs it uses and re-implements the frozen
// canonical encoding, so a divergence between the contract and the
// implementation shows up as a failed run rather than as a shared type.
//
// It is the shape the RA2 adapter's producer half has to take, exercised in a
// language the project can run in CI today.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	adapterID      = "bindery.conformance-producer"
	adapterVersion = "0.1.0"
	payloadVersion = "1"
	// Any sha256-shaped value satisfies the compatibility contract; the
	// producer is not claiming to be a particular game build.
	gameHash = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	modHash  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	mapHash  = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
)

type stepResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

type report struct {
	SchemaVersion string       `json:"schema_version"`
	BaseURL       string       `json:"base_url"`
	Producer      string       `json:"producer"`
	StartedAt     time.Time    `json:"started_at"`
	Steps         []stepResult `json:"steps"`
	Passed        int          `json:"passed"`
	Failed        int          `json:"failed"`
	OK            bool         `json:"ok"`
}

type run struct {
	client *client
	report *report

	accountToken   string
	joinCredential string
	sessionID      string
	executionID    string
	clientLease    string
	clientID       string
	offer          captureOffer
	attribution    attribution
	events         []event
	firstBatchHash string
}

// step records the outcome of one contract obligation. A failing step does not
// abort the run unless the steps after it cannot be attempted, so one report
// shows every obligation that holds and every one that does not.
func (r *run) step(name string, fn func() (string, error)) bool {
	detail, err := fn()
	if err != nil {
		r.report.Steps = append(r.report.Steps, stepResult{Name: name, OK: false, Error: err.Error()})
		r.report.Failed++
		return false
	}
	r.report.Steps = append(r.report.Steps, stepResult{Name: name, OK: true, Detail: detail})
	r.report.Passed++
	return true
}

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:8080", "control plane base URL")
	handle := flag.String("handle", "", "identity handle to claim (default: generated)")
	eventCount := flag.Int("events", 12, "number of events to produce")
	batchSize := flag.Int("batch-size", 5, "events per batch")
	reportPath := flag.String("report", "", "write the JSON conformance report to this path")
	flag.Parse()

	if *eventCount < 1 || *batchSize < 1 {
		fmt.Fprintln(os.Stderr, "events and batch-size must be positive")
		os.Exit(2)
	}
	claimed := *handle
	if claimed == "" {
		claimed = fmt.Sprintf("conformance-%d", time.Now().UnixNano()%1_000_000_000)
	}

	result := &report{
		SchemaVersion: "bindery-capture-conformance/v1",
		BaseURL:       *baseURL,
		Producer:      adapterID + "@" + adapterVersion,
		StartedAt:     time.Now().UTC(),
	}
	r := &run{client: newClient(*baseURL), report: result}
	r.execute(claimed, *eventCount, *batchSize)

	result.OK = result.Failed == 0
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode report: %v\n", err)
		os.Exit(2)
	}
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, encoded, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write report: %v\n", err)
			os.Exit(2)
		}
	}
	fmt.Println(string(encoded))
	for _, step := range result.Steps {
		if !step.OK {
			fmt.Fprintf(os.Stderr, "FAIL %s: %s\n", step.Name, step.Error)
		}
	}
	if !result.OK {
		os.Exit(1)
	}
}

func (r *run) execute(handle string, eventCount, batchSize int) {
	if !r.step("claim-identity", func() (string, error) { return r.claimIdentity(handle) }) {
		return
	}
	if !r.step("create-session", r.createSession) {
		return
	}
	if !r.step("enroll-and-receive-capture-offer", r.enroll) {
		return
	}
	if !r.step("produce-events", func() (string, error) { return r.produce(eventCount) }) {
		return
	}
	// Negative control first. Without it, a broker that ignored producer_digest
	// entirely would pass every positive check below.
	if !r.step("reject-mismatched-producer-digest", func() (string, error) { return r.rejectBadDigest(batchSize) }) {
		return
	}
	if !r.step("ingest-batches-with-independent-digest", func() (string, error) { return r.ingest(batchSize) }) {
		return
	}
	// Ordered after ingest because it replays the first batch; it is a
	// conformance obligation in its own right (NFR-05).
	r.step("replay-batch-is-idempotent", func() (string, error) { return r.replayFirstBatch(batchSize) })
	r.step("store-heavy-object", r.storeObject)
	if !r.step("close-capture", func() (string, error) { return r.closeCapture(eventCount) }) {
		return
	}
	r.step("completeness-manifest-is-published", func() (string, error) { return r.checkCompleteness(eventCount) })
	r.step("normalize-closed-capture", r.normalize)
	r.step("read-events-through-cursor", func() (string, error) { return r.readEvents(eventCount) })
}

func (r *run) claimIdentity(handle string) (string, error) {
	var response identityResponse
	err := r.client.call("POST", "/v1/identities", map[string]any{"handle": handle}, 201, &response,
		callOptions{idempotencyKey: "conformance-identity-" + handle})
	if err != nil {
		return "", err
	}
	if response.AccountToken == "" {
		return "", fmt.Errorf("identity response carried no account token")
	}
	r.accountToken = response.AccountToken
	return "claimed handle " + handle, nil
}

func (r *run) createSession() (string, error) {
	request := map[string]any{
		"compatibility": map[string]any{
			"game_family": "conformance", "game_version": "1", "game_hash": gameHash,
			"adapter_id": adapterID, "adapter_version": adapterVersion,
			"mod_id": "none", "mod_hash": modHash,
			"map_id": "none", "map_hash": mapHash,
		},
		"participant_policy": map[string]any{"required_players": 2, "maximum_players": 2, "maximum_observers": 1},
		"placement":          map[string]any{"allowed_regions": []string{"eu-north"}, "latency_p95_ms": 100},
		"capture":            map[string]any{"semantic_events": true, "post_match_dump": true},
	}
	var response sessionResponse
	err := r.client.call("POST", "/v1/sessions", request, 201, &response,
		callOptions{bearer: r.accountToken, idempotencyKey: "conformance-session"})
	if err != nil {
		return "", err
	}
	r.sessionID = response.PublicSession.SessionID
	r.executionID = response.PublicSession.ExecutionID
	r.joinCredential = response.SessionJoinCredential
	if r.sessionID == "" || r.executionID == "" {
		return "", fmt.Errorf("session response carried no session or execution id")
	}
	return "session " + r.sessionID + " execution " + r.executionID, nil
}

func (r *run) enroll() (string, error) {
	request := map[string]any{
		"client_instance_id": "conformance-producer-1",
		"client_class":       "player",
		"adapter":            map[string]any{"id": adapterID, "version": adapterVersion},
		"compatibility":      map[string]any{"game_hash": gameHash, "mod_hash": modHash, "map_hash": mapHash},
	}
	var response enrollmentResponse
	err := r.client.call("POST", "/v1/sessions/"+r.sessionID+"/enrollments", request, 201, &response,
		callOptions{bearer: r.accountToken, idempotencyKey: "conformance-enroll", joinCredential: r.joinCredential})
	if err != nil {
		return "", err
	}
	if len(response.CaptureStreamOffers) != 1 {
		return "", fmt.Errorf("capture stream offers = %d, want 1", len(response.CaptureStreamOffers))
	}
	r.offer = response.CaptureStreamOffers[0]
	r.clientLease = response.ClientLeaseToken
	r.clientID = response.PublicEnrollment.ClientID
	if r.offer.CaptureID == "" || r.clientLease == "" {
		return "", fmt.Errorf("enrollment response carried no capture id or lease")
	}
	// Everything the canonical encoding needs that is not per-event. If the
	// broker attributes events differently from this, the digests disagree.
	r.attribution = attribution{
		SessionID:        r.sessionID,
		ExecutionID:      r.executionID,
		CaptureID:        r.offer.CaptureID,
		ProducerClientID: r.clientID,
		ProducerClass:    r.offer.ProducerClass,
		CaptureMethod:    r.offer.CaptureMethod,
		AdapterID:        response.PublicEnrollment.AdapterID,
		AdapterVersion:   response.PublicEnrollment.AdapterVersion,
	}
	return fmt.Sprintf("capture %s method %s max %d events/batch", r.offer.CaptureID, r.offer.CaptureMethod, r.offer.MaxBatchEvents), nil
}

func (r *run) produce(count int) (string, error) {
	base := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	r.events = make([]event, 0, count)
	for index := 0; index < count; index++ {
		producerTime := base.Add(time.Duration(index) * time.Second)
		tick := uint64(index * 30)
		payload, err := json.Marshal(map[string]any{"kind": "tick", "index": index})
		if err != nil {
			return "", err
		}
		r.events = append(r.events, event{
			EventID:        fmt.Sprintf("conformance-event-%04d", index),
			Sequence:       uint64(index),
			GameTick:       &tick,
			ProducerTime:   &producerTime,
			EventType:      "conformance.tick",
			PayloadVersion: payloadVersion,
			Payload:        payload,
		})
	}
	return fmt.Sprintf("%d events, sequences 0..%d", count, count-1), nil
}

func (r *run) ingest(batchSize int) (string, error) {
	if r.offer.MaxBatchEvents > 0 && batchSize > r.offer.MaxBatchEvents {
		batchSize = r.offer.MaxBatchEvents
	}
	batches := 0
	for start := 0; start < len(r.events); start += batchSize {
		end := start + batchSize
		if end > len(r.events) {
			end = len(r.events)
		}
		receipt, err := r.sendBatch(r.events[start:end], fmt.Sprintf("conformance-batch-%d", start))
		if err != nil {
			return "", err
		}
		want := int64(end - 1)
		if receipt.AcknowledgedThrough != want {
			return "", fmt.Errorf("acknowledged_through = %d after batch ending at %d, want %d", receipt.AcknowledgedThrough, end-1, want)
		}
		if start == 0 {
			r.firstBatchHash = receipt.RawObjectHash
		}
		if receipt.Duplicate {
			return "", fmt.Errorf("batch starting at %d was answered as a duplicate on first send", start)
		}
		batches++
	}
	return fmt.Sprintf("%d batches accepted; broker agreed with this producer's digest on every one", batches), nil
}

// sendBatch computes the producer digest independently and asks the broker to
// agree with it. The broker recomputes and rejects a mismatch, so a successful
// call is the conformance signal.
func (r *run) sendBatch(events []event, idempotencyKey string) (captureReceipt, error) {
	digest, err := producerDigest(events, r.attribution)
	if err != nil {
		return captureReceipt{}, err
	}
	request := ingestBatch{
		FirstSequence:  events[0].Sequence,
		LastSequence:   events[len(events)-1].Sequence,
		ProducerDigest: digest,
		Events:         events,
	}
	// Acceptance is the conformance signal. The broker recomputes the digest
	// from the events it decoded and answers SEQUENCE_CONFLICT on a mismatch,
	// so a 200 means an independently written encoder agreed with the frozen
	// one, byte for byte. The negative control proves the check is live.
	var receipt captureReceipt
	err = r.client.call("POST", "/v1/captures/"+r.offer.CaptureID+"/batches", request, 200, &receipt,
		callOptions{bearer: r.clientLease, idempotencyKey: idempotencyKey})
	if err != nil {
		return captureReceipt{}, err
	}
	return receipt, nil
}

// rejectBadDigest sends a well-formed batch under a digest that is deliberately
// wrong and requires the broker to refuse it. It runs before any real ingest,
// so a refusal leaves the stream untouched and the positive path unaffected.
func (r *run) rejectBadDigest(batchSize int) (string, error) {
	if batchSize > len(r.events) {
		batchSize = len(r.events)
	}
	events := r.events[0:batchSize]
	request := ingestBatch{
		FirstSequence:  events[0].Sequence,
		LastSequence:   events[len(events)-1].Sequence,
		ProducerDigest: "sha256:" + strings.Repeat("0", 64),
		Events:         events,
	}
	status, code, err := r.client.callExpectingRejection("POST", "/v1/captures/"+r.offer.CaptureID+"/batches", request,
		callOptions{bearer: r.clientLease, idempotencyKey: "conformance-bad-digest"})
	if err != nil {
		return "", err
	}
	if code != "SEQUENCE_CONFLICT" {
		return "", fmt.Errorf("rejected with code %q, want SEQUENCE_CONFLICT", code)
	}
	return fmt.Sprintf("a wrong digest was refused with %d %s", status, code), nil
}

func (r *run) replayFirstBatch(batchSize int) (string, error) {
	if len(r.events) == 0 {
		return "", fmt.Errorf("nothing was produced to replay")
	}
	if batchSize > len(r.events) {
		batchSize = len(r.events)
	}
	receipt, err := r.sendBatch(r.events[0:batchSize], "conformance-batch-0")
	if err != nil {
		return "", err
	}
	want := int64(len(r.events) - 1)
	if receipt.AcknowledgedThrough != want {
		return "", fmt.Errorf("replay moved acknowledged_through to %d, want %d unchanged", receipt.AcknowledgedThrough, want)
	}
	if !receipt.Duplicate {
		return "", fmt.Errorf("an identical retry was not reported as a duplicate")
	}
	if receipt.RawObjectHash != r.firstBatchHash {
		return "", fmt.Errorf("replay named object %s, want the stored %s", receipt.RawObjectHash, r.firstBatchHash)
	}
	return "replayed batch 0; answered as a duplicate naming the already-stored object", nil
}

func (r *run) storeObject() (string, error) {
	body := []byte("conformance post-match dump: not a real game artifact\n")
	var manifest objectManifest
	err := r.client.call("POST", "/v1/captures/"+r.offer.CaptureID+"/objects", nil, 201, &manifest,
		callOptions{bearer: r.clientLease, contentType: "application/octet-stream", rawBody: body})
	if err != nil {
		return "", err
	}
	if manifest.ContentHash == "" || manifest.Bytes != int64(len(body)) {
		return "", fmt.Errorf("object manifest = %+v, want %d bytes and a content hash", manifest, len(body))
	}
	return fmt.Sprintf("stored %d bytes as %s", manifest.Bytes, manifest.ContentHash), nil
}

func (r *run) closeCapture(eventCount int) (string, error) {
	request := map[string]any{
		"final_sequence": eventCount - 1,
		"local_drops":    0,
		"end_reason":     "conformance run complete",
	}
	var record publicCapture
	err := r.client.call("POST", "/v1/captures/"+r.offer.CaptureID+":close", request, 200, &record,
		callOptions{bearer: r.clientLease})
	if err != nil {
		return "", err
	}
	if record.Status == "" {
		return "", fmt.Errorf("close returned no status")
	}
	return "capture status " + record.Status, nil
}

func (r *run) checkCompleteness(eventCount int) (string, error) {
	var record publicCapture
	if err := r.client.call("GET", "/v1/captures/"+r.offer.CaptureID, nil, 200, &record, callOptions{}); err != nil {
		return "", err
	}
	if record.Completeness == nil {
		return "", fmt.Errorf("closed capture published no completeness manifest")
	}
	manifest := record.Completeness
	if !manifest.Closed {
		return "", fmt.Errorf("manifest reports the stream as open after close")
	}
	if len(manifest.MissingRanges) != 0 {
		return "", fmt.Errorf("manifest reports missing ranges %v in a stream produced without gaps", manifest.MissingRanges)
	}
	if manifest.EventCount != uint64(eventCount) {
		return "", fmt.Errorf("event_count = %d, want %d", manifest.EventCount, eventCount)
	}
	last := uint64(eventCount - 1)
	if manifest.ExpectedThrough == nil || *manifest.ExpectedThrough != last {
		return "", fmt.Errorf("expected_through = %v, want %d", manifest.ExpectedThrough, last)
	}
	// One contiguous run is the whole point of producing without gaps; several
	// would mean the broker did not join batches that abut.
	if len(manifest.ObservedRanges) != 1 || manifest.ObservedRanges[0] != [2]uint64{0, last} {
		return "", fmt.Errorf("observed_ranges = %v, want one run of 0..%d", manifest.ObservedRanges, last)
	}
	if len(manifest.RawObjectHashes) == 0 {
		return "", fmt.Errorf("manifest names no raw objects")
	}
	if len(record.Objects) != 1 {
		return "", fmt.Errorf("capture lists %d heavy objects, want 1", len(record.Objects))
	}
	return fmt.Sprintf("closed, no gaps, %d events over %v, %d raw objects", manifest.EventCount, manifest.ObservedRanges, len(manifest.RawObjectHashes)), nil
}

func (r *run) normalize() (string, error) {
	request := map[string]any{"normalizer_id": "bindery.capture-gap", "normalizer_version": "1"}
	var record publicCapture
	err := r.client.call("POST", "/v1/captures/"+r.offer.CaptureID+":normalize", request, 201, &record,
		callOptions{bearer: r.accountToken})
	if err != nil {
		return "", err
	}
	if record.DerivedFromCaptureID != r.offer.CaptureID {
		return "", fmt.Errorf("derived capture points at %q, want %q", record.DerivedFromCaptureID, r.offer.CaptureID)
	}
	if record.Normalizer == nil || record.Normalizer.ID != "bindery.capture-gap" {
		return "", fmt.Errorf("derived capture does not name the normalizer that produced it: %+v", record.Normalizer)
	}
	return fmt.Sprintf("derived capture %s from %s@%s", record.CaptureID, record.Normalizer.ID, record.Normalizer.Version), nil
}

// readEvents pages the public read path to the end, which is the only step
// here that needs no credential at all: PUB-01 makes known records readable
// without authentication.
func (r *run) readEvents(eventCount int) (string, error) {
	seen := 0
	cursor := ""
	pages := 0
	for {
		path := "/v1/captures/" + r.offer.CaptureID + "/events?limit=5"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var page eventPage
		if err := r.client.call("GET", path, nil, 200, &page, callOptions{}); err != nil {
			return "", err
		}
		pages++
		for _, item := range page.Events {
			if item.Sequence != uint64(seen) {
				return "", fmt.Errorf("event %d arrived out of order at sequence %d", seen, item.Sequence)
			}
			seen++
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > eventCount+2 {
			return "", fmt.Errorf("cursor did not terminate after %d pages", pages)
		}
	}
	if seen != eventCount {
		return "", fmt.Errorf("read %d events through the cursor, want %d", seen, eventCount)
	}
	return fmt.Sprintf("%d events over %d pages, unauthenticated", seen, pages), nil
}
