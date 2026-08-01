package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/auth"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/jobs"
	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	nodestate "github.com/Ani-HQ/thirdshift/internal/node/state"
	"github.com/Ani-HQ/thirdshift/internal/shared/ids"
	"github.com/Ani-HQ/thirdshift/internal/shared/nodeauth"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
	"nhooyr.io/websocket"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type Options struct {
	Version                string
	Registration           registration.Service
	SessionStore           SessionStore
	TokenSigner            auth.TokenSigner
	ProtocolValidator      *protocol.Validator
	JobService             *jobs.Service
	OperatorStore          *operatorstore.Store
	CatalogDir             string
	OperatorToken          string
	HeartbeatInterval      time.Duration
	StatusCacheTTL         time.Duration
	RequesterRegionHeader  string
	WaitlistLimitPerMinute int
	Now                    func() time.Time
	Logger                 *slog.Logger
}

type SessionStore interface {
	OpenSession(ctx context.Context, nodeID, protocolVersion, remoteAddr string, now time.Time) (string, error)
	RecordHeartbeat(ctx context.Context, sessionID string, heartbeat protocol.NodeHeartbeatPayload, receivedAt time.Time) error
	RecordStateChanged(ctx context.Context, stateChanged protocol.NodeStateChangedPayload, receivedAt time.Time) error
	RecordSafetyEvent(ctx context.Context, event protocol.NodeSafetyEventPayload, receivedAt time.Time) error
	CloseSession(ctx context.Context, sessionID, nodeID string, now time.Time) error
	ListNodes(ctx context.Context) ([]registration.NodeSummary, error)
	PublicKeyForNode(ctx context.Context, nodeID string) (string, error)
}

func NewMux(version string) http.Handler {
	return NewMuxWithOptions(Options{Version: version})
}

