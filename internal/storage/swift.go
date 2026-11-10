package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type swiftSettings struct {
	AuthURL   string `json:"authUrl"`
	User      string `json:"user"`
	Password  string `json:"password"`
	Project   string `json:"project"`
	Container string `json:"container"`
	Prefix    string `json:"prefix"`
}

type swiftStore struct {
	swiftSettings
	client *http.Client

	mu         sync.Mutex
	token      string
	storageURL string
}

func newSwift(raw json.RawMessage) (Store, error) {
	var settings swiftStore
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.AuthURL == "" || settings.User == "" || settings.Password == "" || settings.Project == "" || settings.Container == "" {
		return nil, fmt.Errorf("authUrl, user, password, project and container are required")
	}
	settings.client = http.DefaultClient
	return &settings, nil
}

func (s *swiftStore) Put(ctx context.Context, key string, r io.Reader) error {
	response, err := s.call(ctx, http.MethodPut, key, r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return nil
}

func (s *swiftStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	response, err := s.call(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (s *swiftStore) Delete(ctx context.Context, key string) error {
	response, err := s.call(ctx, http.MethodDelete, key, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil
		}
		return err
	}
	defer response.Body.Close()
	return nil
}

func (s *swiftStore) Exists(ctx context.Context, key string) (bool, error) {
	response, err := s.call(ctx, http.MethodHead, key, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	defer response.Body.Close()
	return true, nil
}

func (s *swiftStore) Test(ctx context.Context) error { return testObject(ctx, s) }

func (s *swiftStore) call(ctx context.Context, method, key string, body io.Reader) (*http.Response, error) {
	token, storageURL, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := swiftObjectURL(storageURL, s.Container, joinKey(s.Prefix, key))
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Auth-Token", token)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		response.Body.Close()
		s.clearSession()
		token, storageURL, err = s.session(ctx)
		if err != nil {
			return nil, err
		}
		endpoint = swiftObjectURL(storageURL, s.Container, joinKey(s.Prefix, key))
		request, err = http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, err
		}
		request.Header.Set("X-Auth-Token", token)
		response, err = s.client.Do(request)
		if err != nil {
			return nil, err
		}
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("swift %s %s: %s", method, key, strings.TrimSpace(string(payload)+" "+response.Status))
	}
	return response, nil
}

func (s *swiftStore) session(ctx context.Context) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.storageURL != "" {
		return s.token, s.storageURL, nil
	}
	token, storageURL, err := keystoneSession(ctx, s.client, s.swiftSettings)
	if err != nil {
		return "", "", err
	}
	s.token = token
	s.storageURL = storageURL
	return token, storageURL, nil
}

func (s *swiftStore) clearSession() {
	s.mu.Lock()
	s.token = ""
	s.storageURL = ""
	s.mu.Unlock()
}

func keystoneAuthURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	switch {
	case strings.HasSuffix(raw, "/auth/tokens"):
		return raw
	case strings.HasSuffix(raw, "/v3"):
		return raw + "/auth/tokens"
	default:
		return raw + "/v3/auth/tokens"
	}
}

func swiftObjectURL(storageURL, container, key string) string {
	base := strings.TrimRight(storageURL, "/")
	escaped := url.PathEscape(container) + "/" + escapeBlob(key)
	return base + "/" + escaped
}

func keystoneSession(ctx context.Context, client *http.Client, settings swiftSettings) (string, string, error) {
	payload, err := json.Marshal(map[string]any{
		"auth": map[string]any{
			"identity": map[string]any{
				"methods": []string{"password"},
				"password": map[string]any{
					"user": map[string]any{
						"name":     settings.User,
						"password": settings.Password,
						"domain":   map[string]string{"name": "Default"},
					},
				},
			},
			"scope": map[string]any{
				"project": map[string]any{
					"name":   settings.Project,
					"domain": map[string]string{"name": "Default"},
				},
			},
		},
	})
	if err != nil {
		return "", "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, keystoneAuthURL(settings.AuthURL), bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return "", "", fmt.Errorf("swift auth: %s %s", response.Status, strings.TrimSpace(string(body)))
	}
	token := response.Header.Get("X-Subject-Token")
	var catalog struct {
		Token struct {
			Catalog []struct {
				Type      string `json:"type"`
				Endpoints []struct {
					Interface string `json:"interface"`
					URL       string `json:"url"`
				} `json:"endpoints"`
			} `json:"catalog"`
		} `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		return "", "", err
	}
	for _, service := range catalog.Token.Catalog {
		if service.Type != "object-store" {
			continue
		}
		for _, endpoint := range service.Endpoints {
			if endpoint.Interface == "public" && endpoint.URL != "" {
				if token == "" {
					return "", "", fmt.Errorf("swift auth did not return a token")
				}
				return token, endpoint.URL, nil
			}
		}
	}
	return "", "", fmt.Errorf("swift auth catalog has no public object-store")
}
