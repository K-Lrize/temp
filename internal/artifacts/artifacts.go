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
package artifacts

import "path"

// 每个位置下唯一可变的那个文件。其余一律不可变。
const (
	FileCurrent  = "current.json"  // <line>/targets/<t>/<s>/ 下的工具链指针
	FileLatest   = "latest.json"   // devices/<device>/<line>/ 下的固件指针
	FileBuild    = "build.json"    // 一次工具链构建的元数据
	FileManifest = "manifest.json" // 一次固件发布的元数据
	FileMeta     = "build-meta.json"
)

// LineRoot 是一条 line 的命名空间前缀。
func LineRoot(line string) string { return line }

// PackagesDir 是自有业务包的稳定路径。设备的 apk 直接命中这里，只增不删。
func PackagesDir(line, arch string) string {
	return path.Join(line, "packages", arch)
}

// PackagesMetaPath 与包索引同目录，记录这批包对应的 feed 指纹。
func PackagesMetaPath(line, arch string) string {
	return path.Join(PackagesDir(line, arch), FileMeta)
}

// TargetDir 是一条 line 下某个 target/subtarget 的根。
func TargetDir(line, target, subtarget string) string {
	return path.Join(line, "targets", target, subtarget)
}

// CurrentPath 是工具链指针，本目录下唯一可变的文件。
func CurrentPath(line, target, subtarget string) string {
	return path.Join(TargetDir(line, target, subtarget), FileCurrent)
}

// BuildDir 是一次工具链构建的不可变目录。
func BuildDir(line, target, subtarget, buildID string) string {
	return path.Join(TargetDir(line, target, subtarget), "builds", buildID)
}

// BuildJSONPath 是不可变构建目录里的元数据。
func BuildJSONPath(line, target, subtarget, buildID string) string {
	return path.Join(BuildDir(line, target, subtarget, buildID), FileBuild)
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

// LatestPath 是固件指针，本目录下唯一可变的文件。
func LatestPath(device, line string) string {
	return path.Join(DeviceLineDir(device, line), FileLatest)
}

// ReleaseDir 是一次固件发布的不可变目录。
func ReleaseDir(device, line, releaseID string) string {
	return path.Join(DeviceLineDir(device, line), "releases", releaseID)
}

// ManifestPath 是不可变发布目录里的元数据。
func ManifestPath(device, line, releaseID string) string {
	return path.Join(ReleaseDir(device, line, releaseID), FileManifest)
}

// Current 是工具链指针的内容：指向某一次不可变构建。
// 回滚就是把它改回上一个 build_id。
type Current struct {
	BuildID             string `json:"build_id"`
	Vermagic            string `json:"vermagic"`
	SDKArchive          string `json:"sdk_archive"`
	ImageBuilderArchive string `json:"imagebuilder_archive"`
	UpdatedAt           string `json:"updated_at"`
}

// Build 是一次工具链构建的溯源信息。
//
// LineTree 与 UpstreamCommit 刻意分成两个字段：排障时一眼能区分是
// 「我们自己配置改的」还是「源码改的」。plan 比对时用同一种方式把它们重新
// 组合成 line 指纹，远端不必再存一份组合后的字符串。
type Build struct {
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
	KmodCount      int    `json:"kmod_count"`
	CIRunURL       string `json:"ci_run_url"`
	CreatedAt      string `json:"created_at"`
}

// PackagesMeta 记录一批自有包对应的 feed 指纹，供下一次 plan 比对。
type PackagesMeta struct {
	FeedFingerprint string `json:"feed_fingerprint"`
	UpdatedAt       string `json:"updated_at"`
}

// Latest 是固件指针。
type Latest struct {
	ReleaseID string `json:"release_id"`
	UpdatedAt string `json:"updated_at"`
}

// Manifest 回答「这份固件到底是什么」，随固件一起发布，永久可查。
type Manifest struct {
	ReleaseID      string       `json:"release_id"`
	Variant        string       `json:"variant"`
	Device         string       `json:"device"`
	Line           string       `json:"line"`
	BuildID        string       `json:"build_id"`
	Vermagic       string       `json:"vermagic"`
	UpstreamCommit string       `json:"upstream_commit"`
	Fingerprints   Fingerprints `json:"fingerprints"`
	CIRunURL       string       `json:"ci_run_url"`
	CreatedAt      string       `json:"created_at"`
}

// Fingerprints 是随产物一起发布的三层指纹：GC 靠它做引用计数，plan 靠它
// 判定是否需要重建，人靠它定位是哪一层的变化触发了这次构建。
type Fingerprints struct {
	Line    string `json:"line"`
	Feed    string `json:"feed"`
	Variant string `json:"variant"`
}
