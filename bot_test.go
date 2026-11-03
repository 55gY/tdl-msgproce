// tdl-msgproce - Bot 相关单元测试
package main

import (
	"errors"
	"testing"
)

// TestShortNetErr 网络错误 -> 简短中文原因映射
func TestShortNetErr(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`Post "http://x/api": dial tcp: i/o timeout`, "连接超时"},
		{`Post "http://x/api": context deadline exceeded`, "连接超时"},
		{`Post "http://x/api": dial tcp: connection refused`, "连接被拒绝"},
		{`Post "http://x/api": no such host`, "域名解析失败"},
		{`Post "https://x/api": tls: handshake failure`, "TLS 握手失败"},
		{`Post "https://x/api": x509: certificate signed by unknown authority`, "TLS 握手失败"},
		{`Post "http://x/api": something unexpected`, "网络异常"},
	}
	for _, c := range cases {
		if got := shortNetErr(errors.New(c.in)); got != c.want {
			t.Errorf("shortNetErr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
