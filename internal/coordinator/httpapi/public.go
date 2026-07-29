package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/jobs"
	"github.com/anianroid/thirdshift/internal/shared/ids"
)

const chatCompletionsEndpoint = "/v1/chat/completions"

type developerHandler func(http.ResponseWriter, *http.Request, jobs.APIKeyPrincipal)

func (o Options) createOrgHandler() http.HandlerFunc {
	type request struct {
		Name string `json:"name"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if o.JobService == nil {
			writeError(w, http.StatusServiceUnavailable, "job service is not configured")
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		orgID, err := o.JobService.Store.CreateOrg(r.Context(), strings.TrimSpace(req.Name))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"org_id": orgID, "name": strings.TrimSpace(req.Name)})
	}
}

func (o Options) createAPIKeyHandler() http.HandlerFunc {
	type request struct {
		OrgID  string   `json:"org_id"`
		Name   string   `json:"name,omitempty"`
		Models []string `json:"models,omitempty"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if o.JobService == nil {
			writeError(w, http.StatusServiceUnavailable, "job service is not configured")
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		key, err := jobs.NewAPIKey()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		keyID, err := o.JobService.Store.CreateAPIKey(r.Context(), req.OrgID, req.Name, key, req.Models)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"api_key_id": keyID, "key": key})
	}
}

