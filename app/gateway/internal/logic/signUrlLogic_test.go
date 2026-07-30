package logic

import "testing"

// TestOwnsFileKey 校验 file_key 归属判断：结构、env/biz/uid 三段匹配、目录穿越防护。
func TestOwnsFileKey(t *testing.T) {
	const env = "prod"
	const uid = int64(10001)

	cases := []struct {
		name string
		key  string
		ok   bool
	}{
		{"本人头像", "prod/avatar/10001/20260730/1893726490032648193.jpg", true},
		{"本人视频", "prod/video/10001/20260730/1893726490032648193.mp4", true},
		{"他人 uid", "prod/avatar/99999/20260730/1893726490032648193.jpg", false},
		{"错误 env", "dev/avatar/10001/20260730/1893726490032648193.jpg", false},
		{"非法 biz", "prod/evil/10001/20260730/1893726490032648193.jpg", false},
		{"目录穿越", "prod/avatar/10001/../10002/x.jpg", false},
		{"段数不足", "prod/avatar/10001/x.jpg", false},
		{"绝对路径穿越", "../../etc/passwd", false},
	}
	for _, c := range cases {
		if got := ownsFileKey(c.key, uid, env); got != c.ok {
			t.Errorf("%s: ownsFileKey(%q)=%v, want %v", c.name, c.key, got, c.ok)
		}
	}
}
