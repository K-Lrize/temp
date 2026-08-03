// Package artifacts 定义 R2 上的路径规则与元数据 JSON 的形状。
//
// 本包只有类型与纯粹的路径拼装，没有任何 I/O：读侧（plan 的远端比对）与
// 写侧（发布流水线）共用同一份定义，两边不会各写一遍再慢慢漂移。
//
// 两条铁律决定了这套布局：
//
//	设备面向的路径必须永久稳定——它们会被烧进 /etc/apk/repositories.d/，
//	刷机之后改不了。
//	CI 面向的路径必须不可变——可回滚、可溯源。
//
// 于是 SDK/IB 落在不可变的 builds/<build-id>/ 下（只有 CI 自己消费，回滚 =
// 把 current.json 指回另一个 build-id），而 kmod 与 target base 包留在按
// vermagic / target 键控的稳定路径上覆盖式发布，靠引用计数 GC 而不是靠
// 不可变性管理生命周期。
//
// 每层只有两种文件，全线同名同形：
//
//	current.json  被 plan/消费方查询的路径上「当前状态 + 本层指纹」的可变文件。
//	              plan 一次 GET 就拿到指纹判定是否重建（不再翻第二跳）。
//	meta.json     不可变目录里的完整档案：GC 引用计数 + 人工溯源。plan 不读它。
package artifacts

import "path"

// 两种角色的文件名。
const (
	FileCurrent = "current.json" // 每个被查询路径上「当前状态 + 指纹」的可变文件
	FileMeta    = "meta.json"    // 不可变目录里的档案（GC 引用 + 溯源）
)

// LineRoot 是一条 line 的命名空间前缀。
func LineRoot(line string) string { return line }

// PackagesDir 是自有业务包的稳定路径。设备的 apk 直接命中这里，只增不删。
func PackagesDir(line, arch string) string {
	return path.Join(line, "packages", arch)
}

// PackagesCurrentPath 与包索引同目录，记录这批包的 feed 指纹，供 plan 一跳比对。
func PackagesCurrentPath(line, arch string) string {
	return path.Join(PackagesDir(line, arch), FileCurrent)
}

// TargetDir 是一条 line 下某个 target/subtarget 的根。
func TargetDir(line, target, subtarget string) string {
	return path.Join(line, "targets", target, subtarget)
}

// CurrentPath 是工具链「当前状态」文件：本目录下唯一可变的文件。
// 带 line 指纹（plan 一跳）+ 取物句柄（self 线拉 SDK/IB）+ kmod_count（回归门禁）。
func CurrentPath(line, target, subtarget string) string {
	return path.Join(TargetDir(line, target, subtarget), FileCurrent)
}

// BuildDir 是一次工具链构建的不可变目录。
func BuildDir(line, target, subtarget, buildID string) string {
	return path.Join(TargetDir(line, target, subtarget), "builds", buildID)
}

// BuildMetaPath 是不可变构建目录里的溯源档案。plan/GC/门禁都不读它（都读 current.json），
// 纯为人工排障保留。
func BuildMetaPath(line, target, subtarget, buildID string) string {
	return path.Join(BuildDir(line, target, subtarget, buildID), FileMeta)
}

// KmodsDir 按内核 ABI 键控。已刷机设备固化的地址就在这里，路径必须永久稳定。
func KmodsDir(line, target, subtarget, vermagic string) string {
	return path.Join(TargetDir(line, target, subtarget), "kmods", vermagic)
}

// TargetPackagesDir 是 target 基础包（libc/libgcc/fstools/kernel...）的稳定路径。
func TargetPackagesDir(line, target, subtarget string) string {
	return path.Join(TargetDir(line, target, subtarget), "packages")
}

// DeviceLineDir 是一个 variant 的固件根。
//
// device 在顶层、line 居中：人的入口是设备（「给 mt3600be 刷机」应当一个目录
// 下看见它所有版本线），GC 也按设备分组——devices/<d>/*/releases/ 一次列举
// 就够。反过来 line 顶层要跨所有前缀扫再聚合。
func DeviceLineDir(device, line string) string {
	return path.Join("devices", device, line)
}

