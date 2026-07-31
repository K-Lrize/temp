// Package resolve 把配置树展开成 variant——构建的最小单位。
//
// 全系统只有这里产出 Variant，下游（plan、repos、CI 的每个 job）一律消费它，
// 没有任何一方自己回去读 YAML。上一代最贵的教训就是让多处各自解析配置：
// 同一个字段在三个脚本里有三种默认值，谁也不知道构建时实际用的是哪个。
package resolve

import (
	"fmt"
	"path"
	"strings"

	"github.com/K-Lrize/openwrt-build/internal/config"
)

// Separator 分隔 variant id 的两段：<device>@<line>。
const Separator = "@"

// LineFacts 是一条 line 在 variant 里的投影。
//
// 刻意复制而不是引用 config.Line：Variant 会被序列化成 JSON 随固件一起发布，
// 「这份固件是用什么配置编出来的」必须能脱离仓库回答。
type LineFacts struct {
	ID            string           `json:"id"`
	Upstream      string           `json:"upstream"`
	Artifacts     config.Artifacts `json:"artifacts"`
	RequiresBuild bool             `json:"requires_build"`
	Source        *config.Source   `json:"source,omitempty"`
}

// Variant 是 device × line 的展开结果，构建的最小单位。
type Variant struct {
	ID       string            `json:"id"`
	Device   string            `json:"device"`
	Line     LineFacts         `json:"line"`
	Hardware config.Hardware   `json:"hardware"`
	Metadata map[string]string `json:"metadata,omitempty"`
	// Packages 已经是 ImageBuilder 的 PACKAGES 形式（卸载项带 - 前缀）。
	Packages   []string     `json:"packages"`
	Image      config.Image `json:"image"`
	ExtraRepos []string     `json:"extra_repos,omitempty"`
}

// MakeID 拼 variant id。
func MakeID(device, line string) string { return device + Separator + line }

// ParseID 拆 variant id。
func ParseID(id string) (device, line string, err error) {
	device, line, found := strings.Cut(id, Separator)
	switch {
	case !found:
		return "", "", fmt.Errorf("variant id %q 缺少 %q 分隔符，应形如 <device>%s<line>", id, Separator, Separator)
	case device == "" || line == "":
		return "", "", fmt.Errorf("variant id %q 的设备名或 line 为空", id)
	case strings.Contains(line, Separator):
		return "", "", fmt.Errorf("variant id %q 含多个 %q 分隔符", id, Separator)
	}
	return device, line, nil
}

// All 展开整棵配置树的全部 variant。
//
// 顺序确定：设备名字典序，同一设备内按 device.yaml 里 lines 的声明顺序
// （第一条是主线）。CI 矩阵和 golden 基线都靠这个顺序稳定。
func All(cfg *config.Config) ([]Variant, config.Problems) {
	var (
		variants []Variant
		ps       config.Problems
	)
	for _, name := range cfg.SortedDeviceNames() {
		device := cfg.Devices[name]
		for _, lineID := range device.Lines {
			v, one := build(cfg, device, lineID)
			ps = append(ps, one...)
			if one.HasError() {
				continue
			}
			variants = append(variants, v)
		}
	}
	return variants, ps
}

// One 解析单个 variant。id 指向不存在的设备/line，或指向一个这台设备并未
// 声明的 line，都是错误——后者尤其重要：它意味着调用方以为存在一份根本
// 不会被构建的固件。
func One(cfg *config.Config, id string) (Variant, error) {
	deviceName, lineID, err := ParseID(id)
	if err != nil {
		return Variant{}, err
	}

	device, ok := cfg.Devices[deviceName]
	if !ok {
		return Variant{}, fmt.Errorf("设备 %q 不存在", deviceName)
	}
	declared := false
	for _, l := range device.Lines {
		if l == lineID {
			declared = true
			break
		}
	}
	if !declared {
		return Variant{}, fmt.Errorf("设备 %q 没有声明 line %q（当前声明的是 %v）", deviceName, lineID, device.Lines)
	}

	v, ps := build(cfg, device, lineID)
	if ps.HasError() {
		return Variant{}, fmt.Errorf("解析 %s 失败：\n%s", id, ps)
	}
	return v, nil
}

func build(cfg *config.Config, device config.Device, lineID string) (Variant, config.Problems) {
	var ps config.Problems
	deviceSource := path.Join("devices", device.Name, "device.yaml")

	line, ok := cfg.Lines[lineID]
	if !ok {
		one := config.Problems(nil).Errorf("variant.line-ref", "设备 %s 引用的 line %q 不存在", device.Name, lineID)
		return Variant{}, one.WithSource(deviceSource)
	}

	layers := make([]config.Layer, 0, len(device.Packages.Include)+1)
	for _, setName := range device.Packages.Include {
		set, ok := cfg.Sets[setName]
		if !ok {
			ps = ps.Errorf("variant.set-ref", "设备 %s include 的包集 %q 不存在", device.Name, setName)
			continue
		}
		layers = append(layers, config.Layer{
			Name: "set:" + setName,
			Spec: config.PackageSpec{Add: set.Add, Remove: set.Remove},
		})
	}
	layers = append(layers, config.Layer{Name: "device:" + device.Name, Spec: device.Packages})

	merged, mergeProblems := config.MergePackages(layers)
	ps = append(ps, mergeProblems...)
	ps = ps.WithSource(deviceSource)
	if ps.HasError() {
		return Variant{}, ps
	}

	return Variant{
		ID:     MakeID(device.Name, lineID),
		Device: device.Name,
		Line: LineFacts{
			ID:            line.ID,
			Upstream:      line.Upstream,
			Artifacts:     line.Artifacts,
			RequiresBuild: line.RequiresBuild,
			Source:        line.Source,
		},
		Hardware:   device.Hardware,
		Metadata:   device.Metadata,
		Packages:   merged.List(),
		Image:      device.Image,
		ExtraRepos: device.Repos,
	}, ps
}
