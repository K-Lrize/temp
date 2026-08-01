// Package gc 是引用计数回收的纯判定逻辑：给定"现存哪些对象"与"哪些还活着"，
// 算出保留/删除集合。只做集合运算，不发网络、不删任何东西——真正的 R2
// 枚举/拉取/删除在 cmd/wrt 的 gc 命令里，那层是 I/O，靠对真实 R2 的集成验证；
// 这里的判定本身完全离线可测。
//
// 引用计数取代了旧仓库的三套启发式（目录序 Top-N + kmod 按 mtime Top-N + 宽限
// 期）：那些判据都可能在"设备固件还在保留期内、但它引用的 build/kmod 恰好不在
// mtime Top-N 里"时误删。引用计数没有这个问题——只要有一个活的 release 或
// current.json 指向它，它就活着，与新旧、mtime 无关。
package gc

import "sort"

// TopN 返回按字典序从新到旧的前 n 个 id。release_id / build_id 形如
// <utc>-<run>-<sha7>，字典序即时间序，不需要额外的时间字段。
func TopN(ids []string, n int) []string {
	s := append([]string(nil), ids...)
	sort.Sort(sort.Reverse(sort.StringSlice(s)))
	if n >= 0 && n < len(s) {
		s = s[:n]
	}
	return s
}

// LiveReleaseIDs 是单个 (device, line) 的存活 release 集合 = 最新 keepN 个
// ∪ 被 pin 且确实存在的。pin 一个已不存在的 id 会被静默忽略——不该让整个
// GC 判定因为一条失效的 pin 而失败。
func LiveReleaseIDs(all []string, keepN int, pinned []string) []string {
	allSet := make(map[string]bool, len(all))
	for _, id := range all {
		allSet[id] = true
	}
	live := make(map[string]bool)
	for _, id := range TopN(all, keepN) {
		live[id] = true
	}
	for _, p := range pinned {
		if allSet[p] {
			live[p] = true
		}
	}
	return sortedKeys(live)
}

// Entry 是一个待判定存活性的对象。Key 用于和存活集合比对，Path 是它在 R2 上
// 待删除的路径（前缀）。
type Entry struct {
	Key  string
	Path string
}

// Classify 把 existing 按 liveKeys 分成保留 / 删除两组（各返回 Path）。
func Classify(existing []Entry, liveKeys []string) (keep, del []string) {
	live := make(map[string]bool, len(liveKeys))
	for _, k := range liveKeys {
		live[k] = true
	}
	for _, e := range existing {
		if live[e.Key] {
			keep = append(keep, e.Path)
		} else {
			del = append(del, e.Path)
		}
	}
	return keep, del
}

// OverThreshold 报告"计划删除的比例是否超过阈值百分比"。total==0 视为安全
// （无对象可删，不构成熔断）。这是防 GC bug（活跃集合算错、枚举半途失败返回
// 空列表）一次删光大半存量的最后一道闸。
func OverThreshold(total, deleteCount, thresholdPct int) bool {
	if total == 0 {
		return false
	}
	return deleteCount*100/total > thresholdPct
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