// FirmwareCurrentPath 是固件「当前状态」文件：本目录下唯一可变的文件。
// 带 variant 指纹（plan 一跳）+ 指向的 release_id。
func FirmwareCurrentPath(device, line string) string {
	return path.Join(DeviceLineDir(device, line), FileCurrent)
}

// ReleaseDir 是一次固件发布的不可变目录。
func ReleaseDir(device, line, releaseID string) string {
	return path.Join(DeviceLineDir(device, line), "releases", releaseID)
}

// ReleaseMetaPath 是不可变发布目录里的档案：GC 引用计数 + 溯源。
func ReleaseMetaPath(device, line, releaseID string) string {
	return path.Join(ReleaseDir(device, line, releaseID), FileMeta)
}

// Current 是工具链「当前状态」。
//
// Fingerprint（line 指纹）让 plan 一次 GET 就能判定是否重编；回滚就是把它整份
// 改回上一个 build 的事实（指纹/句柄随之一起换，不会与 build 漂移）。
// 其余字段：self 线据 SDKArchive/ImageBuilderArchive 取物；kmod 回归门禁读
// KmodCount 与新构建比。
type Current struct {
	Fingerprint         string `json:"fingerprint"`
	BuildID             string `json:"build_id"`
	Vermagic            string `json:"vermagic"`
	SDKArchive          string `json:"sdk_archive"`
	ImageBuilderArchive string `json:"imagebuilder_archive"`
	KmodCount           int    `json:"kmod_count"`
	UpdatedAt           string `json:"updated_at"`
}

// PackagesCurrent 是自有包层「当前状态」：只有一个 feed 指纹供 plan 比对。
type PackagesCurrent struct {
	Fingerprint string `json:"fingerprint"`
	UpdatedAt   string `json:"updated_at"`
}

// FirmwareCurrent 是固件「当前状态」：variant 指纹（plan 一跳）+ 指向的 release。
type FirmwareCurrent struct {
	Fingerprint string `json:"fingerprint"`
	ReleaseID   string `json:"release_id"`
	UpdatedAt   string `json:"updated_at"`
}

// BuildMeta 是一次工具链构建的不可变溯源档案。
//
// LineTree 与 UpstreamCommit 分成两个字段：排障时一眼区分「配置改的」还是
// 「源码改的」。指纹已在 current.json 里算好，这里不再重复承担 plan 的判定。
type BuildMeta struct {
	BuildID        string `json:"build_id"`
	Line           string `json:"line"`
	Target         string `json:"target"`
	Subtarget      string `json:"subtarget"`
	UpstreamCommit string `json:"upstream_commit"`
	LineTree       string `json:"line_tree"`
	Vermagic       string `json:"vermagic"`
	KernelVersion  string `json:"kernel_version"`
	SDKSHA256      string `json:"sdk_sha256"`
	IBSHA256       string `json:"ib_sha256"`
	CIRunURL       string `json:"ci_run_url"`
	CreatedAt      string `json:"created_at"`
}

// ReleaseMeta 是一次固件发布的不可变档案。
//
// GC 靠 BuildID/Vermagic 做引用计数（保活对应工具链 build 与 kmod 仓）；
// 回归门禁靠 ManifestFile 定位本版随固件发布的官方包清单（原名不改）。
// Fingerprint 是 variant 指纹（与 current.json 一致，供人核对）。
type ReleaseMeta struct {
	ReleaseID      string `json:"release_id"`
	Variant        string `json:"variant"`
	Device         string `json:"device"`
	Line           string `json:"line"`
	BuildID        string `json:"build_id"`
	Vermagic       string `json:"vermagic"`
	UpstreamCommit string `json:"upstream_commit"`
	Fingerprint    string `json:"fingerprint"`
	ManifestFile   string `json:"manifest_file"`
	CIRunURL       string `json:"ci_run_url"`
	CreatedAt      string `json:"created_at"`
}
