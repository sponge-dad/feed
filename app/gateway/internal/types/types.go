// Package types 定义 Gateway HTTP 层对外暴露的请求/响应结构体。
//
// 这些类型与 docs/design/api-spec/user.md 中的 REST 接口对应，
// 与内部 user.proto 的消息相互独立，避免把 proto 细节泄漏给 HTTP 调用方。
package types

// User 用户基础信息（对外展示用）。
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	CityName string `json:"city_name"`
}

// UserDetail 用户详情（包含主页聚合数据）。
type UserDetail struct {
	ID             int64  `json:"id"`
	Username       string `json:"username"`
	Nickname       string `json:"nickname"`
	Avatar         string `json:"avatar"`
	Bio            string `json:"bio"`
	CityName       string `json:"city_name"`
	FollowingCount int64  `json:"following_count"`
	FollowerCount  int64  `json:"follower_count"`
	FeedCount      int64  `json:"feed_count"`
	IsFollowing    bool   `json:"is_following"`
}

// RegisterReq 注册请求。
type RegisterReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

// RegisterResp 注册响应。
type RegisterResp struct {
	User  *User  `json:"user"`
	Token string `json:"token"`
}

// LoginReq 登录请求。
type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResp 登录响应。
type LoginResp struct {
	User  *User  `json:"user"`
	Token string `json:"token"`
}

// GetUserReq 获取用户详情请求（path 参数）。
type GetUserReq struct {
	UserID int64 `path:"userId"`
}

// UpdateUserReq 更新当前用户请求。
type UpdateUserReq struct {
	Nickname string `json:"nickname,optional"`
	Avatar   string `json:"avatar,optional"`
	Bio      string `json:"bio,optional"`
	CityCode string `json:"city_code,optional"`
	CityName string `json:"city_name,optional"`
}

// UpdateUserResp 更新当前用户响应。
type UpdateUserResp struct {
	User *User `json:"user"`
}

// UploadTokenReq 获取上传凭证请求。
type UploadTokenReq struct {
	FileType string `json:"file_type"` // image / video
	FileExt  string `json:"file_ext"`
}

// UploadCredentials 上传临时凭证。
type UploadCredentials struct {
	TmpSecretID  string `json:"tmp_secret_id"`
	TmpSecretKey string `json:"tmp_secret_key"`
	SessionToken string `json:"session_token"`
	ExpiredTime  int64  `json:"expired_time"`
}

// UploadTokenResp 上传凭证响应。
type UploadTokenResp struct {
	UploadURL   string            `json:"upload_url"`
	Credentials UploadCredentials `json:"credentials"`
	FileKey     string            `json:"file_key"`
	FileURL     string            `json:"file_url"`
}
