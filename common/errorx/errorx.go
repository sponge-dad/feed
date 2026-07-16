// Package errorx 统一业务错误码定义。
//
// 业务码分段（见 docs/api/README.md）：
//
//	0            成功
//	10000~10999  User 服务
//	11000~11999  Relation 服务
//	12000~12999  Feed 服务
//	13000~13999  Comment 服务
//	14000~14999  Interaction 服务
//
// 用法：
//
//	return nil, errorx.New(errorx.UserNotFound)
//	return nil, errorx.NewWithMsg(errorx.UserNotFound, "自定义提示")
package errorx

import "fmt"

// CodeError 业务错误，携带业务码和提示信息
type CodeError struct {
	Code    int
	Message string
}

func (e *CodeError) Error() string {
	return fmt.Sprintf("code: %d, message: %s", e.Code, e.Message)
}

// New 根据预定义错误码创建错误（使用默认提示）
func New(code int) *CodeError {
	return &CodeError{Code: code, Message: message(code)}
}

// NewWithMsg 创建错误并自定义提示
func NewWithMsg(code int, msg string) *CodeError {
	return &CodeError{Code: code, Message: msg}
}

// ---------- 通用错误码 ----------
const (
	Success       = 0
	ServerError   = 1     // 服务器内部错误
	ParamError    = 2     // 参数错误
	Unauthorized  = 3     // 未认证
	Forbidden     = 4     // 无权限
	TooManyReq    = 5     // 请求过于频繁
)

// ---------- User 服务 10000~10999 ----------
const (
	UserExists       = 10001 // 用户名已存在
	UserPasswordWrong = 10002 // 用户名或密码错误
	UserNotFound     = 10003 // 用户不存在
	UserPasswordWeak = 10004 // 密码格式不符合要求
	UserDisabled     = 10005 // 用户已被禁用
	UploadTokenFail  = 10006 // 获取上传凭证失败
)

// ---------- Relation 服务 11000~11999 ----------
const (
	RelationSelf         = 11001 // 不能关注自己
	RelationAlreadyFollow = 11002 // 已经关注该用户
	RelationNotFollow    = 11003 // 未关注该用户
	RelationTargetNotFound = 11004 // 目标用户不存在
)

// ---------- Feed 服务 12000~12999 ----------
const (
	FeedNotFound     = 12001 // 帖子不存在
	FeedNoPermission = 12002 // 无权限操作该帖子
	FeedEmptyContent = 12003 // 帖子内容为空
	FeedEmptyMedia   = 12004 // 媒体资源为空
	FeedBadType      = 12005 // 不支持的帖子类型
	FeedIPLocateFail = 12006 // IP 定位失败
)

// ---------- Comment 服务 13000~13999 ----------
const (
	CommentNotFound     = 13001 // 评论不存在
	CommentFeedNotFound = 13002 // 帖子不存在
	CommentNoPermission = 13003 // 无权限删除该评论
	CommentEmpty        = 13004 // 评论内容为空
	CommentTooLong      = 13005 // 评论内容超长
	CommentParentNotFound = 13006 // 父评论不存在
)

// ---------- Interaction 服务 14000~14999 ----------
const (
	InteractionFeedNotFound = 14001 // 帖子不存在
	InteractionTooFrequent  = 14002 // 操作过于频繁
)

// messages 错误码 → 默认提示 映射
var messages = map[int]string{
	Success:      "success",
	ServerError:  "服务器内部错误",
	ParamError:   "参数错误",
	Unauthorized: "未认证",
	Forbidden:    "无权限",
	TooManyReq:   "请求过于频繁",

	UserExists:        "用户名已存在",
	UserPasswordWrong: "用户名或密码错误",
	UserNotFound:      "用户不存在",
	UserPasswordWeak:  "密码格式不符合要求",
	UserDisabled:      "用户已被禁用",
	UploadTokenFail:   "获取上传凭证失败",

	RelationSelf:           "不能关注自己",
	RelationAlreadyFollow:  "已经关注该用户",
	RelationNotFollow:      "未关注该用户",
	RelationTargetNotFound: "目标用户不存在",

	FeedNotFound:     "帖子不存在",
	FeedNoPermission: "无权限操作该帖子",
	FeedEmptyContent: "帖子内容为空",
	FeedEmptyMedia:   "媒体资源为空",
	FeedBadType:      "不支持的帖子类型",
	FeedIPLocateFail: "IP 定位失败",

	CommentNotFound:       "评论不存在",
	CommentFeedNotFound:   "帖子不存在",
	CommentNoPermission:   "无权限删除该评论",
	CommentEmpty:          "评论内容为空",
	CommentTooLong:        "评论内容超长",
	CommentParentNotFound: "父评论不存在",

	InteractionFeedNotFound: "帖子不存在",
	InteractionTooFrequent:  "操作过于频繁",
}

// message 返回错误码的默认提示
func message(code int) string {
	if msg, ok := messages[code]; ok {
		return msg
	}
	return "未知错误"
}
