package feed

import "strings"

// CustomFeedName 是注入 SDK 的自有 feed 名。它同时决定产物路径
// bin/packages/<arch>/<CustomFeedName>/，收产物那步按这个名字找 .apk。
const CustomFeedName = "custom_feed"

// MergeFeedsConf 把仓库的 pin 列表覆盖进 SDK 自带的 feeds.conf.default，并追加
// 一条指向自有 feed 的 src-link，返回合并后的完整内容。
//
// 为什么要 pin：SDK 自带的 feeds.conf.default 只把外部 feed pin 到分支
// （openwrt-25.12），同一个 commit 隔天构建时该分支已推进，golang 之类的依赖
// 版本会漂移，构建不可复现。pin 列表（feed/feeds.conf）按 feed 名整行覆盖同名
// 条目，把它钉到具体 commit；未列出的 feed（routing/telephony）保持 SDK 原样。
//
// 这是上一代 pin-feeds.sh 的等价物，但按字段匹配而非 sed 正则——feed 名匹配的
// 是「第二个字段」这件事在这里是显式的，不靠一串反斜杠维持。
//
// 匹配规则与 scripts/feeds 一致：一行 `src-<kind> <name> <url...>`，name 是第二
// 个字段。pin 行、src-link 行都按 name 覆盖同名的既有行，没有则追加到末尾。
func MergeFeedsConf(sdkDefault, pins, customPath string) string {
	lines := splitLines(sdkDefault)

	// index 把 feed 名映射到它在 lines 里的下标，供整行覆盖。
	index := make(map[string]int, len(lines))
	for i, ln := range lines {
		if name, ok := feedName(ln); ok {
			index[name] = i
		}
	}

	apply := func(name, newLine string) {
		if i, ok := index[name]; ok {
			lines[i] = newLine
			return
		}
		index[name] = len(lines)
		lines = append(lines, newLine)
	}

	for _, ln := range splitLines(pins) {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, ok := feedName(ln)
		if !ok {
			continue
		}
		apply(name, trimmed)
	}

	apply(CustomFeedName, "src-link "+CustomFeedName+" "+customPath)

	return strings.Join(lines, "\n") + "\n"
}

// feedName 取一行的 feed 名（第二个字段），要求第一个字段形如 src-<kind>。
// 非 feed 行（注释、空行、其它）返回 ok=false。
func feedName(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "src-") {
		return "", false
	}
	return fields[1], true
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
