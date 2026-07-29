package httpapi

import (
	"encoding/json"
	"net/http"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func NewMux(version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(version))
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
