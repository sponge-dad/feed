package logic

import (
	"context"
	"strings"
	"unicode"

	"github.com/sponge-dad/feed/app/content/rpc/content"
	"github.com/sponge-dad/feed/app/content/search"
	"github.com/sponge-dad/feed/app/content/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"

	"github.com/zeromicro/go-zero/core/logx"
)

type SearchContentLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSearchContentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SearchContentLogic {
	return &SearchContentLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// SearchContent 结构化内容检索：
//   - 入参为结构化条件（05-content-search.md §4），全空条件返回 15006
//   - 三路召回 + RRF 融合（hybrid）
//   - 结果必须经 Feed RPC 回查真实存在且 status=NORMAL（不信任索引，A4 硬要求）
func (l *SearchContentLogic) SearchContent(in *content.SearchContentReq) (*content.SearchContentResp, error) {
	q, err := l.validate(in)
	if err != nil {
		return nil, err
	}

	hits, err := l.svcCtx.Es.Search(l.ctx, *q)
	if err != nil {
		// ES 不可用 → 15007（Agent 告知数据获取失败，不得编造结果）。
		return nil, errorx.NewWithMsg(errorx.ContentSearchUnavailable, "检索服务暂不可用")
	}
	if len(hits) == 0 {
		return &content.SearchContentResp{NoMatchCode: "NO_MATCH"}, nil
	}

	// 回查 Feed 存在性与状态（BatchGetFeeds ≤100，索引可能残留已删除 feed）。
	ids := make([]int64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.FeedID)
	}
	batch, err := l.svcCtx.FeedRpc.BatchGetFeeds(l.ctx, &feedpb.BatchGetFeedsReq{
		FeedIds: ids,
		UserId:  in.ViewerId,
	})
	if err != nil {
		// 回查失败：记录参数便于定位，并封装友好错误（与 ES 失败处理一致）。
		l.Logger.Errorf("search content feed re-check failed feed_ids=%v viewer=%d err=%v", ids, in.ViewerId, err)
		return nil, errorx.NewWithMsg(errorx.ContentSearchUnavailable, "检索服务暂不可用")
	}

	items := make([]*content.SearchResultItem, 0, len(hits))
	for _, h := range hits {
		fi, ok := batch.Feeds[h.FeedID]
		if !ok || fi.Status != 1 { // 不存在或非 NORMAL → 过滤
			continue
		}
		item := &content.SearchResultItem{
			FeedId:          h.FeedID,
			Title:           fi.Title,
			CoverUrl:        fi.CoverUrl,
			Category:        h.Category,
			MatchedTopics:   h.MatchedTopics,
			MediaDurationMs: h.MediaDurationMs,
			PublishedAt:     h.PublishedAt,
			Score:           h.Score,
		}
		for _, r := range h.Reasons {
			item.MatchReasons = append(item.MatchReasons, &content.MatchReason{
				Code:   r.Code,
				Detail: r.Detail,
			})
		}
		items = append(items, item)
	}

	resp := &content.SearchContentResp{
		TotalCandidates: int32(len(hits)),
		Items:           items,
	}
	if len(items) == 0 {
		resp.NoMatchCode = "FILTERED_OUT"
	}
	return resp, nil
}

// validate 校验并归一化结构化检索条件（05-content-search.md §4 校验规则）。
func (l *SearchContentLogic) validate(in *content.SearchContentReq) (*search.Query, error) {
	q := &search.Query{
		CityName: in.CityName,
		FeedType: in.FeedType,
		Sort:     in.Sort,
	}
	// Category 必须落在类目白名单内，否则降级为不限类目（05-content-search.md §4）。
	if l.svcCtx.Config.IsValidCategory(in.Category) {
		q.Category = in.Category
	}

	// keywords：≤5 个，单个 1~20 字符，去除控制字符
	for _, kw := range in.Keywords {
		kw = strings.TrimSpace(sanitizeKeyword(kw))
		if kw == "" {
			continue
		}
		if len([]rune(kw)) > 20 {
			kw = string([]rune(kw)[:20])
		}
		if len(q.Keywords) >= 5 {
			break
		}
		q.Keywords = append(q.Keywords, kw)
	}

	// topics：≤5 个
	for _, t := range in.Topics {
		t = strings.TrimSpace(sanitizeKeyword(t))
		if t == "" {
			continue
		}
		if len(q.Topics) >= 5 {
			break
		}
		q.Topics = append(q.Topics, t)
	}

	// limit：1~20 收敛到边界
	q.Limit = int(in.Limit)
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 20 {
		q.Limit = 20
	}

	// 发布时间窗口：1~365 收敛
	if in.PublishedWithinDays > 0 {
		q.WithinDays = in.PublishedWithinDays
		if q.WithinDays < 1 {
			q.WithinDays = 1
		}
		if q.WithinDays > 365 {
			q.WithinDays = 365
		}
	}

	// sort 白名单
	switch in.Sort {
	case "latest", "hot":
	default:
		q.Sort = "relevance"
	}

	// 全空条件（keywords 与 topics、category 全空）→ 15006，避免全库扫描。
	if len(q.Keywords) == 0 && len(q.Topics) == 0 && q.Category == "" {
		return nil, errorx.New(errorx.ContentSearchEmptyQuery)
	}

	return q, nil
}

// sanitizeKeyword 去除控制字符。
func sanitizeKeyword(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
