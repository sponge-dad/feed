package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// 混合召回参数（见 05-content-search.md §4 结构化条件）。
type Query struct {
	Keywords    []string  // ≤5，BM25 全文检索
	Category    string    // 类目白名单（R3 精确匹配）
	Topics      []string  // ≤5（R3 标签匹配）
	CityName    string    // 城市名（city_name 精确匹配）
	FeedType    int32     // 1 图文 / 2 视频，0=不限
	WithinDays  int32     // 发布时间窗口（1~365），0=不限
	Sort        string    // relevance / latest / hot
	Limit       int       // 1~20
	QueryVector []float32 // 可选：R2 kNN 查询向量（无 embedding 服务时不传则跳过该路）
}

// 召回路径权重（05-content-search.md §5 RRF）。
const (
	weightBM25   = 1.0
	weightVector = 1.0
	weightTag    = 0.6
	rrfK         = 60 // RRF 常数
)

// Hit 融合后的检索命中项（业务过滤由调用方通过 Feed RPC 回查完成）。
type Hit struct {
	FeedID          int64
	Title           string
	Category        string
	MatchedTopics   []string
	PublishedAt     int64 // unix ms
	MediaDurationMs int64
	LikeCount       int64
	CollectCount    int64
	Score           float64 // 融合分
	Reasons         []MatchReason
}

// MatchReason 命中原因（后端生成，模型只负责转述）。
type MatchReason struct {
	Code   string
	Detail string
}

type searchHit struct {
	feedID          int64
	title           string
	category        string
	matchedTopics   []string
	publishedAt     int64
	mediaDurationMs int64
	likeCount       int64
	collectCount    int64
}

