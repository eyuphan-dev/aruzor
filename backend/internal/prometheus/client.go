// Package prometheus provides a thin client over the Prometheus HTTP API
// used by Aruzor's no-code query builder and auto-discovery engine.
package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type apiResponse struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  string          `json:"error"`
}

// Query runs an instant PromQL query (/api/v1/query).
func (c *Client) Query(ctx context.Context, promQL string, at time.Time) (json.RawMessage, error) {
	params := url.Values{"query": {promQL}}
	if !at.IsZero() {
		params.Set("time", strconv.FormatInt(at.Unix(), 10))
	}
	return c.get(ctx, "/api/v1/query", params)
}

// QueryRange runs a range PromQL query (/api/v1/query_range).
func (c *Client) QueryRange(ctx context.Context, promQL string, start, end time.Time, step time.Duration) (json.RawMessage, error) {
	params := url.Values{
		"query": {promQL},
		"start": {strconv.FormatInt(start.Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}
	return c.get(ctx, "/api/v1/query_range", params)
}

// LabelValues fetches all values for a given label, used by the
// no-code query builder's dropdowns and the auto-discovery engine.
func (c *Client) LabelValues(ctx context.Context, label string) (json.RawMessage, error) {
	return c.get(ctx, "/api/v1/label/"+url.PathEscape(label)+"/values", nil)
}

// MetricNames returns every metric name currently exposed by Prometheus.
func (c *Client) MetricNames(ctx context.Context) (json.RawMessage, error) {
	return c.LabelValues(ctx, "__name__")
}

// Targets returns Prometheus' scrape target list (/api/v1/targets), which
// tells you whether an exporter has actually been reachable — something a
// metric query can't distinguish from "the exporter is up but idle".
func (c *Client) Targets(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/api/v1/targets", nil)
}

func (c *Client) get(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	endpoint := c.baseURL + path
	if params != nil {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus istegi basarisiz: %w", err)
	}
	defer resp.Body.Close()

	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("prometheus yaniti okunamadi: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prometheus hatasi: %s", parsed.Error)
	}
	return parsed.Data, nil
}