func NewMuxWithOptions(opts Options) http.Handler {
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 15 * time.Second
	}
	if opts.TokenSigner.Now == nil {
		opts.TokenSigner.Now = opts.now
	}
	if opts.TokenSigner.TTL <= 0 {
		opts.TokenSigner.TTL = time.Hour
	}
	if opts.Registration.Now == nil {
		opts.Registration.Now = opts.now
	}
	if opts.ProtocolValidator == nil {
		opts.ProtocolValidator, _ = protocol.NewValidator("")
	}
	if opts.JobService != nil && opts.JobService.RateLimiter == nil {
		opts.JobService.RateLimiter = &jobs.RateLimiter{LimitPerMinute: 60, Now: opts.now}
	}
	if opts.StatusCacheTTL <= 0 {
		opts.StatusCacheTTL = 10 * time.Second
	}
	if opts.RequesterRegionHeader == "" {
		opts.RequesterRegionHeader = "X-Geo-Region"
	}
	if opts.WaitlistLimitPerMinute <= 0 {
		opts.WaitlistLimitPerMinute = 5
	}

	mux := http.NewServeMux()
	statusCache := &publicStatusCache{}
	waitlistLimiter := newWaitlistRateLimiter(opts.WaitlistLimitPerMinute, time.Minute, opts.now)
	mux.HandleFunc("GET /healthz", healthHandler(opts.Version))
	mux.HandleFunc("GET /v1/status", publicCORS(opts.publicStatusHandler(statusCache)))
	mux.HandleFunc("POST /v1/waitlist", publicCORS(opts.waitlistSignupHandler(waitlistLimiter)))
	mux.HandleFunc("OPTIONS /v1/status", publicCORSPreflight)
	mux.HandleFunc("OPTIONS /v1/waitlist", publicCORSPreflight)
	mux.HandleFunc("GET /v1/nodes/{node_id}/card", publicCORS(opts.publicNodeCardHandler()))
	mux.HandleFunc("POST /internal/v1/orgs", opts.operatorOnly(opts.createOrgHandler()))
	mux.HandleFunc("POST /internal/v1/api-keys", opts.operatorOnly(opts.createAPIKeyHandler()))
	mux.HandleFunc("POST /internal/v1/catalog/sync", opts.operatorOnly(opts.catalogSyncHandler()))
	mux.HandleFunc("POST /internal/v1/invites", opts.operatorOnly(opts.createInviteHandler()))
	mux.HandleFunc("GET /internal/v1/nodes", opts.operatorOnly(opts.nodesListHandler()))
	mux.HandleFunc("GET /internal/v1/overview", opts.operatorOnly(opts.operatorOverviewHandler()))
	mux.HandleFunc("GET /internal/v1/alerts", opts.operatorOnly(opts.operatorAlertsHandler()))
	mux.HandleFunc("GET /internal/v1/nodes/{node_id}", opts.operatorOnly(opts.operatorNodeDetailHandler()))
	mux.HandleFunc("POST /internal/v1/nodes/{node_id}/drain", opts.operatorOnly(opts.operatorNodeActionHandler("drain")))
	mux.HandleFunc("POST /internal/v1/nodes/{node_id}/pause", opts.operatorOnly(opts.operatorNodeActionHandler("pause")))
	mux.HandleFunc("POST /internal/v1/nodes/{node_id}/quarantine", opts.operatorOnly(opts.operatorNodeActionHandler("quarantine")))
	mux.HandleFunc("GET /internal/v1/models", opts.operatorOnly(opts.operatorModelsHandler()))
	mux.HandleFunc("GET /internal/v1/jobs", opts.operatorOnly(opts.operatorJobsHandler()))
	mux.HandleFunc("GET /internal/v1/jobs/{job_id}", opts.operatorOnly(opts.operatorJobDetailHandler()))
	mux.HandleFunc("POST /internal/v1/jobs/{job_id}/retry", opts.operatorOnly(opts.operatorJobActionHandler("retry")))
	mux.HandleFunc("POST /internal/v1/jobs/{job_id}/cancel", opts.operatorOnly(opts.operatorJobActionHandler("cancel")))
	mux.HandleFunc("GET /internal/v1/ledger", opts.operatorOnly(opts.operatorLedgerHandler()))
	mux.HandleFunc("POST /internal/v1/ledger/credits/release", opts.operatorOnly(opts.operatorCreditsReleaseHandler()))
	mux.HandleFunc("POST /internal/v1/payout-batches", opts.operatorOnly(opts.operatorPayoutCreateHandler()))
	mux.HandleFunc("GET /internal/v1/payout-batches/{batch_id}/export", opts.operatorOnly(opts.operatorPayoutExportHandler()))
	mux.HandleFunc("POST /internal/v1/payout-batches/{batch_id}/confirm", opts.operatorOnly(opts.operatorPayoutConfirmHandler()))
	mux.HandleFunc("GET /internal/v1/audit", opts.operatorOnly(opts.operatorAuditHandler()))
	mux.HandleFunc("POST /internal/v1/fleets", opts.operatorOnly(opts.operatorFleetCreateHandler()))
	mux.HandleFunc("POST /internal/v1/fleets/{fleet_id}/region", opts.operatorOnly(opts.operatorFleetRegionHandler()))
	mux.HandleFunc("GET /internal/v1/fleets/{fleet_id}/report", opts.operatorOnly(opts.operatorFleetReportHandler()))
	mux.HandleFunc("POST /internal/v1/nodes/{node_id}/region", opts.operatorOnly(opts.operatorNodeRegionHandler()))
	mux.HandleFunc("GET /internal/v1/waitlist", opts.operatorOnly(opts.operatorWaitlistHandler()))
	mux.HandleFunc("GET /internal/v1/waitlist/export", opts.operatorOnly(opts.operatorWaitlistExportHandler()))
	mux.HandleFunc("GET /v1/models", opts.developerOnly(opts.modelsHandler()))
	mux.HandleFunc("POST /v1/chat/completions", opts.developerOnly(opts.chatCompletionsHandler()))
	mux.HandleFunc("POST /v1/jobs", opts.developerOnly(opts.createJobHandler()))
	mux.HandleFunc("GET /v1/jobs/{job_id}", opts.developerOnly(opts.getJobHandler()))
	mux.HandleFunc("POST /v1/jobs/{job_id}/cancel", opts.developerOnly(opts.cancelJobHandler()))
	mux.HandleFunc("POST /v1/node/register", opts.registerNodeHandler())
	mux.HandleFunc("POST /v1/node/token", opts.exchangeBootstrapHandler())
	mux.HandleFunc("POST /v1/node/token/refresh", opts.refreshTokenHandler())
	mux.HandleFunc("GET /v1/node/session", opts.sessionHandler())
	return mux
}

func healthHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthResponse{
			Status:  "ok",
			Version: version,
		})
	}
}

func (o Options) createInviteHandler() http.HandlerFunc {
	type request struct {
		FleetID          string `json:"fleet_id"`
		ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
	}
	type response struct {
		InviteID  string    `json:"invite_id"`
		FleetID   string    `json:"fleet_id"`
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		service := o.Registration
		if req.ExpiresInSeconds > 0 {
			service.InviteTTL = time.Duration(req.ExpiresInSeconds) * time.Second
		}
		invite, err := service.CreateInvite(r.Context(), req.FleetID)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, response{
			InviteID:  invite.ID,
			FleetID:   invite.FleetID,
			Token:     invite.Token,
			ExpiresAt: invite.ExpiresAt,
		})
	}
}

func (o Options) registerNodeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registration.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		result, err := o.Registration.RegisterNode(r.Context(), req)
		if err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (o Options) exchangeBootstrapHandler() http.HandlerFunc {
	type request struct {
		NodeID         string `json:"node_id"`
		BootstrapToken string `json:"bootstrap_token"`
	}
	type response struct {
		AccessToken          string    `json:"access_token"`
		AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := o.Registration.ConsumeBootstrap(r.Context(), req.NodeID, req.BootstrapToken); err != nil {
			writeError(w, statusForError(err), err.Error())
			return
		}
		token, expiresAt, err := o.TokenSigner.Issue(req.NodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response{AccessToken: token, AccessTokenExpiresAt: expiresAt})
	}
}

func (o Options) refreshTokenHandler() http.HandlerFunc {
	type response struct {
		AccessToken          string    `json:"access_token"`
		AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if o.SessionStore == nil {
			writeError(w, http.StatusServiceUnavailable, "node store is not configured")
			return
		}
		var req nodeauth.RefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		publicEncoded, err := o.SessionStore.PublicKeyForNode(r.Context(), req.NodeID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "node key not found")
			return
		}
		publicRaw, err := base64.StdEncoding.DecodeString(publicEncoded)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "stored node public key is invalid")
			return
		}
		if err := nodeauth.VerifyRefresh(ed25519.PublicKey(publicRaw), req, o.now(), 5*time.Minute); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		token, expiresAt, err := o.TokenSigner.Issue(req.NodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response{AccessToken: token, AccessTokenExpiresAt: expiresAt})
	}
}

func (o Options) nodesListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o.OperatorStore != nil {
			nodes, err := o.OperatorStore.ListNodes(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
			return
		}
		if o.SessionStore == nil {
			writeError(w, http.StatusServiceUnavailable, "node store is not configured")
			return
		}
		nodes, err := o.SessionStore.ListNodes(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	}
}

func (o Options) sessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o.SessionStore == nil {
			writeError(w, http.StatusServiceUnavailable, "node store is not configured")
			return
		}
		if o.ProtocolValidator == nil {
			writeError(w, http.StatusInternalServerError, "protocol validator is not configured")
			return
		}
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := o.TokenSigner.Verify(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionDisabled,
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		o.handleSession(r.Context(), conn, claims.NodeID, r.RemoteAddr)
	}
}

