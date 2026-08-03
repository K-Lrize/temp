// Package manifest 解析 OpenWrt 镜像的 .manifest——ImageBuilder 每次 make image
// 都会连带产出一份，逐行列出这个镜像里实际装了哪些包。firmware 阶段的回归门禁
// 靠它做「与上一个 release 比，有包消失即 fail」：某个包上次在、这次没了，多半是
// IB 阶段某条件依赖静默失效（漏依赖那类坑不会报错，只会悄悄少装一个包）。
package manifest

import (
	"sort"
	"strings"
)

// Names 取出 .manifest 里的全部包名（去重、排序）。每行的首个空白分隔字段即包名
// ——包名恒在行首，后面跟版本（历史上 opkg 用 `name - version`、apk 用
// `name version`，取首字段对两者都成立）。空行与 # 注释跳过。
func Names(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := strings.Fields(line)[0]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Disappeared 返回上一版有、这一版没有的包名（排序后）。只看包名的存在与否，
// 不管版本变化——门禁要拦的是「包整个掉了」，升降级是正常的。
func Disappeared(prev, curr string) []string {
	present := map[string]bool{}
	for _, n := range Names(curr) {
		present[n] = true
	}
	var gone []string
	for _, n := range Names(prev) {
		if !present[n] {
			gone = append(gone, n)
		}
	}
	sort.Strings(gone)
	return gone
}
