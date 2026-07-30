package logic

import (
	"strings"
	"testing"
)

// TestBuildFileKey 校验 file_key 格式与唯一性：{env}/{biz}/{uid}/{yyyyMMdd}/{snowflake}.{ext}
func TestBuildFileKey(t *testing.T) {
	key := buildFileKey("prod", "avatar", 10001, "jpg")
	parts := strings.Split(key, "/")
	if len(parts) != 5 {
		t.Fatalf("file_key 段数错误, got=%q", key)
	}
	if parts[0] != "prod" || parts[1] != "avatar" || parts[2] != "10001" {
		t.Fatalf("file_key 前缀错误, got=%q", key)
	}
	if parts[3] != "20060102" && len(parts[3]) != 8 { // 备注：仅校验格式，具体日期由 time.Now 决定
		t.Fatalf("file_key 日期段错误, got=%q", parts[3])
	}
	if !strings.HasSuffix(key, ".jpg") {
		t.Fatalf("file_key 后缀错误, got=%q", key)
	}

	// 重复调用应生成不同 snowflake，保证唯一
	other := buildFileKey("prod", "avatar", 10001, "jpg")
	if key == other {
		t.Fatalf("重复生成的 file_key 不应相同: %q", key)
	}
}

// TestIsValidExt 校验文件后缀白名单
func TestIsValidExt(t *testing.T) {
	cases := []struct {
		ext string
		ok  bool
	}{
		{"jpg", true},
		{"JPG", true},  // 大小写不敏感
		{".png", true}, // 兼容带点
		{"mp4", true},
		{"exe", false}, // 危险后缀拒绝
		{"", false},    // 空拒绝
		{"php", false},
	}
	for _, c := range cases {
		if got := isValidExt(c.ext); got != c.ok {
			t.Errorf("isValidExt(%q)=%v, want %v", c.ext, got, c.ok)
		}
	}
}

// TestBizDirWhitelist 校验业务类型白名单完整性
func TestBizDirWhitelist(t *testing.T) {
	for biz := range bizDir {
		if biz == "" {
			t.Fatal("bizDir 含空 key")
		}
	}
	// 已知业务类型必须存在
	for _, must := range []string{"avatar", "cover", "image", "video"} {
		if _, ok := bizDir[must]; !ok {
			t.Errorf("白名单缺少必需业务类型 %q", must)
		}
	}
}
