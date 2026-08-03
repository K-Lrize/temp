package feed

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/diag"
)

// minimal 是一份能通过全部规则的 Makefile 骨架，各用例只改其中一处。
const minimal = `include $(TOPDIR)/rules.mk

PKG_NAME:=demo
PKG_VERSION:=1.2.3
PKG_RELEASE:=1

include $(INCLUDE_DIR)/package.mk

define Package/demo
  TITLE:=demo
endef

$(eval $(call BuildPackage,demo))
`

// overriding 是「顶替官方同名包」场景的骨架：PKG_RELEASE 已经足够高，
// 这样 PROVIDES 各用例只会触发被测的那一条规则。
var overriding = replaceVar(minimal, "PKG_RELEASE", "100")

func loadOne(t *testing.T, dirName, makefile string) (Package, diag.Problems) {
	t.Helper()
	root := t.TempDir()
	if makefile != "" {
		dir := filepath.Join(root, "feed", dirName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkgs, ps, err := Load(root)
	if err != nil {
		t.Fatalf("Load 报 I/O 错误: %v", err)
	}
	if len(pkgs) == 0 {
		return Package{}, ps
	}
	return pkgs[0], ps
}

func TestLoadParsesVariables(t *testing.T) {
	pkg, ps := loadOne(t, "demo", minimal)
	if ps.HasError() {
		t.Fatalf("骨架不该有错：\n%s", ps)
	}
	if pkg.Name != "demo" {
		t.Errorf("Name = %q", pkg.Name)
	}
	want := map[string]string{"PKG_NAME": "demo", "PKG_VERSION": "1.2.3", "PKG_RELEASE": "1"}
	for k, v := range want {
		if pkg.Vars[k] != v {
			t.Errorf("%s = %q, want %q", k, pkg.Vars[k], v)
		}
	}
	if !pkg.HasBuildPackage {
		t.Error("应当认出 $(eval $(call BuildPackage,...))")
	}
}

func TestValidationRules(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		makefile string
		rules    []string
	}{
		{
			name:     "骨架无问题",
			dir:      "demo",
			makefile: minimal,
		},
		{
			// 目录名与 PKG_NAME 不一致时，CI 按目录名下的 make package/<dir>/compile
			// 会找不到目标，而错误信息与真实原因毫不相干。
			name:     "目录名与 PKG_NAME 不符",
			dir:      "other",
			makefile: minimal,
			rules:    []string{"feed.pkg-name"},
		},
		{
			name:     "既没有 BuildPackage 也没有 KernelPackage",
			dir:      "demo",
			makefile: "PKG_NAME:=demo\nPKG_VERSION:=1.0\n",
			rules:    []string{"feed.build-package"},
		},
		{
			name:     "KernelPackage 也算数",
			dir:      "demo",
			makefile: "PKG_NAME:=demo\nPKG_VERSION:=1.0\n$(eval $(call KernelPackage,demo))\n",
		},

		// ── apk 版本号规范（Alpine 从 Gentoo 继承的 ebuild 语法）──
		{
			name:     "上游的 -alpha.4 写法不合规",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_VERSION", "1.2.3-alpha.4"),
			rules:    []string{"feed.pkg-version"},
		},
		{
			name:     "后缀后面带点号不合规",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_VERSION", "1.14.0_alpha.33"),
			rules:    []string{"feed.pkg-version"},
		},
		{
			name:     "不在白名单里的后缀不合规",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_VERSION", "1.0.0_snapshot"),
			rules:    []string{"feed.pkg-version"},
		},
		{
			name:     "合规的预发布版",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_VERSION", "1.14.0_alpha33"),
		},
		{
			name:     "合规：单个小写字母尾巴",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_VERSION", "1.14.0a"),
		},
		{
			name:     "合规：带 epoch 与本地修订号",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_VERSION", "1:2.0-r3"),
		},
		{
			// PKG_VERSION 常被写成引用另一个变量，展开后的值这里看不到，
			// 只能跳过而不是误报。
			name:     "值是 make 变量引用时跳过校验",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_VERSION", "$(PKG_SOURCE_DATE)"),
		},

		// ── 源码完整性 ──
		{
			name:     "PKG_HASH:=skip 关掉了完整性校验",
			dir:      "demo",
			makefile: minimal + "PKG_SOURCE_URL:=https://example.invalid/x.tar.gz\nPKG_HASH:=skip\n",
			rules:    []string{"feed.pkg-hash"},
		},
		{
			name:     "有下载地址却没有校验和",
			dir:      "demo",
			makefile: minimal + "PKG_SOURCE_URL:=https://example.invalid/x.tar.gz\n",
			rules:    []string{"feed.pkg-hash-missing"},
		},
		{
			name:     "本地源码不需要校验和",
			dir:      "demo",
			makefile: minimal,
		},

		// ── PROVIDES 三件套：顶替官方同名包时的必要配套 ──
		{
			name:     "顶替官方包却没有 CONFLICTS",
			dir:      "demo",
			makefile: overriding + "PROVIDES:=sing-box\nDEFAULT_VARIANT:=1\nPROVIDER_PRIORITY:=200\n",
			rules:    []string{"feed.provides.conflicts"},
		},
		{
			name:     "顶替官方包但缺 DEFAULT_VARIANT 与 PROVIDER_PRIORITY",
			dir:      "demo",
			makefile: overriding + "PROVIDES:=sing-box\nCONFLICTS:=sing-box\n",
			rules:    []string{"feed.provides.default-variant", "feed.provides.priority"},
		},
		{
			name:     "PROVIDER_PRIORITY 低于官方默认供应商",
			dir:      "demo",
			makefile: overriding + "PROVIDES:=sing-box\nCONFLICTS:=sing-box\nDEFAULT_VARIANT:=1\nPROVIDER_PRIORITY:=50\n",
			rules:    []string{"feed.provides.priority"},
		},
		{
			// @ 开头的是构建配置符号而不是包名，不存在「顶替官方包」这回事。
			name:     "PROVIDES 指向构建配置符号时不适用三件套",
			dir:      "demo",
			makefile: overriding + "PROVIDES:=@FOO\n",
		},
		{
			name: "顶替官方包时 PKG_RELEASE 太低会被官方包盖过",
			dir:  "demo",
			makefile: replaceVar(minimal, "PKG_RELEASE", "1") +
				"PROVIDES:=sing-box\nCONFLICTS:=sing-box\nDEFAULT_VARIANT:=1\nPROVIDER_PRIORITY:=200\n",
			rules: []string{"feed.pkg-release"},
		},
		{
			// 不顶替任何东西的自建包，PKG_RELEASE 从 1 开始完全正常，
			// 在这里报警只会变成人人无视的噪音。
			name:     "纯自建包的 PKG_RELEASE 不该被挑剔",
			dir:      "demo",
			makefile: replaceVar(minimal, "PKG_RELEASE", "1"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ps := loadOne(t, tc.dir, tc.makefile)
			if got := ps.Rules(); !reflect.DeepEqual(got, tc.rules) {
				t.Fatalf("规则不符\n want: %v\n got:  %v\n%s", tc.rules, got, ps)
			}
		})
	}
}

func TestPackageDirWithoutMakefileIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "feed", "orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, ps, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(ps, "feed.makefile") {
		t.Fatalf("want feed.makefile：\n%s", ps)
	}
}

