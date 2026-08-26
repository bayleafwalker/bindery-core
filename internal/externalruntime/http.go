package externalruntime

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) http.Handler { return &Handler{service: service} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID, err := newUUIDv7(h.service.now())
	if err != nil {
		requestID = "request-id-unavailable"
	}
	w.Header().Set("X-Request-ID", requestID)
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			h.fail(w, requestID, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		h.fail(w, requestID, http.StatusNotFound, "NOT_FOUND", "resource was not found")
		return
	}
	if err := h.service.Converge(h.service.now()); err != nil {
		h.fail(w, requestID, http.StatusServiceUnavailable, "STATE_PERSISTENCE_FAILED", "durable control state is unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "identities" && r.Method == http.MethodPost:
		h.createIdentity(w, r, requestID)
	case len(parts) == 2 && parts[0] == "identities" && r.Method == http.MethodGet:
		h.getIdentity(w, requestID, parts[1])
	case len(parts) == 1 && parts[0] == "sessions" && r.Method == http.MethodPost:
		h.createSession(w, r, requestID)
	case len(parts) == 1 && parts[0] == "sessions":
		// Deliberately absent: known IDs are public, discovery is not.
		h.fail(w, requestID, http.StatusNotFound, "NOT_FOUND", "session discovery is not exposed")
	case len(parts) == 2 && parts[0] == "sessions" && r.Method == http.MethodGet:
		h.getSession(w, requestID, parts[1])
	case len(parts) == 2 && parts[0] == "enrollments" && r.Method == http.MethodGet:
		h.getEnrollment(w, requestID, parts[1])
	case len(parts) == 2 && parts[0] == "placements" && r.Method == http.MethodGet:
		h.getPlacement(w, requestID, parts[1])
	case len(parts) == 2 && parts[0] == "executions" && r.Method == http.MethodGet:
		h.getExecution(w, requestID, parts[1])
	case len(parts) == 3 && parts[0] == "executions" && parts[2] == "evidence-sets" && r.Method == http.MethodPost:
		h.createEvidenceSet(w, r, requestID, parts[1])
	case len(parts) == 2 && parts[0] == "evidence-sets" && r.Method == http.MethodGet:
		h.getEvidenceSet(w, requestID, parts[1])
	case len(parts) == 2 && parts[0] == "captures" && strings.HasSuffix(parts[1], ":close") && r.Method == http.MethodPost:
		h.closeCapture(w, r, requestID, strings.TrimSuffix(parts[1], ":close"))
	case len(parts) == 2 && parts[0] == "captures" && strings.HasSuffix(parts[1], ":normalize") && r.Method == http.MethodPost:
		h.normalizeCapture(w, r, requestID, strings.TrimSuffix(parts[1], ":normalize"))
	case len(parts) == 3 && parts[0] == "captures" && parts[2] == "events" && r.Method == http.MethodGet:
		h.readCaptureEvents(w, r, requestID, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "events" && r.Method == http.MethodGet:
		h.readSessionEvents(w, r, requestID, parts[1])
	case len(parts) == 3 && parts[0] == "captures" && parts[2] == "objects" && r.Method == http.MethodPost:
		h.storeCaptureObject(w, r, requestID, parts[1])
	case len(parts) == 3 && parts[0] == "captures" && parts[2] == "batches" && r.Method == http.MethodPost:
		h.ingestCaptureBatch(w, r, requestID, parts[1])
	case len(parts) == 2 && parts[0] == "captures" && r.Method == http.MethodGet:
		h.getCapture(w, requestID, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "captures" && r.Method == http.MethodGet:
		h.listSessionCaptures(w, requestID, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "enrollments" && r.Method == http.MethodPost:
		h.enroll(w, r, requestID, parts[1])
	case len(parts) == 3 && parts[0] == "enrollments" && parts[2] == "reports" && r.Method == http.MethodPost:
		h.report(w, r, requestID, parts[1])
	case len(parts) == 2 && parts[0] == "enrollments" && strings.HasSuffix(parts[1], ":heartbeat") && r.Method == http.MethodPost:
		h.heartbeat(w, r, requestID, strings.TrimSuffix(parts[1], ":heartbeat"))
	default:
		h.fail(w, requestID, http.StatusNotFound, "NOT_FOUND", "resource was not found")
	}
}

func (h *Handler) createIdentity(w http.ResponseWriter, r *http.Request, requestID string) {
	var req CreateIdentityRequest
	if !decodeJSON(w, r, &req, requestID) {
		return
	}
	response, err := h.service.CreateIdentity(req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) getIdentity(w http.ResponseWriter, requestID, accountID string) {
	identity, err := h.service.GetIdentity(accountID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, identity)
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request, requestID string) {
	var req CreateSessionRequest
	if !decodeJSON(w, r, &req, requestID) {
		return
	}
	response, err := h.service.CreateSession(bearer(r), r.Header.Get("Idempotency-Key"), req)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) getSession(w http.ResponseWriter, requestID, sessionID string) {
	session, err := h.service.GetSession(sessionID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *Handler) getEnrollment(w http.ResponseWriter, requestID, clientID string) {
	enrollment, err := h.service.GetEnrollment(clientID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollment)
}

func (h *Handler) getPlacement(w http.ResponseWriter, requestID, placementID string) {
	placement, err := h.service.GetPlacement(placementID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, placement)
}

func (h *Handler) getExecution(w http.ResponseWriter, requestID, executionID string) {
	execution, err := h.service.GetExecution(executionID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, execution)
}

func (h *Handler) createEvidenceSet(w http.ResponseWriter, r *http.Request, requestID, executionID string) {
	var req ReconcileEvidenceRequest
	if !decodeJSON(w, r, &req, requestID) {
		return
	}
	set, err := h.service.CreateEvidenceSet(bearer(r), executionID, r.Header.Get("Idempotency-Key"), req)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusCreated, set)
}

func (h *Handler) getEvidenceSet(w http.ResponseWriter, requestID, evidenceSetID string) {
	set, err := h.service.GetEvidenceSet(evidenceSetID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func (h *Handler) ingestCaptureBatch(w http.ResponseWriter, r *http.Request, requestID, captureID string) {
	var req IngestBatchRequest
	if !decodeJSONLimited(w, r, &req, requestID, MaxCaptureBatchBytes) {
		return
	}
	receipt, err := h.service.IngestCaptureBatch(bearer(r), captureID, r.Header.Get("Idempotency-Key"), req)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (h *Handler) closeCapture(w http.ResponseWriter, r *http.Request, requestID, captureID string) {
	var req CaptureCloseRequest
	if !decodeJSON(w, r, &req, requestID) {
		return
	}
	record, err := h.service.CloseCapture(bearer(r), captureID, req)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) storeCaptureObject(w http.ResponseWriter, r *http.Request, requestID, captureID string) {
	// The bytes arrive directly rather than through an upload reservation.
	// A reservation would have to hand back a location, and a public response
	// naming where to send bytes is exactly what the redaction oracle exists
	// to keep out of this contract.
	body, ok := readBodyLimited(w, r, requestID, capture.MaxObjectBytes)
	if !ok {
		return
	}
	manifest, err := h.service.StoreCaptureObject(bearer(r), captureID, r.Header.Get("Content-Type"), body)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusCreated, manifest)
}

func (h *Handler) readCaptureEvents(w http.ResponseWriter, r *http.Request, requestID, captureID string) {
	page, err := h.service.ReadCaptureEvents(captureID, r.URL.Query().Get("cursor"), r.URL.Query().Get("kind"), pageLimit(r))
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) readSessionEvents(w http.ResponseWriter, r *http.Request, requestID, sessionID string) {
	page, err := h.service.ReadSessionEvents(sessionID, r.URL.Query().Get("cursor"), r.URL.Query().Get("kind"), pageLimit(r))
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// pageLimit treats an unparseable limit as "unspecified" rather than an error:
// the value is clamped anyway, so rejecting the request would trade a working
// read for a lecture.
func pageLimit(r *http.Request) int {
	value, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		return 0
	}
	return value
}

func (h *Handler) normalizeCapture(w http.ResponseWriter, r *http.Request, requestID, captureID string) {
	var req NormalizeRequest
	if !decodeJSON(w, r, &req, requestID) {
		return
	}
	record, err := h.service.NormalizeCapture(bearer(r), captureID, req)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handler) getCapture(w http.ResponseWriter, requestID, captureID string) {
	record, err := h.service.GetCapture(captureID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) listSessionCaptures(w http.ResponseWriter, requestID, sessionID string) {
	captures, err := h.service.ListSessionCaptures(sessionID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": SchemaVersion, "captures": captures})
}

func (h *Handler) enroll(w http.ResponseWriter, r *http.Request, requestID, sessionID string) {
	var req EnrollmentRequest
	if !decodeJSON(w, r, &req, requestID) {
		return
	}
	response, err := h.service.Enroll(bearer(r), r.Header.Get("X-Session-Join-Credential"), sessionID, r.Header.Get("Idempotency-Key"), req)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request, requestID, clientID string) {
	var req LifecycleReportRequest
	if !decodeJSON(w, r, &req, requestID) {
		return
	}
	response, err := h.service.Report(bearer(r), clientID, r.Header.Get("Idempotency-Key"), req)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) heartbeat(w http.ResponseWriter, r *http.Request, requestID, clientID string) {
	response, err := h.service.Heartbeat(bearer(r), clientID)
	if err != nil {
		h.writeDomainError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[len("Bearer "):])
}

// defaultBodyLimit is the control-plane request cap. Capture batches negotiate
// a larger one through their stream offer; everything else stays here.
const defaultBodyLimit = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, requestID string) bool {
	return decodeJSONLimited(w, r, destination, requestID, defaultBodyLimit)
}

// decodeJSONLimited distinguishes "this body is too big" from "this body is
// malformed". io.LimitReader cannot: it truncates, and the truncation surfaces
// as a JSON syntax error, which sends the client off to debug its serializer
// over what is really a size limit. http.MaxBytesReader reports the overrun.
func decodeJSONLimited(w http.ResponseWriter, r *http.Request, destination any, requestID string, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if bodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, ErrorResponse{Code: "PAYLOAD_TOO_LARGE", Message: "request body exceeds the negotiated limit", RequestID: requestID})
			return false
		}
		writeError(w, http.StatusBadRequest, ErrorResponse{Code: "INVALID_JSON", Message: "request body is invalid", RequestID: requestID})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if bodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, ErrorResponse{Code: "PAYLOAD_TOO_LARGE", Message: "request body exceeds the negotiated limit", RequestID: requestID})
			return false
		}
		writeError(w, http.StatusBadRequest, ErrorResponse{Code: "INVALID_JSON", Message: "request body must contain one JSON value", RequestID: requestID})
		return false
	}
	return true
}

func bodyTooLarge(err error) bool {
	var overrun *http.MaxBytesError
	return errors.As(err, &overrun)
}

// readBodyLimited returns the raw bytes of a non-JSON body, used by the heavy
// capture object lane where the payload is opaque.
func readBodyLimited(w http.ResponseWriter, r *http.Request, requestID string, limit int64) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if bodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, ErrorResponse{Code: "PAYLOAD_TOO_LARGE", Message: "request body exceeds the negotiated limit", RequestID: requestID})
			return nil, false
		}
		writeError(w, http.StatusBadRequest, ErrorResponse{Code: "INVALID_BODY", Message: "request body could not be read", RequestID: requestID})
		return nil, false
	}
	return body, true
}

