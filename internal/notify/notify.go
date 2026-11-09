// Package notify delivers one short message about a database event to one
// channel: email, Telegram, Slack, Discord, Teams, Mattermost or a webhook.
// It does not know which events a channel wants; the caller decides that.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type Event string

const (
	EventBackupSuccess  Event = "BACKUP_SUCCESS"
	EventBackupFailed   Event = "BACKUP_FAILED"
	EventHealthDown     Event = "HEALTH_UNAVAILABLE"
	EventHealthUp       Event = "HEALTH_AVAILABLE"
	EventRestoreSuccess Event = "RESTORE_SUCCESS"
	EventRestoreFailed  Event = "RESTORE_FAILED"
	EventTest           Event = "TEST"
)

// Events lists every event a channel can subscribe to. EventTest is not one of
// them; a test message is always delivered.
var Events = []Event{EventBackupSuccess, EventBackupFailed, EventHealthDown, EventHealthUp, EventRestoreSuccess, EventRestoreFailed}

func ValidEvent(name string) bool {
	for _, event := range Events {
		if string(event) == name {
			return true
		}
	}
	return false
}

type Message struct {
	Event   Event
	Heading string
	Text    string
}

func (m Message) body() string {
	if m.Text == "" {
		return m.Heading
	}
	return m.Heading + "\n\n" + m.Text
}

const (
	TypeEmail      = "EMAIL"
	TypeTelegram   = "TELEGRAM"
	TypeSlack      = "SLACK"
	TypeDiscord    = "DISCORD"
	TypeTeams      = "TEAMS"
	TypeMattermost = "MATTERMOST"
	TypeWebhook    = "WEBHOOK"
)

// SecretFields are the settings keys that are sealed in the catalog and blanked
// in API responses. A chat webhook URL authorizes the post by itself.
var SecretFields = []string{"password", "botToken", "webhookUrl", "proxyUrl", "headers"}

type Sender interface {
	Send(ctx context.Context, msg Message) error
}

func New(kind string, raw json.RawMessage) (Sender, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	var sender interface {
		Sender
		validate() error
	}
	switch strings.ToUpper(kind) {
	case TypeEmail:
		sender = &email{}
	case TypeTelegram:
		sender = &telegram{}
	case TypeSlack:
		sender = &slack{}
	case TypeDiscord:
		sender = &discord{}
	case TypeTeams:
		sender = &teams{}
	case TypeMattermost:
		sender = &mattermost{}
	case TypeWebhook:
		sender = &webhook{}
	default:
		return nil, fmt.Errorf("unknown notifier type %q", kind)
	}
	if err := json.Unmarshal(raw, sender); err != nil {
		return nil, err
	}
	if err := sender.validate(); err != nil {
		return nil, err
	}
	return sender, nil
}

var client = &http.Client{Timeout: 20 * time.Second}

// postJSON sends body and treats any status outside 2xx as a failure. secrets
// are removed from the error, because Go puts the request URL in it.
func postJSON(ctx context.Context, url string, header http.Header, body any, secrets ...string) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return do(ctx, http.MethodPost, url, header, "application/json", payload, secrets...)
}

func do(ctx context.Context, method, url string, header http.Header, contentType string, payload []byte, secrets ...string) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, scrub(err, secrets...)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for name, values := range header {
		for _, value := range values {
			req.Header.Set(name, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, scrub(err, secrets...)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return answer, scrub(fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(clip(string(answer), 300))), secrets...)
	}
	return answer, nil
}

func scrub(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return errors.New(text)
}

// clip keeps at most n runes.
func clip(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	runes := []rune(text)
	return string(runes[:n-1]) + "…"
}
