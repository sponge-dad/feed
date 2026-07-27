package mq

import (
	"context"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

type Consumer struct {
	c rocketmq.PushConsumer
}

func NewConsumer(nameServers []string, groupName string) (*Consumer, error) {
	c, err := rocketmq.NewPushConsumer(
		consumer.WithGroupName(groupName),
		consumer.WithNameServer(nameServers),
	)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		c: c,
	}, nil
}

func (m *Consumer) Subscribe(topic string, fn func(ctx context.Context, msg *primitive.MessageExt) error) error {
	return m.c.Subscribe(topic, consumer.MessageSelector{},
		func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
			for _, msg := range msgs {
				if err := fn(ctx, msg); err != nil {
					logx.Errorf("consume failed topic=%s msgId=%s body=%s err=%v",
						topic, msg.MsgId, string(msg.Body), err)
					// 返回重试：RocketMQ 会重新投递，直至进入死信队列
					return consumer.ConsumeRetryLater, err
				}
			}
			return consumer.ConsumeSuccess, nil
		})
}

func (m *Consumer) Start() error {
	return m.c.Start()
}

func (m *Consumer) Shutdown() error {
	if m == nil || m.c == nil {
		return nil
	}
	return m.c.Shutdown()
}
