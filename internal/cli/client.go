package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/inferbolthq/inferbolt/internal/jobs"
	"github.com/inferbolthq/inferbolt/internal/router"
)

// Client is the typed HTTP client for the InferBolt gateway.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	version    string
}

// NewClient creates a Client with a 60-second timeout.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		version:    "0.1.0",
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "inferbolt-cli/"+c.version)
	return c.httpClient.Do(req)
}

func decodeResp[T any](resp *http.Response) (T, error) {
	var zero T
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		msg := errBody.Error
		if msg == "" {
			msg = resp.Status
		}
		return zero, fmt.Errorf("server error %d: %s", resp.StatusCode, msg)
	}
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return v, nil
}

// ── Request / response types ──────────────────────────────────────────────────

type CreateJobRequest struct {
	Model      string              `json:"model"`
	Engines    []string            `json:"engines"`
	Workload   jobs.WorkloadConfig `json:"workload"`
	GPUProfile string              `json:"gpu_profile"`
	AutoRoute  bool                `json:"auto_route"`
}

type JobResponse struct {
	JobID             string `json:"job_id"`
	State             string `json:"state"`
	AutoRouted        bool   `json:"auto_routed"`
	RecommendedEngine string `json:"recommended_engine"`
}

type APIKeyResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type HealthResponse struct {
	Status   string `json:"status"`
	Postgres string `json:"postgres"`
	Version  string `json:"version"`
}

// ── Typed API methods ─────────────────────────────────────────────────────────

func (c *Client) CreateJob(ctx context.Context, req CreateJobRequest) (*JobResponse, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/jobs", req)
	if err != nil {
		return nil, err
	}
	r, err := decodeResp[JobResponse](resp)
	return &r, err
}

func (c *Client) GetJob(ctx context.Context, jobID string) (*jobs.Job, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/jobs/"+jobID, nil)
	if err != nil {
		return nil, err
	}
	r, err := decodeResp[jobs.Job](resp)
	return &r, err
}

func (c *Client) GetJobResults(ctx context.Context, jobID string) ([]jobs.Result, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/jobs/"+jobID+"/results", nil)
	if err != nil {
		return nil, err
	}
	return decodeResp[[]jobs.Result](resp)
}

func (c *Client) ListJobs(ctx context.Context, state string, limit int) ([]jobs.Job, error) {
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	if state != "" {
		q.Set("state", state)
	}
	resp, err := c.do(ctx, http.MethodGet, "/v1/jobs?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var out struct {
		Jobs []jobs.Job `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

func (c *Client) GetMetrics(ctx context.Context, engine, model string, since time.Time) ([]jobs.Result, error) {
	q := url.Values{}
	q.Set("engine", engine)
	q.Set("model", model)
	q.Set("since", since.Format(time.RFC3339))
	resp, err := c.do(ctx, http.MethodGet, "/v1/metrics?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	return decodeResp[[]jobs.Result](resp)
}

func (c *Client) ClassifyWorkload(ctx context.Context, input router.ClassificationInput) (*router.ClassificationResult, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/route", input)
	if err != nil {
		return nil, err
	}
	r, err := decodeResp[router.ClassificationResult](resp)
	return &r, err
}

func (c *Client) CreateAPIKey(ctx context.Context, tenantID string, scopes []string, expiryDays int) (*APIKeyResponse, error) {
	body := map[string]any{
		"tenant_id":   tenantID,
		"scopes":      scopes,
		"expiry_days": expiryDays,
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/admin/apikeys", body)
	if err != nil {
		return nil, err
	}
	r, err := decodeResp[APIKeyResponse](resp)
	return &r, err
}

func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	resp, err := c.do(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return nil, err
	}
	r, err := decodeResp[HealthResponse](resp)
	return &r, err
}

// PollJob polls for job completion every 3 seconds, calling onUpdate on each poll.
// Returns the terminal-state job or an error. Times out after 60 minutes.
func (c *Client) PollJob(ctx context.Context, jobID string, onUpdate func(jobs.Job)) (*jobs.Job, error) {
	deadline := time.Now().Add(60 * time.Minute)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("job polling timed out after 60 minutes")
			}
			job, err := c.GetJob(ctx, jobID)
			if err != nil {
				continue
			}
			if onUpdate != nil {
				onUpdate(*job)
			}
			if jobs.IsTerminal(job.State) {
				return job, nil
			}
		}
	}
}
