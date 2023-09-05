package rest

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
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

func PostJson(path string, data []byte) ([]byte, error) {
	if !strings.HasPrefix(path, "http://") {
		path = "http://" + strings.TrimSpace(path)
	}
	resp, err := http.Post(path, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
func HttpGet(path string) ([]byte, error) {
	if !strings.HasPrefix(path, "http://") {
		path = "http://" + strings.TrimSpace(path)
	}
	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
