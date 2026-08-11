// Package errorx 统一业务错误码定义。
//
// 业务码分段（见 ../design/api-spec/README.md）：
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
	Success      = 0
	ServerError  = 1 // 服务器内部错误
	ParamError   = 2 // 参数错误
	Unauthorized = 3 // 未认证
	Forbidden    = 4 // 无权限
	TooManyReq   = 5 // 请求过于频繁
)

// ---------- User 服务 10000~10999 ----------
const (
	UserExists        = 10001 // 用户名已存在
	UserPasswordWrong = 10002 // 用户名或密码错误
	UserNotFound      = 10003 // 用户不存在
	UserPasswordWeak  = 10004 // 密码格式不符合要求
	UserDisabled      = 10005 // 用户已被禁用
	UploadTokenFail   = 10006 // 获取上传凭证失败
)

// ---------- Relation 服务 11000~11999 ----------
const (
	RelationSelf           = 11001 // 不能关注自己
	RelationAlreadyFollow  = 11002 // 已经关注该用户
	RelationNotFollow      = 11003 // 未关注该用户
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
	CommentNotFound       = 13001 // 评论不存在
	CommentFeedNotFound   = 13002 // 帖子不存在
	CommentNoPermission   = 13003 // 无权限删除该评论
	CommentEmpty          = 13004 // 评论内容为空
	CommentTooLong        = 13005 // 评论内容超长
	CommentParentNotFound = 13006 // 父评论不存在
)

// ---------- Interaction 服务 14000~14999 ----------
const (
	InteractionFeedNotFound = 14001 // 帖子不存在
	InteractionTooFrequent  = 14002 // 操作过于频繁

	BehaviorInvalid      = 14003 // 埋点非法（feed 不存在 / status 异常 / 字段非法）
	BehaviorFeedNotFound = 14004 // 帖子不存在（埋点上报时）

	InteractionMetricsForbidden = 14005 // 无权查看该作品指标（非作者本人）
	InteractionPeerInsufficient = 14006 // 同类作品样本不足，暂不做对比
)

// ---------- Content 服务 15000~15999 ----------
const (
	ContentProfileNotFound   = 15001 // 内容画像不存在
	ContentAnalysisRunning   = 15002 // 内容分析进行中
	ContentAnalysisFailed    = 15003 // 内容分析失败
	ContentTypeUnsupported   = 15004 // 该内容类型不支持分析
	ContentMediaInvalid      = 15005 // 媒体地址非法或不可访问
	ContentSearchEmptyQuery  = 15006 // 检索条件为空
	ContentSearchUnavailable = 15007 // 检索服务不可用
	ContentProfileForbidden  = 15008 // 无权查看该内容的完整画像
)

// ---------- Agent 服务 16000~16999 ----------
const (
	AgentSessionNotFound   = 16001 // 会话不存在
	AgentSessionForbidden  = 16002 // 无权访问该会话
	AgentRunNotFound       = 16003 // 执行任务不存在
	AgentRunNotCancelable  = 16004 // 任务已结束，无法取消
	AgentIntentUnsupported = 16005 // 暂不支持该类问题
	AgentToolParamInvalid  = 16006 // 工具参数非法
	AgentToolCallFailed    = 16007 // 数据获取失败
	AgentLimitExceeded     = 16008 // 本次执行超过调用上限
	AgentModelError        = 16009 // 模型服务不可用
	AgentDataForbidden     = 16010 // 只能查询本人数据
	AgentInputTooLong      = 16011 // 输入内容过长
	AgentRunConflict       = 16012 // 当前会话已有任务在执行
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

	BehaviorInvalid:      "埋点非法",
	BehaviorFeedNotFound: "帖子不存在",

	InteractionMetricsForbidden: "无权查看该作品指标",
	InteractionPeerInsufficient: "同类作品样本不足，暂不做对比",

	ContentProfileNotFound:   "内容画像不存在",
	ContentAnalysisRunning:   "内容分析进行中",
	ContentAnalysisFailed:    "内容分析失败",
	ContentTypeUnsupported:   "该内容类型不支持分析",
	ContentMediaInvalid:      "媒体地址非法或不可访问",
	ContentSearchEmptyQuery:  "检索条件为空",
	ContentSearchUnavailable: "检索服务不可用",
	ContentProfileForbidden:  "无权查看该内容的完整画像",

	AgentSessionNotFound:   "会话不存在",
	AgentSessionForbidden:  "无权访问该会话",
	AgentRunNotFound:       "执行任务不存在",
	AgentRunNotCancelable:  "任务已结束，无法取消",
	AgentIntentUnsupported: "暂不支持该类问题",
	AgentToolParamInvalid:  "工具参数非法",
	AgentToolCallFailed:    "数据获取失败",
	AgentLimitExceeded:     "本次执行超过调用上限",
	AgentModelError:        "模型服务不可用",
	AgentDataForbidden:     "只能查询本人数据",
	AgentInputTooLong:      "输入内容过长",
	AgentRunConflict:       "当前会话已有任务在执行",
}

// message 返回错误码的默认提示
func message(code int) string {
	if msg, ok := messages[code]; ok {
		return msg
	}
	return "未知错误"
}
