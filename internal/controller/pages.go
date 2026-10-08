package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Metadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	DestinationIP     string `json:"destinationIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationPort   string `json:"destinationPort"`
	Host              string `json:"host"`
	Process           string `json:"process"`
	ProcessPath       string `json:"processPath"`
	RemoteDestination string `json:"remoteDestination"`
	DNSMode           string `json:"dnsMode"`
	InboundName       string `json:"inboundName"`
	InboundUser       string `json:"inboundUser"`
}
type Connection struct {
	ID            string    `json:"id"`
	Metadata      Metadata  `json:"metadata"`
	Upload        uint64    `json:"upload"`
	Download      uint64    `json:"download"`
	Start         time.Time `json:"start"`
	Chains        []string  `json:"chains"`
	Rule          string    `json:"rule"`
	RulePayload   string    `json:"rulePayload"`
	UploadSpeed   uint64    `json:"-"`
	DownloadSpeed uint64    `json:"-"`
}
type Connections struct {
	UploadTotal   uint64       `json:"uploadTotal"`
	DownloadTotal uint64       `json:"downloadTotal"`
	Connections   []Connection `json:"connections"`
}
type Rule struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
	Size    int    `json:"size"`
}
type RuleProvider struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	VehicleType string    `json:"vehicleType"`
	Behavior    string    `json:"behavior"`
	RuleCount   int       `json:"ruleCount"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
type Rules struct {
	Rules     []Rule                  `json:"rules"`
	Providers map[string]RuleProvider `json:"providers"`
}
type Log struct {
	Time    string `json:"time"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

func (c *Client) Connections(ctx context.Context) (Connections, error) {
	var out Connections
	err := c.Request(ctx, "GET", "/connections", nil, &out)
	return out, err
}
func (c *Client) Rules(ctx context.Context) (Rules, error) {
	var out Rules
	if err := c.Request(ctx, "GET", "/rules", nil, &out); err != nil {
		return out, err
	}
	err := c.Request(ctx, "GET", "/providers/rules", nil, &out)
	return out, err
}
func (c *Client) CloseConnection(ctx context.Context, id string) error {
	path := "/connections"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	return c.Request(ctx, "DELETE", path, nil, nil)
}
func (c *Client) UpdateProvider(ctx context.Context, name string) error {
	return c.Request(ctx, "PUT", "/providers/rules/"+url.PathEscape(name), nil, nil)
}

// mihomo exposes the same log feed as newline-delimited JSON over HTTP.
// A separate client removes the ordinary request timeout while retaining the
// private Unix transport. Context cancellation closes the response immediately.
func (c *Client) Logs(ctx context.Context, emit func(Log) error) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.Base+"/logs?level=debug", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	client := *c.HTTP
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core logs: HTTP %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	for scanner.Scan() {
		var entry Log
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return fmt.Errorf("invalid core log: %w", err)
		}
		entry.Time = time.Now().Format("15:04:05")
		entry.Type = strings.ToLower(entry.Type)
		if len(entry.Payload) > 16384 {
			entry.Payload = entry.Payload[:16384] + "…"
		}
		if err := emit(entry); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("core log stream closed")
}
