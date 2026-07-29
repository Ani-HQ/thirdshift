package jobs

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	mu           sync.Mutex
	sessions     map[string]sessionRef
	activeByNode map[string]activeJob
	pending      map[string]chan Result
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
			_ = s.Store.FailJob(context.Background(), active.jobID, active.attemptID, apiErr.Code, apiErr.Message, apiErr.Retryable, true, s.now())
			s.notify(active.jobID, Result{Error: apiErr})
		}
	}
}

func (s *Service) Schedule(ctx context.Context, jobID string, model ModelInfo, request protocol.ChatRequest, deadlineAt time.Time) (ScheduledAttempt, error) {
	now := s.now()
	if _, err := s.Store.ExpireLeases(ctx, now); err != nil {
		return ScheduledAttempt{}, err
	}
	staleAfter := s.StaleAfter
	if staleAfter <= 0 {
		staleAfter = 45 * time.Second
	}
	candidates, err := s.Store.EligibleNodes(ctx, model.ID, now.Add(-staleAfter))
	if err != nil {
		return ScheduledAttempt{}, err
	}
	s.mu.Lock()
	s.initLocked()
	filtered := candidates[:0]
	for _, candidate := range candidates {
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
		_ = s.Store.FailJob(ctx, jobID, attempt.AttemptID, CodeJobFailed, "Failed to deliver job offer.", true, true, now)
		return ScheduledAttempt{}, err
	}
	return attempt, nil
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
		s.clearActive(nodeID)
		apiErr := APIError{Code: CodeNoCapacity, Message: payload.Message, Retryable: payload.Retryable, Status: 503}
		_ = s.Store.FailJob(ctx, payload.JobID, payload.AttemptID, apiErr.Code, apiErr.Message, apiErr.Retryable, true, payload.RejectedAt)
		s.notify(payload.JobID, Result{Error: apiErr})
		return nil
	case protocol.TypeJobFailed:
		var payload protocol.JobFailedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		s.clearActive(nodeID)
		apiErr := APIError{Code: CodeJobFailed, Message: payload.Message, Retryable: payload.Retryable, Status: 502}
		_ = s.Store.FailJob(ctx, payload.JobID, payload.AttemptID, apiErr.Code, apiErr.Message, apiErr.Retryable, true, payload.FailedAt)
		s.notify(payload.JobID, Result{Error: apiErr})
		return nil
	case protocol.TypeJobCompleted:
		var payload protocol.JobCompletedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return err
		}
		if err := s.verifyCompletion(ctx, nodeID, payload); err != nil {
			s.clearActive(nodeID)
			apiErr := APIError{Code: CodeJobFailed, Message: err.Error(), Retryable: false, Status: 502}
			_ = s.Store.FailJob(ctx, payload.JobID, payload.AttemptID, apiErr.Code, apiErr.Message, false, false, payload.CompletedAt)
			s.notify(payload.JobID, Result{Error: apiErr})
			return nil
		}
		response, err := s.Store.CompleteJob(ctx, payload)
		s.clearActive(nodeID)
		if err != nil {
			return err
		}
		s.notify(payload.JobID, Result{Response: response})
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
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