func (h *Handler) writeDomainError(w http.ResponseWriter, requestID string, err error) {
	var domain *DomainError
	if errors.As(err, &domain) {
		status := http.StatusBadRequest
		switch domain.Code {
		case "TOKEN_INVALID", "JOIN_CREDENTIAL_INVALID", "IDENTITY_SUSPENDED":
			status = http.StatusUnauthorized
		case "IDENTITY_NOT_FOUND", "SESSION_NOT_FOUND", "ENROLLMENT_NOT_FOUND", "PLACEMENT_NOT_FOUND", "EXECUTION_NOT_FOUND", "EVIDENCE_SET_NOT_FOUND", "CAPTURE_NOT_FOUND":
			status = http.StatusNotFound
		case "HANDLE_TAKEN", "IDEMPOTENCY_CONFLICT", "SEQUENCE_CONFLICT", "OBSERVATION_ADJUDICATION_FORBIDDEN":
			status = http.StatusConflict
		case "PAYLOAD_TOO_LARGE":
			status = http.StatusRequestEntityTooLarge
		case "SESSION_NOT_ADMITTING", "LEASE_EXPIRED":
			status = http.StatusGone
		case "CAPTURE_NOT_OPEN", "CAPTURE_NOT_CLOSED", "CAPTURE_IS_DERIVED":
			status = http.StatusConflict
		case "NORMALIZER_UNKNOWN":
			status = http.StatusNotFound
		case "STATE_INTEGRITY_ERROR":
			status = http.StatusInternalServerError
		case "STATE_PERSISTENCE_FAILED":
			status = http.StatusServiceUnavailable
		case "OBSERVATION_UNREADABLE":
			status = http.StatusInternalServerError
		}
		h.fail(w, requestID, status, domain.Code, domain.Message)
		return
	}
	h.fail(w, requestID, http.StatusInternalServerError, "INTERNAL_ERROR", "internal service error")
}

func (h *Handler) fail(w http.ResponseWriter, requestID string, status int, code, message string) {
	writeError(w, status, ErrorResponse{Code: code, Message: message, RequestID: requestID})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, value ErrorResponse) {
	writeJSON(w, status, value)
}
