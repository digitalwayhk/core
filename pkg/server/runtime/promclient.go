package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrPrometheusUnavailable 表示 Prometheus 查询不可用。
var ErrPrometheusUnavailable = fmt.Errorf("prometheus unavailable")

// Sample 是一个时间点样本。
type Sample struct {
	Timestamp time.Time
	Value     float64
}

// Vector 是 instant query 结果。
type Vector []Sample

// PromClient Prometheus HTTP API 客户端。
type PromClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewPromClient 创建客户端。baseURL 如 http://prometheus:9090。
func NewPromClient(baseURL string, timeout time.Duration) *PromClient {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &PromClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Query 执行 instant query。
func (c *PromClient) Query(ctx context.Context, query string, ts time.Time) (Vector, error) {
	if c == nil || c.baseURL == "" {
		return nil, ErrPrometheusUnavailable
	}
	u, err := url.Parse(c.baseURL + "/api/v1/query")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base url", ErrPrometheusUnavailable)
	}
	q := u.Query()
	q.Set("query", query)
	if !ts.IsZero() {
		q.Set("time", strconv.FormatFloat(float64(ts.UnixNano())/1e9, 'f', -1, 64))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request", ErrPrometheusUnavailable)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPrometheusUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read body", ErrPrometheusUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrPrometheusUnavailable, resp.StatusCode)
	}
	return parseQueryResponse(body)
}

type promAPIResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []interface{} `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func parseQueryResponse(body []byte) (Vector, error) {
	var parsed promAPIResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: decode", ErrPrometheusUnavailable)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("%w: status=%s", ErrPrometheusUnavailable, parsed.Status)
	}
	out := make(Vector, 0, len(parsed.Data.Result))
	for _, r := range parsed.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		ts, _ := r.Value[0].(float64)
		vs, _ := r.Value[1].(string)
		v, err := strconv.ParseFloat(vs, 64)
		if err != nil {
			continue
		}
		out = append(out, Sample{
			Timestamp: time.Unix(0, int64(ts*1e9)),
			Value:     v,
		})
	}
	return out, nil
}
