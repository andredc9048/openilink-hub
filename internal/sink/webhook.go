package sink

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openilink/openilink-hub/internal/provider"
)

// Webhook pushes messages to a configured HTTP endpoint.
type Webhook struct{}

func (s *Webhook) Name() string { return "webhook" }

func (s *Webhook) Handle(d Delivery) {
	url := d.Channel.WebhookURL
	if url == "" {
		return
	}

	payload := webhookPayload{
		Event:     "message",
		ChannelID: d.Channel.ID,
		BotID:     d.BotDBID,
		SeqID:     d.SeqID,
		Sender:    d.Message.Sender,
		MsgType:   d.MsgType,
		Content:   d.Content,
		Timestamp: d.Message.Timestamp,
		Items:     d.Message.Items,
	}

	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		slog.Error("webhook request build failed", "channel", d.Channel.ID, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Event", "message")
	req.Header.Set("X-Hub-Channel", d.Channel.ID)

	// Apply auth from webhook_secret field
	// Formats:
	//   bearer:<token>        → Authorization: Bearer <token>
	//   header:<name>:<value> → Custom header
	//   hmac:<secret>         → X-Hub-Signature: sha256=<hmac>
	//   <raw>                 → X-Hub-Signature: sha256=<hmac> (legacy)
	applyWebhookAuth(req, d.Channel.WebhookSecret, body)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("webhook delivery failed", "channel", d.Channel.ID, "url", url, "err", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("webhook returned error", "channel", d.Channel.ID, "status", resp.StatusCode)
	}
}

func applyWebhookAuth(req *http.Request, secret string, body []byte) {
	if secret == "" {
		return
	}

	if strings.HasPrefix(secret, "bearer:") {
		req.Header.Set("Authorization", "Bearer "+secret[7:])
		return
	}

	if strings.HasPrefix(secret, "header:") {
		// header:X-Custom-Key:my-value
		parts := strings.SplitN(secret[7:], ":", 2)
		if len(parts) == 2 {
			req.Header.Set(parts[0], parts[1])
		}
		return
	}

	// hmac:<secret> or raw secret → HMAC signature
	key := secret
	if strings.HasPrefix(secret, "hmac:") {
		key = secret[5:]
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-Hub-Signature", "sha256="+sig)
}

type webhookPayload struct {
	Event     string                 `json:"event"`
	ChannelID string                 `json:"channel_id"`
	BotID     string                 `json:"bot_id"`
	SeqID     int64                  `json:"seq_id"`
	Sender    string                 `json:"sender"`
	MsgType   string                 `json:"msg_type"`
	Content   string                 `json:"content"`
	Timestamp int64                  `json:"timestamp"`
	Items     []provider.MessageItem `json:"items"`
}
