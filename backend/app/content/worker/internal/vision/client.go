// Package vision 多模态标签生成接入层（接口 + HTTP + fake，CI 注入 fake）。
package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Input 多模态分析输入（顺序即优先级，超长按此顺序截断）。
type Input struct {
	Title       string   // ≤200 字
	Description string   // ≤200 字
	Transcript  string   // ≤TranscriptMaxChars
	OcrText     []string // ≤30 条
	KeyFrames   []string // 图片路径，≤8 张（首帧必送）
}

// Result 多模态标签生成结果（模型输出，必须经结构/范围/白名单/敏感词四层校验后才允许入库）。
type Result struct {
	Category string   // 白名单类目
	Summary  string   // 20~200 字
	Topics   []string // ≤10，单条 1~20 字符
	Objects  []string // ≤15
	Scenes   []string // ≤8
	Styles   []string // ≤5
}

// Client 多模态标签生成客户端。
type Client interface {
	// Analyze 生成结构化标签与摘要。
	Analyze(ctx context.Context, in Input) (*Result, error)
}

// HTTPClient 真实实现：JSON POST（调用 ARK 多模态 / 自建服务）。
type HTTPClient struct {
	Endpoint string
	APIKey   string
	Model    string
	HTTP     *http.Client
}

func NewHTTPClient(endpoint, apiKey, model string) *HTTPClient {
	return &HTTPClient{
		Endpoint: endpoint,
		APIKey:   apiKey,
		Model:    model,
		HTTP:     &http.Client{Timeout: 90 * time.Second},
	}
}

func (c *HTTPClient) Analyze(ctx context.Context, in Input) (*Result, error) {
	payload := map[string]any{
		"model":       c.Model,
		"title":       in.Title,
		"description": in.Description,
		"transcript":  in.Transcript,
		"ocr_text":    in.OcrText,
		"key_frames":  in.KeyFrames,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("vision failed: status=%d body=%s", resp.StatusCode, string(b))
	}
	var r Result
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// FakeClient 测试用（CI 固定输出）。
type FakeClient struct {
	Result *Result
	Err    error
}

func (f *FakeClient) Analyze(ctx context.Context, in Input) (*Result, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	if f.Result != nil {
		return f.Result, nil
	}
	return &Result{
		Category: "户外旅行",
		Summary:  "视频介绍了西安周边一处适合周末露营的营地",
		Topics:   []string{"露营", "西安周边"},
		Scenes:   []string{"户外", "营地"},
		Styles:   []string{"攻略"},
	}, nil
}
