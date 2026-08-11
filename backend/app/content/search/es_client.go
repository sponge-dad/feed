// Package search 封装 Elasticsearch 客户端与混合检索。
//
// 索引约定（见 docs/design/agent/05-content-search.md §3）：
//   - 索引 feed_content_v1，写别名 feed_content_write，读别名 feed_content
//   - _id = feed_id，upsert 天然幂等（重复消费不产生重复文档）
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// Client ES 客户端封装。
type Client struct {
	es         *elasticsearch.Client
	readAlias  string // 读别名 feed_content
	writeAlias string // 写别名 feed_content_write
}

// NewClient 创建 ES 客户端。
func NewClient(addr, readAlias, writeAlias string) (*Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("elasticsearch addr is empty")
	}
	es, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{addr},
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		es:         es,
		readAlias:  readAlias,
		writeAlias: writeAlias,
	}, nil
}

// Document 索引文档结构（与 feed_content_v1 mapping 一一对应）。
type Document struct {
	FeedID          int64     `json:"feed_id"`
	AuthorID        int64     `json:"author_id"`
	FeedType        int8      `json:"feed_type"`
	Status          int8      `json:"status"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Summary         string    `json:"summary"`
	Transcript      string    `json:"transcript"`
	OcrText         string    `json:"ocr_text"`
	Category        string    `json:"category"`
	Topics          []string  `json:"topics"`
	Scenes          []string  `json:"scenes"`
	Objects         []string  `json:"objects"`
	Styles          []string  `json:"styles"`
	CityCode        string    `json:"city_code"`
	CityName        string    `json:"city_name"`
	Language        string    `json:"language"`
	MediaDurationMs int64     `json:"media_duration_ms"`
	PublishedAt     int64     `json:"published_at"` // unix ms
	LikeCount       int64     `json:"like_count"`
	CollectCount    int64     `json:"collect_count"`
	Embedding       []float32 `json:"embedding,omitempty"`
}

// IndexProfile 以 _id=feed_id 写入/更新画像文档（upsert 幂等）。
func (c *Client) IndexProfile(ctx context.Context, doc *Document) error {
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	req := esapi.IndexRequest{
		Index:      c.writeAlias,
		DocumentID: strconv.FormatInt(doc.FeedID, 10),
		Body:       bytes.NewReader(body),
	}
	res, err := req.Do(ctx, c.es)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("es index feed %d failed: %s", doc.FeedID, res.String())
	}
	return nil
}

// DeleteProfile 删除索引文档（幂等：文档不存在也算成功）。
func (c *Client) DeleteProfile(ctx context.Context, feedID int64) error {
	req := esapi.DeleteRequest{
		Index:      c.writeAlias,
		DocumentID: strconv.FormatInt(feedID, 10),
	}
	res, err := req.Do(ctx, c.es)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// 404（文档不存在）视为成功。
	if res.StatusCode == 404 {
		return nil
	}
	if res.IsError() {
		return fmt.Errorf("es delete feed %d failed: %s", feedID, res.String())
	}
	return nil
}
