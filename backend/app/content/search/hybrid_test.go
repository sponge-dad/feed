// hybrid_test.go
//
// 职责：混合检索（三路召回 + RRF 融合）的单元测试（T066）。
// 用 httptest 模拟 ES：按请求体内容（multi_match / terms / knn）返回不同的召回结果。
package search

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustWrite(s string) []byte {
	return []byte(s)
}

const (
	bm25Resp = `{"hits":{"hits":[
		{"_source":{"feed_id":1,"title":"露营攻略","category":"户外旅行","topics":["露营"],"published_at":1710000000000,"media_duration_ms":120000,"like_count":10,"collect_count":2}},
		{"_source":{"feed_id":2,"title":"露营装备清单","category":"户外旅行","topics":["露营","装备"],"published_at":1720000000000,"media_duration_ms":60000,"like_count":5,"collect_count":0}}]}}`

	tagResp = `{"hits":{"hits":[
		{"_source":{"feed_id":3,"title":"某美食视频","category":"美食","topics":["露营"],"published_at":1730000000000,"media_duration_ms":90000,"like_count":1,"collect_count":0}},
		{"_source":{"feed_id":1,"title":"露营攻略","category":"户外旅行","topics":["露营"],"published_at":1710000000000,"media_duration_ms":120000,"like_count":10,"collect_count":2}}]}}`

	vecResp = `{"hits":{"hits":[
		{"_source":{"feed_id":5,"title":"语义相近内容","category":"生活方式","topics":["露营","露营装备"],"published_at":1700000000000,"media_duration_ms":45000,"like_count":3,"collect_count":0}}]}}`
)

// newMockClient 构造指向 mock ES 的客户端。
func newMockClient(t *testing.T) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// go-elasticsearch 产品校验
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), "multi_match"):
			_, _ = w.Write(mustWrite(bm25Resp))
		case strings.Contains(string(body), "terms"):
			_, _ = w.Write(mustWrite(tagResp))
		case strings.Contains(string(body), "knn"):
			_, _ = w.Write(mustWrite(vecResp))
		default:
			_, _ = w.Write([]byte(`{"hits":{"hits":[]}}`))
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "feed_content", "feed_content_write")
	require.NoError(t, err)
	return c
}

func TestSearch_KeywordOnly(t *testing.T) {
	c := newMockClient(t)
	hits, err := c.Search(context.Background(), Query{
		Keywords: []string{"露营"},
		Limit:    10,
	})
	require.NoError(t, err)
	// 只有 BM25 路有结果
	require.Len(t, hits, 2)
	// relevance 排序：feed1（RRF rank1）排前
	assert.Equal(t, int64(1), hits[0].FeedID)
	// 命中原因含 KEYWORD_HIT
	assert.True(t, hasReason(hits[0].Reasons, "KEYWORD_HIT"), "应包含 KEYWORD_HIT 命中原因")
}

func hasReason(rs []MatchReason, code string) bool {
	for _, r := range rs {
		if r.Code == code {
			return true
		}
	}
	return false
}

func TestSearch_KeywordAndTag_RRF(t *testing.T) {
	c := newMockClient(t)
	hits, err := c.Search(context.Background(), Query{
		Keywords:    []string{"露营"},
		Topics:      []string{"露营"},
		Category:    "户外旅行",
		QueryVector: make([]float32, 1024), // 启用 VECTOR 路
		Limit:       10,
	})
	require.NoError(t, err)
	// 三路结果融合：feed1 同时被 BM25(rank1)+TAG(rank2) 命中 → RRF 最高，排第一
	require.NotEmpty(t, hits)
	assert.Equal(t, int64(1), hits[0].FeedID)
	ids := feedIDs(hits)
	assert.Contains(t, ids, int64(3)) // 仅 TAG 命中
	assert.Contains(t, ids, int64(5)) // 仅 VECTOR 命中
}

func TestSearch_WithVector(t *testing.T) {
	c := newMockClient(t)
	hits, err := c.Search(context.Background(), Query{
		QueryVector: make([]float32, 1024),
		Limit:       5,
	})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, int64(5), hits[0].FeedID)
	assert.True(t, hasReason(hits[0].Reasons, "SEMANTIC_HIT"), "应包含 SEMANTIC_HIT 命中原因")
}

func TestSearch_EmptyConditions(t *testing.T) {
	c := newMockClient(t)
	hits, err := c.Search(context.Background(), Query{Limit: 10})
	require.NoError(t, err)
	// 无任何召回条件 → 空结果，不报错（全空条件在 logic 层已拦截）
	assert.Empty(t, hits)
}

func TestSearch_LimitClamp(t *testing.T) {
	c := newMockClient(t)
	hits, err := c.Search(context.Background(), Query{Keywords: []string{"露营"}, Limit: 100})
	require.NoError(t, err)
	// limit 上限 20（R1 只有 2 条，验证不会因 limit 报错）
	assert.LessOrEqual(t, len(hits), 20)
}

func TestSearch_LatestSort(t *testing.T) {
	c := newMockClient(t)
	hits, err := c.Search(context.Background(), Query{Keywords: []string{"露营"}, Sort: "latest", Limit: 10})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	// latest：发布时间新在前（feed2=1720... > feed1=1710...）
	assert.Equal(t, int64(2), hits[0].FeedID)
}

func feedIDs(hits []*Hit) []int64 {
	ids := make([]int64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.FeedID)
	}
	return ids
}
