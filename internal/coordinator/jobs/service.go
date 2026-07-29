package jobs

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/anianroid/thirdshift/internal/shared/nodeauth"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
)

type SendFunc func(ctx context.Context, typ protocol.MessageType, payload any) error

type Service struct {
	Store        PGStore
	Scheduler    Scheduler
	RateLimiter  *RateLimiter
	StaleAfter   time.Duration
	LeaseTTL     time.Duration
	SyncTimeout  time.Duration
	Now          func() time.Time
	Logger       *slog.Logger
	mu           sync.Mutex
	sessions     map[string]sessionRef
	activeByNode map[string]activeJob
	pending      map[string]chan Result
	runs         map[string]*runState
}

type sessionRef struct {
	nodeID    string
	sessionID string
	send      SendFunc
}

type activeJob struct {
	jobID     string
	attemptID string
}

type runState struct {
	model      ModelInfo
	request    protocol.ChatRequest
	deadlineAt time.Time
	excluded   map[string]bool
	attempts   int
}

type Result struct {
	Response OpenAIResponse
	Error    APIError
}

func (s *Service) AttachSession(nodeID, sessionID string, send SendFunc) func() {
	s.mu.Lock()
	s.initLocked()
	s.sessions[nodeID] = sessionRef{nodeID: nodeID, sessionID: sessionID, send: send}
	s.mu.Unlock()
	return func() {
		var active activeJob
		s.mu.Lock()
		current := s.sessions[nodeID]
		if current.sessionID == sessionID {
			delete(s.sessions, nodeID)
			active = s.activeByNode[nodeID]
			delete(s.activeByNode, nodeID)
		}
		s.mu.Unlock()
		if active.jobID != "" {
			apiErr := APIError{Code: CodeJobFailed, Message: "Node session disconnected before completion.", Retryable: true, Status: 502}
			_ = s.handleAttemptFailure(context.Background(), nodeID, active.jobID, active.attemptID, apiErr, true, s.now(), CodeJobFailed)
		}
	}
}

func (s *Service) Schedule(ctx context.Context, jobID string, model ModelInfo, request protocol.ChatRequest, deadlineAt time.Time) (ScheduledAttempt, error) {
	s.mu.Lock()
	s.initLocked()
	s.runs[jobID] = &runState{model: model, request: request, deadlineAt: deadlineAt, excluded: map[string]bool{}}
	s.mu.Unlock()
	return s.scheduleAttempt(ctx, jobID)
}

func (s *Service) scheduleAttempt(ctx context.Context, jobID string) (ScheduledAttempt, error) {
	return s.scheduleAttemptInternal(ctx, jobID, true)
}

func (s *Service) scheduleAttemptInternal(ctx context.Context, jobID string, processExpired bool) (ScheduledAttempt, error) {
	now := s.now()
	if processExpired {
		if err := s.expireLeases(ctx, now); err != nil {
			return ScheduledAttempt{}, err
		}
	}
	staleAfter := s.StaleAfter
	if staleAfter <= 0 {
		staleAfter = 45 * time.Second
	}
	s.mu.Lock()
	s.initLocked()
	run := s.runs[jobID]
	if run == nil {
		s.mu.Unlock()
		return ScheduledAttempt{}, fmt.Errorf("job run context is missing")
	}
	model := run.model
	request := run.request
	deadlineAt := run.deadlineAt
	s.mu.Unlock()

	candidates, err := s.Store.EligibleNodes(ctx, model.ID, now.Add(-staleAfter))
	if err != nil {
		return ScheduledAttempt{}, err
	}
	s.mu.Lock()
	s.initLocked()
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if run.excluded[candidate.NodeID] {
			continue
		}
		session := s.sessions[candidate.NodeID]
		if session.sessionID == "" || session.sessionID != candidate.SessionID {
			continue
		}
		if _, busy := s.activeByNode[candidate.NodeID]; busy {
			continue
		}
		filtered = append(filtered, candidate)
	}
	chosen, ok := s.Scheduler.Choose(filtered)
	if ok {
		s.activeByNode[chosen.NodeID] = activeJob{jobID: jobID}
		run.attempts++
	}
	session := s.sessions[chosen.NodeID]
	s.mu.Unlock()
	if !ok {
		return ScheduledAttempt{}, APIError{Code: CodeNoCapacity, Message: "No eligible node is currently available for this model.", Retryable: true, Status: 503}
	}
	leaseTTL := s.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = 10 * time.Second
	}
	if deadlineAt.IsZero() {
		deadlineAt = now.Add(120 * time.Second)
	}
	attempt, err := s.Store.CreateAttempt(ctx, jobID, chosen.NodeID, now.Add(leaseTTL), deadlineAt)
	if err != nil {
		s.clearActive(chosen.NodeID)
		return ScheduledAttempt{}, err
	}
	s.setActive(chosen.NodeID, activeJob{jobID: jobID, attemptID: attempt.AttemptID})
	attempt.SessionID = chosen.SessionID
	attempt.ModelHash = chosen.ModelHash
	attempt.RuntimeHash = chosen.RuntimeHash
	offer := protocol.JobOfferPayload{
		JobID:          jobID,
		AttemptID:      attempt.AttemptID,
		LeaseExpiresAt: attempt.LeaseExpiresAt,
		DeadlineAt:     attempt.DeadlineAt,
		ModelID:        model.ID,
		Request:        request,
		Verification:   protocol.JobVerification{Kind: "standard"},
	}
	if err := session.send(ctx, protocol.TypeJobOffer, offer); err != nil {
		s.clearActive(chosen.NodeID)
		_ = s.Store.FailAttempt(ctx, jobID, attempt.AttemptID, CodeJobFailed, true, now)
		return ScheduledAttempt{}, err
	}
	s.logger().InfoContext(ctx, "job offer sent", "job_id", jobID, "attempt_id", attempt.AttemptID, "node_id", chosen.NodeID)
	return attempt, nil
}

