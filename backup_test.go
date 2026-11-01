// tdl-msgproce - 配置备份功能测试
package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// buildFakeTdlRoot 构造模拟的 tdl 目录结构
func buildFakeTdlRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".tdl")
	files := map[string]string{
		"config.toml":                             "tdl config",
		"data.kv":                                 "kv file",
		"session/default/session.json":            "session data",
		"data/kv.db":                              "kv data",
		"extensions/data/msgproce/config.yaml":    "bot config",
		"extensions/data/msgproce/log/latest.log": "log, should be excluded",
		"tdl":                                  "fake binary, should be excluded",
		"extensions/tdl-msgproce/tdl-msgproce": "fake binary, should be excluded",
	}
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCollectBackupEntries(t *testing.T) {
	root := buildFakeTdlRoot(t)
	entries, err := collectBackupEntries(root)
	if err != nil {
		t.Fatalf("collectBackupEntries 失败: %v", err)
	}

	var got []string
	for _, e := range entries {
		got = append(got, e.arcPath)
	}
	sort.Strings(got)

	want := []string{
		".tdl/config.toml",
		".tdl/data.kv",
		".tdl/data/kv.db",
		".tdl/extensions/data/msgproce/config.yaml",
		".tdl/session/default/session.json",
	}
	if len(got) != len(want) {
		t.Fatalf("备份清单数量不对: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("备份清单不对: got %v, want %v", got, want)
		}
	}
}

func TestCreateBackupArchive(t *testing.T) {
	root := buildFakeTdlRoot(t)
	entries, err := collectBackupEntries(root)
	if err != nil {
		t.Fatalf("collectBackupEntries 失败: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := createBackupArchive(entries, dest); err != nil {
		t.Fatalf("createBackupArchive 失败: %v", err)
	}

	// 读回压缩包，校验内容与目录结构
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	contents := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		contents[hdr.Name] = string(data)
	}

	if len(contents) != 5 {
		t.Fatalf("压缩包文件数不对: %d, names=%v", len(contents), keys(contents))
	}
	if contents[".tdl/data.kv"] != "kv file" {
		t.Fatalf("data.kv 内容不对")
	}
	if contents[".tdl/session/default/session.json"] != "session data" {
		t.Fatalf("登录会话文件内容不对")
	}
	if contents[".tdl/data/kv.db"] != "kv data" {
		t.Fatalf("data 内容不对")
	}
	if contents[".tdl/extensions/data/msgproce/config.yaml"] != "bot config" {
		t.Fatalf("扩展配置内容不对")
	}
	// 排除项必须不在包里
	for _, excluded := range []string{".tdl/tdl", ".tdl/extensions/tdl-msgproce/tdl-msgproce", ".tdl/extensions/data/msgproce/log/latest.log"} {
		if _, ok := contents[excluded]; ok {
			t.Fatalf("排除项不应出现在包里: %s", excluded)
		}
	}
}

func keys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
