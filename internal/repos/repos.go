// Package repos 装配三层 apk 软件源地址。
//
// 这是全系统最容易把路由器刷砖的一个产出：地址写错，固件能编出来、能刷进去，
// 问题要到设备联网更新时才暴露。所以本包是纯函数——只做字符串拼装，不发网络
// 请求、不读写文件。所有环境相关的事实（kmod vermagic、R2 公网根、构建机上
// 是否已预同步本地镜像）都由调用方以参数注入。
//
// 上一代在这里内嵌过一个 curl 去 R2 拉 current.json 取 vermagic。为了让它
// 可测、可离线跑基线，先后长出三个环境变量后门、一张硬编码的假 vermagic 表和
// 一个穿透三层脚本的参数。一个 curl 的代价。不要再往这一层放 I/O。
package repos

import (
	"cmp"
	"errors"
	"strings"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

// DefaultUpstreamRoot 是 OpenWrt 官方发布站。
//
// 写死在代码里而不是让每条 line 各写一份：line.yaml 只填 release 号，域名统一
// 在这里拼，杜绝「同一条版本线不同设备各自指向不同 patch」这种漂移。
const DefaultUpstreamRoot = "https://downloads.openwrt.org"

// communityFeeds 是 L3 借用的官方社区 feed，按 arch 键控。
// 顺序固定：它决定 apk 解依赖时的检索次序，也决定本包输出是否可比对。
var communityFeeds = []string{"base", "luci", "packages", "routing", "telephony"}

// indexFile 是 apk 索引文件名。软件源地址一律指到索引本身而不是目录——
// 这是 OpenWrt 25.12 起 apk 的要求。
const indexFile = "packages.adb"

// Options 是装配时需要注入的外部事实。
type Options struct {
	// RepoBase 是自有产物的公网访问根（R2 的自定义域名）。必填。
	RepoBase string
	// Vermagic 是本次固件对应的内核 ABI 标识。必填——L2 驱动层按它键控，
	// 缺了这一层固件装不上任何驱动，而现象要到设备上才暴露。
	Vermagic string
	// LocalL1 / LocalKmod 是构建机上已预同步的索引文件路径，非空时构建期
	// 走 file:// 而不再绕一圈公网。只影响 Build，永远不影响 Runtime。
	LocalL1   string
	LocalKmod string
	// UpstreamRoot 覆盖官方发布站（内网镜像或测试替身），空则用官方。
	UpstreamRoot string
}

// Repos 是两份软件源列表。
//
// 两份必须分开：Build 是 ImageBuilder 组装固件时用的，可以指向构建机本地
// 路径；Runtime 会被写进设备的 /etc/apk/repositories.d/，刷机之后改不了，
// 因此永远只能是在线 URL。构建期的 file:// 漏进设备 = 一条永久失效的软件源。
type Repos struct {
	Build   []string `json:"build"`
	Runtime []string `json:"runtime"`
}

// Assemble 为一个 variant 装配三层软件源。
//
//	L1  自有业务包    <repo_base>/<line>/packages/<arch>/
//	L2  内核驱动与底座 官方线借官方，自建线用自有 R2（两段必须同源）
//	L3  官方社区 feed  一律借官方，按 arch 键控
//	    额外源        device.repos[] 原样追加
func Assemble(v resolve.Variant, opt Options) (Repos, error) {
	if opt.RepoBase == "" {
		return Repos{}, errors.New("repos: RepoBase 必填——自有业务包层没有它就没有地址可拼")
	}
	if opt.Vermagic == "" {
		return Repos{}, errors.New("repos: Vermagic 必填——L2 驱动层按它键控，缺了会产出一份看着正常、实际装不上任何 kmod 的固件")
	}

	var (
		repoBase     = strings.TrimRight(opt.RepoBase, "/")
		upstreamRoot = strings.TrimRight(cmp.Or(opt.UpstreamRoot, DefaultUpstreamRoot), "/")
		upstreamBase = upstreamRoot + "/releases/" + v.Line.Upstream
		lineBase     = repoBase + "/" + v.Line.ID
		targetPath   = "/targets/" + v.Hardware.TargetKey()
		r            Repos
	)

	// L2 两段（内核驱动 + target 基础包）必须整体同源：自编内核的 vermagic
	// 含配置哈希，官方 kmods/ 下不会有那个目录；反过来官方底座配自编驱动，
	// 装上去也对不上 ABI。artifacts 是一个开关正是因为这个。
	kernelBase := upstreamBase
	if v.Line.Artifacts == config.ArtifactsSelf {
		kernelBase = lineBase
	}

	add := func(build, runtime string) {
		r.Build = append(r.Build, build)
		r.Runtime = append(r.Runtime, runtime)
	}

	// L1 自有业务包：无论哪条线都来自我们自己的 R2。
	l1 := lineBase + "/packages/" + v.Hardware.Arch + "/" + indexFile
	add(localOr(opt.LocalL1, l1), l1)

	// L2 内核驱动，按 vermagic 键控。
	kmod := kernelBase + targetPath + "/kmods/" + opt.Vermagic + "/" + indexFile
	add(localOr(opt.LocalKmod, kmod), kmod)

	// L2 target 基础包（libc/libgcc/fstools/kernel...）。
	base := kernelBase + targetPath + "/packages/" + indexFile
	add(base, base)

	// L3 官方社区 feed。
	for _, feed := range communityFeeds {
		url := upstreamBase + "/packages/" + v.Hardware.Arch + "/" + feed + "/" + indexFile
		add(url, url)
	}

	// 额外的第三方源，原样追加在最后。
	for _, extra := range v.ExtraRepos {
		add(extra, extra)
	}

	return r, nil
}

// localOr 在给了本地预同步路径时返回 file:// 形式，否则返回在线地址。
func localOr(localPath, online string) string {
	if localPath == "" {
		return online
	}
	return "file://" + localPath
}
