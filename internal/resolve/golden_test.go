package resolve

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/config"
)

var update = flag.Bool("update", false, "重新生成 golden 基线")

const goldenDir = "testdata/variants"

// TestVariantGolden 是「这次改动到底改没改行为」的裁决。
//
// 每个 variant 一份基线，纯重构类改动要求零 diff；功能性改动的 diff 必须在
// commit message 里逐条说清楚为什么变。这比读代码 diff 有效得多——解析逻辑
// 改一行，影响的是每台设备最终装什么包、去哪个源拉包。
//
// 刷新基线：go test ./internal/resolve -update
func TestVariantGolden(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cfg, ps, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if ps.HasError() {
		t.Fatalf("配置树有错：\n%s", ps)
	}

	variants, ps := All(cfg)
	if ps.HasError() {
		t.Fatalf("展开 variant 失败：\n%s", ps)
	}

	if *update {
		if err := os.RemoveAll(goldenDir); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	for _, v := range variants {
		name := goldenName(v.ID)
		seen[name] = true
		t.Run(v.ID, func(t *testing.T) {
			checkGolden(t, filepath.Join(goldenDir, name), marshal(t, v))
		})
	}

	// 删掉一台设备或一条 line 之后，遗留的基线文件会让人以为它还在出货。
	assertNoStaleGolden(t, seen)
}

func goldenName(variantID string) string {
	// "@" 在文件名里合法，但换成 "--" 在 shell 补全和 URL 里都省事。
	return strings.ReplaceAll(variantID, Separator, "--") + ".json"
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取基线 %s 失败：%v\n新增 variant 时用 `go test ./internal/resolve -update` 生成", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("与基线不一致（确认这是有意的行为变更后，用 -update 刷新并在 commit message 里说明）\n"+
			"--- 基线 %s\n%s\n--- 当前\n%s", path, want, got)
	}
}

func assertNoStaleGolden(t *testing.T, seen map[string]bool) {
	t.Helper()
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var stale []string
	for _, e := range entries {
		if !seen[e.Name()] {
			stale = append(stale, e.Name())
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("以下基线没有对应的 variant，应当删掉：%v", stale)
	}
}
