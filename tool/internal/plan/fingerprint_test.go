package plan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

// tree 铺一棵最小配置树：两条 line、两个包集、两台设备。
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"lines/25.12/line.yaml": "id: \"25.12\"\nopenwrt_version: 25.12.5\nartifacts: official\n",
		"lines/25.12-mtk/line.yaml": `
id: 25.12-mtk
openwrt_version: 25.12.5
artifacts: self
source:
  repo: https://github.com/openwrt/openwrt
  ref: v25.12.5
  commit: f0a60eee2fe051741c643ea6118718aae1ef17fb
`,
		"lines/25.12-mtk/overlay/target/linux/x/patches-6.12/900.patch": "diff --git a b\n",
		"sets/common.yaml":   "name: common\nadd: [curl]\n",
		"sets/unused.yaml":   "name: unused\nadd: [htop]\n",
		"feed/demo/Makefile": "PKG_NAME:=demo\n$(eval $(call BuildPackage,demo))\n",
		"files/etc/banner":   "hi\n",
		"devices/router/device.yaml": `
name: router
hardware: {target: mediatek, subtarget: filogic, profile: p, arch: aarch64_cortex-a53}
lines: ["25.12", 25.12-mtk]
packages:
  include: [common]
  add: [sing-box]
`,
		"devices/router/files/etc/sysctl.d/99.conf": "x\n",
		"devices/vm/device.yaml": `
name: vm
hardware: {target: armsr, subtarget: armv8, profile: generic, arch: aarch64_generic}
lines: ["25.12"]
packages:
  include: [common]
`,
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func computeAll(t *testing.T, root string) (map[string]Fingerprints, *config.Config) {
	t.Helper()
	cfg, ps, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if ps.HasError() {
		t.Fatalf("fixture 配置有错：\n%s", ps)
	}
	variants, ps := resolve.All(cfg)
	if ps.HasError() {
		t.Fatalf("展开失败：\n%s", ps)
	}

	c := NewComputer(root)
	out := map[string]Fingerprints{}
	for _, v := range variants {
		fp, err := c.For(cfg, v)
		if err != nil {
			t.Fatal(err)
		}
		out[v.ID] = fp
	}
	return out, cfg
}

func TestFingerprintsAreStableAcrossRuns(t *testing.T) {
	root := tree(t)
	first, _ := computeAll(t, root)
	second, _ := computeAll(t, root)
	for id, fp := range first {
		if second[id] != fp {
			t.Fatalf("%s 的指纹不稳定\n%+v\n%+v", id, fp, second[id])
		}
	}
}