func (o Options) handleSession(ctx context.Context, conn *websocket.Conn, tokenNodeID, remoteAddr string) {
	_, helloData, err := conn.Read(ctx)
	if err != nil {
		return
	}
	hello, err := o.ProtocolValidator.ValidateEnvelope(helloData)
	if err != nil || hello.Type != protocol.TypeNodeHello {
		// Logged because a schema rejection here is indistinguishable from a
		// network drop at the node, and is exactly what a coordinator running
		// an older protocol than its nodes produces.
		o.logger().WarnContext(ctx, "rejected node hello", "node_id", tokenNodeID, "type", hello.Type, "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}
	var helloPayload protocol.NodeHelloPayload
	if err := json.Unmarshal(hello.Payload, &helloPayload); err != nil || helloPayload.NodeID != tokenNodeID {
		_ = conn.Close(websocket.StatusPolicyViolation, "node identity mismatch")
		return
	}
	sessionID, err := o.SessionStore.OpenSession(ctx, tokenNodeID, hello.ProtocolVersion, remoteAddr, o.now())
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session store failed")
		return
	}
	defer o.SessionStore.CloseSession(context.Background(), sessionID, tokenNodeID, o.now())

	if err := o.writeEnvelope(ctx, conn, protocol.TypeSessionAccepted, protocol.SessionAcceptedPayload{
		NodeID:                   tokenNodeID,
		SessionID:                sessionID,
		HeartbeatIntervalSeconds: heartbeatIntervalSeconds(o.HeartbeatInterval),
		AcceptedAt:               o.now(),
	}); err != nil {
		return
	}
	var writeMu sync.Mutex
	if o.JobService != nil {
		detach := o.JobService.AttachSession(tokenNodeID, sessionID, func(ctx context.Context, typ protocol.MessageType, payload any) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			return o.writeEnvelope(ctx, conn, typ, payload)
		})
		defer detach()
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		envelope, err := o.ProtocolValidator.ValidateEnvelope(data)
		if err != nil {
			o.logger().WarnContext(ctx, "rejected node message", "node_id", tokenNodeID, "session_id", sessionID, "error", err)
			_ = conn.Close(websocket.StatusPolicyViolation, "invalid protocol message")
			return
		}
		switch envelope.Type {
		case protocol.TypeNodeHeartbeat:
			var heartbeat protocol.NodeHeartbeatPayload
			if err := json.Unmarshal(envelope.Payload, &heartbeat); err != nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid heartbeat payload")
				return
			}
			if heartbeat.NodeID != tokenNodeID {
				_ = conn.Close(websocket.StatusPolicyViolation, "node identity mismatch")
				return
			}
			if _, err := nodestate.Parse(heartbeat.State); err != nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid node state")
				return
			}
			if err := o.SessionStore.RecordHeartbeat(ctx, sessionID, heartbeat, o.now()); err != nil {
				_ = conn.Close(websocket.StatusInternalError, "heartbeat store failed")
				return
			}
		case protocol.TypeNodeStateChanged:
			var stateChanged protocol.NodeStateChangedPayload
			if err := json.Unmarshal(envelope.Payload, &stateChanged); err != nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid state change payload")
				return
			}
			if stateChanged.NodeID != tokenNodeID {
				_ = conn.Close(websocket.StatusPolicyViolation, "node identity mismatch")
				return
			}
			if _, err := nodestate.Parse(stateChanged.State); err != nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid node state")
				return
			}
			if err := o.SessionStore.RecordStateChanged(ctx, stateChanged, o.now()); err != nil {
				_ = conn.Close(websocket.StatusInternalError, "state change store failed")
				return
			}
		case protocol.TypeNodeSafetyEvent:
			var event protocol.NodeSafetyEventPayload
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid safety event payload")
				return
			}
			if event.NodeID != tokenNodeID {
				_ = conn.Close(websocket.StatusPolicyViolation, "node identity mismatch")
				return
			}
			if err := o.SessionStore.RecordSafetyEvent(ctx, event, o.now()); err != nil {
				_ = conn.Close(websocket.StatusInternalError, "safety event store failed")
				return
			}
		case protocol.TypeJobAccepted, protocol.TypeJobStarted, protocol.TypeJobRejected, protocol.TypeJobFailed, protocol.TypeJobCompleted:
			if o.JobService == nil {
				_ = conn.Close(websocket.StatusUnsupportedData, "job service is not configured")
				return
			}
			if err := o.JobService.HandleNodeMessage(ctx, tokenNodeID, sessionID, envelope); err != nil {
				o.logger().ErrorContext(ctx, "job message handling failed", "node_id", tokenNodeID, "session_id", sessionID, "message_type", envelope.Type, "error", err)
				_ = conn.Close(websocket.StatusInternalError, "job message handling failed")
				return
			}
		default:
			_ = conn.Close(websocket.StatusUnsupportedData, "unsupported message type")
			return
		}
	}
}

