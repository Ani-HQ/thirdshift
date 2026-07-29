package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/auth"
	"github.com/anianroid/thirdshift/internal/coordinator/registration"
	nodestate "github.com/anianroid/thirdshift/internal/node/state"
	"github.com/anianroid/thirdshift/internal/shared/ids"
	"github.com/anianroid/thirdshift/internal/shared/nodeauth"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
	"nhooyr.io/websocket"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type Options struct {
	Version           string
	Registration      registration.Service
	SessionStore      SessionStore
	TokenSigner       auth.TokenSigner
	ProtocolValidator *protocol.Validator
	OperatorToken     string
	HeartbeatInterval time.Duration
	Now               func() time.Time
}

type SessionStore interface {
	OpenSession(ctx context.Context, nodeID, protocolVersion, remoteAddr string, now time.Time) (string, error)
	RecordHeartbeat(ctx context.Context, sessionID string, heartbeat protocol.NodeHeartbeatPayload, receivedAt time.Time) error
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(opts.Version))
	mux.HandleFunc("POST /internal/v1/invites", opts.operatorOnly(opts.createInviteHandler()))
	mux.HandleFunc("GET /internal/v1/nodes", opts.operatorOnly(opts.nodesListHandler()))
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

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		envelope, err := o.ProtocolValidator.ValidateEnvelope(data)
		if err != nil {
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
		default:
			_ = conn.Close(websocket.StatusUnsupportedData, "unsupported message type in M2")
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
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
