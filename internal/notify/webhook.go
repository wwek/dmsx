package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var headerName = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// webhook calls an endpoint the operator runs. GET carries heading and message
// in the query; POST carries a JSON body, or BodyTemplate with {{event}},
// {{heading}} and {{message}} replaced by JSON-escaped text.
type webhook struct {
	URL          string `json:"url"`
	Method       string `json:"method"`
	BodyTemplate string `json:"bodyTemplate"`
	// Headers is one "Name: value" per line. It is sealed as a whole because a
	// header usually carries the endpoint's credentials.
	Headers string `json:"headers"`
}

func (w *webhook) validate() error {
	if w.URL == "" {
		return fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(w.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("url must be an http or https address")
	}
	switch strings.ToUpper(w.Method) {
	case "", http.MethodPost, http.MethodGet:
	default:
		return fmt.Errorf("method must be POST or GET")
	}
	_, err = w.header()
	return err
}

func (w *webhook) header() (http.Header, error) {
	header := http.Header{}
	for i, line := range strings.Split(w.Headers, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || !headerName.MatchString(name) {
			return nil, fmt.Errorf("header line %d must be Name: value", i+1)
		}
		header.Set(name, strings.TrimSpace(value))
	}
	return header, nil
}

func (w *webhook) Send(ctx context.Context, msg Message) error {
	header, err := w.header()
	if err != nil {
		return err
	}
	var secrets []string
	for _, values := range header {
		secrets = append(secrets, values...)
	}
	if strings.ToUpper(w.Method) == http.MethodGet {
		target := w.URL + separator(w.URL) + "event=" + url.QueryEscape(string(msg.Event)) +
			"&heading=" + url.QueryEscape(msg.Heading) + "&message=" + url.QueryEscape(msg.Text)
		if _, err := do(ctx, http.MethodGet, target, header, "", nil, secrets...); err != nil {
			return fmt.Errorf("webhook: %w", err)
		}
		return nil
	}
	var body []byte
	if w.BodyTemplate != "" {
		replacer := strings.NewReplacer(
			"{{event}}", jsonText(string(msg.Event)),
			"{{heading}}", jsonText(msg.Heading),
			"{{message}}", jsonText(msg.Text),
		)
		body = []byte(replacer.Replace(w.BodyTemplate))
	} else {
		body, _ = json.Marshal(map[string]string{"event": string(msg.Event), "heading": msg.Heading, "message": msg.Text})
	}
	contentType := "application/json"
	if header.Get("Content-Type") != "" {
		contentType = ""
	}
	if _, err := do(ctx, http.MethodPost, w.URL, header, contentType, body, secrets...); err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	return nil
}

func separator(rawURL string) string {
	if strings.Contains(rawURL, "?") {
		return "&"
	}
	return "?"
}

// jsonText is text escaped for the inside of a JSON string, without quotes.
func jsonText(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded[1 : len(encoded)-1])
}