func (o Options) catalogSyncHandler() http.HandlerFunc {
	type request struct {
		CatalogDir string `json:"catalog_dir,omitempty"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if o.JobService == nil {
			writeError(w, http.StatusServiceUnavailable, "job service is not configured")
			return
		}
		var req request
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
		catalogDir := firstNonEmpty(req.CatalogDir, o.CatalogDir, "models/catalog")
		count, err := o.JobService.Store.SyncCatalog(r.Context(), catalogDir)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"synced": count})
	}
}

func (o Options) modelsHandler() developerHandler {
	return func(w http.ResponseWriter, r *http.Request, _ jobs.APIKeyPrincipal) {
		modelsOut, err := o.JobService.Store.ListModels(r.Context(), o.now().Add(-o.staleAfter()))
		if err != nil {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Could not list models.", Retryable: true, Status: 500}, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": modelsOut})
	}
}

func (o Options) chatCompletionsHandler() developerHandler {
	return func(w http.ResponseWriter, r *http.Request, principal jobs.APIKeyPrincipal) {
		requestID := newRequestID()
		body, apiErr := readPublicBody(r.Body, jobs.MaxPublicRequestBytes)
		if apiErr.Code != "" {
			o.writeAPIError(w, apiErr, requestID)
			return
		}
		req, apiErr := jobs.DecodeChatCompletionRequest(body)
		if apiErr.Code != "" {
			o.writeAPIError(w, apiErr, requestID)
			return
		}
		model, apiErr := o.authorizedModel(r.Context(), principal, req.Model)
		if apiErr.Code != "" {
			o.writeAPIError(w, apiErr, requestID)
			return
		}
		chatRequest, apiErr := jobs.ValidateChatCompletion(req, model, len(body))
		if apiErr.Code != "" {
			o.writeAPIError(w, apiErr, requestID)
			return
		}
		bodyHash := jobs.RequestHash(body)
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if idempotencyKey != "" {
			record, found, err := o.JobService.Store.IdempotencyRecord(r.Context(), principal.ID, chatCompletionsEndpoint, idempotencyKey)
			if err != nil {
				o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Could not read idempotency record.", Retryable: true, Status: 500}, requestID)
				return
			}
			if found {
				decision := jobs.ResolveIdempotency(record, found, bodyHash)
				if decision.Error.Code != "" {
					o.writeAPIError(w, decision.Error, requestID)
					return
				}
				if decision.Replay {
					o.logger().InfoContext(r.Context(), "idempotent chat completion replayed", "request_id", requestID, "job_id", record.JobID, "api_key_id", principal.ID)
					writeJSON(w, decision.Status, json.RawMessage(decision.Body))
					return
				}
			}
		}

		timeout := 120 * time.Second
		if o.JobService.SyncTimeout > 0 && o.JobService.SyncTimeout < timeout {
			timeout = o.JobService.SyncTimeout
		}
		deadlineAt := o.now().Add(timeout)
		jobID, err := o.JobService.Store.CreateJob(r.Context(), principal.OrganizationID, principal.ID, model.ID, "standard", req, deadlineAt)
		if err != nil {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Could not create job.", Retryable: true, Status: 500}, requestID)
			return
		}
		o.logger().InfoContext(r.Context(), "chat completion queued", "request_id", requestID, "job_id", jobID, "api_key_id", principal.ID, "model", model.ID)
		waitCh := o.JobService.RegisterWait(jobID)
		if _, err := o.JobService.Schedule(r.Context(), jobID, model, chatRequest, deadlineAt); err != nil {
			o.JobService.AbandonWait(jobID)
			var apiErr jobs.APIError
			if errors.As(err, &apiErr) {
				_ = o.JobService.Store.FailJob(r.Context(), jobID, "", apiErr.Code, apiErr.Message, apiErr.Retryable, true, o.now())
				o.writeAPIError(w, apiErr, requestID)
				return
			}
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Could not schedule job.", Retryable: true, Status: 500}, requestID)
			return
		}
		waitCtx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		select {
		case result := <-waitCh:
			if result.Error.Code != "" {
				o.writeAPIError(w, result.Error, requestID)
				return
			}
			o.logger().InfoContext(r.Context(), "chat completion completed", "request_id", requestID, "job_id", jobID, "api_key_id", principal.ID, "attempts", result.Response.Thirdshift.Attempts)
			if idempotencyKey != "" {
				_ = o.JobService.Store.StoreIdempotencyResponse(r.Context(), principal.ID, chatCompletionsEndpoint, idempotencyKey, bodyHash, jobID, http.StatusOK, result.Response, o.now().Add(24*time.Hour))
				if record, found, err := o.JobService.Store.IdempotencyRecord(r.Context(), principal.ID, chatCompletionsEndpoint, idempotencyKey); err == nil && found && record.ResponseStatus > 0 {
					writeJSON(w, record.ResponseStatus, json.RawMessage(record.ResponseMetadata))
					return
				}
			}
			writeJSON(w, http.StatusOK, result.Response)
		case <-waitCtx.Done():
			o.JobService.AbandonWait(jobID)
			apiErr := jobs.APIError{Code: jobs.CodeJobTimeout, Message: "Job timed out waiting for completion.", Retryable: true, Status: 504}
			_ = o.JobService.Store.FailJob(context.Background(), jobID, "", apiErr.Code, apiErr.Message, apiErr.Retryable, true, o.now())
			o.writeAPIError(w, apiErr, requestID)
		}
	}
}

func (o Options) createJobHandler() developerHandler {
	return func(w http.ResponseWriter, r *http.Request, principal jobs.APIKeyPrincipal) {
		requestID := newRequestID()
		body, apiErr := readPublicBody(r.Body, jobs.MaxPublicRequestBytes)
		if apiErr.Code != "" {
			o.writeAPIError(w, apiErr, requestID)
			return
		}
		var req jobs.AsyncJobRequest
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInvalidRequest, Message: "Request body must be valid async job JSON.", Retryable: false, Status: 400}, requestID)
			return
		}
		if req.Model == "" {
			req.Model = req.Input.Model
		}
		req.Input.Model = req.Model
		model, apiErr := o.authorizedModel(r.Context(), principal, req.Model)
		if apiErr.Code != "" {
			o.writeAPIError(w, apiErr, requestID)
			return
		}
		if _, apiErr := jobs.ValidateChatCompletion(req.Input, model, len(body)); apiErr.Code != "" {
			o.writeAPIError(w, apiErr, requestID)
			return
		}
		if req.Priority == "" {
			req.Priority = "standard"
		}
		if req.Priority != "standard" && req.Priority != "low" && req.Priority != "high" {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInvalidRequest, Message: "priority must be standard, low, or high.", Retryable: false, Status: 400}, requestID)
			return
		}
		deadlineSeconds := req.DeadlineSeconds
		if deadlineSeconds == 0 {
			deadlineSeconds = 900
		}
		if deadlineSeconds < 1 || deadlineSeconds > 900 {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInvalidRequest, Message: "deadline_seconds must be between 1 and 900.", Retryable: false, Status: 400}, requestID)
			return
		}
		jobID, err := o.JobService.Store.CreateJob(r.Context(), principal.OrganizationID, principal.ID, model.ID, req.Priority, req, o.now().Add(time.Duration(deadlineSeconds)*time.Second))
		if err != nil {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Could not create job.", Retryable: true, Status: 500}, requestID)
			return
		}
		status, err := o.JobService.Store.GetJob(r.Context(), principal.OrganizationID, jobID)
		if err != nil {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Could not load job.", Retryable: true, Status: 500}, requestID)
			return
		}
		writeJSON(w, http.StatusAccepted, status)
	}
}

func (o Options) getJobHandler() developerHandler {
	return func(w http.ResponseWriter, r *http.Request, principal jobs.APIKeyPrincipal) {
		requestID := newRequestID()
		status, err := o.JobService.Store.GetJob(r.Context(), principal.OrganizationID, r.PathValue("job_id"))
		if err != nil {
			o.writeErr(w, err, requestID)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func (o Options) cancelJobHandler() developerHandler {
	return func(w http.ResponseWriter, r *http.Request, principal jobs.APIKeyPrincipal) {
		requestID := newRequestID()
		status, err := o.JobService.Store.CancelJob(r.Context(), principal.OrganizationID, r.PathValue("job_id"), o.now())
		if err != nil {
			o.writeErr(w, err, requestID)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func (o Options) developerOnly(next developerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o.JobService == nil {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Job service is not configured.", Retryable: true, Status: 503}, "")
			return
		}
		token := bearerToken(r)
		if token == "" {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeUnauthorized, Message: "Missing bearer token.", Retryable: false, Status: 401}, newRequestID())
			return
		}
		principal, err := o.JobService.Store.APIKeyByHash(r.Context(), jobs.HashAPIKey(token))
		if err != nil {
			o.writeErr(w, err, newRequestID())
			return
		}
		if o.JobService.RateLimiter != nil && !o.JobService.RateLimiter.Allow(principal.ID) {
			o.writeAPIError(w, jobs.APIError{Code: jobs.CodeQuotaExceeded, Message: "API key rate limit exceeded.", Retryable: true, Status: 429}, newRequestID())
			return
		}
		next(w, r, principal)
	}
}

func (o Options) authorizedModel(ctx context.Context, principal jobs.APIKeyPrincipal, modelID string) (jobs.ModelInfo, jobs.APIError) {
	if modelID == "" {
		return jobs.ModelInfo{}, jobs.APIError{Code: jobs.CodeInvalidRequest, Message: "model is required.", Retryable: false, Status: 400}
	}
	if !principal.AllowedModels[modelID] {
		return jobs.ModelInfo{}, jobs.APIError{Code: jobs.CodeModelNotFound, Message: "Model not found.", Retryable: false, Status: 404}
	}
	model, err := o.JobService.Store.ModelInfo(ctx, modelID, o.now().Add(-o.staleAfter()))
	if err != nil {
		var apiErr jobs.APIError
		if errors.As(err, &apiErr) {
			return jobs.ModelInfo{}, apiErr
		}
		return jobs.ModelInfo{}, jobs.APIError{Code: jobs.CodeInternalError, Message: "Could not load model.", Retryable: true, Status: 500}
	}
	return model, jobs.APIError{}
}

func readPublicBody(body io.Reader, maxBytes int64) ([]byte, jobs.APIError) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, jobs.APIError{Code: jobs.CodeInvalidRequest, Message: "Could not read request body.", Retryable: false, Status: 400}
	}
	if int64(len(data)) > maxBytes {
		return nil, jobs.APIError{Code: jobs.CodeInvalidRequest, Message: "Request body exceeds the public API byte limit.", Retryable: false, Status: 413}
	}
	return data, jobs.APIError{}
}

func (o Options) writeErr(w http.ResponseWriter, err error, requestID string) {
	var apiErr jobs.APIError
	if errors.As(err, &apiErr) {
		o.writeAPIError(w, apiErr, requestID)
		return
	}
	o.writeAPIError(w, jobs.APIError{Code: jobs.CodeInternalError, Message: "Internal server error.", Retryable: true, Status: 500}, requestID)
}

func (o Options) writeAPIError(w http.ResponseWriter, apiErr jobs.APIError, requestID string) {
	if apiErr.Status == 0 {
		apiErr.Status = http.StatusBadRequest
	}
	if apiErr.RequestID == "" {
		apiErr.RequestID = requestID
	}
	if apiErr.RequestID == "" {
		apiErr.RequestID = newRequestID()
	}
	writeJSON(w, apiErr.Status, map[string]jobs.APIError{"error": apiErr})
}

func newRequestID() string {
	requestID, err := ids.New("req")
	if err != nil {
		return "req_00000000000000000000000000"
	}
	return requestID
}

func (o Options) staleAfter() time.Duration {
	if o.JobService != nil && o.JobService.StaleAfter > 0 {
		return o.JobService.StaleAfter
	}
	return 45 * time.Second
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
