// Package feed 校验 feed/ 下自有软件包的 Makefile。
//
// 这里的每一条规则都对应一次真实的翻车，而不是风格偏好——它们共同的特点是
// 「构建全绿、产物看着正常，问题只在设备上暴露」：版本号不合 apk 语法会让
// 索引里的包永远排不到最新；PROVIDES 没配套 CONFLICTS 会让官方包和自有包
// 同时装上；PKG_HASH 关掉校验会让上游 tarball 被换掉也无人知晓。
//
// 上一代把这些规则写在 shell 里，用一串 grep + sed 拼；也把 apk 版本号规范
// 写成一篇 markdown 放在 docs/ 里。文档会腐烂，校验不会——所以规范内化成
// 这里的代码，文档不再单独维护一份。
package feed

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/K-Lrize/openwrt-build/internal/diag"
)

const (
	dirFeed  = "feed"
	fileMake = "Makefile"

	// 顶替官方同名包时，供应商优先级必须高过官方默认值。
	minProviderPriority = 200
	// 顶替官方同名包时，PKG_RELEASE 必须高到不会被官方的发布号盖过。
	minOverridingRelease = 100
)

var (
	// Makefile 变量赋值：允许前导空白（PROVIDES 这类常写在 define 块里缩进）。
	reAssign = regexp.MustCompile(`(?m)^[ \t]*([A-Z][A-Z0-9_]*)[ \t]*[:?+]?=[ \t]*(.*)$`)
	// 包最终要靠这一句才会被构建出来。
	reBuildPackage = regexp.MustCompile(`\$\(eval[ \t]+\$\(call[ \t]+(BuildPackage|KernelPackage)`)
	// 值里含 make 变量引用时，展开后的内容这里看不到，只能跳过校验而不是误报。
	reMakeVar = regexp.MustCompile(`\$[({]`)

	// apk（Alpine 从 Gentoo ebuild 继承的）版本号语法：
	//
	//	[epoch:]主版本[单个小写字母][_后缀数字...][-r修订号]
	//
	// 后缀只能从白名单里挑，且后面只能直接跟数字——这就是为什么上游的
	// `1.2.3-alpha.4` 必须改写成 `1.2.3_alpha4`。前八个后缀排在正式版之前，
	// `_p` 排在正式版之后（打包者给未发版的上游打补丁时用）。
	reAPKVersion = regexp.MustCompile(
		`^([0-9]+:)?[0-9]+(\.[0-9]+)*[a-z]?((_alpha|_beta|_pre|_rc|_cvs|_svn|_git|_hg|_p)[0-9]*)*(-r[0-9]+)?$`)
)

// Package 是 feed/ 下的一个自有软件包。
type Package struct {
	// Name 是目录名。它同时是 CI 里 `make package/<name>/compile` 的目标名，
	// 所以必须与 PKG_NAME 一致。
	Name string
	// Vars 是 Makefile 里的顶层变量赋值，未做 make 展开。
	Vars            map[string]string
	HasBuildPackage bool
}

// Load 扫描 <root>/feed/ 下的全部自有包并校验。
//
// 与 config.Load 一致：error 只用于「连目录都读不了」，包本身的毛病走 Problems。
// feed/ 不存在不是错误——还没有任何自有包是合法状态。
func Load(root string) ([]Package, diag.Problems, error) {
	dir := filepath.Join(root, dirFeed)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("读取 %s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var (
		packages []Package
		ps       diag.Problems
	)
	for _, name := range names {
		rel := path.Join(dirFeed, name, fileMake)
		content, err := os.ReadFile(filepath.Join(root, rel))
		if errors.Is(err, fs.ErrNotExist) {
			var one diag.Problems
			one = one.Errorf("feed.makefile", "包目录 %s 下没有 Makefile；feed 扫描按 <目录>/Makefile 发现包，这个目录永远不会被构建", name)
			ps = append(ps, one.WithSource(rel)...)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("读取 %s: %w", rel, err)
		}

		pkg := parse(name, string(content))
		packages = append(packages, pkg)
		ps = append(ps, pkg.Validate().WithSource(rel)...)
	}
	return packages, ps, nil
}

func parse(name, content string) Package {
	pkg := Package{
		Name:            name,
		Vars:            map[string]string{},
		HasBuildPackage: reBuildPackage.MatchString(content),
	}
	for _, m := range reAssign.FindAllStringSubmatch(content, -1) {
		key, value := m[1], strings.TrimSpace(m[2])
		// 同名变量取第一次赋值，与 shell 版本的 `head -1` 行为一致。
		if _, seen := pkg.Vars[key]; !seen {
			pkg.Vars[key] = value
		}
	}
	return pkg
}

