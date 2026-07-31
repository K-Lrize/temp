package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// 配置树的固定布局。三者都是可选的——刚 init 完还没加设备是合法中间状态。
const (
	dirLines   = "lines"
	dirDevices = "devices"
	dirSets    = "sets"

	fileLine   = "line.yaml"
	fileDevice = "device.yaml"
)

// sourceDirs 是「源码相对官方有实质修改」的三个证据目录，用于派生
// Line.RequiresBuild。
var sourceDirs = []string{"overlay", "patches", "config"}

// Config 是一整棵配置树载入内存后的样子。
type Config struct {
	Root    string
	Lines   map[string]Line
	Devices map[string]Device
	Sets    map[string]Set
}

// Load 读入 root 下的整棵配置树并做全部校验。
//
// 返回值分工：error 只用于「连目录都读不了」这类环境问题；配置本身的毛病
// 一律走 Problems，这样一次运行能报出全部问题，而不是修一条跑一遍。
// 单个 YAML 文件解析失败也算 Problems——一个文件写坏不该让其余文件失去校验。
func Load(root string) (*Config, Problems, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, fmt.Errorf("读取配置根目录 %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("配置根目录 %s 不是目录", root)
	}

	cfg := &Config{
		Root:    root,
		Lines:   map[string]Line{},
		Devices: map[string]Device{},
		Sets:    map[string]Set{},
	}
	var ps Problems

	more, err := cfg.loadLines()
	if err != nil {
		return nil, nil, err
	}
	ps = append(ps, more...)

	more, err = cfg.loadSets()
	if err != nil {
		return nil, nil, err
	}
	ps = append(ps, more...)

	more, err = cfg.loadDevices()
	if err != nil {
		return nil, nil, err
	}
	ps = append(ps, more...)

	return cfg, append(ps, cfg.validateCrossFile()...), nil
}

// SortedLineIDs / SortedDeviceNames / SortedSetNames 给调用方一个确定的遍历
// 顺序。map 迭代顺序随机，直接拿去生成矩阵或 golden 会让输出在两次运行之间抖。
func (c *Config) SortedLineIDs() []string     { return sortedKeys(c.Lines) }
func (c *Config) SortedDeviceNames() []string { return sortedKeys(c.Devices) }
func (c *Config) SortedSetNames() []string    { return sortedKeys(c.Sets) }

func (c *Config) loadLines() (Problems, error) {
	var ps Problems
	entries, err := readDirNames(filepath.Join(c.Root, dirLines))
	if err != nil {
		return nil, err
	}
	for _, id := range entries {
		rel := path.Join(dirLines, id, fileLine)
		var line Line
		if problem := decodeFile(filepath.Join(c.Root, rel), &line); problem != nil {
			ps = append(ps, problem.WithSource(rel)...)
			continue
		}

		requiresBuild, err := lineHasSourceChanges(filepath.Join(c.Root, dirLines, id))
		if err != nil {
			return nil, err
		}
		line.RequiresBuild = requiresBuild

		var one Problems
		if line.ID != id {
			one = one.errorf("line.id-path", "line.id %q 与目录名 %q 不符：目录名同时是 R2 命名空间前缀，两者必须一致", line.ID, id)
		}
		one = append(one, line.Validate()...)
		ps = append(ps, one.WithSource(rel)...)
		c.Lines[id] = line
	}
	return ps, nil
}

func (c *Config) loadSets() (Problems, error) {
	var ps Problems
	dir := filepath.Join(c.Root, dirSets)
	files, err := readDirNames(dir)
	if err != nil {
		return nil, err
	}
	for _, name := range files {
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(name, ".yaml")
		rel := path.Join(dirSets, name)

		var set Set
		if problem := decodeFile(filepath.Join(dir, name), &set); problem != nil {
			ps = append(ps, problem.WithSource(rel)...)
			continue
		}
		var one Problems
		if set.Name != id {
			one = one.errorf("set.name-path", "set.name %q 与文件名 %q 不符", set.Name, name)
		}
		one = append(one, set.Validate()...)
		ps = append(ps, one.WithSource(rel)...)
		c.Sets[id] = set
	}
	return ps, nil
}

