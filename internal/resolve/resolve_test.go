package resolve

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/config"
)

func writeTree(t *testing.T, files map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, ps, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if ps.HasError() {
		t.Fatalf("fixture 配置有错：\n%s", ps)
	}
	return cfg
}

// twoLineTree：一台设备两条 line，正是上一代要靠复制设备目录才能表达的场景。
func twoLineTree(t *testing.T) *config.Config {
	return writeTree(t, map[string]string{
		"lines/25.12/line.yaml": `
id: "25.12"
upstream: 25.12.5
artifacts: official
`,
		"lines/25.12-mtk/line.yaml": `
id: 25.12-mtk
upstream: 25.12.5
artifacts: self
source:
  repo: https://github.com/openwrt/openwrt
  ref: v25.12.5
  commit: f0a60eee2fe051741c643ea6118718aae1ef17fb
`,
		"sets/common.yaml": `
name: common
add: [curl, jq]
`,
		"sets/router.yaml": `
name: router
add: [dnsmasq-full]
remove: [dnsmasq]
`,
		"devices/vm-armsr/device.yaml": `
name: vm-armsr
hardware: {target: armsr, subtarget: armv8, profile: generic, arch: aarch64_generic}
lines: ["25.12"]
packages:
  include: [common]
  add: [luci]
`,
		"devices/mt3600be/device.yaml": `
name: mt3600be
hardware: {target: mediatek, subtarget: filogic, profile: glinet_gl-mt3600be, arch: aarch64_cortex-a53}
metadata: {soc: MT7987a}
lines: ["25.12", 25.12-mtk]
packages:
  include: [common, router]
  add: [sing-box]
image: {rootfs_partsize: 256}
`,
	})
}

func TestAllExpandsDeviceTimesLine(t *testing.T) {
	variants, ps := All(twoLineTree(t))
	if ps.HasError() {
		t.Fatalf("不该有错：\n%s", ps)
	}

	// 顺序：设备名字典序，同一设备内按 device.yaml 里 lines 的声明顺序
	// （第一条是主线）。确定的顺序是 golden 与 CI 矩阵能稳定的前提。
	want := []string{"mt3600be@25.12", "mt3600be@25.12-mtk", "vm-armsr@25.12"}
	var got []string
	for _, v := range variants {
		got = append(got, v.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestVariantCarriesLineFacts(t *testing.T) {
	variants, _ := All(twoLineTree(t))
	byID := map[string]Variant{}
	for _, v := range variants {
		byID[v.ID] = v
	}

	official := byID["mt3600be@25.12"]
	if official.Line.Artifacts != config.ArtifactsOfficial {
		t.Errorf("artifacts = %q", official.Line.Artifacts)
	}
	if official.Line.Source != nil {
		t.Error("官方线不该带 source")
	}

	self := byID["mt3600be@25.12-mtk"]
	if self.Line.Artifacts != config.ArtifactsSelf {
		t.Errorf("artifacts = %q", self.Line.Artifacts)
	}
	if self.Line.Source == nil || self.Line.Source.Commit == "" {
		t.Fatal("自建线必须带上 source.commit——构建时只信它")
	}
	if self.Line.Upstream != "25.12.5" {
		t.Errorf("upstream = %q", self.Line.Upstream)
	}
}

func TestVariantPackagesMergeSetsThenDevice(t *testing.T) {
	variants, _ := All(twoLineTree(t))
	for _, v := range variants {
		if v.Device != "mt3600be" {
			continue
		}
		want := []string{"curl", "jq", "dnsmasq-full", "sing-box", "-dnsmasq"}
		if !reflect.DeepEqual(v.Packages, want) {
			t.Fatalf("want %v\n got %v", want, v.Packages)
		}
	}
}

func TestSameDeviceDifferentLinesShareThePackageList(t *testing.T) {
	// 包列表只由 device + sets 决定，与 line 无关。两条线的固件差别在
	// 底座与内核，不在装了什么用户态包。
	variants, _ := All(twoLineTree(t))
	var lists [][]string
	for _, v := range variants {
		if v.Device == "mt3600be" {
			lists = append(lists, v.Packages)
		}
	}
	if len(lists) != 2 {
		t.Fatalf("want 2 variants, got %d", len(lists))
	}
	if !reflect.DeepEqual(lists[0], lists[1]) {
		t.Fatalf("同一设备不同 line 的包列表应相同\n%v\n%v", lists[0], lists[1])
	}
}

func TestVariantCarriesHardwareAndImage(t *testing.T) {
	v := mustOne(t, twoLineTree(t), "mt3600be@25.12")
	if v.Hardware.TargetKey() != "mediatek/filogic" {
		t.Errorf("target key = %q", v.Hardware.TargetKey())
	}
	if v.Image.RootfsPartsize != 256 {
		t.Errorf("rootfs_partsize = %d", v.Image.RootfsPartsize)
	}
	if v.Metadata["soc"] != "MT7987a" {
		t.Errorf("metadata.soc = %q", v.Metadata["soc"])
	}
}

func TestOne(t *testing.T) {
	cfg := twoLineTree(t)

	if _, err := One(cfg, "mt3600be@25.12"); err != nil {
		t.Fatalf("存在的 variant 不该报错: %v", err)
	}
	for _, id := range []string{"nope@25.12", "mt3600be@nope", "mt3600be"} {
		if _, err := One(cfg, id); err == nil {
			t.Errorf("%q 应该报错", id)
		}
	}
	// 设备存在、line 也存在，但这台设备没声明这条线——不是合法 variant。
	if _, err := One(cfg, "vm-armsr@25.12-mtk"); err == nil {
		t.Error("设备未声明的 line 不该被解析出来")
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		in     string
		device string
		line   string
		bad    bool
	}{
		{in: "mt3600be@25.12", device: "mt3600be", line: "25.12"},
		{in: "vm@25.12-selfbuild", device: "vm", line: "25.12-selfbuild"},
		{in: "mt3600be", bad: true},
		{in: "@25.12", bad: true},
		{in: "mt3600be@", bad: true},
		{in: "a@b@c", bad: true},
	}
	for _, tc := range tests {
		device, line, err := ParseID(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("%q 应该报错", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if device != tc.device || line != tc.line {
			t.Errorf("%q -> (%q, %q)", tc.in, device, line)
		}
	}
}

func TestCrossLayerPackageConflictSurfacesPerVariant(t *testing.T) {
	// 包集装上、设备卸掉——合并层面的冲突要在展开 variant 时报出来，
	// 并且能指回是哪台设备。
	cfg := writeTree(t, map[string]string{
		"lines/25.12/line.yaml": `
id: "25.12"
upstream: 25.12.5
artifacts: official
`,
		"sets/router.yaml": `
name: router
add: [dnsmasq-full]
`,
		"devices/vm-armsr/device.yaml": `
name: vm-armsr
hardware: {target: armsr, subtarget: armv8, profile: generic, arch: aarch64_generic}
lines: ["25.12"]
packages:
  include: [router]
  remove: [dnsmasq-full]
`,
	})

	_, ps := All(cfg)
	if !ps.HasError() {
		t.Fatal("跨层冲突应当报错")
	}
	if ps[0].Source != "devices/vm-armsr/device.yaml" {
		t.Errorf("冲突要指回设备文件，得到 %q", ps[0].Source)
	}
}

func mustOne(t *testing.T, cfg *config.Config, id string) Variant {
	t.Helper()
	v, err := One(cfg, id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
