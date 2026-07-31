package config

import (
	"regexp"
	"strings"
)

var (
	// id / 设备名：小写 kebab，同时要能安全地当 R2 路径片段与目录名。
	reIdent = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	// 完整 patch 号。只写 25.12 会让「同一条版本线不同设备各自指向
	// 25.12.4 / 25.12.5」这种漂移无法被发现。
	reUpstream = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reCommit   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	rePackage  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]*$`)
	// 从 ref 里认版本线：v25.12.5、openwrt-25.12、25.12 都能认出 25.12。
	reRefVersion = regexp.MustCompile(`([0-9]+)\.([0-9]+)`)
)

// archByTarget 只收录本仓库真实用到过的 target 组合。
//
// 未收录的组合一律降级为 warn 而不是 fail：为尚未接入的假设性 target 预先猜
// 一个 arch 字符串，猜错的代价（悄悄记下一个只在配置里出现过的错值）比空着
// 不查更高。接新硬件时，在同一次提交里把实测值（来自
// bin/targets/<t>/<s>/*.manifest）加一行即可。
var archByTarget = map[string]string{
	"armsr/armv8":      "aarch64_generic",
	"mediatek/filogic": "aarch64_cortex-a53",
}

// Validate 校验单个 line 自身自洽。跨文件的规则（id 与目录名一致、
// requires_build 与 artifacts 的关系）在载入层。
func (l Line) Validate() Problems {
	var ps Problems

	if !reIdent.MatchString(l.ID) {
		ps = ps.Errorf("line.id", "id %q 非法：只允许小写字母、数字、点、连字符、下划线，且首字符为字母或数字", l.ID)
	}
	if !reUpstream.MatchString(l.Upstream) {
		ps = ps.Errorf("line.upstream", "upstream %q 必须是完整 patch 号（如 25.12.5），不接受 25.12 这类让系统猜测的写法", l.Upstream)
	}

	switch l.Artifacts {
	case ArtifactsOfficial:
		if l.Source != nil {
			ps = ps.Errorf("line.source.unexpected", "artifacts=official 的 line 不该声明 source：产物直接借官方，源码字段不参与任何决策，留着只会误导")
		}
	case ArtifactsSelf:
		if l.Source == nil {
			ps = ps.Errorf("line.source.missing", "artifacts=self 必须声明 source（repo/commit/ref）")
		}
	default:
		ps = ps.Errorf("line.artifacts", "artifacts %q 非法：只能是 %q 或 %q", l.Artifacts, ArtifactsOfficial, ArtifactsSelf)
	}

	if l.Source != nil {
		ps = append(ps, l.validateSource()...)
	}
	return ps
}

func (l Line) validateSource() Problems {
	var ps Problems
	src := l.Source

	if !strings.HasPrefix(src.Repo, "http://") && !strings.HasPrefix(src.Repo, "https://") {
		ps = ps.Errorf("line.source.repo", "source.repo %q 必须是 http(s) URL——CI 里没有 ssh 凭据", src.Repo)
	}
	if !reCommit.MatchString(src.Commit) {
		ps = ps.Errorf("line.source.commit",
			"source.commit %q 必须是 40 位完整哈希；用 `git ls-remote --tags <repo> '<tag>*'` 取值，"+
				"带注解的 tag 要取 ^{} 剥离后的提交对象", src.Commit)
	}

	// artifacts=self 时 L3 社区 feed 仍然借 upstream 那条线，两者必须同版本线，
	// 否则借来的 luci/packages 与自编 libc 对不上。commit 无法离线核对，
	// 只能用 ref 做这道检查——这是 ref 唯一的机器用途。
	if src.Ref == "" {
		ps = ps.Errorf("line.source.ref", "artifacts=self 时 source.ref 必填：它是离线核对「upstream 与源码是否同一条版本线」的唯一依据")
		return ps
	}
	refLine := versionLine(src.Ref)
	if refLine == "" {
		ps = ps.Warnf("line.upstream-ref-unknown",
			"source.ref %q 里没有版本号，无法核对它与 upstream %s 是否同一条版本线；"+
				"跟踪 master 是合法的，但要自己确认借来的 L3 社区包与自编 libc 兼容", src.Ref, l.Upstream)
		return ps
	}
	if up := versionLine(l.Upstream); up != "" && up != refLine {
		ps = ps.Errorf("line.upstream-ref-mismatch",
			"upstream %s（%s 线）与 source.ref %s（%s 线）不是同一条版本线："+
				"L3 社区 feed 借 upstream，自编 libc 来自 source，两者错线会在设备上表现为依赖装不上",
			l.Upstream, up, src.Ref, refLine)
	}
	return ps
}

// versionLine 从版本号或 ref 里取 <major>.<minor>，取不到返回空串。
func versionLine(s string) string {
	m := reRefVersion.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2]
}

// Validate 校验单台设备自身自洽。跨文件规则（name 与目录名一致、
// lines 引用的 line 存在、include 引用的 set 存在）在载入层。
func (d Device) Validate() Problems {
	var ps Problems

	if !reIdent.MatchString(d.Name) {
		ps = ps.Errorf("device.name", "name %q 非法：只允许小写字母、数字、点、连字符、下划线", d.Name)
	}

	// 用有序切片而不是 map：map 迭代顺序随机，会让 lint 的输出在两次运行之间
	// 换行序，diff 和 golden 都不稳。
	for _, f := range []struct{ name, value string }{
		{"target", d.Hardware.Target},
		{"subtarget", d.Hardware.Subtarget},
		{"profile", d.Hardware.Profile},
		{"arch", d.Hardware.Arch},
	} {
		if f.value == "" {
			ps = ps.Errorf("device.hardware", "hardware.%s 必填", f.name)
		}
	}
	ps = append(ps, d.Hardware.validateArch()...)

	switch {
	case len(d.Lines) == 0:
		ps = ps.Errorf("device.lines", "lines 至少要有一条：它是这台设备的出货矩阵，空列表意味着永远不会被构建")
	default:
		if dup := firstDuplicate(d.Lines); dup != "" {
			ps = ps.Errorf("device.lines", "lines 里 %q 重复", dup)
		}
	}

	if d.Image.RootfsPartsize < 0 {
		ps = ps.Errorf("device.image", "image.rootfs_partsize 不能为负（当前 %d）", d.Image.RootfsPartsize)
	}

	ps = append(ps, validatePackages(d.Packages.Add, d.Packages.Remove)...)
	return ps
}

// validateArch 拦「arch 与 target/subtarget 不对应」。
//
// arch 打错一个字母的后果是：从错误架构的仓库拉包 -> 设备不可开机，
// 而其余每一项校验都是绿的。这是这张收录表存在的全部理由。
func (h Hardware) validateArch() Problems {
	var ps Problems
	key := h.TargetKey()
	want, known := archByTarget[key]
	if !known {
		return ps.Warnf("device.arch", "target 组合 %q 尚未收录，无法核对 arch=%q；接入后请把实测值加进 archByTarget", key, h.Arch)
	}
	if h.Arch != want {
		ps = ps.Errorf("device.arch", "target 组合 %q 的 arch 应为 %q，当前为 %q", key, want, h.Arch)
	}
	return ps
}

// Validate 校验单个包集自身自洽。
func (s Set) Validate() Problems {
	var ps Problems
	if !reIdent.MatchString(s.Name) {
		ps = ps.Errorf("set.name", "name %q 非法：只允许小写字母、数字、点、连字符、下划线", s.Name)
	}
	if len(s.Add) == 0 && len(s.Remove) == 0 {
		ps = ps.Errorf("set.empty", "包集 %q 的 add 与 remove 都为空，没有任何作用", s.Name)
	}
	ps = append(ps, validatePackages(s.Add, s.Remove)...)
	return ps
}

// validatePackages 是 device 与 set 共用的包名与冲突检查。
//
// 只查同一份文件内部的冲突；跨层（多个 set + device 合并之后）的冲突由
// 合并算法在展开 variant 时报，那时才知道完整的层列表。
func validatePackages(add, remove []string) Problems {
	var ps Problems

	for _, list := range [][]string{add, remove} {
		for _, name := range list {
			if rePackage.MatchString(name) {
				continue
			}
			hint := ""
			if strings.HasPrefix(name, "-") {
				hint = "；remove 列表里直接写包名即可，`-` 前缀是 ImageBuilder 的 PACKAGES 语法，由本工具在最后拼上"
			}
			ps = ps.Errorf("packages.name", "包名 %q 非法%s", name, hint)
		}
	}

	inRemove := make(map[string]bool, len(remove))
	for _, name := range remove {
		inRemove[name] = true
	}
	for _, name := range add {
		if inRemove[name] {
			ps = ps.Errorf("packages.conflict", "%q 同时出现在 add 与 remove 里", name)
		}
	}
	return ps
}

func firstDuplicate(items []string) string {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item] {
			return item
		}
		seen[item] = true
	}
	return ""
}
