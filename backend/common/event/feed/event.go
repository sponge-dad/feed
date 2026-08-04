package feed

import "github.com/google/uuid"

/*
{
  "event_id": "uuid-v4",
  "event_type": "feed.created",
  "feed_id": 123456789,
  "user_id": 10001,
  "is_vip_feed": false,
  "city_code": "440300",
  "created_at": 1752998400000
}
*/

// Topic 命名注意：RocketMQ topic 仅允许 ^[%|a-zA-Z0-9_-]+$，不能含 "."，故用连字符。
const TopicFeedCreated = "feed-created"
const TopicFeedDeleted = "feed-deleted"

type EventFeedCreate struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	FeedID    int64  `json:"feed_id"`
	UserID    int64  `json:"user_id"`
	IsVipFeed bool   `json:"is_vip_feed"`
	CityCode  string `json:"city_code"`
	CreatedAt int64  `json:"created_at"`
	RequestID string `json:"request_id"`
}

func NewEventFeedCreated(feedId, userId int64, isVipFeed bool, cityCode string, createAt int64, requestID string) *EventFeedCreate {
	return &EventFeedCreate{
		EventID:    uuid.NewString(), // TODO 应该需要修改
		EventType:  TopicFeedCreated,
		FeedID:     feedId,
		UserID:     userId,
		IsVipFeed:  isVipFeed,
		CityCode:   cityCode,
		CreatedAt:  createAt,
		RequestID:  requestID,
	}
}

type EventFeedDeleted struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	FeedID    int64  `json:"feed_id"`
	UserID    int64  `json:"user_id"`
	IsVipFeed bool   `json:"is_vip_feed"`
	CityCode  string `json:"city_code"`
	RequestID string `json:"request_id"`
}

func NewEventFeedDeleted(feedId, userId int64, isVipFeed bool, cityCode string, requestID string) *EventFeedDeleted {
	return &EventFeedDeleted{
		EventID:    uuid.NewString(), // TODO 应该需要修改
		EventType:  TopicFeedDeleted,
		FeedID:     feedId,
		UserID:     userId,
		IsVipFeed:  isVipFeed,
		CityCode:   cityCode,
		RequestID:  requestID,
	}
}
