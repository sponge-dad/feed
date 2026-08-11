// Package ocr 画面文字识别接入层（接口 + HTTP + fake，CI 注入 fake）。
package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client 文字识别客户端。
type Client interface {
	// Recognize 对多张关键帧批量识别，返回去重后的文字数组（≤30 条，单条 ≤100 字符）。
	Recognize(ctx context.Context, imagePaths []string) ([]string, error)
}

// HTTPResponse 外部 OCR API 响应约定：
//
//	{"items":["文字1","文字2"],"results":[{"text":"...","confidence":0.9}]}
type HTTPResponse struct {
	Items []string `json:"items"`
}

// HTTPClient 真实实现：multipart 上传多张图片。
type HTTPClient struct {
	Endpoint string
	APIKey   string
	HTTP     *http.Client
}

func NewHTTPClient(endpoint, apiKey string) *HTTPClient {
	return &HTTPClient{
		Endpoint: endpoint,
		APIKey:   apiKey,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *HTTPClient) Recognize(ctx context.Context, imagePaths []string) ([]string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range imagePaths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		fw, err := w.CreateFormFile("images", filepath.Base(p))
		if err != nil {
			f.Close()
			return nil, err
		}
		if _, err := io.Copy(fw, f); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
	}
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ocr failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	var hr HTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		return nil, err
	}
	return normalizeOCR(hr.Items), nil
}

// normalizeOCR 归一化：去空、去重复（文本归一化后完全相同合并）、单条截断 100 字符、上限 30 条。
func normalizeOCR(items []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, it := range items {
		norm := strings.TrimSpace(strings.ToLower(it))
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		s := strings.TrimSpace(it)
		if len([]rune(s)) > 100 {
			s = string([]rune(s)[:100])
		}
		out = append(out, s)
		if len(out) >= 30 {
			break
		}
	}
	return out
}

// FakeClient 测试用（CI 固定输出）。
type FakeClient struct {
	Items []string
	Err   error
}

func (f *FakeClient) Recognize(ctx context.Context, imagePaths []string) ([]string, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	if f.Items != nil {
		return f.Items, nil
	}
	return []string{"OCR 测试文字"}, nil
}
