package mq

import (
	"context"
	"sync"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/zeromicro/go-zero/core/logx"
)

type Producer struct {
	p rocketmq.Producer
	// mu 串行化对底层 rocketmq Producer 的并发调用。
	// 底层 Producer 在并发 SendAsync 时会并发读写内部 topic 路由缓存，
	// 触发数据竞争（-race 下交互并发测试可复现）。串行化后消除该竞争，
	// 且 SendAsync 立即返回的语义不变，不影响调用方吞吐。
	mu sync.Mutex
}

func NewProducer(nameServers []string, groupName string) (*Producer, error) {
	p, err := rocketmq.NewProducer(
		producer.WithNameServer(nameServers),
		producer.WithGroupName(groupName),
	)
	if err != nil {
		return nil, err
	}
	if err = p.Start(); err != nil {
		return nil, err
	}
	return &Producer{
		p: p,
	}, nil
}

func (m *Producer) SendSync(topic string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.p.SendAsync(context.Background(), func(ctx context.Context, result *primitive.SendResult, err error) {
		if err != nil {
			logx.Errorf("send MQ message failed topic=%s err=%v", topic, err)
			return
		}
		logx.Infof("send MQ message success topic=%s, msgId=%s", topic, result.MsgID)
	}, primitive.NewMessage(topic, body))
}

func (m *Producer) Close() error {
	if m == nil || m.p == nil {
		return nil
	}
	return m.p.Shutdown()
}
