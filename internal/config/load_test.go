package config

import (
	"github.com/K-Lrize/openwrt-build/internal/diag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeTree 在临时目录里铺一棵配置树。键是相对路径，值是文件内容；
// 值为空串时只建目录（用来测「空的 overlay/ 目录不算源码改动」）。
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if content == "" {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const (
	yamlLineOfficial = `
id: "25.12"
openwrt_version: 25.12.5
artifacts: official
`
	yamlLineSelf = `
id: 25.12-selfbuild
openwrt_version: 25.12.5
artifacts: self
source:
  repo: https://github.com/openwrt/openwrt
  ref: v25.12.5
  commit: f0a60eee2fe051741c643ea6118718aae1ef17fb
`
	yamlSetCommon = `
name: common
add: [curl, jq]
`
	yamlDeviceVM = `
name: vm-armsr
hardware:
  target: armsr
  subtarget: armv8
  profile: generic
  arch: aarch64_generic
lines: ["25.12"]
packages:
  include: [common]
  add: [luci]
`
)

func goodTree() map[string]string {
	return map[string]string{
		"lines/25.12/line.yaml":        yamlLineOfficial,
		"sets/common.yaml":             yamlSetCommon,
		"devices/vm-armsr/device.yaml": yamlDeviceVM,
	}
}

func loadTree(t *testing.T, files map[string]string) (*Config, diag.Problems) {
	t.Helper()
	cfg, ps, err := Load(writeTree(t, files))
	if err != nil {
		t.Fatalf("Load 返回 I/O 错误: %v", err)
	}
	return cfg, ps
}

func TestLoadGoodTree(t *testing.T) {
	cfg, ps := loadTree(t, goodTree())
	if ps.HasError() {
		t.Fatalf("干净的配置树不该有错：\n%s", ps)
	}
	if len(cfg.Lines) != 1 || len(cfg.Devices) != 1 || len(cfg.Sets) != 1 {
		t.Fatalf("载入数量不对：lines=%d devices=%d sets=%d", len(cfg.Lines), len(cfg.Devices), len(cfg.Sets))
	}
	if got := cfg.Devices["vm-armsr"].Hardware.Arch; got != "aarch64_generic" {
		t.Errorf("arch = %q", got)
	}
}

func TestLoadDerivesRequiresBuildFromDisk(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]string
		want  bool
	}{
		{"三个目录都不存在", nil, false},
		{"空的 overlay 目录不算源码改动", map[string]string{"lines/25.12-selfbuild/overlay": ""}, false},
		{
			"overlay 下有文件",
			map[string]string{"lines/25.12-selfbuild/overlay/target/linux/x/patches-6.12/900.patch": "diff\n"},
			true,
		},
		{"patches 下有文件", map[string]string{"lines/25.12-selfbuild/patches/0001.patch": "diff\n"}, true},
		{"config 下有文件", map[string]string{"lines/25.12-selfbuild/config/common.config": "CONFIG_X=y\n"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"lines/25.12-selfbuild/line.yaml": yamlLineSelf}
			for k, v := range tc.extra {
				files[k] = v
			}
			cfg, _ := loadTree(t, files)
			if got := cfg.Lines["25.12-selfbuild"].RequiresBuild; got != tc.want {
				t.Fatalf("RequiresBuild = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadRejectsSourceChangesOnOfficialLine(t *testing.T) {
	// 有 overlay/patches/config 却声明 artifacts: official —— 官方不可能有
	// 你的产物，这份配置永远编不出你想要的东西。
	_, ps := loadTree(t, map[string]string{
		"lines/25.12/line.yaml":          yamlLineOfficial,
		"lines/25.12/patches/0001.patch": "diff\n",
	})
	if !hasRule(ps, "line.requires-build") {
		t.Fatalf("want line.requires-build：\n%s", ps)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	// 上一代的 device.yaml 有 channel: 字段。写错字段名或留着已删除的旧字段，
	// 都必须当场报错而不是静默忽略——静默忽略意味着配置写了等于没写。
	files := goodTree()
	files["devices/vm-armsr/device.yaml"] = yamlDeviceVM + "\nchannel: \"25.12\"\n"
	_, ps := loadTree(t, files)
	if !hasRule(ps, "yaml") {
		t.Fatalf("want yaml 规则：\n%s", ps)
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	files := goodTree()
	files["sets/common.yaml"] = "name: common\nadd: [curl\n"
	_, ps := loadTree(t, files)
	if !hasRule(ps, "yaml") {
		t.Fatalf("want yaml 规则：\n%s", ps)
	}
}

func TestLoadIdentityMustMatchPath(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		rule  string
	}{
		{
			"line.id 与目录名不符",
			map[string]string{"lines/25.12-mtk/line.yaml": yamlLineOfficial},
			"line.id-path",
		},
		{
			"device.name 与目录名不符",
			map[string]string{"devices/vm/device.yaml": yamlDeviceVM},
			"device.name-path",
		},
		{
			"set.name 与文件名不符",
			map[string]string{"sets/base.yaml": yamlSetCommon},
			"set.name-path",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ps := loadTree(t, tc.files)
			if !hasRule(ps, tc.rule) {
				t.Fatalf("want %s：\n%s", tc.rule, ps)
			}
		})
	}
}

func TestLoadDanglingReferences(t *testing.T) {
	t.Run("引用了不存在的 line", func(t *testing.T) {
		files := goodTree()
		delete(files, "lines/25.12/line.yaml")
		_, ps := loadTree(t, files)
		if !hasRule(ps, "device.lines-ref") {
			t.Fatalf("want device.lines-ref：\n%s", ps)
		}
	})

	t.Run("include 了不存在的 set", func(t *testing.T) {
		files := goodTree()
		delete(files, "sets/common.yaml")
		_, ps := loadTree(t, files)
		if !hasRule(ps, "device.include-ref") {
			t.Fatalf("want device.include-ref：\n%s", ps)
		}
	})
}

func TestLoadWarnsOnUnreferencedConfig(t *testing.T) {
	// 没有任何设备引用的 line / set 是死配置：它会照常进 lint、照常被人读到，
	// 但永远不生效。只 warn 不 error——刚建好还没接设备是正常中间状态。
	files := goodTree()
	files["sets/unused.yaml"] = "name: unused\nadd: [htop]\n"
	files["lines/25.12-selfbuild/line.yaml"] = yamlLineSelf
	_, ps := loadTree(t, files)

	if ps.HasError() {
		t.Fatalf("死配置只该 warn：\n%s", ps)
	}
	want := []string{"line.unreferenced", "set.unreferenced"}
	if got := ps.Rules(); !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v\n%s", want, got, ps)
	}
}

func TestLoadStampsSourcePath(t *testing.T) {
	files := goodTree()
	files["lines/25.12/line.yaml"] = "id: \"25.12\"\nopenwrt_version: bogus\nartifacts: official\n"
	_, ps := loadTree(t, files)
	for _, p := range ps {
		if p.Source == "" {
			t.Fatalf("每条问题都要能指回文件：%+v", p)
		}
	}
	if ps[0].Source != "lines/25.12/line.yaml" {
		t.Fatalf("Source = %q", ps[0].Source)
	}
}

func TestLoadMissingRootIsAnError(t *testing.T) {
	if _, _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("根目录不存在时应返回 error 而不是空配置")
	}
}

func TestLoadEmptyTreeIsNotAnError(t *testing.T) {
	// 空仓库（刚 init，还没加设备）是合法中间状态，不该 fail。
	cfg, ps, err := Load(writeTree(t, map[string]string{"lines": "", "devices": "", "sets": ""}))
	if err != nil {
		t.Fatalf("空树不该报 I/O 错误: %v", err)
	}
	if ps.HasError() {
		t.Fatalf("空树不该有错：\n%s", ps)
	}
	if len(cfg.Lines)+len(cfg.Devices)+len(cfg.Sets) != 0 {
		t.Fatal("空树应载入零条配置")
	}
}

func hasRule(ps diag.Problems, rule string) bool {
	for _, p := range ps {
		if p.Rule == rule {
			return true
		}
	}
	return false
}
