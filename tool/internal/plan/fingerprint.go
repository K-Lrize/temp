// Package plan 回答「这次改动到底要不要重新构建」。
//
// 判定依据是内容寻址指纹，严格分三层依赖：
//
//	line    → 源码基线（line.yaml + overlay/patches/config + upstream commit）
//	feed    → 自有软件包（feed/ 整棵树 + 上层 line 指纹）
//	variant → 固件（device.yaml + overlay 各层 + 实际引用的包集 + 最终包列表
//	          + 上面两层指纹）
//
// 上层输入一变，指纹链条自动把变化传导到依赖它的下层，不需要在任何地方
// 显式列举「改了 A 就要连带重建 B」这类规则——那种规则一定会漏。
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

// Fingerprints 是一个 variant 的三层指纹。
type Fingerprints struct {
	// LineTree 是 line 目录树的哈希，**不含** upstream commit。
	//
	// 单独暴露是为了让 build.json 把「我们自己配置改的」与「源码改的」分成
	// 两个字段存——排障时一眼能区分是哪一边动了。
	LineTree string `json:"line_tree"`
	Line     string `json:"line"`
	Feed     string `json:"feed"`
	Variant  string `json:"variant"`
}

// Computer 按需计算指纹，并缓存已经算过的目录树哈希。
//
// 缓存不是优化：feed/ 整棵树会被每个 variant 用到，没有缓存就要为每台设备
// 重复遍历一遍全部源码。
type Computer struct {
	root  string
	cache map[string]string
}

func NewComputer(root string) *Computer {
	return &Computer{root: root, cache: map[string]string{}}
}

// For 算出一个 variant 的三层指纹。
func (c *Computer) For(cfg *config.Config, v resolve.Variant) (Fingerprints, error) {
	lineDir := path.Join("lines", v.Line.ID)
	lineTree, err := c.hashPaths(
		path.Join(lineDir, "line.yaml"),
		path.Join(lineDir, "overlay"),
		path.Join(lineDir, "patches"),
		path.Join(lineDir, "config"),
	)
	if err != nil {
		return Fingerprints{}, err
	}

	// upstream commit 是源码基线的另一半，但它不在磁盘上——它是 line.yaml
	// 里的一个字段，已经被 lineTree 覆盖。这里再显式拼一次是为了让
	// build.json 能用同样的方式从两个独立字段重新组合出 Line 指纹，
	// 而不必让远端也存一份组合后的字符串。
	upstreamCommit := ""
	if v.Line.Source != nil {
		upstreamCommit = v.Line.Source.Commit
	}
	lineFP := combine(lineTree, upstreamCommit)

	feedTree, err := c.hashPaths("feed")
	if err != nil {
		return Fingerprints{}, err
	}
	feedFP := combine(feedTree, lineFP)

	// 只把这台设备**实际 include 的**包集计入。
	//
	// 上一代在这里哈希整棵 packages/ 树，结果是改一个没有任何设备引用的
	// 东西也会触发全设备重建。引入包集之后这个洞会立刻变成日常痛点。
	device, ok := cfg.Devices[v.Device]
	if !ok {
		return Fingerprints{}, fmt.Errorf("设备 %q 不存在", v.Device)
	}
	deviceDir := path.Join("devices", v.Device)
	paths := []string{
		path.Join(deviceDir, "device.yaml"),
		path.Join(deviceDir, "files"),
		path.Join(deviceDir, "files-gen"),
		// 仓库根的 overlay 层与构建期脚本对所有设备生效
		"files",
		"files-gen",
	}
	for _, setName := range device.Packages.Include {
		paths = append(paths, path.Join("sets", setName+".yaml"))
	}
	deviceTree, err := c.hashPaths(paths...)
	if err != nil {
		return Fingerprints{}, err
	}

	// 最终包列表单独参与：它是合并后的结果，不等于任何一份输入文件的内容。
	variantFP := combine(
		deviceTree,
		hashString(strings.Join(v.Packages, " ")),
		lineFP,
		feedFP,
	)

	return Fingerprints{LineTree: lineTree, Line: lineFP, Feed: feedFP, Variant: variantFP}, nil
}

// hashPaths 对一组相对仓库根的路径求内容哈希。
//
// 只含相对路径名、文件内容与可执行位——不含 mtime、属主、其余权限位，
// 否则同一份代码在两台机器上会算出不同指纹。路径不存在按空处理：
// line 的 overlay/patches/config、设备的 files/ 都是可选的。
func (c *Computer) hashPaths(paths ...string) (string, error) {
	var lines []string
	for _, p := range paths {
		if cached, ok := c.cache[p]; ok {
			lines = append(lines, cached)
			continue
		}
		one, err := c.hashOne(p)
		if err != nil {
			return "", err
		}
		c.cache[p] = one
		lines = append(lines, one)
	}
	// 先各自算好再排序：调用方传入的顺序不该影响结果。
	sort.Strings(lines)
	return hashString(strings.Join(lines, "\n")), nil
}

func (c *Computer) hashOne(rel string) (string, error) {
	full := filepath.Join(c.root, rel)
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return rel + " -", nil
	}
	if err != nil {
		return "", err
	}

	if !info.IsDir() {
		sum, err := hashFile(full, info)
		if err != nil {
			return "", err
		}
		return rel + " " + sum, nil
	}

	var entries []string
	err = filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		sub, err := filepath.Rel(full, p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		sum, err := hashFile(p, fi)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(sub)+" "+sum)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("哈希目录 %s: %w", rel, err)
	}
	sort.Strings(entries)
	return rel + " " + hashString(strings.Join(entries, "\n")), nil
}

// hashFile 把内容与可执行位一起算进去。
//
// 可执行位是实质内容：overlay 里的脚本从不可执行变成可执行，固件的行为
// 就变了。其余权限位与属主不算——它们在 git 里本来就不被记录。
func hashFile(path string, info fs.FileInfo) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	exec := "-"
	if info.Mode().Perm()&0o111 != 0 {
		exec = "x"
	}
	return exec + hex.EncodeToString(h.Sum(nil)), nil
}

// combine 把若干段拼成一个指纹。用 ":" 分隔而不是直接连接，避免
// ("ab","c") 与 ("a","bc") 撞出同一个值。
func combine(parts ...string) string {
	return hashString(strings.Join(parts, ":"))
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
