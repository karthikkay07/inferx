package handler

import "time"

type SubmitJobRequest struct {
	Model    string   `json:"model"`
	Engine   string   `json:"engine"`
	Workload Workload `json:"workload"`
}

type Workload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	Concurrency      int `json:"concurrency"`
	DurationSecs     int `json:"duration_secs"`
}

type Job struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // pending | running | done | failed
	Model     string    `json:"model"`
	Engine    string    `json:"engine"`
	CreatedAt time.Time `json:"created_at"`
}
