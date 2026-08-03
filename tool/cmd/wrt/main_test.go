package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// runCLI 跑一次命令，返回 stdout / stderr / error。
func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut)
	return out.String(), errOut.String(), err
}

func atRepo(t *testing.T, args ...string) []string {
	t.Helper()
	return append([]string{"-C", repoRoot(t)}, args...)
}

func TestLintAcceptsRepositoryTree(t *testing.T) {
	_, stderr, err := runCLI(t, atRepo(t, "lint")...)
	if err != nil {
		t.Fatalf("本仓库的配置树应当通过 lint: %v\n%s", err, stderr)
	}
}

func TestLintReportsEveryProblemAtOnce(t *testing.T) {
	// 两个文件各有一处错，一次运行要全报出来——修一条跑一遍是上一代
	// lint 最消耗人的地方。
	root := t.TempDir()
	write(t, root, "lines/25.12/line.yaml", "id: \"25.12\"\nopenwrt_version: nope\nartifacts: official\n")
	write(t, root, "devices/vm/device.yaml", `
name: vm
hardware: {target: armsr, subtarget: armv8, profile: generic, arch: WRONG}
lines: ["25.12"]
packages: {}
`)

	stdout, _, err := runCLI(t, "-C", root, "lint")
	if err == nil {
		t.Fatal("有错时 lint 必须返回非零")
	}
	for _, want := range []string{"line.openwrt_version", "device.arch"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("输出里缺少规则 %q：\n%s", want, stdout)
		}
	}
}

func TestLintPassesWithWarningsOnly(t *testing.T) {
	// 只有 warn（这里是「line 没被任何设备引用」）不该阻断。
	root := t.TempDir()
	write(t, root, "lines/25.12/line.yaml", "id: \"25.12\"\nopenwrt_version: 25.12.5\nartifacts: official\n")

	stdout, _, err := runCLI(t, "-C", root, "lint")
	if err != nil {
		t.Fatalf("只有 warn 时不该失败: %v", err)
	}
	if !strings.Contains(stdout, "line.unreferenced") {
		t.Errorf("warn 仍要打印出来：\n%s", stdout)
	}
}

func TestLintCoversFeedMakefiles(t *testing.T) {
	// 配置全绿但自有包的 Makefile 有问题时，lint 同样要拦——feed 里的错
	// （版本号不合 apk 语法、PROVIDES 缺配套）一样是刷机之后才暴露的那类。
	root := t.TempDir()
	write(t, root, "feed/demo/Makefile", "PKG_NAME:=demo\nPKG_VERSION:=1.2.3-alpha.4\n$(eval $(call BuildPackage,demo))\n")

	stdout, _, err := runCLI(t, "-C", root, "lint")
	if err == nil {
		t.Fatal("feed 有错时 lint 必须返回非零")
	}
	if !strings.Contains(stdout, "feed.pkg-version") {
		t.Errorf("输出里缺少 feed 规则：\n%s", stdout)
	}
}

func TestResolveAllEmitsJSONArray(t *testing.T) {
	stdout, _, err := runCLI(t, atRepo(t, "resolve", "--all")...)
	if err != nil {
		t.Fatal(err)
	}

	var variants []map[string]any
	if err := json.Unmarshal([]byte(stdout), &variants); err != nil {
		t.Fatalf("输出不是 JSON 数组: %v\n%s", err, stdout)
	}
	if len(variants) == 0 {
		t.Fatal("一个 variant 都没有")
	}

	// vm-armsr 声明了两条 line，两条都要出现——这正是上一代要复制设备目录
	// 才能表达的东西。
	ids := map[string]bool{}
	for _, v := range variants {
		ids[v["id"].(string)] = true
	}
	for _, want := range []string{"vm-armsr@25.12", "vm-armsr@25.12-selfbuild", "mt3600be@25.12"} {
		if !ids[want] {
			t.Errorf("缺少 variant %q，实际有 %v", want, keys(ids))
		}
	}
}

func TestResolveOneEmitsJSONObject(t *testing.T) {
	stdout, _, err := runCLI(t, atRepo(t, "resolve", "mt3600be@25.12")...)
	if err != nil {
		t.Fatal(err)
	}

	var v map[string]any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("输出不是 JSON 对象: %v\n%s", err, stdout)
	}
	if v["id"] != "mt3600be@25.12" {
		t.Errorf("id = %v", v["id"])
	}
	if pkgs, ok := v["packages"].([]any); !ok || len(pkgs) == 0 {
		t.Errorf("packages 应当非空: %v", v["packages"])
	}
}

func TestResolveRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"不存在的 variant", []string{"resolve", "nope@25.12"}},
		{"设备没声明的 line", []string{"resolve", "mt3600be@25.12-selfbuild"}},
		{"缺少参数", []string{"resolve"}},
		{"同时给了 --all 与具体 variant", []string{"resolve", "--all", "mt3600be@25.12"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := runCLI(t, atRepo(t, tc.args...)...); err == nil {
				t.Fatal("应当返回错误")
			}
		})
	}
}

