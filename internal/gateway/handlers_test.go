package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iauth "github.com/inferbolthq/inferbolt/internal/auth"
	"github.com/inferbolthq/inferbolt/internal/gateway"
	"github.com/inferbolthq/inferbolt/internal/jobs"
	"github.com/inferbolthq/inferbolt/internal/queue"
)

// ── mock implementations ───────────────────────────────────────────────────────

type mockStore struct {
	job       *jobs.Job
	getErr    error
	saveErr   error
	updateErr error
}

func (m *mockStore) SaveJob(_ context.Context, j jobs.Job) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	cp := j
	m.job = &cp
	return nil
}

func (m *mockStore) GetJob(_ context.Context, jobID, tenantID string) (*jobs.Job, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.job != nil && m.job.ID == jobID && m.job.TenantID == tenantID {
		return m.job, nil
	}
	return nil, errors.New("not found")
}

func (m *mockStore) ListJobs(_ context.Context, _, _ string, _, _ int) ([]jobs.Job, error) {
	return []jobs.Job{}, nil
}

func (m *mockStore) CountJobs(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func (m *mockStore) UpdateJobState(_ context.Context, _ string, _ jobs.JobState, _ string) error {
	return m.updateErr
}

type mockQueue struct{ enqueueErr error }

func (m *mockQueue) Enqueue(_ context.Context, _ queue.BenchmarkJobArgs) error {
	return m.enqueueErr
}

type mockMetrics struct{}

func (m *mockMetrics) QueryByJob(_ context.Context, _ string) ([]jobs.Result, error) {
	return nil, nil
}
func (m *mockMetrics) QueryByEngineAndModel(_ context.Context, _, _ string, _ time.Time) ([]jobs.Result, error) {
	return nil, nil
}

type mockPinger struct{ err error }

func (m *mockPinger) Ping(_ context.Context) error { return m.err }

// ── factory helpers ────────────────────────────────────────────────────────────

func newHandler(t *testing.T, store gateway.JobStorer, q gateway.JobQueuer, pinger gateway.DBPinger) *gateway.Handler {
	t.Helper()
	c := newCache(t) // defined in middleware_test.go (same package)
	km := iauth.NewKeyManager("test-secret-must-be-32-chars-long!!", c)
	return gateway.NewHandler(store, q, &mockMetrics{}, km, pinger, nil, "http://localhost:9999")
}

func withTenant(r *http.Request, tenantID string) *http.Request {
	return r.WithContext(iauth.SetTenantID(r.Context(), tenantID))
}

func withChiParam(r *http.Request, key, val string) *http.Request {
	rc := chi.NewRouteContext()
	rc.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewBuffer(b)
}

// ── CreateJob ──────────────────────────────────────────────────────────────────

func TestCreateJob_Valid(t *testing.T) {
	h := newHandler(t, &mockStore{}, &mockQueue{}, &mockPinger{})

	body := jsonBody(t, map[string]any{
		"model":       "meta-llama/Llama-3.1-8B",
		"engines":     []string{"vllm"},
		"gpu_profile": "a100-80gb",
		"workload":    map[string]any{"concurrency": 32, "prompt_tokens": 512, "output_tokens": 256, "num_requests": 100},
	})
	r := withTenant(httptest.NewRequest(http.MethodPost, "/v1/jobs", body), "tenant1")
	w := httptest.NewRecorder()
	h.CreateJob(w, r)

	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp["job_id"])
}

func TestCreateJob_MissingModel(t *testing.T) {
	h := newHandler(t, &mockStore{}, &mockQueue{}, &mockPinger{})

	body := jsonBody(t, map[string]any{"engines": []string{"vllm"}, "gpu_profile": "a100-80gb"})
	r := withTenant(httptest.NewRequest(http.MethodPost, "/v1/jobs", body), "tenant1")
	w := httptest.NewRecorder()
	h.CreateJob(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "model")
}

func TestCreateJob_InvalidEngine(t *testing.T) {
	h := newHandler(t, &mockStore{}, &mockQueue{}, &mockPinger{})

	body := jsonBody(t, map[string]any{
		"model":       "some-model",
		"engines":     []string{"unknown-engine"},
		"gpu_profile": "a100-80gb",
	})
	r := withTenant(httptest.NewRequest(http.MethodPost, "/v1/jobs", body), "tenant1")
	w := httptest.NewRecorder()
	h.CreateJob(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── GetJob ─────────────────────────────────────────────────────────────────────

func TestGetJob_NotFound(t *testing.T) {
	store := &mockStore{getErr: errors.New("not found")}
	h := newHandler(t, store, &mockQueue{}, &mockPinger{})

	r := withTenant(httptest.NewRequest(http.MethodGet, "/v1/jobs/missing", nil), "tenant1")
	r = withChiParam(r, "jobID", "missing")
	w := httptest.NewRecorder()
	h.GetJob(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetJob_WrongTenant(t *testing.T) {
	// Store returns job owned by tenant1, but request comes from tenant2
	store := &mockStore{
		job: &jobs.Job{ID: "job-abc", TenantID: "tenant1", State: jobs.StatePending},
	}
	h := newHandler(t, store, &mockQueue{}, &mockPinger{})

	r := httptest.NewRequest(http.MethodGet, "/v1/jobs/job-abc", nil)
	r = withTenant(r, "tenant2")
	r = withChiParam(r, "jobID", "job-abc")
	w := httptest.NewRecorder()
	h.GetJob(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── CancelJob ──────────────────────────────────────────────────────────────────

func TestCancelJob_AlreadyCompleted(t *testing.T) {
	store := &mockStore{
		job: &jobs.Job{ID: "job-done", TenantID: "tenant1", State: jobs.StateCompleted},
	}
	h := newHandler(t, store, &mockQueue{}, &mockPinger{})

	r := httptest.NewRequest(http.MethodDelete, "/v1/jobs/job-done", nil)
	r = withTenant(r, "tenant1")
	r = withChiParam(r, "jobID", "job-done")
	w := httptest.NewRecorder()
	h.CancelJob(w, r)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "terminal")
}

// ── Health ──────────────────────────────────────────────────────────────────────

func TestHealth_DBUp(t *testing.T) {
	h := newHandler(t, &mockStore{}, &mockQueue{}, &mockPinger{err: nil})

	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ok", resp["status"])
	assert.Equal(t, "ok", resp["postgres"])
}
