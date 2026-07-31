package plan

import (
	"reflect"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

// fakeRemote 按 key 返回远端已构建的指纹，未收录的 key 返回空串（= 不知道）。
type fakeRemote map[string]string

func (f fakeRemote) ToolchainFingerprint(line, target, subtarget string) string {
	return f[line+"|"+target+"/"+subtarget]
}
func (f fakeRemote) PackagesFingerprint(line, arch string) string { return f[line+"|"+arch] }
func (f fakeRemote) FirmwareFingerprint(device, line string) string {
	return f[device+"@"+line]
}

func buildPlan(t *testing.T, root string, remote Remote) Plan {
	t.Helper()
	cfg, ps, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if ps.HasError() {
		t.Fatalf("fixture 有错：\n%s", ps)
	}
	variants, ps := resolve.All(cfg)
	if ps.HasError() {
		t.Fatalf("展开失败：\n%s", ps)
	}
	p, err := Build(root, cfg, variants, remote)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestToolchainMatrixOnlyCoversSelfBuiltLines(t *testing.T) {
	// 官方线的 SDK/IB 直接下载，没有「要不要编工具链」这回事。
	p := buildPlan(t, tree(t), NoRemote{})

	var keys []string
	for _, e := range p.Toolchain {
		keys = append(keys, e.Line+"|"+e.Target+"/"+e.Subtarget)
	}
	want := []string{"25.12-mtk|mediatek/filogic"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("want %v, got %v", want, keys)
	}
	if got := p.Toolchain[0].Commit; got != "f0a60eee2fe051741c643ea6118718aae1ef17fb" {
		t.Errorf("工具链条目要带上源码 commit，CI 只信它: %q", got)
	}
}

func TestPackagesMatrixIsKeyedByLineAndArch(t *testing.T) {
	// 同一条 line 下相同 arch 的设备共用一份用户态包，只编一次。
	p := buildPlan(t, tree(t), NoRemote{})

	var keys []string
	for _, e := range p.Packages {
		keys = append(keys, e.Line+"|"+e.Arch)
	}
	want := []string{
		"25.12-mtk|aarch64_cortex-a53",
		"25.12|aarch64_cortex-a53",
		"25.12|aarch64_generic",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("want %v, got %v", want, keys)
	}
}

func TestFirmwareMatrixHasOneEntryPerVariant(t *testing.T) {
	p := buildPlan(t, tree(t), NoRemote{})

	var ids []string
	for _, e := range p.Firmware {
		ids = append(ids, e.Variant)
	}
	want := []string{"router@25.12", "router@25.12-mtk", "vm@25.12"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("want %v, got %v", want, ids)
	}
}

func TestWithoutRemoteEverythingNeedsBuilding(t *testing.T) {
	// 远端不可达或没配地址时保守处理：宁可多编，不可漏编。
	p := buildPlan(t, tree(t), NoRemote{})
	for _, e := range p.Firmware {
		if !e.NeedsBuild {
			t.Errorf("%s 在无法判定远端状态时应当被列为需要构建", e.Variant)
		}
	}
	for _, e := range p.Toolchain {
		if !e.NeedsBuild {
			t.Errorf("%s 同上", e.Line)
		}
	}
}

func TestMatchingRemoteFingerprintSkipsTheBuild(t *testing.T) {
	root := tree(t)
	// 先算一遍拿到指纹，再喂给假远端——模拟「上次就是用这份代码构建的」。
	full := buildPlan(t, root, NoRemote{})
	remote := fakeRemote{}
	for _, e := range full.Toolchain {
		remote[e.Line+"|"+e.Target+"/"+e.Subtarget] = e.Fingerprint
	}
	for _, e := range full.Packages {
		remote[e.Line+"|"+e.Arch] = e.Fingerprint
	}
	for _, e := range full.Firmware {
		remote[e.Variant] = e.Fingerprint
	}

	p := buildPlan(t, root, remote)
	for _, e := range p.Toolchain {
		if e.NeedsBuild {
			t.Errorf("工具链 %s 指纹一致，不该重建", e.Line)
		}
	}
	for _, e := range p.Packages {
		if e.NeedsBuild {
			t.Errorf("软件包 %s|%s 指纹一致，不该重建", e.Line, e.Arch)
		}
	}
	for _, e := range p.Firmware {
		if e.NeedsBuild {
			t.Errorf("固件 %s 指纹一致，不该重建", e.Variant)
		}
	}
	// 「无变更时三个矩阵为空」说的是待构建的那一份——Build 本身返回的是
	// 全部候选（带 NeedsBuild 标记），供人核对「为什么这条被跳过了」。
	if !p.Pending().Empty() {
		t.Errorf("全部命中时待构建矩阵应为空 —— 这是幂等性的证明\n%+v", p.Pending())
	}
}

func TestPendingKeepsOnlyWhatNeedsBuilding(t *testing.T) {
	root := tree(t)
	full := buildPlan(t, root, NoRemote{})

	// 只让一台设备的固件命中，其余全部落空。
	remote := fakeRemote{}
	for _, e := range full.Firmware {
		if e.Variant == "vm@25.12" {
			remote[e.Variant] = e.Fingerprint
		}
	}

	pending := buildPlan(t, root, remote).Pending()
	var ids []string
	for _, e := range pending.Firmware {
		ids = append(ids, e.Variant)
	}
	want := []string{"router@25.12", "router@25.12-mtk"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("want %v, got %v", want, ids)
	}
	if pending.Empty() {
		t.Error("还有条目要构建时 Empty() 不该为 true")
	}
}

func TestStaleRemoteFingerprintTriggersRebuild(t *testing.T) {
	root := tree(t)
	remote := fakeRemote{"router@25.12": "上一次构建时的旧指纹"}
	p := buildPlan(t, root, remote)

	for _, e := range p.Firmware {
		if e.Variant == "router@25.12" && !e.NeedsBuild {
			t.Fatal("远端指纹与本地不一致时必须重建")
		}
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	root := tree(t)
	first := buildPlan(t, root, NoRemote{})
	for range 3 {
		if got := buildPlan(t, root, NoRemote{}); !reflect.DeepEqual(got, first) {
			t.Fatal("同样的输入必须得到相同的矩阵——CI 矩阵与人工核对都依赖这一点")
		}
	}
}

func TestPackagesEntryPicksADeterministicSDKTarget(t *testing.T) {
	// 同一条 line 下同一个 arch 可能来自多个 target。用哪套 SDK 编用户态包
	// 都行（同 arch 同 ABI），但必须是确定的选择而不是目录遍历的副产物——
	// 上一代就栽在「取分组里的第一个」上，把设备改个名就翻转了行为。
	p := buildPlan(t, tree(t), NoRemote{})
	for _, e := range p.Packages {
		if e.SDKTarget == "" {
			t.Errorf("%s|%s 缺少 SDKTarget", e.Line, e.Arch)
		}
	}
	if got := findPackages(t, p, "25.12", "aarch64_cortex-a53").SDKTarget; got != "mediatek/filogic" {
		t.Errorf("SDKTarget = %q", got)
	}
}

func TestPackagesEntryCarriesArtifactsSource(t *testing.T) {
	// _packages 流水线要据此决定 SDK 是从官方下载还是从自有 R2 取。
	p := buildPlan(t, tree(t), NoRemote{})
	if got := findPackages(t, p, "25.12", "aarch64_generic").Artifacts; got != config.ArtifactsOfficial {
		t.Errorf("artifacts = %q", got)
	}
	if got := findPackages(t, p, "25.12-mtk", "aarch64_cortex-a53").Artifacts; got != config.ArtifactsSelf {
		t.Errorf("artifacts = %q", got)
	}
}

func findPackages(t *testing.T, p Plan, line, arch string) PackagesEntry {
	t.Helper()
	for _, e := range p.Packages {
		if e.Line == line && e.Arch == arch {
			return e
		}
	}
	t.Fatalf("没找到 %s|%s", line, arch)
	return PackagesEntry{}
}