func TestReposEmitsBothLists(t *testing.T) {
	stdout, _, err := runCLI(t, atRepo(t,
		"repos", "mt3600be@25.12",
		"--repo-base", "https://repo.example.com",
		"--vermagic", "6.12.94-1-abc")...)
	if err != nil {
		t.Fatal(err)
	}

	var r struct {
		Build   []string `json:"build"`
		Runtime []string `json:"runtime"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("输出不是 JSON: %v\n%s", err, stdout)
	}
	if len(r.Build) == 0 || len(r.Runtime) == 0 {
		t.Fatalf("两份列表都不该为空: %+v", r)
	}
}

func TestFlagsWorkOnEitherSideOfThePositional(t *testing.T) {
	// 标准库的 flag 遇到第一个非 flag 参数就停止解析。这条如果回归，
	// 表现是 --vermagic 被静默当成位置参数，而不是报错。
	before := atRepo(t, "repos", "--repo-base", "https://r.example.com", "--vermagic", "vm", "mt3600be@25.12")
	after := atRepo(t, "repos", "mt3600be@25.12", "--repo-base", "https://r.example.com", "--vermagic", "vm")

	outBefore, _, err := runCLI(t, before...)
	if err != nil {
		t.Fatalf("flag 在前应当能用: %v", err)
	}
	outAfter, _, err := runCLI(t, after...)
	if err != nil {
		t.Fatalf("flag 在后应当能用: %v", err)
	}
	if outBefore != outAfter {
		t.Error("两种写法应当得到相同结果")
	}
}

func TestReposRequiresVermagic(t *testing.T) {
	// 缺 vermagic 时宁可失败也不能产出一份「看着正常、实际装不上任何
	// 驱动」的软件源列表。
	_, _, err := runCLI(t, atRepo(t, "repos", "mt3600be@25.12", "--repo-base", "https://r.example.com")...)
	if err == nil {
		t.Fatal("缺 vermagic 应当报错")
	}
}

func TestFilesAssemblesOverlay(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "overlay")
	if _, _, err := runCLI(t, atRepo(t, "files", "vm-armsr@25.12", dest)...); err != nil {
		t.Fatal(err)
	}
	// vm-armsr 没有 files-gen 脚本；通用层的文件必须在。
	if _, err := os.Stat(filepath.Join(dest, "etc/uci-defaults/00-base")); err != nil {
		t.Fatalf("通用层 overlay 没有落地: %v", err)
	}
}

func TestFilesRejectsWrongArgCount(t *testing.T) {
	for _, args := range [][]string{{"files"}, {"files", "vm-armsr@25.12"}} {
		if _, _, err := runCLI(t, atRepo(t, args...)...); err == nil {
			t.Errorf("%v 应当报错", args)
		}
	}
}

func TestPlanWithoutRemoteListsEverything(t *testing.T) {
	// 没配 repo base 就无从判定远端状态，保守地把全部条目当作需要构建。
	stdout, _, err := runCLI(t, atRepo(t, "plan")...)
	if err != nil {
		t.Fatal(err)
	}

	var p struct {
		Toolchain []struct {
			Line       string `json:"line"`
			NeedsBuild bool   `json:"needs_build"`
		} `json:"toolchain"`
		Packages []struct{} `json:"packages"`
		Firmware []struct {
			Variant    string `json:"variant"`
			NeedsBuild bool   `json:"needs_build"`
		} `json:"firmware"`
	}
	if err := json.Unmarshal([]byte(stdout), &p); err != nil {
		t.Fatalf("输出不是 JSON: %v\n%s", err, stdout)
	}

	// 只有 25.12-selfbuild 是自建线，工具链矩阵就该只有它。
	if len(p.Toolchain) != 1 || p.Toolchain[0].Line != "25.12-selfbuild" {
		t.Errorf("工具链矩阵 = %+v", p.Toolchain)
	}
	if len(p.Firmware) != 3 {
		t.Errorf("固件矩阵应当覆盖全部 variant，得到 %d 条", len(p.Firmware))
	}
	for _, e := range p.Firmware {
		if !e.NeedsBuild {
			t.Errorf("%s 在无法判定远端状态时应当被列为需要构建", e.Variant)
		}
	}
}

func TestPlanRejectsPositionalArgs(t *testing.T) {
	if _, _, err := runCLI(t, atRepo(t, "plan", "mt3600be@25.12")...); err == nil {
		t.Fatal("plan 不接受位置参数")
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	stdout, _, err := runCLI(t, "help")
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range commandNames() {
		if !strings.Contains(stdout, cmd) {
			t.Errorf("help 里缺少子命令 %q：\n%s", cmd, stdout)
		}
	}
}

func TestNoArgsShowsHelpAndFails(t *testing.T) {
	if _, _, err := runCLI(t); err == nil {
		t.Fatal("不给子命令应当返回非零，否则在脚本里会被当成成功")
	}
}

func TestUnknownCommandFails(t *testing.T) {
	if _, _, err := runCLI(t, "frobnicate"); err == nil {
		t.Fatal("未知子命令应当返回错误")
	}
}

func TestRootDefaultsToRepositoryContainingCwd(t *testing.T) {
	// 不给 -C 时应当从当前目录向上找到仓库根（cmd/wrt 也能跑）。
	if _, _, err := runCLI(t, "lint"); err != nil {
		t.Fatalf("从 cmd/wrt 目录跑 lint 应当能找到仓库根: %v", err)
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