func (s *Service) expireLeases(ctx context.Context, now time.Time) error {
	expired, err := s.Store.ExpireLeases(ctx, now)
	if err != nil {
		return err
	}
	for _, attempt := range expired {
		apiErr := APIError{Code: CodeJobTimeout, Message: "Job lease expired before node acceptance.", Retryable: true, Status: 504}
		if err := s.handleExpiredAttempt(ctx, attempt, apiErr, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleExpiredAttempt(ctx context.Context, attempt ExpiredAttempt, apiErr APIError, occurredAt time.Time) error {
	s.clearActive(attempt.NodeID)
	if s.prepareRetry(attempt.JobID, attempt.NodeID) {
		s.logger().InfoContext(ctx, "job lease expired; retrying", "job_id", attempt.JobID, "attempt_id", attempt.AttemptID, "node_id", attempt.NodeID)
		if _, err := s.scheduleAttemptInternal(ctx, attempt.JobID, false); err == nil {
			return nil
		}
	}
	if err := s.Store.FailJob(ctx, attempt.JobID, attempt.AttemptID, apiErr.Code, apiErr.Message, apiErr.Retryable, true, occurredAt); err != nil {
		return err
	}
	s.removeRun(attempt.JobID)
	s.notify(attempt.JobID, Result{Error: apiErr})
	s.logger().InfoContext(ctx, "job failed after lease expiry", "job_id", attempt.JobID, "attempt_id", attempt.AttemptID, "node_id", attempt.NodeID, "error_code", apiErr.Code)
	return nil
}

func (s *Service) RegisterWait(jobID string) <-chan Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
	ch := make(chan Result, 1)
	s.pending[jobID] = ch
	return ch
}

func (s *Service) AbandonWait(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
	delete(s.pending, jobID)
	delete(s.runs, jobID)
}

func (s *Service) Wait(ctx context.Context, jobID string) (OpenAIResponse, APIError) {
	ch := s.RegisterWait(jobID)
	select {
	case result := <-ch:
		if result.Error.Code != "" {
			return OpenAIResponse{}, result.Error
		}
		return result.Response, APIError{}
	case <-ctx.Done():
		return OpenAIResponse{}, APIError{Code: CodeJobTimeout, Message: "Job timed out waiting for completion.", Retryable: true, Status: 504}
	}
}

func (s *Service) HandleNodeMessage(ctx context.Context, nodeID, sessionID string, envelope protocol.Envelope) error {
	switch envelope.Type {
	case protocol.TypeJobAccepted:
		var payload protocol.JobAcceptedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		return s.Store.MarkAccepted(ctx, payload.JobID, payload.AttemptID, payload.AcceptedAt)
	case protocol.TypeJobStarted:
		var payload protocol.JobStartedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		return s.Store.MarkStarted(ctx, payload.JobID, payload.AttemptID, payload.StartedAt)
	case protocol.TypeJobRejected:
		var payload protocol.JobRejectedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		apiErr := APIError{Code: CodeNoCapacity, Message: payload.Message, Retryable: payload.Retryable, Status: 503}
		return s.handleAttemptFailure(ctx, nodeID, payload.JobID, payload.AttemptID, apiErr, payload.Retryable, payload.RejectedAt, payload.ReasonCode)
	case protocol.TypeJobFailed:
		var payload protocol.JobFailedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		apiErr := APIError{Code: CodeJobFailed, Message: payload.Message, Retryable: payload.Retryable, Status: 502}
		return s.handleAttemptFailure(ctx, nodeID, payload.JobID, payload.AttemptID, apiErr, payload.Retryable, payload.FailedAt, payload.ErrorCode)
	case protocol.TypeJobCompleted:
		var payload protocol.JobCompletedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		if err := s.verifyCompletion(ctx, nodeID, payload); err != nil {
			s.clearActive(nodeID)
			apiErr := APIError{Code: CodeJobFailed, Message: err.Error(), Retryable: false, Status: 502}
			_ = s.Store.FailJob(ctx, payload.JobID, payload.AttemptID, apiErr.Code, apiErr.Message, false, false, payload.CompletedAt)
			s.removeRun(payload.JobID)
			s.notify(payload.JobID, Result{Error: apiErr})
			return nil
		}
		response, err := s.Store.CompleteJob(ctx, payload)
		s.clearActive(nodeID)
		if err != nil {
			return err
		}
		s.removeRun(payload.JobID)
		s.notify(payload.JobID, Result{Response: response})
		s.logger().InfoContext(ctx, "job completed", "job_id", payload.JobID, "attempt_id", payload.AttemptID, "node_id", nodeID, "duration_millis", payload.DurationMillis)
		return nil
	default:
		return fmt.Errorf("unsupported node message %s", envelope.Type)
	}
}

func (s *Service) verifyCompletion(ctx context.Context, nodeID string, payload protocol.JobCompletedPayload) error {
	hashes, err := s.Store.ModelHashes(ctx, payload.ModelID)
	if err != nil {
		return err
	}
	if payload.ModelHash != hashes.ModelHash {
		return fmt.Errorf("model hash mismatch")
	}
	if payload.RuntimeHash != hashes.RuntimeHash {
		return fmt.Errorf("runtime hash mismatch")
	}
	publicEncoded, err := s.Store.PublicKeyForNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("lookup node public key: %w", err)
	}
	publicRaw, err := base64.StdEncoding.DecodeString(publicEncoded)
	if err != nil {
		return fmt.Errorf("decode node public key: %w", err)
	}
	return nodeauth.VerifyJobCompleted(ed25519.PublicKey(publicRaw), payload)
}

func (s *Service) handleAttemptFailure(ctx context.Context, nodeID, jobID, attemptID string, apiErr APIError, transient bool, occurredAt time.Time, attemptCode string) error {
	s.clearActive(nodeID)
	if attemptCode == "" {
		attemptCode = apiErr.Code
	}
	if transient && s.prepareRetry(jobID, nodeID) {
		s.logger().InfoContext(ctx, "job attempt transient failure; retrying", "job_id", jobID, "attempt_id", attemptID, "node_id", nodeID, "error_code", attemptCode)
		if err := s.Store.FailAttempt(ctx, jobID, attemptID, attemptCode, true, occurredAt); err != nil {
			return err
		}
		if _, err := s.scheduleAttempt(ctx, jobID); err == nil {
			return nil
		}
	}
	if err := s.Store.FailJob(ctx, jobID, attemptID, attemptCode, apiErr.Message, apiErr.Retryable, transient, occurredAt); err != nil {
		return err
	}
	s.removeRun(jobID)
	s.notify(jobID, Result{Error: apiErr})
	s.logger().InfoContext(ctx, "job failed", "job_id", jobID, "attempt_id", attemptID, "node_id", nodeID, "error_code", attemptCode, "retryable", apiErr.Retryable)
	return nil
}

func (s *Service) prepareRetry(jobID, failedNodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
	run := s.runs[jobID]
	if run == nil || run.attempts >= 2 {
		return false
	}
	if run.excluded == nil {
		run.excluded = map[string]bool{}
	}
	run.excluded[failedNodeID] = true
	return true
}

func (s *Service) removeRun(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
	delete(s.runs, jobID)
}

func (s *Service) clearActive(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
	delete(s.activeByNode, nodeID)
}

func (s *Service) setActive(nodeID string, active activeJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()
	s.activeByNode[nodeID] = active
}

func (s *Service) notify(jobID string, result Result) {
	s.mu.Lock()
	ch := s.pending[jobID]
	delete(s.pending, jobID)
	s.mu.Unlock()
	if ch != nil {
		ch <- result
	}
}

func (s *Service) initLocked() {
	if s.sessions == nil {
		s.sessions = map[string]sessionRef{}
	}
	if s.activeByNode == nil {
		s.activeByNode = map[string]activeJob{}
	}
	if s.pending == nil {
		s.pending = map[string]chan Result{}
	}
	if s.runs == nil {
		s.runs = map[string]*runState{}
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
