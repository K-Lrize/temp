// Package files 组装设备的 rootfs overlay。
//
// 「合并文件层」与「执行构建期钩子」是一件事而不是两件：它们必须按固定顺序
// 发生（钩子加工的正是刚合并出来的那棵树），拆成两个入口只会让每个调用方都
// 得记得配对调用，忘一次就产出一份少了内容却看着正常的固件。
//
// 两类「脚本」不要混淆：
//   - 运行时脚本（etc/uci-defaults/*）：内容是 shell，但以**文件**形式打进
//     固件，设备开机时执行。归 overlay 层，这里只当普通文件复制——所以可
//     执行位必须原样保留。
//   - 构建期钩子（files-hooks/*.sh）：在构建机上执行，加工 overlay 本身
//     （例如按需拉 zsh 插件）。由本包执行，可读下面注入的 WRT_* 变量。
package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

const (
	dirFiles = "files"
	dirHooks = "files-hooks"
	hookExt  = ".sh"
)

// Assemble 把有序的 overlay 层合并进 dest，然后按同样的顺序执行构建期钩子。
//
// 层序（后者覆盖前者同路径文件）：
//
//	<root>/files/                        所有设备共用
//	<root>/devices/<device>/files/       本设备
//
// 钩子同序：`<root>/files-hooks/*.sh` 然后 `<root>/devices/<device>/files-hooks/*.sh`，
// 各自按文件名字典序。
//
// dest 必须不存在或为空：就地叠加到有残留的目录，会把上一次构建的文件悄悄
// 打进这次的固件。
func Assemble(root string, v resolve.Variant, dest string) error {
	if err := prepareDest(dest); err != nil {
		return err
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	deviceDir := filepath.Join(root, "devices", v.Device)
	for _, layer := range []string{
		filepath.Join(root, dirFiles),
		filepath.Join(deviceDir, dirFiles),
	} {
		if err := copyTree(layer, absDest); err != nil {
			return fmt.Errorf("合并 overlay 层 %s: %w", layer, err)
		}
	}

	env := hookEnv(root, absDest, v)
	for _, dir := range []string{
		filepath.Join(root, dirHooks),
		filepath.Join(deviceDir, dirHooks),
	} {
		if err := runHooks(root, dir, env); err != nil {
			return err
		}
	}
	return nil
}

func prepareDest(dest string) error {
	entries, err := os.ReadDir(dest)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.MkdirAll(dest, 0o755)
	case err != nil:
		return fmt.Errorf("检查目标目录 %s: %w", dest, err)
	case len(entries) > 0:
		return fmt.Errorf("目标目录 %s 非空：就地叠加会把上一次构建的残留打进这次的固件，请换一个空目录或先清空", dest)
	}
	return nil
}

// hookEnv 是注入给构建期钩子的事实。
//
// 一律加 WRT_ 前缀：DEVICE、PACKAGES 这类名字在 shell 环境里太通用，和
// 上游脚本或 CI 注入的变量撞车时，症状是钩子按别人的值干活。
func hookEnv(root, dest string, v resolve.Variant) []string {
	return append(os.Environ(),
		"WRT_ROOT="+root,
		"WRT_FILES_DIR="+dest,
		"WRT_VARIANT="+v.ID,
		"WRT_DEVICE="+v.Device,
		"WRT_LINE="+v.Line.ID,
		// 钩子不该自己再解析一遍配置——包列表在这里已经合并好了。
		"WRT_PACKAGES="+strings.Join(v.Packages, " "),
	)
}

func runHooks(root, dir string, env []string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取钩子目录 %s: %w", dir, err)
	}

	var scripts []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), hookExt) {
			scripts = append(scripts, e.Name())
		}
	}
	sort.Strings(scripts)

	for _, name := range scripts {
		path := filepath.Join(dir, name)
		// 显式用 bash 跑而不依赖 shebang 与可执行位：钩子从 git 检出后
		// 权限位在不同平台上未必一致，靠它决定跑不跑会时灵时不灵。
		cmd := exec.Command("bash", path)
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdout = os.Stderr // 钩子的输出是构建日志，不能污染 stdout 上的 JSON
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("构建期钩子 %s 失败: %w", name, err)
		}
	}
	return nil
}

// copyTree 把 src 整棵树复制进 dst，保留权限位与符号链接。
// src 不存在时静默跳过——overlay 层都是可选的。
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s 不是目录", src)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			// 保留链接而不是解引用：rootfs 里的符号链接是有意义的内容，
			// 解引用会把一个链接变成一份副本。
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target) // 后层覆盖前层
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			return copyFile(path, target)
		default:
			// 设备节点、FIFO 之类不该出现在 git 里。静默跳过会让固件少东西
			// 而毫无迹象，所以直接报错。
			return fmt.Errorf("%s 是不支持的文件类型 %v", path, d.Type())
		}
	})
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// 先删再建：后层覆盖前层时，若前层文件是只读的，直接写会失败。
	_ = os.Remove(dst)
	// 权限位原样带过去——uci-defaults 里的脚本不可执行就等于没写，
	// 设备开机不会跑它，而固件看起来一切正常。
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
