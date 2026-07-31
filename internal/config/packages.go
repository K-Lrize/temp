package config

import "github.com/K-Lrize/openwrt-build/internal/diag"

// Layer 是参与合并的一层包清单。Name 只用于报错定位（"set:base-router"、
// "device:mt3600be"）——五层合并之后，只说「dnsmasq 冲突」等于让人挨个翻文件。
type Layer struct {
	Name string
	Spec PackageSpec
}

// Packages 是合并后的最终包清单。
type Packages struct {
	Add    []string
	Remove []string
}

// List 产出 ImageBuilder `make image PACKAGES=` 需要的形式：装的包直接写，
// 卸的包带 "-" 前缀且一律排在最后。
func (p Packages) List() []string {
	out := make([]string, 0, len(p.Add)+len(p.Remove))
	out = append(out, p.Add...)
	for _, name := range p.Remove {
		out = append(out, "-"+name)
	}
	return out
}

// MergePackages 把有序的层列表合并成最终包清单。
//
// 规则（这是全系统最容易出错的一处，所以定死）：
//
//	add    = 各层 .add 按序并集，去重后保留首次出现的位置
//	remove = 各层 .remove 并集，同样去重
//	冲突   = 某个包同时落进最终 add 与最终 remove -> 报错，要求显式解决
//
// 刻意不做「后面的层覆盖前面的」：那种规则在五层之后没人能预测结果，而赌错的
// 代价是设备上少一个或多一个包，要刷机之后才发现。真需要 device 撤销某个 set
// 的 remove 时，再加一个显式的 force_add 字段，而不是让覆盖悄悄发生。
//
// 去重保留首次出现的位置而不是最后一次：包列表顺序参与固件指纹，靠后去重会让
// 「往某个 set 里加一个别处已有的包」凭空改变全部引用它的设备的指纹，触发一次
// 无意义的全量重建。
func MergePackages(layers []Layer) (Packages, diag.Problems) {
	var (
		ps     diag.Problems
		result Packages
		// 记住每个包名第一次是被哪一层引入的，冲突时才能点名两边。
		addedBy   = map[string]string{}
		removedBy = map[string]string{}
	)

	for _, l := range layers {
		for _, name := range l.Spec.Add {
			if _, seen := addedBy[name]; seen {
				continue
			}
			addedBy[name] = l.Name
			result.Add = append(result.Add, name)
		}
		for _, name := range l.Spec.Remove {
			if _, seen := removedBy[name]; seen {
				continue
			}
			removedBy[name] = l.Name
			result.Remove = append(result.Remove, name)
		}
	}

	// 按 Add 的顺序检查冲突，保证报错顺序稳定（map 迭代顺序是随机的）。
	for _, name := range result.Add {
		if remover, conflict := removedBy[name]; conflict {
			ps = ps.Errorf("packages.conflict",
				"%q 被 %s 装上又被 %s 卸掉：合并不做「后面的层覆盖前面的」，"+
					"请改掉其中一层，或者这台设备不要 include 那个包集",
				name, addedBy[name], remover)
		}
	}

	return result, ps
}
