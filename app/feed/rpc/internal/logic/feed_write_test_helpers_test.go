// feed_write_test_helpers_test.go
//
// CreateFeed / DeleteFeed 单元测试专用桩：
//   - recordingPublisher：svc.Publisher 的可记录实现（Topic/Body/错误注入）；
//   - ctrlFeedsModel：在 stubFeedsModel 基础上增加错误注入与真实软删除语义
//     （置 status=2 保留记录，而非物理删除）；
//   - errRelation：可注入 IsVip 错误的 relation 桩。
package logic

import (
	"context"
	"database/sql"
	"sync"

	"google.golang.org/grpc"

	"github.com/sponge-dad/feed/app/feed/model"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
	"github.com/sponge-dad/feed/app/relation/rpc/relationclient"
)

// sentMessage 记录一次 SendSync 调用。
type sentMessage struct {
	Topic string
	Body  []byte
}

// recordingPublisher svc.Publisher 桩实现，记录全部发送并可注入错误。
type recordingPublisher struct {
	mu      sync.Mutex
	sent    []sentMessage
	sendErr error
	calls   int
}

func (p *recordingPublisher) SendSync(topic string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.sendErr != nil {
		return p.sendErr
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	p.sent = append(p.sent, sentMessage{Topic: topic, Body: cp})
	return nil
}

func (p *recordingPublisher) messages() []sentMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]sentMessage(nil), p.sent...)
}

func (p *recordingPublisher) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// ctrlFeedsModel 支持错误注入与软删除语义的 FeedsModel 桩。
type ctrlFeedsModel struct {
	*stubFeedsModel
	insertErr       error
	findOneErr      error
	softDeleteErr   error
	softDeleteRes   bool // 无错误时 SoftDeleteByUserId 的返回值
	insertCalls     int
	softDeleteCalls int
}

func newCtrlFeedsModel() *ctrlFeedsModel {
	return &ctrlFeedsModel{stubFeedsModel: newStubFeedsModel(), softDeleteRes: true}
}

func (s *ctrlFeedsModel) Insert(ctx context.Context, data *model.Feeds) (sql.Result, error) {
	s.insertCalls++
	if s.insertErr != nil {
		return nil, s.insertErr
	}
	return s.stubFeedsModel.Insert(ctx, data)
}

func (s *ctrlFeedsModel) FindOne(ctx context.Context, id uint64) (*model.Feeds, error) {
	if s.findOneErr != nil {
		return nil, s.findOneErr
	}
	return s.stubFeedsModel.FindOne(ctx, id)
}

// SoftDeleteByUserId 软删除语义：仅置 status=2，保留记录（区别于物理删除）。
func (s *ctrlFeedsModel) SoftDeleteByUserId(_ context.Context, feedID, userID uint64) (bool, error) {
	s.softDeleteCalls++
	if s.softDeleteErr != nil {
		return false, s.softDeleteErr
	}
	if !s.softDeleteRes {
		return false, nil
	}
	if f, ok := s.byID[feedID]; ok && f.UserId == userID {
		f.Status = 2
		return true, nil
	}
	return false, nil
}

// errRelation IsVip 可注入错误的 relation 桩。
type errRelation struct {
	relationclient.Relation
	isVip    bool
	isVipErr error
}

func (s *errRelation) IsVip(_ context.Context, _ *relation.IsVipReq, _ ...grpc.CallOption) (*relation.IsVipResp, error) {
	if s.isVipErr != nil {
		return nil, s.isVipErr
	}
	return &relation.IsVipResp{IsVip: s.isVip}, nil
}
