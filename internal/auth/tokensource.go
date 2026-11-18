// Package auth obtains and renews the gateway's backend access token from its
// device credential.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenPath is the backend endpoint that exchanges a device credential for an access token.
const TokenPath = "/api/v1/gateway/auth/token"

const (
	renewFraction   = 0.8
	minBackoff      = 5 * time.Second
	maxBackoff      = 5 * time.Minute
	rejectedBackoff = 5 * time.Minute
	maxRetryAfter   = 15 * time.Minute
)

// ErrCredentialRejected means the backend refused the device credential (401/403).
var ErrCredentialRejected = errors.New("gateway credential rejected by backend")

// Credential identifies this gateway to the backend.
type Credential struct {
	GatewayID string
	Secret    string
}

// TokenSource caches the current access token and renews it before it expires.
// Concurrent callers share a single renewal.
type TokenSource struct {
	endpoint   string
	cred       Credential
	httpClient *http.Client
	now        func() time.Time

	mu          sync.Mutex
	token       string
	refreshAt   time.Time
	expiresAt   time.Time
	nextAttempt time.Time
	backoff     time.Duration
	lastErr     error
}

// NewTokenSource builds a TokenSource for the backend at baseURL.
// A nil httpClient uses a client with a 15 second timeout.
func NewTokenSource(baseURL string, cred Credential, httpClient *http.Client) *TokenSource {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &TokenSource{
		endpoint:   strings.TrimSuffix(baseURL, "/") + TokenPath,
		cred:       cred,
		httpClient: httpClient,
		now:        time.Now,
	}
}

// Token returns a valid access token, renewing it when due. While the backend is
// unreachable it keeps returning the current token until that token expires.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	valid := s.token != "" && now.Before(s.expiresAt)
	if valid && now.Before(s.refreshAt) {
		return s.token, nil
	}
	if now.Before(s.nextAttempt) {
		if valid {
			return s.token, nil
		}
		return "", s.lastErr
	}

	token, lifetime, err := s.fetch(ctx)
	now = s.now()
	if err != nil {
		// A caller that gave up must not push its failure onto other callers.
		if ctx.Err() == nil {
			s.lastErr = err
			s.scheduleRetry(now, err)
		}
		if valid && now.Before(s.expiresAt) {
			return s.token, nil
		}
		return "", err
	}

	s.token = token
	s.expiresAt = now.Add(lifetime)
	s.refreshAt = now.Add(time.Duration(float64(lifetime) * renewFraction))
	s.nextAttempt = time.Time{}
	s.backoff = 0
	s.lastErr = nil
	return token, nil
}

// Invalidate discards token if it is still the current one, so the next Token
// call renews it. A late call for an already replaced token is ignored.
func (s *TokenSource) Invalidate(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || token != s.token {
		return
	}
	s.token = ""
	s.nextAttempt = time.Time{}
}

func (s *TokenSource) scheduleRetry(now time.Time, err error) {
	if errors.Is(err, ErrCredentialRejected) {
		s.backoff = rejectedBackoff
	} else if s.backoff == 0 {
		s.backoff = minBackoff
	} else {
		s.backoff = min(s.backoff*2, maxBackoff)
	}
	wait := s.backoff
	var rl *rateLimitedError
	if errors.As(err, &rl) && rl.retryAfter > wait {
		wait = min(rl.retryAfter, maxRetryAfter)
	}
	s.nextAttempt = now.Add(wait)
}

// rateLimitedError carries the backend's Retry-After hint for a 429 response.
type rateLimitedError struct {
	retryAfter time.Duration
}

func (e *rateLimitedError) Error() string {
	return fmt.Sprintf("token endpoint rate limited (retry after %s)", e.retryAfter)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (s *TokenSource) fetch(ctx context.Context) (string, time.Duration, error) {
	body, err := json.Marshal(map[string]string{
		"gateway_id": s.cred.GatewayID,
		"secret":     s.cred.Secret,
	})
	if err != nil {
		return "", 0, fmt.Errorf("encode token request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", 0, fmt.Errorf("%w (gateway_id %q, status %d)", ErrCredentialRejected, s.cred.GatewayID, resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return "", 0, &rateLimitedError{retryAfter: time.Duration(max(secs, 0)) * time.Second}
	case resp.StatusCode != http.StatusOK:
		return "", 0, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&tr); err != nil {
		return "", 0, fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" || tr.ExpiresIn <= 0 {
		return "", 0, errors.New("token response missing access_token or expires_in")
	}
	return tr.AccessToken, time.Duration(tr.ExpiresIn) * time.Second, nil
}

// LoadCredentialFile reads the device secret from path. The file must not be
// readable or writable by group or others.
func LoadCredentialFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("credential file: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Errorf("credential file %s has mode %04o; want 0600", path, perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("credential file: %w", err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("credential file %s is empty", path)
	}
	return secret, nil
}