func TestMissingFeedDirIsNotAnError(t *testing.T) {
	// 还没有任何自有包是合法状态。
	pkgs, ps, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("feed/ 不存在不该报错: %v", err)
	}
	if len(pkgs) != 0 || len(ps) != 0 {
		t.Fatalf("want empty, got %d packages / %d problems", len(pkgs), len(ps))
	}
}

func TestProblemsPointAtTheMakefile(t *testing.T) {
	_, ps := loadOne(t, "demo", "PKG_NAME:=demo\n")
	if len(ps) == 0 {
		t.Fatal("应当有问题")
	}
	if ps[0].Source != "feed/demo/Makefile" {
		t.Fatalf("Source = %q", ps[0].Source)
	}
}

func TestRepositoryFeedIsValid(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, ps, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if ps.HasError() {
		t.Fatalf("本仓库的 feed 有错：\n%s", ps)
	}
	if len(ps) > 0 {
		t.Logf("非阻断的提示：\n%s", ps)
	}
	if len(pkgs) == 0 {
		t.Fatal("一个自有包都没载入")
	}
}

// replaceVar 改掉骨架里某个变量的值。
func replaceVar(makefile, name, value string) string {
	re := regexp.MustCompile(`(?m)^` + name + `:=.*$`)
	return re.ReplaceAllString(makefile, name+":="+value)
}

func hasRule(ps diag.Problems, rule string) bool {
	for _, p := range ps {
		if p.Rule == rule {
			return true
		}
	}
	return false
}