// Validate 检查单个包。Source 由 Load 回填。
func (p Package) Validate() diag.Problems {
	var ps diag.Problems

	if !p.HasBuildPackage {
		ps = ps.Errorf("feed.build-package",
			"缺 $(eval $(call BuildPackage,...)) 或 KernelPackage：没有这一句，这个 Makefile 不会产出任何 apk")
	}

	if pkgName, ok := p.Vars["PKG_NAME"]; ok && pkgName != p.Name {
		ps = ps.Errorf("feed.pkg-name",
			"目录名 %q 与 PKG_NAME %q 不符：CI 按目录名跑 make package/%s/compile，对不上时报的错与真实原因毫不相干",
			p.Name, pkgName, p.Name)
	}

	ps = append(ps, p.validateVersion()...)
	ps = append(ps, p.validateSourceIntegrity()...)
	ps = append(ps, p.validateProvides()...)
	return ps
}

func (p Package) validateVersion() diag.Problems {
	var ps diag.Problems
	version, ok := p.Vars["PKG_VERSION"]
	if !ok || reMakeVar.MatchString(version) {
		return ps
	}
	if !reAPKVersion.MatchString(version) {
		ps = ps.Errorf("feed.pkg-version",
			"PKG_VERSION %q 不符合 apk 版本号语法 [epoch:]主版本[字母][_后缀数字][-r修订号]；"+
				"后缀只能取 _alpha/_beta/_pre/_rc/_cvs/_svn/_git/_hg/_p 且后面只能直接跟数字"+
				"（上游的 1.2.3-alpha.4 要写成 1.2.3_alpha4）。写错的后果是索引里的版本排序错乱，"+
				"设备上 apk 永远升不到新版",
			version)
	}
	return ps
}

func (p Package) validateSourceIntegrity() diag.Problems {
	var ps diag.Problems
	hash, hasHash := p.Vars["PKG_HASH"]
	_, hasURL := p.Vars["PKG_SOURCE_URL"]

	if hasHash && strings.EqualFold(hash, "skip") {
		ps = ps.Errorf("feed.pkg-hash",
			"PKG_HASH:=skip 关掉了源码完整性校验，上游 tarball 被替换也不会被发现；"+
				"填真实 sha256（curl -sSL '<PKG_SOURCE_URL>' | shasum -a 256）")
		return ps
	}
	if hasURL && !hasHash {
		ps = ps.Warnf("feed.pkg-hash-missing", "声明了 PKG_SOURCE_URL 却没有 PKG_HASH，下载内容无从校验")
	}
	return ps
}

// validateProvides 查「顶替官方同名包」这件事的配套是否齐全。
//
// PROVIDES 只声明「我能提供 X」，不声明「别装官方的 X」。少了配套，官方包与
// 自有包会同时进 rootfs，或者 apk 在解依赖时随机挑一个——两种结果都要刷机
// 之后才看得出来。
func (p Package) validateProvides() diag.Problems {
	var ps diag.Problems
	provides, ok := p.Vars["PROVIDES"]
	// @ 开头的是构建配置符号而不是包名，不存在「顶替官方包」这回事。
	if !ok || provides == "" || strings.HasPrefix(provides, "@") {
		return ps
	}

	if _, ok := p.Vars["CONFLICTS"]; !ok {
		ps = ps.Errorf("feed.provides.conflicts",
			"PROVIDES:=%s 顶替官方同名包，必须同时声明 CONFLICTS，否则两个包会同时装进 rootfs", provides)
	}
	if p.Vars["DEFAULT_VARIANT"] != "1" {
		ps = ps.Warnf("feed.provides.default-variant",
			"PROVIDES:=%s 建议配 DEFAULT_VARIANT:=1，让 apk 在多个提供者之间有确定的默认选择", provides)
	}

	switch priority, ok := p.Vars["PROVIDER_PRIORITY"]; {
	case !ok:
		ps = ps.Warnf("feed.provides.priority",
			"PROVIDES:=%s 建议显式声明 PROVIDER_PRIORITY（顶替官方默认供应商需 >= %d）", provides, minProviderPriority)
	default:
		if n, err := strconv.Atoi(priority); err == nil && n < minProviderPriority {
			ps = ps.Warnf("feed.provides.priority",
				"PROVIDER_PRIORITY:=%d 低于官方默认供应商，顶替不会生效；建议 >= %d", n, minProviderPriority)
		}
	}

	// PKG_RELEASE 只在「顶替官方包」时才有下限要求：纯自建包从 1 开始完全
	// 正常，在那里报警只会变成人人无视的噪音。
	if release, ok := p.Vars["PKG_RELEASE"]; ok && !reMakeVar.MatchString(release) {
		if n, err := strconv.Atoi(release); err == nil && n < minOverridingRelease {
			ps = ps.Warnf("feed.pkg-release",
				"顶替官方包时 PKG_RELEASE:=%d 偏低，官方发布号一旦追上就会盖过自有包；建议 >= %d", n, minOverridingRelease)
		}
	}
	return ps
}