func (o Options) writeEnvelope(ctx context.Context, conn *websocket.Conn, typ protocol.MessageType, payload any) error {
	messageID, err := ids.New("msg")
	if err != nil {
		return err
	}
	envelope, err := protocol.NewEnvelope(messageID, typ, o.now(), payload)
	if err != nil {
		return err
	}
	data, err := o.ProtocolValidator.MarshalAndValidate(envelope)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func (o Options) operatorOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o.OperatorToken == "" {
			writeError(w, http.StatusUnauthorized, "operator token is not configured")
			return
		}
		if bearerToken(r) != o.OperatorToken {
			writeError(w, http.StatusUnauthorized, "invalid operator token")
			return
		}
		next(w, r)
	}
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func heartbeatIntervalSeconds(interval time.Duration) int {
	if interval <= 0 {
		return 15
	}
	seconds := int((interval + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, registration.ErrInvalidInvite), errors.Is(err, registration.ErrInvalidBootstrap):
		return http.StatusUnauthorized
	case errors.Is(err, registration.ErrInviteUsed), errors.Is(err, registration.ErrBootstrapUsed):
		return http.StatusConflict
	case errors.Is(err, registration.ErrInviteExpired), errors.Is(err, registration.ErrBootstrapExpired):
		return http.StatusGone
	case errors.Is(err, registration.ErrRepositoryMissing):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(normalizeNilSlicesForJSON(body))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func normalizeNilSlicesForJSON(body any) any {
	if body == nil {
		return body
	}
	normalized := normalizeNilSlices(reflect.ValueOf(body))
	if !normalized.IsValid() || !normalized.CanInterface() {
		return body
	}
	return normalized.Interface()
}

func normalizeNilSlices(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Type() == rawMessageType {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		return normalizeNilSlices(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return value
		}
		normalized := normalizeNilSlices(value.Elem())
		if !normalized.IsValid() || !normalized.Type().AssignableTo(value.Elem().Type()) {
			return value
		}
		out := reflect.New(value.Elem().Type())
		out.Elem().Set(normalized)
		return out
	case reflect.Struct:
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for idx := 0; idx < value.NumField(); idx++ {
			fieldInfo := value.Type().Field(idx)
			if fieldInfo.PkgPath != "" {
				continue
			}
			normalized := normalizeNilSlices(value.Field(idx))
			target := out.Field(idx)
			if normalized.IsValid() && target.CanSet() && normalized.Type().AssignableTo(target.Type()) {
				target.Set(normalized)
			}
		}
		return out
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return value
		}
		if value.IsNil() {
			return reflect.MakeSlice(value.Type(), 0, 0)
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for idx := 0; idx < value.Len(); idx++ {
			normalized := normalizeNilSlices(value.Index(idx))
			target := out.Index(idx)
			if normalized.IsValid() && normalized.Type().AssignableTo(target.Type()) {
				target.Set(normalized)
			} else {
				target.Set(value.Index(idx))
			}
		}
		return out
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			normalized := normalizeNilSlices(iter.Value())
			if normalized.IsValid() && normalized.Type().AssignableTo(value.Type().Elem()) {
				out.SetMapIndex(iter.Key(), normalized)
			} else if normalized.IsValid() && normalized.Type().ConvertibleTo(value.Type().Elem()) {
				out.SetMapIndex(iter.Key(), normalized.Convert(value.Type().Elem()))
			} else {
				out.SetMapIndex(iter.Key(), iter.Value())
			}
		}
		return out
	default:
		return value
	}
}

// publicCORS wraps unauthenticated public endpoints so browser pages on any
// origin (status embeds, marketing sites) can read them. Never apply this to
// authenticated or internal routes.
func publicCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next(w, r)
	}
}

func publicCORSPreflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}
