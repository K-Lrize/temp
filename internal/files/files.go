// Package files 组装设备的 rootfs overlay。
//
// 「合并文件层」与「执行构建期脚本」是一件事而不是两件：它们必须按固定顺序
// 发生（脚本加工的正是刚合并出来的那棵树），拆成两个入口只会让每个调用方都
// 得记得配对调用，忘一次就产出一份少了内容却看着正常的固件。
//
// 两类「脚本」不要混淆：
//   - 运行时脚本（etc/uci-defaults/*）：内容是 shell，但以**文件**形式打进
//     固件，设备开机时执行。归 overlay 层，这里只当普通文件复制——所以可
//     执行位必须原样保留。
//   - 构建期脚本（files-gen/*.sh）：在构建机上执行，对刚合并出来的 overlay
//     做纯加工（生成文件、改权限、按模板展开）。它是一个哑执行器——只知道
//     overlay 目录在哪，不知道自己在给哪个 variant 干活。
//
// 要往 rootfs 放「有版本、要追更新」的第三方代码（如 zsh 插件），走 feed/apk，
// 不要在这里 fetch——构建期拉取会绕过内容寻址，plan 看不见它的变化，会一边
// 报「无需重建」一边让固件内容悄悄漂移。
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
	dirGen   = "files-gen"
	genExt   = ".sh"
)

// Assemble 把有序的 overlay 层合并进 dest，然后按同样的顺序执行 files-gen 脚本。
//
// 层序（后者覆盖前者同路径文件）：
//
//	<root>/files/                        所有设备共用
//	<root>/devices/<device>/files/       本设备
//
// 脚本同序：`<root>/files-gen/*.sh` 然后 `<root>/devices/<device>/files-gen/*.sh`，
// 各自按文件名字典序。执行时 cwd 为仓库根（脚本按相对路径读仓库内文件），
// overlay 目录经 WRT_FILES_DIR 注入。
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

	env := genEnv(absDest)
	for _, dir := range []string{
		filepath.Join(root, dirGen),
		filepath.Join(deviceDir, dirGen),
	} {
		if err := runGen(root, dir, env); err != nil {
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

// genEnv 是注入给 files-gen 脚本的唯一事实：overlay 目录在哪。
//
// 刻意只给这一个。脚本一旦要读 variant / device / 包列表来做分支，那就是在做
// 本该在 Go / config 里做的决定——把上下文砍到只剩「往哪写」，这个口子就焊死。
// 仓库根不进环境：cmd.Dir 已经是它，脚本按相对路径读即可。加 WRT_ 前缀是因为
// FILES_DIR 这类名字在 shell 环境里太通用，容易和上游脚本或 CI 注入的变量撞车。
func genEnv(dest string) []string {
	return append(os.Environ(), "WRT_FILES_DIR="+dest)
}

func runGen(root, dir string, env []string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 files-gen 目录 %s: %w", dir, err)
	}

	var scripts []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), genExt) {
			scripts = append(scripts, e.Name())
		}
	}
	sort.Strings(scripts)

	for _, name := range scripts {
		path := filepath.Join(dir, name)
		// 显式用 bash 跑而不依赖 shebang 与可执行位：脚本从 git 检出后权限位
		// 在不同平台上未必一致，靠它决定跑不跑会时灵时不灵。
		cmd := exec.Command("bash", path)
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdout = os.Stderr // 脚本的输出是构建日志，不能污染 stdout 上的 JSON
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("files-gen 脚本 %s 失败: %w", name, err)
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
