// Package agentclient — клиент API rendol-agent поверх unix-сокета.
package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"rendolnet/internal/agent"
	"rendolnet/internal/dataplane"
	"rendolnet/internal/netcfg"
)

type Client struct {
	hc *http.Client
}

func New(socket string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns: 4,
	}
	return &Client{hc: &http.Client{Transport: tr, Timeout: 60 * time.Second}}
}

// Error — ответ агента с кодом ошибки. Validation заполнен при 422.
type Error struct {
	Status     int
	Message    string
	Validation *netcfg.ValidationError
}

func (e *Error) Error() string {
	if e.Validation != nil {
		return e.Validation.Error()
	}
	return fmt.Sprintf("agent: %d: %s", e.Status, e.Message)
}

var ErrUnavailable = errors.New("agent: агент недоступен")

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://agent"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		e := &Error{Status: resp.StatusCode}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == http.StatusUnprocessableEntity {
			var ve netcfg.ValidationError
			if json.Unmarshal(raw, &ve) == nil {
				e.Validation = &ve
				return e
			}
		}
		var m struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &m)
		e.Message = m.Error
		return e
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Status(ctx context.Context) (*agent.Status, error) {
	var s agent.Status
	return &s, c.do(ctx, http.MethodGet, "/v1/status", nil, &s)
}

func (c *Client) Config(ctx context.Context) (*agent.Applied, error) {
	var a agent.Applied
	return &a, c.do(ctx, http.MethodGet, "/v1/config", nil, &a)
}

func (c *Client) Push(ctx context.Context, rev int64, cfg *netcfg.Config, confirm time.Duration) (*agent.Status, error) {
	req := map[string]any{"revision": rev, "config": cfg, "confirm_seconds": int(confirm.Seconds())}
	var s agent.Status
	return &s, c.do(ctx, http.MethodPut, "/v1/config", req, &s)
}

func (c *Client) Confirm(ctx context.Context, rev int64) error {
	return c.do(ctx, http.MethodPost, "/v1/config/confirm?revision="+strconv.FormatInt(rev, 10), nil, nil)
}

func (c *Client) Links(ctx context.Context) ([]agent.Link, error) {
	var l []agent.Link
	return l, c.do(ctx, http.MethodGet, "/v1/links", nil, &l)
}

func (c *Client) Sessions(ctx context.Context, limit int) ([]dataplane.Session, error) {
	var s []dataplane.Session
	return s, c.do(ctx, http.MethodGet, "/v1/sessions?limit="+strconv.Itoa(limit), nil, &s)
}

func (c *Client) FlushSessions(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/v1/sessions", nil, nil)
}

func (c *Client) Counters(ctx context.Context) (map[string]dataplane.RuleCounter, error) {
	var m map[string]dataplane.RuleCounter
	return m, c.do(ctx, http.MethodGet, "/v1/counters", nil, &m)
}

func (c *Client) Events(ctx context.Context, since uint64, limit int) ([]agent.SeqEvent, error) {
	q := url.Values{"since": {strconv.FormatUint(since, 10)}, "limit": {strconv.Itoa(limit)}}
	var e []agent.SeqEvent
	return e, c.do(ctx, http.MethodGet, "/v1/events?"+q.Encode(), nil, &e)
}
