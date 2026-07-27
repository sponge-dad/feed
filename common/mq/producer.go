package mq

import (
	"context"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/zeromicro/go-zero/core/logx"
)

type Producer struct {
	p rocketmq.Producer
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
