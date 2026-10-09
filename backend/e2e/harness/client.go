package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Client is a tiny JSON client for the Jarvis api with fake bearer auth.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

// Response is a raw API response.
type Response struct {
	Status int
	Body   []byte
}

// Decode unmarshals the body into v.
func (r Response) Decode(v any) error { return json.Unmarshal(r.Body, v) }

// ErrorCode returns error.code from an error envelope ("" if absent).
func (r Response) ErrorCode() string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Body, &e)
	return e.Error.Code
}

// As returns a copy of the client using another token.
func (c *Client) As(token string) *Client {
	cp := *c
	cp.Token = token
	return &cp
}

// Do sends a request; body (if not nil) is JSON-encoded.
func (c *Client) Do(ctx context.Context, method, path string, body any) (Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return Response{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return Response{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return Response{Status: resp.StatusCode, Body: b}, err
}
