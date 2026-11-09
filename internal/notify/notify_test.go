package notify

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type captured struct {
	method string
	path   string
	query  string
	header http.Header
	body   string
}

func recorder(t *testing.T, reply string) (*httptest.Server, *captured) {
	t.Helper()
	got := &captured{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = captured{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: string(body)}
		if reply != "" {
			_, _ = w.Write([]byte(reply))
		}
	}))
	t.Cleanup(server.Close)
	return server, got
}

func mustSender(t *testing.T, kind string, settings any) Sender {
	t.Helper()
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := New(kind, raw)
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

var sample = Message{Event: EventBackupFailed, Heading: "备份失败: shop", Text: "mysqldump: access denied"}

func TestTelegramPostsToBotAPI(t *testing.T) {
	server, got := recorder(t, `{"ok":true}`)
	sender := mustSender(t, TypeTelegram, map[string]any{"botToken": "123:abc", "chatId": "-100", "threadId": 7, "apiUrl": server.URL})
	if err := sender.Send(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	if got.path != "/bot123:abc/sendMessage" {
		t.Fatalf("path %q", got.path)
	}
	if !strings.Contains(got.body, "chat_id=-100") || !strings.Contains(got.body, "message_thread_id=7") {
		t.Fatalf("body %q", got.body)
	}
}

func TestTelegramErrorHidesToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer server.Close()
	sender := mustSender(t, TypeTelegram, map[string]any{"botToken": "secret-token", "chatId": "1", "apiUrl": server.URL})
	err := sender.Send(context.Background(), sample)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error %v", err)
	}
	unreachable := mustSender(t, TypeTelegram, map[string]any{"botToken": "secret-token", "chatId": "1", "apiUrl": "http://127.0.0.1:1"})
	err = unreachable.Send(context.Background(), sample)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("dial error %v", err)
	}
}

func TestSlackBotReadsOKField(t *testing.T) {
	server, got := recorder(t, `{"ok":false,"error":"channel_not_found"}`)
	old := slackAPI
	slackAPI = server.URL
	defer func() { slackAPI = old }()
	sender := mustSender(t, TypeSlack, map[string]any{"botToken": "xoxb-1", "channel": "C1"})
	err := sender.Send(context.Background(), sample)
	if err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("error %v", err)
	}
	if got.header.Get("Authorization") != "Bearer xoxb-1" || got.path != "/chat.postMessage" {
		t.Fatalf("request %+v", got)
	}
}

func TestChatWebhooks(t *testing.T) {
	cases := []struct {
		kind  string
		extra map[string]any
		want  string
	}{
		{TypeDiscord, nil, `"content":"备份失败: shop\n\nmysqldump: access denied"`},
		{TypeTeams, nil, `"contentType":"application/vnd.microsoft.card.adaptive"`},
		{TypeMattermost, map[string]any{"channel": "ops"}, `"channel":"ops"`},
		{TypeSlack, nil, `"text":"*备份失败: shop*`},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			server, got := recorder(t, "")
			settings := map[string]any{"webhookUrl": server.URL + "/hook"}
			for k, v := range tc.extra {
				settings[k] = v
			}
			if err := mustSender(t, tc.kind, settings).Send(context.Background(), sample); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got.body, tc.want) {
				t.Fatalf("body %s does not contain %s", got.body, tc.want)
			}
		})
	}
}

func TestWebhookTemplateAndHeaders(t *testing.T) {
	server, got := recorder(t, "")
	sender := mustSender(t, TypeWebhook, map[string]any{
		"url":          server.URL,
		"bodyTemplate": `{"title":"{{heading}}","detail":"{{message}}","kind":"{{event}}"}`,
		"headers":      "Authorization: Bearer abc\nX-Env: prod",
	})
	msg := sample
	msg.Text = `quote " and newline` + "\n"
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got.body), &decoded); err != nil {
		t.Fatalf("body %q is not JSON: %v", got.body, err)
	}
	if decoded["detail"] != msg.Text || decoded["kind"] != "BACKUP_FAILED" {
		t.Fatalf("decoded %+v", decoded)
	}
	if got.header.Get("Authorization") != "Bearer abc" || got.header.Get("X-Env") != "prod" {
		t.Fatalf("headers %v", got.header)
	}
}

func TestWebhookGetQuery(t *testing.T) {
	server, got := recorder(t, "")
	sender := mustSender(t, TypeWebhook, map[string]any{"url": server.URL + "/n?src=dmsx", "method": "GET"})
	if err := sender.Send(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodGet || !strings.HasPrefix(got.query, "src=dmsx&event=BACKUP_FAILED&heading=") {
		t.Fatalf("query %q", got.query)
	}
}

func TestValidation(t *testing.T) {
	bad := []struct {
		kind     string
		settings string
	}{
		{TypeTelegram, `{"chatId":"1"}`},
		{TypeWebhook, `{"url":"ftp://x"}`},
		{TypeWebhook, `{"url":"http://x","headers":"no colon"}`},
		{TypeEmail, `{"host":"smtp","to":"not an address"}`},
		{TypeEmail, `{"host":"smtp","to":"a@b.c","security":"SSL3"}`},
		{TypeMattermost, `{"serverUrl":"http://m"}`},
		{"PAGER", `{}`},
	}
	for _, tc := range bad {
		if _, err := New(tc.kind, json.RawMessage(tc.settings)); err == nil {
			t.Fatalf("%s %s accepted", tc.kind, tc.settings)
		}
	}
}

func TestEmailOverPlainSMTP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	data := make(chan string, 1)
	go fakeSMTP(listener, data)
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	sender := mustSender(t, TypeEmail, map[string]any{
		"host": host, "port": mustAtoi(port), "security": "NONE",
		"from": "dmsx <dmsx@example.com>", "to": "ops@example.com, dba@example.com",
	})
	if err := sender.Send(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	message := <-data
	if !strings.Contains(message, "To: <ops@example.com>, <dba@example.com>") || !strings.Contains(message, "Subject: =?UTF-8?q?") {
		t.Fatalf("message %q", message)
	}
	parts := strings.SplitN(message, "\r\n\r\n", 2)
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(parts[1]), "\r\n", ""))
	if err != nil || !strings.Contains(string(decoded), "access denied") {
		t.Fatalf("body %q: %v", parts[1], err)
	}
}

func fakeSMTP(listener net.Listener, data chan<- string) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	reply("220 fake ESMTP")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			reply("250 fake")
		case strings.HasPrefix(command, "DATA"):
			reply("354 go ahead")
			var body strings.Builder
			for {
				part, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if part == ".\r\n" {
					break
				}
				body.WriteString(part)
			}
			data <- body.String()
			reply("250 queued")
		case strings.HasPrefix(command, "QUIT"):
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func mustAtoi(text string) int {
	n := 0
	for _, ch := range text {
		n = n*10 + int(ch-'0')
	}
	return n
}
