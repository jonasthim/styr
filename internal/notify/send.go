package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sendTimeout bounds a single HTTP attempt (the retry, if any, gets its own
// budget on top of this).
var sendTimeout = 10 * time.Second

// retryDelay is how long Send waits before its one retry on a network error
// or 5xx response. A package var (rather than a const) so tests can shrink
// it.
var retryDelay = 2 * time.Second

// Service sends notifications over HTTP.
type Service struct {
	client *http.Client
	logger *slog.Logger
}

// New returns a Service that sends over client (a nil client gets a default
// &http.Client{}) and logs to logger (a nil logger gets slog.Default()).
func New(client *http.Client, logger *slog.Logger) *Service {
	if client == nil {
		client = &http.Client{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{client: client, logger: logger}
}

// webhookPayload is the JSON body posted to a "webhook" channel.
type webhookPayload struct {
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	URL      string   `json:"url"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags"`
	SentAt   string   `json:"sent_at"`
}

// Send delivers ev to ch: a plain POST for ntfy (body, headers) or a JSON
// POST for webhook. Every attempt is bounded by sendTimeout; a network
// error or 5xx response is retried once after retryDelay. The token is
// never included in a returned error.
func (s *Service) Send(ctx context.Context, ch Channel, ev Event) error {
	switch ch.Kind {
	case "ntfy":
		return s.sendWithRetry(ctx, ch, s.buildNtfyRequest(ch, ev))
	case "webhook":
		return s.sendWithRetry(ctx, ch, s.buildWebhookRequest(ch, ev))
	default:
		return fmt.Errorf("notify: unknown channel kind %q", ch.Kind)
	}
}

// requestBuilder builds a fresh *http.Request bound to ctx. It is called
// once per attempt, since an *http.Request (and its body reader) cannot be
// reused across a retry.
type requestBuilder func(ctx context.Context) (*http.Request, error)

type attemptResult struct {
	statusCode int
	err        error // set only for a transport-level failure, not a non-2xx response
}

func (s *Service) doOnce(ctx context.Context, build requestBuilder) attemptResult {
	reqCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := build(reqCtx)
	if err != nil {
		return attemptResult{err: err}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return attemptResult{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	return attemptResult{statusCode: resp.StatusCode}
}

func (r attemptResult) ok() bool {
	return r.err == nil && r.statusCode >= 200 && r.statusCode < 300
}

func (r attemptResult) retryable() bool {
	return r.err != nil || r.statusCode >= 500
}

func (s *Service) sendWithRetry(ctx context.Context, ch Channel, build requestBuilder) error {
	r := s.doOnce(ctx, build)
	if r.ok() {
		return nil
	}
	if r.retryable() {
		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return resultErr(ch.Kind, r)
		}
		r = s.doOnce(ctx, build)
		if r.ok() {
			return nil
		}
	}
	return resultErr(ch.Kind, r)
}

func resultErr(kind string, r attemptResult) error {
	if r.err != nil {
		return fmt.Errorf("notify: %s: %w", kind, r.err)
	}
	return fmt.Errorf("notify: %s: HTTP %d", kind, r.statusCode)
}

func (s *Service) buildNtfyRequest(ch Channel, ev Event) requestBuilder {
	return func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.URL, strings.NewReader(ev.Body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Title", ev.Title)
		if ev.Priority != 0 {
			req.Header.Set("Priority", strconv.Itoa(ev.Priority))
		}
		if len(ev.Tags) > 0 {
			req.Header.Set("Tags", strings.Join(ev.Tags, ","))
		}
		if ev.URL != "" {
			req.Header.Set("Click", ev.URL)
		}
		if ch.Token != "" {
			req.Header.Set("Authorization", "Bearer "+ch.Token)
		}
		return req, nil
	}
}

func (s *Service) buildWebhookRequest(ch Channel, ev Event) requestBuilder {
	return func(ctx context.Context) (*http.Request, error) {
		body, err := json.Marshal(webhookPayload{
			Kind:     ev.Kind,
			Title:    ev.Title,
			Body:     ev.Body,
			URL:      ev.URL,
			Priority: ev.Priority,
			Tags:     ev.Tags,
			SentAt:   time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return nil, fmt.Errorf("notify: encode webhook body: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.URL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Styr-Event", ev.Kind)
		if ch.Token != "" {
			req.Header.Set("Authorization", "Bearer "+ch.Token)
		}
		return req, nil
	}
}
