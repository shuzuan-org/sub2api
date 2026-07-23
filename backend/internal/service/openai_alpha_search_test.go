package service

import "testing"

func TestBuildOpenAIAlphaSearchURL(t *testing.T) {
	cases := []struct{ base, want string }{
		// lisa cc2codex 生产形态: base 以 /v1 结尾
		{"https://lisa.vspeak.top/v1", "https://lisa.vspeak.top/v1/alpha/search"},
		{"https://lisa.vspeak.top/v1/", "https://lisa.vspeak.top/v1/alpha/search"},
		// 配置写了完整 responses 路径
		{"https://x.example/v1/responses", "https://x.example/v1/alpha/search"},
		// 裸域名
		{"https://x.example", "https://x.example/v1/alpha/search"},
		// 已经是 alpha/search
		{"https://x.example/v1/alpha/search", "https://x.example/v1/alpha/search"},
	}
	for _, c := range cases {
		if got := buildOpenAIAlphaSearchURL(c.base); got != c.want {
			t.Errorf("buildOpenAIAlphaSearchURL(%q) = %q, want %q", c.base, got, c.want)
		}
	}
}
