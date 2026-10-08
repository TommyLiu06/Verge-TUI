package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"verge-tui/internal/state"
)

type History struct {
	Delay int `json:"delay"`
}

const DelayTimeoutMillis = 5000

type Proxy struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Now     string    `json:"now"`
	All     []string  `json:"all"`
	History []History `json:"history"`
	Hidden  bool      `json:"hidden"`
	TestURL string    `json:"testUrl"`
}
type Config struct {
	Mode      string `json:"mode"`
	MixedPort int    `json:"mixed-port"`
	Tun       struct {
		Enable bool `json:"enable"`
	} `json:"tun"`
}
type Snapshot struct {
	Version string
	Config  Config
	Proxies map[string]Proxy
	Rules   int
}
type Client struct {
	Base, Secret string
	HTTP         *http.Client
}

func New(c state.Settings) *Client {
	return &Client{Base: "http://127.0.0.1:" + strconv.Itoa(c.APIPort), Secret: c.Secret,
		HTTP: &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2}}}
}
func (c *Client) Close() { c.HTTP.CloseIdleConnections() }
func (c *Client) Request(ctx context.Context, method, path string, body, result any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("core API unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("core API %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(result)
	}
	return nil
}
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var out Snapshot
	var v struct {
		Version string `json:"version"`
	}
	if err := c.Request(ctx, "GET", "/version", nil, &v); err != nil {
		return out, err
	}
	out.Version = v.Version
	if err := c.Request(ctx, "GET", "/configs", nil, &out.Config); err != nil {
		return out, err
	}
	var p struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := c.Request(ctx, "GET", "/proxies", nil, &p); err != nil {
		return out, err
	}
	out.Proxies = p.Proxies
	var r struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if err := c.Request(ctx, "GET", "/rules", nil, &r); err != nil {
		return out, err
	}
	out.Rules = len(r.Rules)
	return out, nil
}
func (c *Client) Select(ctx context.Context, group, node string) error {
	return c.Request(ctx, "PUT", "/proxies/"+url.PathEscape(group), map[string]string{"name": node}, nil)
}
func (c *Client) Reload(ctx context.Context, payload []byte) error {
	return c.Request(ctx, "PUT", "/configs?force=true", map[string]string{"payload": string(payload)}, nil)
}
func (c *Client) Delay(ctx context.Context, name, testURL string) (int, error) {
	return c.DelayWithTimeout(ctx, name, testURL, DelayTimeoutMillis)
}

func (c *Client) DelayWithTimeout(ctx context.Context, name, testURL string, timeout int) (int, error) {
	var result struct {
		Delay int `json:"delay"`
	}
	if timeout < 100 || timeout > 60000 {
		return 0, fmt.Errorf("invalid delay timeout")
	}
	query := url.Values{"url": {testURL}, "timeout": {strconv.Itoa(timeout)}}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout+1000)*time.Millisecond)
	defer cancel()
	err := c.Request(ctx, "GET", "/proxies/"+url.PathEscape(name)+"/delay?"+query.Encode(), nil, &result)
	return result.Delay, err
}
