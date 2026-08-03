package plan

import (
	"fmt"
	"sort"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

// Remote 回答「远端现在构建到什么状态」。
//
// 返回空字符串表示「不知道」——远端不可达、没配地址、那个位置还没构建过，
// 一律当作需要构建。宁可多编一次，也不要漏编之后拿到一份对不上的产物。
type Remote interface {
	ToolchainFingerprint(line, target, subtarget string) string
	PackagesFingerprint(line, arch string) string
	FirmwareFingerprint(device, line string) string
}

// NoRemote 什么都不知道，于是一切都需要构建。本地跑 plan 或强制全量重建时用。
type NoRemote struct{}

func (NoRemote) ToolchainFingerprint(string, string, string) string { return "" }
func (NoRemote) PackagesFingerprint(string, string) string          { return "" }
func (NoRemote) FirmwareFingerprint(string, string) string          { return "" }

// ToolchainEntry 是一次工具链构建：编出 SDK/IB/kmod/target base。
// 只有 artifacts=self 的 line 才有这回事。
type ToolchainEntry struct {
	Line      string `json:"line"`
	Target    string `json:"target"`
	Subtarget string `json:"subtarget"`
	Repo      string `json:"repo"`
	// Commit 是 CI 唯一信任的源码坐标，ref 只供人读。
	Commit string `json:"commit"`
	// LineTree 写进工具链 meta.json，与 Commit 分开存——排障时一眼区分是
	// 「我们自己配置改的」还是「源码改的」。
	LineTree    string `json:"line_tree"`
	Fingerprint string `json:"fingerprint"`
	NeedsBuild  bool   `json:"needs_build"`
}

// PackagesEntry 是一次自有软件包构建，按 (line, arch) 分组：同一条 line 下
// 相同 arch 的设备共用一份用户态包。
type PackagesEntry struct {
	Line string `json:"line"`
	Arch string `json:"arch"`
	// SDKTarget 决定用哪套 SDK 来编。同 arch 即同 ABI，用哪个 target 的 SDK
	// 都行，但必须是确定的选择——见 pickSDKTarget。
	SDKTarget        string           `json:"sdk_target"`
	Artifacts        config.Artifacts `json:"artifacts"`
	OpenWrtVersion string           `json:"openwrt_version"`
	Commit           string           `json:"commit,omitempty"`
	Fingerprint      string           `json:"fingerprint"`
	NeedsBuild       bool             `json:"needs_build"`
}

// FirmwareEntry 是一次固件组装，一个 variant 一条。
type FirmwareEntry struct {
	Variant     string `json:"variant"`
	Device      string `json:"device"`
	Line        string `json:"line"`
	Fingerprint string `json:"fingerprint"`
	NeedsBuild  bool   `json:"needs_build"`
}

// Plan 是三个构建矩阵。
type Plan struct {
	Toolchain []ToolchainEntry `json:"toolchain"`
	Packages  []PackagesEntry  `json:"packages"`
	Firmware  []FirmwareEntry  `json:"firmware"`
}

// Empty 报告是否无事可做。无变更时它必须为 true——这是「重跑一次是幂等的」
// 这句话的机器可验证形式。
func (p Plan) Empty() bool {
	return len(p.Toolchain) == 0 && len(p.Packages) == 0 && len(p.Firmware) == 0
}

// Pending 只保留真正需要构建的条目，用来喂 CI 矩阵。
func (p Plan) Pending() Plan {
	// 空矩阵必须序列化成 []（而非 nil→null）：release.yml 用 `!= '[]'` 判断某条
	// 线是否需要构建，null 会漏过这个门、让 job 带空矩阵启动而报错。
	out := Plan{
		Toolchain: []ToolchainEntry{},
		Packages:  []PackagesEntry{},
		Firmware:  []FirmwareEntry{},
	}
	for _, e := range p.Toolchain {
		if e.NeedsBuild {
			out.Toolchain = append(out.Toolchain, e)
		}
	}
	for _, e := range p.Packages {
		if e.NeedsBuild {
			out.Packages = append(out.Packages, e)
		}
	}
	for _, e := range p.Firmware {
		if e.NeedsBuild {
			out.Firmware = append(out.Firmware, e)
		}
	}
	return out
}

// Build 算出三个构建矩阵。
//
// 职责边界：本函数只做「算指纹 + 和远端比」。指纹计算是纯的（Computer），
// 远端状态由 Remote 提供——把 I/O 放在接口后面，判定逻辑才能离线测试。
func Build(root string, cfg *config.Config, variants []resolve.Variant, remote Remote) (Plan, error) {
	computer := NewComputer(root)

	fps := make(map[string]Fingerprints, len(variants))
	for _, v := range variants {
		fp, err := computer.For(cfg, v)
		if err != nil {
			return Plan{}, fmt.Errorf("计算 %s 的指纹: %w", v.ID, err)
		}
		fps[v.ID] = fp
	}

	return Plan{
		Toolchain: toolchainMatrix(variants, fps, remote),
		Packages:  packagesMatrix(variants, fps, remote),
		Firmware:  firmwareMatrix(variants, fps, remote),
	}, nil
}

func toolchainMatrix(variants []resolve.Variant, fps map[string]Fingerprints, remote Remote) []ToolchainEntry {
	byKey := map[string]ToolchainEntry{}
	for _, v := range variants {
		// 官方线的 SDK/IB 直接从上游下载，没有「要不要编」这回事。
		if v.Line.Artifacts != config.ArtifactsSelf {
			continue
		}
		key := v.Line.ID + "|" + v.Hardware.TargetKey()
		if _, seen := byKey[key]; seen {
			continue
		}
		fp := fps[v.ID]
		entry := ToolchainEntry{
			Line:        v.Line.ID,
			Target:      v.Hardware.Target,
			Subtarget:   v.Hardware.Subtarget,
			LineTree:    fp.LineTree,
			Fingerprint: fp.Line,
		}
		if v.Line.Source != nil {
			entry.Repo = v.Line.Source.Repo
			entry.Commit = v.Line.Source.Commit
		}
		entry.NeedsBuild = entry.Fingerprint != remote.ToolchainFingerprint(entry.Line, entry.Target, entry.Subtarget)
		byKey[key] = entry
	}

	out := make([]ToolchainEntry, 0, len(byKey))
	for _, key := range sortedKeys(byKey) {
		out = append(out, byKey[key])
	}
	return out
}

func packagesMatrix(variants []resolve.Variant, fps map[string]Fingerprints, remote Remote) []PackagesEntry {
	byKey := map[string]PackagesEntry{}
	for _, v := range variants {
		key := v.Line.ID + "|" + v.Hardware.Arch
		entry, seen := byKey[key]
		if !seen {
			fp := fps[v.ID]
			entry = PackagesEntry{
				Line:             v.Line.ID,
				Arch:             v.Hardware.Arch,
				SDKTarget:        v.Hardware.TargetKey(),
				Artifacts:        v.Line.Artifacts,
				OpenWrtVersion: v.Line.OpenWrtVersion,
				Fingerprint:      fp.Feed,
			}
			if v.Line.Source != nil {
				entry.Commit = v.Line.Source.Commit
			}
			entry.NeedsBuild = entry.Fingerprint != remote.PackagesFingerprint(entry.Line, entry.Arch)
		} else {
			entry.SDKTarget = pickSDKTarget(entry.SDKTarget, v.Hardware.TargetKey())
		}
		byKey[key] = entry
	}

	out := make([]PackagesEntry, 0, len(byKey))
	for _, key := range sortedKeys(byKey) {
		out = append(out, byKey[key])
	}
	return out
}

// pickSDKTarget 在同一个 (line, arch) 分组内选一套 SDK。
//
// 同 arch 即同 ABI，用哪个 target 的 SDK 编用户态包都能用，但选择必须是
// 确定的：上一代取「分组里的第一个」，而分组顺序来自目录遍历，于是把设备
// 改个名就能翻转「用官方 SDK 还是自建 SDK」这种事。这里取字典序最小值，
// 与遍历顺序、设备名、文件系统全都无关。
func pickSDKTarget(a, b string) string {
	if b < a {
		return b
	}
	return a
}

func firmwareMatrix(variants []resolve.Variant, fps map[string]Fingerprints, remote Remote) []FirmwareEntry {
	out := make([]FirmwareEntry, 0, len(variants))
	for _, v := range variants {
		entry := FirmwareEntry{
			Variant:     v.ID,
			Device:      v.Device,
			Line:        v.Line.ID,
			Fingerprint: fps[v.ID].Variant,
		}
		entry.NeedsBuild = entry.Fingerprint != remote.FirmwareFingerprint(v.Device, v.Line.ID)
		out = append(out, entry)
	}
	// variants 已按「设备名字典序 + line 声明顺序」排好，这里保持不动：
	// 声明顺序是有意义的（第一条 line 是主线）。
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
