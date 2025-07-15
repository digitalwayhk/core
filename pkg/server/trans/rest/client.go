package rest

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/types"
)

func Post(path string, data url.Values) ([]byte, error) {
	if !strings.HasPrefix(path, "http://") {
		path = "http://" + strings.TrimSpace(path)
	}
	resp, err := http.PostForm(path, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, err
}

func PostJson(path string, data []byte, payload *types.PayLoad) ([]byte, error) {
	token := ""
	traceID := ""
	if payload != nil {
		token = payload.Token
		traceID = payload.TraceID
	}
	if !strings.HasPrefix(path, "http://") {
		path = "http://" + strings.TrimSpace(path)
	}
	var resp *http.Response
	var err error
	req, err := http.NewRequest("POST", path, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if traceID != "" {
		req.Header.Set("X-Trace-Id", traceID)
	}
	client := &http.Client{}
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
func HttpGet(path string, payload *types.PayLoad) ([]byte, error) {
	if !strings.HasPrefix(path, "http://") {
		path = "http://" + strings.TrimSpace(path)
	}
	token := ""
	traceID := ""
	if payload != nil {
		token = payload.Token
		traceID = payload.TraceID
	}
	var resp *http.Response
	var err error
	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if traceID != "" {
		req.Header.Set("X-Trace-Id", traceID)
	}
	client := &http.Client{}
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
