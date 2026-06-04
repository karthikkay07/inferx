package drift

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type Notifier struct {
	webhookURL string
	httpClient *http.Client
}

func NewNotifier(webhookURL string) *Notifier {
	return &Notifier{
		webhookURL: webhookURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (n *Notifier) Start(ctx context.Context, alerts <-chan DriftAlert) {
	go func() {
		slog.Info("starting drift notifier", 
			"webhook_enabled", n.webhookURL != "")

		for {
			select {
			case <-ctx.Done():
				slog.Info("drift notifier stopped")
				return
			case alert, ok := <-alerts:
				if !ok {
					slog.Info("alerts channel closed, stopping notifier")
					return
				}
				if err := n.send(alert); err != nil {
					slog.Error("failed to send notification", 
						"error", err,
						"engine", alert.Engine,
						"model", alert.Model,
						"metric", alert.Metric)
				}
			}
		}
	}()
}

type SlackMessage struct {
	Text   string       `json:"text"`
	Blocks []SlackBlock `json:"blocks"`
}

type SlackBlock struct {
	Type string      `json:"type"`
	Text SlackText   `json:"text"`
}

type SlackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (n *Notifier) send(alert DriftAlert) error {
	// Always log the alert
	slog.Info("drift alert", 
		"engine", alert.Engine,
		"model", alert.Model,
		"metric", alert.Metric,
		"severity", alert.Severity,
		"baseline_value", alert.BaselineValue,
		"current_value", alert.CurrentValue,
		"deviation_pct", alert.DeviationPct)

	// If no webhook URL configured, just log
	if n.webhookURL == "" {
		return nil
	}

	// Format Slack message
	severity := "🟡"
	if alert.Severity == "critical" {
		severity = "🔴"
	}

	messageText := fmt.Sprintf("%s *%s* drift on `%s/%s`\nMetric: `%s` deviated `%.1f%%` from baseline\nBaseline: `%.2f` → Current: `%.2f`",
		severity,
		alert.Severity,
		alert.Engine,
		alert.Model,
		alert.Metric,
		alert.DeviationPct,
		alert.BaselineValue,
		alert.CurrentValue)

	message := SlackMessage{
		Text: "🚨 InferX drift detected",
		Blocks: []SlackBlock{
			{
				Type: "section",
				Text: SlackText{
					Type: "mrkdwn",
					Text: messageText,
				},
			},
		},
	}

	// Send to Slack
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal slack message: %w", err)
	}

	req, err := http.NewRequest("POST", n.webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send slack webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	slog.Info("drift alert sent to Slack",
		"engine", alert.Engine,
		"model", alert.Model,
		"metric", alert.Metric,
		"severity", alert.Severity)

	return nil
}