func (c *Config) loadDevices() (Problems, error) {
	var ps Problems
	entries, err := readDirNames(filepath.Join(c.Root, dirDevices))
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		rel := path.Join(dirDevices, name, fileDevice)
		var device Device
		if problem := decodeFile(filepath.Join(c.Root, rel), &device); problem != nil {
			ps = append(ps, problem.WithSource(rel)...)
			continue
		}
		var one Problems
		if device.Name != name {
			one = one.errorf("device.name-path", "device.name %q 与目录名 %q 不符", device.Name, name)
		}
		one = append(one, device.Validate()...)
		ps = append(ps, one.WithSource(rel)...)
		c.Devices[name] = device
	}
	return ps, nil
}

// validateCrossFile 查只有看到整棵树才能判定的规则。
func (c *Config) validateCrossFile() Problems {
	var ps Problems

	usedLines := map[string]bool{}
	usedSets := map[string]bool{}

	for _, id := range c.SortedLineIDs() {
		line := c.Lines[id]
		// 有 overlay/patches/config 却借官方产物：官方那边不可能有你改过的东西，
		// 这份配置永远编不出你想要的结果，而且不会有任何运行期迹象。
		if line.RequiresBuild && line.Artifacts == ArtifactsOfficial {
			var one Problems
			one = one.errorf("line.requires-build",
				"line %s 目录下有 %s 之一（源码相对官方有实质修改），却声明 artifacts=%s："+
					"官方发布产物里不会包含你的改动",
				id, strings.Join(sourceDirs, "/"), ArtifactsOfficial)
			ps = append(ps, one.WithSource(path.Join(dirLines, id, fileLine))...)
		}
	}

	for _, name := range c.SortedDeviceNames() {
		device := c.Devices[name]
		rel := path.Join(dirDevices, name, fileDevice)
		var one Problems

		for _, lineID := range device.Lines {
			if _, ok := c.Lines[lineID]; ok {
				usedLines[lineID] = true
				continue
			}
			one = one.errorf("device.lines-ref", "引用的 line %q 不存在（应为 %s/%s/%s）", lineID, dirLines, lineID, fileLine)
		}
		for _, setName := range device.Packages.Include {
			if _, ok := c.Sets[setName]; ok {
				usedSets[setName] = true
				continue
			}
			one = one.errorf("device.include-ref", "include 的包集 %q 不存在（应为 %s/%s.yaml）", setName, dirSets, setName)
		}
		ps = append(ps, one.WithSource(rel)...)
	}

	// 死配置：能通过全部校验、能被人读到，但永远不生效。只 warn——刚建好还没
	// 接设备是正常的中间状态。
	for _, id := range c.SortedLineIDs() {
		if usedLines[id] {
			continue
		}
		var one Problems
		one = one.warnf("line.unreferenced", "line %s 没有被任何设备引用", id)
		ps = append(ps, one.WithSource(path.Join(dirLines, id, fileLine))...)
	}
	for _, name := range c.SortedSetNames() {
		if usedSets[name] {
			continue
		}
		var one Problems
		one = one.warnf("set.unreferenced", "包集 %s 没有被任何设备 include", name)
		ps = append(ps, one.WithSource(path.Join(dirSets, name+".yaml"))...)
	}

	return ps
}

// decodeFile 以严格模式解析 YAML：出现类型里没有的字段直接报错。
//
// 静默忽略未知字段等于「配置写了等于没写」——上一代 device.yaml 的 channel:
// 字段在改名之后如果被静默吞掉，设备会安静地落到错误的版本线上。
func decodeFile(fullPath string, out any) Problems {
	f, err := os.Open(fullPath)
	if err != nil {
		var one Problems
		return one.errorf("yaml", "打开失败：%v", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		var one Problems
		return one.errorf("yaml", "解析失败：%v", err)
	}
	return nil
}

// lineHasSourceChanges 判断 lines/<id>/ 下三个证据目录里有没有真实文件。
// 空目录不算——建了目录还没往里放东西是常见的中间状态。
func lineHasSourceChanges(lineDir string) (bool, error) {
	for _, name := range sourceDirs {
		found, err := dirHasFile(filepath.Join(lineDir, name))
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func dirHasFile(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("扫描 %s: %w", dir, err)
	}
	return found, nil
}

// readDirNames 列出目录内容，目录不存在时返回空列表而非错误。
func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