func TestFingerprintsAreLayered(t *testing.T) {
	// 上层变了，依赖它的下层必须跟着变——不需要显式列举「改了 A 要连带
	// 重建 B」这类规则。
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		// 期望发生变化的 variant 与它们变化的那几层
		changedLine    []string
		changedFeed    []string
		changedVariant []string
	}{
		{
			name: "改 line 的 overlay",
			mutate: func(t *testing.T, root string) {
				write(t, root, "lines/25.12-mtk/overlay/target/linux/x/patches-6.12/900.patch", "diff 改了\n")
			},
			changedLine:    []string{"router@25.12-mtk"},
			changedFeed:    []string{"router@25.12-mtk"},
			changedVariant: []string{"router@25.12-mtk"},
		},
		{
			name: "改 line.yaml 本身",
			mutate: func(t *testing.T, root string) {
				write(t, root, "lines/25.12/line.yaml", "id: \"25.12\"\nopenwrt_version: 25.12.6\nartifacts: official\n")
			},
			changedLine:    []string{"router@25.12", "vm@25.12"},
			changedFeed:    []string{"router@25.12", "vm@25.12"},
			changedVariant: []string{"router@25.12", "vm@25.12"},
		},
		{
			name: "改自有包 feed",
			mutate: func(t *testing.T, root string) {
				write(t, root, "feed/demo/Makefile", "PKG_NAME:=demo\nPKG_VERSION:=2.0\n$(eval $(call BuildPackage,demo))\n")
			},
			changedFeed:    []string{"router@25.12", "router@25.12-mtk", "vm@25.12"},
			changedVariant: []string{"router@25.12", "router@25.12-mtk", "vm@25.12"},
		},
		{
			name: "改设备自己的 files",
			mutate: func(t *testing.T, root string) {
				write(t, root, "devices/router/files/etc/sysctl.d/99.conf", "y\n")
			},
			changedVariant: []string{"router@25.12", "router@25.12-mtk"},
		},
		{
			name: "改所有设备共用的 files 层",
			mutate: func(t *testing.T, root string) {
				write(t, root, "files/etc/banner", "换了\n")
			},
			changedVariant: []string{"router@25.12", "router@25.12-mtk", "vm@25.12"},
		},
		{
			name: "改被 include 的包集",
			mutate: func(t *testing.T, root string) {
				write(t, root, "sets/common.yaml", "name: common\nadd: [curl, jq]\n")
			},
			changedVariant: []string{"router@25.12", "router@25.12-mtk", "vm@25.12"},
		},
		{
			// 这一条是上一代真实存在的洞：那时 firmware 指纹哈希整棵
			// packages/ 树，改一个没人用的东西会触发全设备重建。
			name: "改一个没有任何设备 include 的包集",
			mutate: func(t *testing.T, root string) {
				write(t, root, "sets/unused.yaml", "name: unused\nadd: [htop, ncdu]\n")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := tree(t)
			before, _ := computeAll(t, root)
			tc.mutate(t, root)
			after, _ := computeAll(t, root)

			assertChanged(t, "line", before, after, tc.changedLine, func(f Fingerprints) string { return f.Line })
			assertChanged(t, "feed", before, after, tc.changedFeed, func(f Fingerprints) string { return f.Feed })
			assertChanged(t, "variant", before, after, tc.changedVariant, func(f Fingerprints) string { return f.Variant })
		})
	}
}

func TestLineTreeIsExposedSeparatelyFromUpstreamCommit(t *testing.T) {
	// 工具链 meta.json 把「我们自己配置改的」与「源码改的」分成两个字段存，
	// 排障时一眼能区分。所以树哈希要单独暴露，而不只有组合后的结果。
	fps, _ := computeAll(t, tree(t))
	fp := fps["router@25.12-mtk"]
	if fp.LineTree == "" {
		t.Fatal("LineTree 应当单独暴露")
	}
	if fp.LineTree == fp.Line {
		t.Error("LineTree 不该等于组合了 upstream commit 之后的 Line 指纹")
	}
}

func TestSameDeviceDifferentLinesGetDifferentVariantFingerprints(t *testing.T) {
	fps, _ := computeAll(t, tree(t))
	if fps["router@25.12"].Variant == fps["router@25.12-mtk"].Variant {
		t.Fatal("同一设备的两条 line 必须有不同的固件指纹，否则一条线的产物会被误判成另一条已经构建过")
	}
}

func TestExecutableBitParticipatesInFingerprint(t *testing.T) {
	// overlay 里的脚本从不可执行变成可执行，是固件内容的实质变化。
	root := tree(t)
	path := filepath.Join(root, "devices/router/files/etc/sysctl.d/99.conf")
	before, _ := computeAll(t, root)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	after, _ := computeAll(t, root)

	if before["router@25.12"].Variant == after["router@25.12"].Variant {
		t.Fatal("可执行位变化没有反映到指纹里")
	}
}

func TestMissingOptionalTreesAreTreatedAsEmpty(t *testing.T) {
	// 官方线没有 overlay/patches/config 目录，不该因此算不出指纹。
	fps, _ := computeAll(t, tree(t))
	if fps["vm@25.12"].Line == "" || fps["vm@25.12"].Variant == "" {
		t.Fatalf("缺可选目录时指纹不该为空: %+v", fps["vm@25.12"])
	}
}

func assertChanged(t *testing.T, layer string, before, after map[string]Fingerprints, want []string, pick func(Fingerprints) string) {
	t.Helper()
	shouldChange := map[string]bool{}
	for _, id := range want {
		shouldChange[id] = true
	}
	for id := range before {
		changed := pick(before[id]) != pick(after[id])
		switch {
		case shouldChange[id] && !changed:
			t.Errorf("%s 的 %s 指纹应当变化但没变", id, layer)
		case !shouldChange[id] && changed:
			t.Errorf("%s 的 %s 指纹不该变化却变了", id, layer)
		}
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
