package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// OrchestratorClient is the interface this handler needs from the orchestrator.
// The real HTTP implementation is built in the orchestrator layer.
type OrchestratorClient interface {
	Pinger
	SubmitJob(ctx context.Context, req SubmitJobRequest) (Job, error)
	GetJob(ctx context.Context, id string) (Job, error)
	ListJobs(ctx context.Context) ([]Job, error)
	CancelJob(ctx context.Context, id string) error
}

type JobHandler struct {
	client OrchestratorClient
}

func NewJobHandler(c OrchestratorClient) *JobHandler {
	return &JobHandler{client: c}
}

// Ping delegates to the underlying client so JobHandler satisfies handler.Pinger.
func (h *JobHandler) Ping(ctx context.Context) error {
	return h.client.Ping(ctx)
}

func (h *JobHandler) Submit(w http.ResponseWriter, r *http.Request) {
	var req SubmitJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" || req.Engine == "" {
		writeError(w, http.StatusBadRequest, "model and engine are required")
		return
	}

	job, err := h.client.SubmitJob(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *JobHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, err := h.client.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *JobHandler) List(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.client.ListJobs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (h *JobHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.client.CancelJob(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
