package httpapi

import (
	"net/http"
	"sync"
	"time"

	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
)

type publicStatusCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	status    operatorstore.PublicStatus
}

func (o Options) publicStatusHandler(cache *publicStatusCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := o.now()
		if cached, ok := cache.get(now); ok {
			cached.RequesterRegion = o.requesterRegion(r)
			writeJSON(w, http.StatusOK, cached)
			return
		}
		status := operatorstore.PublicStatus{
			Cities:          []string{},
			ModelsAvailable: []operatorstore.ModelAvailability{},
			Models:          []operatorstore.PublicModelStatus{},
			RegionsOnline:   []string{},
			GeneratedAt:     now,
		}
		if o.OperatorStore != nil {
			next, err := o.OperatorStore.PublicStatus(r.Context(), now)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			status = next
		}
		cache.set(status, now.Add(o.StatusCacheTTL))
		status.RequesterRegion = o.requesterRegion(r)
		writeJSON(w, http.StatusOK, status)
	}
}

func (o Options) publicNodeCardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o.OperatorStore == nil {
			writeError(w, http.StatusServiceUnavailable, "operator store is not configured")
			return
		}
		card, err := o.OperatorStore.ContributionCard(r.Context(), r.PathValue("node_id"), o.now())
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, card)
	}
}

func (c *publicStatusCache) get(now time.Time) (operatorstore.PublicStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.expiresAt.IsZero() || !now.Before(c.expiresAt) {
		return operatorstore.PublicStatus{}, false
	}
	return c.status, true
}

func (c *publicStatusCache) set(status operatorstore.PublicStatus, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = status
	c.expiresAt = expiresAt
}
