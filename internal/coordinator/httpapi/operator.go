package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	operatorstore "github.com/anianroid/thirdshift/internal/coordinator/operator"
)

type operatorActionRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (o Options) operatorOverviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		overview, err := store.Overview(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, overview)
	}
}

func (o Options) operatorAlertsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		alerts, err := store.AlertsList(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts})
	}
}

func (o Options) operatorNodeDetailHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		node, err := store.NodeDetail(r.Context(), r.PathValue("node_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, node)
	}
}

func (o Options) operatorNodeActionHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		req := decodeOperatorAction(r)
		nodeID := r.PathValue("node_id")
		var err error
		switch action {
		case "drain":
			err = store.DrainNode(r.Context(), nodeID, req.Reason, o.now())
		case "pause":
			err = store.PauseNode(r.Context(), nodeID, req.Reason, o.now())
		case "quarantine":
			err = store.QuarantineNode(r.Context(), nodeID, req.Reason, o.now())
		default:
			writeError(w, http.StatusNotFound, "unknown node action")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "node_id": nodeID, "action": action})
	}
}

func (o Options) operatorModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		models, err := store.ListModels(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
	}
}

func (o Options) operatorJobsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		jobs, err := store.ListJobs(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
	}
}

func (o Options) operatorJobDetailHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		job, err := store.JobDetail(r.Context(), r.PathValue("job_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, job)
	}
}

func (o Options) operatorJobActionHandler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		req := decodeOperatorAction(r)
		jobID := r.PathValue("job_id")
		var err error
		switch action {
		case "retry":
			err = store.RetryJob(r.Context(), jobID, req.Reason, o.now())
		case "cancel":
			err = store.CancelJob(r.Context(), jobID, req.Reason, o.now())
		default:
			writeError(w, http.StatusNotFound, "unknown job action")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "job_id": jobID, "action": action})
	}
}

func (o Options) operatorLedgerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		ledger, err := store.LedgerOverview(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ledger)
	}
}

func (o Options) operatorCreditsReleaseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		released, err := store.PromoteCredits(r.Context(), o.now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"released": released})
	}
}

func (o Options) operatorPayoutCreateHandler() http.HandlerFunc {
	type request struct {
		OrgID string `json:"org_id,omitempty"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		var req request
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}
		batch, err := store.CreatePayoutBatch(r.Context(), strings.TrimSpace(req.OrgID), o.now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, batch)
	}
}

func (o Options) operatorPayoutExportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		body, batch, err := store.ExportPayoutBatch(r.Context(), r.PathValue("batch_id"), o.now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("X-Thirdshift-Payout-Batch", batch.ID)
		w.Header().Set("X-Thirdshift-Payout-Status", batch.Status)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

func (o Options) operatorPayoutConfirmHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read confirmation CSV")
			return
		}
		batch, err := store.ConfirmPayoutBatch(r.Context(), r.PathValue("batch_id"), body, o.now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, batch)
	}
}

func (o Options) operatorAuditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		audit, err := store.Audit(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, audit)
	}
}

func (o Options) operatorFleetCreateHandler() http.HandlerFunc {
	type request struct {
		OrgID            string `json:"org_id"`
		Name             string `json:"name"`
		ScheduleFrom     string `json:"schedule_from,omitempty"`
		ScheduleUntil    string `json:"schedule_until,omitempty"`
		ScheduleTimezone string `json:"schedule_timezone,omitempty"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		fleet, err := store.CreateFleet(r.Context(), req.OrgID, req.Name, operatorstore.ScheduleDefaults{
			From:     req.ScheduleFrom,
			Until:    req.ScheduleUntil,
			Timezone: req.ScheduleTimezone,
		}, o.now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, fleet)
	}
}

func (o Options) operatorFleetReportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		from, err := parseOperatorTime(r.URL.Query().Get("from"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		until, err := parseOperatorTime(r.URL.Query().Get("to"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		body, err := store.FleetReportCSV(r.Context(), r.PathValue("fleet_id"), from, until)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

func (o Options) requireOperatorStore(w http.ResponseWriter) (*operatorstore.Store, bool) {
	if o.OperatorStore == nil {
		writeError(w, http.StatusServiceUnavailable, "operator store is not configured")
		return nil, false
	}
	return o.OperatorStore, true
}

func decodeOperatorAction(r *http.Request) operatorActionRequest {
	var req operatorActionRequest
	if r.Body == nil || r.ContentLength == 0 {
		return req
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	return req
}

func parseOperatorTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
