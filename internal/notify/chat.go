package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	telegramAPI = "https://api.telegram.org"
	slackAPI    = "https://slack.com/api"
)

type telegram struct {
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
	ThreadID int64  `json:"threadId"`
	// APIURL is empty for the official Bot API.
	APIURL string `json:"apiUrl"`
}

func (t *telegram) validate() error {
	if t.BotToken == "" || t.ChatID == "" {
		return fmt.Errorf("botToken and chatId are required")
	}
	return nil
}

func (t *telegram) Send(ctx context.Context, msg Message) error {
	base := strings.TrimRight(t.APIURL, "/")
	if base == "" {
		base = telegramAPI
	}
	form := url.Values{}
	form.Set("chat_id", t.ChatID)
	form.Set("text", clip(msg.body(), 4096))
	if t.ThreadID != 0 {
		form.Set("message_thread_id", strconv.FormatInt(t.ThreadID, 10))
	}
	answer, err := do(ctx, http.MethodPost, base+"/bot"+t.BotToken+"/sendMessage", nil,
		"application/x-www-form-urlencoded", []byte(form.Encode()), t.BotToken)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	var reply struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if json.Unmarshal(answer, &reply) == nil && !reply.OK {
		return fmt.Errorf("telegram: %s", reply.Description)
	}
	return nil
}

// slack posts as a bot through chat.postMessage, or to an incoming webhook
// when webhookUrl is set.
type slack struct {
	BotToken   string `json:"botToken"`
	Channel    string `json:"channel"`
	WebhookURL string `json:"webhookUrl"`
}

func (s *slack) validate() error {
	if s.WebhookURL == "" && (s.BotToken == "" || s.Channel == "") {
		return fmt.Errorf("webhookUrl, or botToken and channel, are required")
	}
	return nil
}

func (s *slack) Send(ctx context.Context, msg Message) error {
	text := "*" + msg.Heading + "*"
	if msg.Text != "" {
		text += "\n\n" + msg.Text
	}
	if s.WebhookURL != "" {
		if _, err := postJSON(ctx, s.WebhookURL, nil, map[string]any{"text": text}, s.WebhookURL); err != nil {
			return fmt.Errorf("slack: %w", err)
		}
		return nil
	}
	header := http.Header{"Authorization": {"Bearer " + s.BotToken}}
	answer, err := postJSON(ctx, slackAPI+"/chat.postMessage", header,
		map[string]any{"channel": s.Channel, "text": text, "mrkdwn": true}, s.BotToken)
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	// Slack answers 200 to a refused message too.
	var reply struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(answer, &reply) == nil && !reply.OK {
		return fmt.Errorf("slack: %s", reply.Error)
	}
	return nil
}

type discord struct {
	WebhookURL string `json:"webhookUrl"`
}

func (d *discord) validate() error {
	if d.WebhookURL == "" {
		return fmt.Errorf("webhookUrl is required")
	}
	return nil
}

func (d *discord) Send(ctx context.Context, msg Message) error {
	if _, err := postJSON(ctx, d.WebhookURL, nil, map[string]any{"content": clip(msg.body(), 2000)}, d.WebhookURL); err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	return nil
}

// teams posts an adaptive card to a Power Automate workflow URL.
type teams struct {
	WebhookURL string `json:"webhookUrl"`
}

func (t *teams) validate() error {
	if t.WebhookURL == "" {
		return fmt.Errorf("webhookUrl is required")
	}
	return nil
}

func (t *teams) Send(ctx context.Context, msg Message) error {
	card := map[string]any{
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body": []any{
			map[string]any{"type": "TextBlock", "size": "Medium", "weight": "Bolder", "text": msg.Heading},
			map[string]any{"type": "TextBlock", "wrap": true, "text": msg.Text},
		},
	}
	payload := map[string]any{
		"title": msg.Heading,
		"text":  msg.Text,
		"attachments": []any{map[string]any{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content":     card,
		}},
	}
	if _, err := postJSON(ctx, t.WebhookURL, nil, payload, t.WebhookURL); err != nil {
		return fmt.Errorf("teams: %w", err)
	}
	return nil
}

// mattermost posts to an incoming webhook, or as a bot when serverUrl and
// botToken are set.
type mattermost struct {
	WebhookURL string `json:"webhookUrl"`
	Channel    string `json:"channel"`
	Username   string `json:"username"`
	IconURL    string `json:"iconUrl"`
	ServerURL  string `json:"serverUrl"`
	BotToken   string `json:"botToken"`
	ChannelID  string `json:"channelId"`
}

func (m *mattermost) validate() error {
	if m.WebhookURL != "" {
		return nil
	}
	if m.ServerURL == "" || m.BotToken == "" || m.ChannelID == "" {
		return fmt.Errorf("webhookUrl, or serverUrl, botToken and channelId, are required")
	}
	return nil
}

func (m *mattermost) Send(ctx context.Context, msg Message) error {
	text := "#### " + msg.Heading
	if msg.Text != "" {
		text += "\n\n" + msg.Text
	}
	if m.WebhookURL != "" {
		payload := map[string]any{"text": text}
		if m.Channel != "" {
			payload["channel"] = m.Channel
		}
		if m.Username != "" {
			payload["username"] = m.Username
		}
		if m.IconURL != "" {
			payload["icon_url"] = m.IconURL
		}
		if _, err := postJSON(ctx, m.WebhookURL, nil, payload, m.WebhookURL); err != nil {
			return fmt.Errorf("mattermost: %w", err)
		}
		return nil
	}
	header := http.Header{"Authorization": {"Bearer " + m.BotToken}}
	endpoint := strings.TrimRight(m.ServerURL, "/") + "/api/v4/posts"
	if _, err := postJSON(ctx, endpoint, header, map[string]any{"channel_id": m.ChannelID, "message": text}, m.BotToken); err != nil {
		return fmt.Errorf("mattermost: %w", err)
	}
	return nil
}
