// Package asr 语音识别接入层。
//
// 接口 + HTTP 实现 + fake 实现（CI 注入 fake，禁止真实计费调用）。
// API Key 从环境变量读取，不落代码。
package asr

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
	"time"
)

// Segment 分段字幕。
type Segment struct {
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

// Result ASR 结果。
type Result struct {
	Segments []Segment
	Language string // 如 zh-CN；空表示未检测
	NoSpeech bool   // 无语音（纯音乐/静音）
}

// Client 语音识别客户端。
type Client interface {
	// Transcribe 对 16kHz 单声道 wav 转写。
	Transcribe(ctx context.Context, audioPath string) (*Result, error)
}

// HTTPResponse 外部 ASR API 响应约定：
//
//	{"segments":[{"start_ms":0,"end_ms":1200,"text":"..."}],"language":"zh-CN"}
type HTTPResponse struct {
	Segments []struct {
		StartMs int64  `json:"start_ms"`
		EndMs   int64  `json:"end_ms"`
		Text    string `json:"text"`
	} `json:"segments"`
	Language string `json:"language"`
	NoSpeech bool   `json:"no_speech"`
}

// HTTPClient 真实实现：multipart 上传 wav，单次调用超时 60s。
type HTTPClient struct {
	Endpoint string // ASR 服务地址（含鉴权，走环境变量）
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

func (c *HTTPClient) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("audio", filepath.Base(audioPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("asr failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	var hr HTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		return nil, err
	}
	res := &Result{Language: hr.Language, NoSpeech: hr.NoSpeech}
	for _, s := range hr.Segments {
		res.Segments = append(res.Segments, Segment{StartMs: s.StartMs, EndMs: s.EndMs, Text: s.Text})
	}
	return res, nil
}

// FakeClient 测试用（CI 固定输出，禁止真实计费调用）。
type FakeClient struct {
	Result *Result
	Err    error
}

func (f *FakeClient) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	if f.Result != nil {
		return f.Result, nil
	}
	return &Result{
		Segments: []Segment{{StartMs: 0, EndMs: 1500, Text: "测试字幕"}},
		Language: "zh-CN",
	}, nil
}