type esSearchResp struct {
	Hits struct {
		Hits []struct {
			Source json.RawMessage `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

// 检索只需要这些字段（不含 transcript/ocr_text，避免大字段传输）。
var sourceFields = []string{
	"feed_id", "title", "category", "topics", "published_at",
	"media_duration_ms", "like_count", "collect_count",
}

// Search 执行三路召回 + RRF 融合，返回按 sort 排序的 Top-N 命中。
func (c *Client) Search(ctx context.Context, q Query) ([]*Hit, error) {
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 20 {
		q.Limit = 20
	}

	topK := 50 // 每路召回数（05-content-search.md §5）

	type recResult struct {
		path string   // BM25 / VECTOR / TAG
		hits []*searchHit
		err  error
	}

	recs := make(chan recResult, 3)
	var wg sync.WaitGroup
	launch := func(path string, body map[string]any) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := c.rawSearch(ctx, body)
			if err != nil {
				recs <- recResult{path: path, err: err}
				return
			}
			if len(hits) > topK {
				hits = hits[:topK]
			}
			recs <- recResult{path: path, hits: hits}
		}()
	}

	filter := buildFilter(q)

	// R1：BM25 全文（multi_match，best_fields，带字段权重）
	if len(q.Keywords) > 0 {
		mm := map[string]any{
			"multi_match": map[string]any{
				"query":  strings.Join(q.Keywords, " "),
				"fields": []string{"title^3", "topics^3", "summary^2", "ocr_text^1.5", "transcript^1", "description^1"},
				"type":   "best_fields",
			},
		}
		launch("BM25", map[string]any{"query": boolMust(mm, filter), "_source": sourceFields, "size": topK})
	}

	// R2：向量 kNN（query_vector 由调用方提供，无 embedding 服务时跳过该路）
	if len(q.QueryVector) > 0 {
		body := map[string]any{
			"knn": map[string]any{
				"field":          "embedding",
				"query_vector":   q.QueryVector,
				"k":              topK,
				"num_candidates": topK * 2,
			},
			"_source": sourceFields,
			"size":    topK,
		}
		if len(filter) > 0 {
			body["post_filter"] = map[string]any{"bool": map[string]any{"filter": filter}}
		}
		launch("VECTOR", body)
	}

	// R3：标签精确匹配（topics / category / scenes）
	if q.Category != "" || len(q.Topics) > 0 {
		should := make([]map[string]any, 0, 3)
		if len(q.Topics) > 0 {
			should = append(should, map[string]any{"terms": map[string]any{"topics": q.Topics, "boost": 2.0}})
		}
		if q.Category != "" {
			should = append(should, map[string]any{"term": map[string]any{"category": q.Category, "boost": 1.5}})
		}
		if len(q.Topics) > 0 {
			should = append(should, map[string]any{"terms": map[string]any{"scenes": q.Topics, "boost": 1.0}})
		}
		body := map[string]any{
			"query":     map[string]any{"bool": map[string]any{"should": should, "minimum_should_match": 1}},
			"_source":   sourceFields,
			"size":      topK,
		}
		if len(filter) > 0 {
			body["query"].(map[string]any)["bool"].(map[string]any)["filter"] = filter
		}
		launch("TAG", body)
	}

	wg.Wait()
	close(recs)

	// RRF 融合：score(d) = Σ_r w_r / (60 + rank_r(d))
	rrfScore := make(map[int64]float64)
	meta := make(map[int64]*searchHit) // feedID -> 文档信息（取最先命中的那路）
	pathHit := make(map[int64]map[string]bool)
	var errs []error

	for r := range recs {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		w := pathWeight(r.path)
		for i, h := range r.hits {
			rank := i + 1 // 1-based
			rrfScore[h.feedID] += w / (rrfK + float64(rank))
			if _, ok := meta[h.feedID]; !ok {
				meta[h.feedID] = h
			}
			if pathHit[h.feedID] == nil {
				pathHit[h.feedID] = make(map[string]bool)
			}
			pathHit[h.feedID][r.path] = true
		}
	}
	if len(errs) > 0 && len(meta) == 0 {
		return nil, fmt.Errorf("hybrid search: %v", errs)
	}

	return finalize(q, rrfScore, meta, pathHit), nil
}

// pathWeight 返回召回路径权重。
func pathWeight(path string) float64 {
	switch path {
	case "BM25":
		return weightBM25
	case "VECTOR":
		return weightVector
	default:
		return weightTag
	}
}

// finalize 排序 + 命中原因，返回 Top-N（过滤逻辑由调用方完成）。
func finalize(q Query, rrfScore map[int64]float64, meta map[int64]*searchHit, pathHit map[int64]map[string]bool) []*Hit {
	hits := make([]*Hit, 0, len(meta))
	// 互动质量：log1p(like + 2*collect) 归一化到 0~1
	maxQuality := 1.0
	now := time.Now()
	for feedID, m := range meta {
		qScore := math.Log1p(float64(m.likeCount + 2*m.collectCount))
		if qScore > maxQuality {
			maxQuality = qScore
		}
		h := &Hit{
			FeedID:          feedID,
			Title:           m.title,
			Category:        m.category,
			MatchedTopics:   m.matchedTopics,
			PublishedAt:     m.publishedAt,
			MediaDurationMs: m.mediaDurationMs,
			LikeCount:       m.likeCount,
			CollectCount:    m.collectCount,
			Score:           rrfScore[feedID],
		}
		// 命中原因（后端生成）
		if pathHit[feedID]["BM25"] {
			h.Reasons = append(h.Reasons, MatchReason{Code: "KEYWORD_HIT", Detail: "标题/正文/字幕命中关键词"})
		}
		if pathHit[feedID]["VECTOR"] {
			h.Reasons = append(h.Reasons, MatchReason{Code: "SEMANTIC_HIT", Detail: "内容与查询语义相近"})
		}
		if pathHit[feedID]["TAG"] {
			h.Reasons = append(h.Reasons, MatchReason{Code: "TOPIC_MATCH", Detail: "内容标签匹配"})
		}
		// 新鲜度 exp(-Δdays/30)
		fresh := 0.0
		if m.publishedAt > 0 {
			days := now.Sub(time.UnixMilli(m.publishedAt)).Hours() / 24
			if days < 0 {
				days = 0
			}
			fresh = math.Exp(-days / 30)
		}
		quality := math.Log1p(float64(m.likeCount + 2*m.collectCount)) / maxQuality
		// 综合分（仅用于 relevance 排序）：0.7×RRF + 0.15×freshness + 0.15×quality
		h.Score = 0.7*h.Score + 0.15*fresh + 0.15*quality
		hits = append(hits, h)
	}

	switch q.Sort {
	case "latest":
		sort.Slice(hits, func(i, j int) bool { return hits[i].PublishedAt > hits[j].PublishedAt })
	case "hot":
		sort.Slice(hits, func(i, j int) bool {
			qi := hits[i].LikeCount + 2*hits[i].CollectCount
			qj := hits[j].LikeCount + 2*hits[j].CollectCount
			return qi > qj
		})
	default: // relevance
		sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	}

	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits
}

// rawSearch 执行一次 _search 并解析命中。
func (c *Client) rawSearch(ctx context.Context, body map[string]any) ([]*searchHit, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	res, err := c.es.Search(
		c.es.Search.WithContext(ctx),
		c.es.Search.WithIndex(c.readAlias),
		c.es.Search.WithBody(bytes.NewReader(payload)),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("es search failed: %s", res.String())
	}
	var resp esSearchResp
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	hits := make([]*searchHit, 0, len(resp.Hits.Hits))
	for _, h := range resp.Hits.Hits {
		var src struct {
			FeedID          int64    `json:"feed_id"`
			Title           string   `json:"title"`
			Category        string   `json:"category"`
			Topics          []string `json:"topics"`
			PublishedAt     int64    `json:"published_at"`
			MediaDurationMs int64    `json:"media_duration_ms"`
			LikeCount       int64    `json:"like_count"`
			CollectCount    int64    `json:"collect_count"`
		}
		if err := json.Unmarshal(h.Source, &src); err != nil {
			continue
		}
		hits = append(hits, &searchHit{
			feedID:          src.FeedID,
			title:           src.Title,
			category:        src.Category,
			matchedTopics:   src.Topics,
			publishedAt:     src.PublishedAt,
			mediaDurationMs: src.MediaDurationMs,
			likeCount:       src.LikeCount,
			collectCount:    src.CollectCount,
		})
	}
	return hits, nil
}

// buildFilter 构造时间窗口 / feed_type / 城市过滤（返回 filter 数组或 nil）。
func buildFilter(q Query) []map[string]any {
	var filter []map[string]any
	if q.WithinDays > 0 {
		from := time.Now().AddDate(0, 0, -int(q.WithinDays)).UnixMilli()
		filter = append(filter, map[string]any{"range": map[string]any{"published_at": map[string]any{"gt": from}}})
	}
	if q.FeedType == 1 || q.FeedType == 2 {
		filter = append(filter, map[string]any{"term": map[string]any{"feed_type": q.FeedType}})
	}
	if q.CityName != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"city_name": q.CityName}})
	}
	return filter
}

// boolMust 组合 must + filter。
func boolMust(must map[string]any, filter []map[string]any) map[string]any {
	if len(filter) == 0 {
		return must
	}
	return map[string]any{"bool": map[string]any{"must": []any{must}, "filter": filter}}
}
