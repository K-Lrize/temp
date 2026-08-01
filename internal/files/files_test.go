package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

func testVariant() resolve.Variant {
	return resolve.Variant{
		ID:     "mt3600be@25.12",
		Device: "mt3600be",
		Line:   resolve.LineFacts{ID: "25.12", OpenWrtVersion: "25.12.5", Artifacts: config.ArtifactsOfficial},
		Hardware: config.Hardware{
			Target: "mediatek", Subtarget: "filogic",
			Profile: "glinet_gl-mt3600be", Arch: "aarch64_cortex-a53",
		},
		Packages: []string{"zsh", "luci", "-dnsmasq"},
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDeviceLayerOverridesCommonLayer(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files/etc/banner"), "通用\n", 0o644)
	writeFile(t, filepath.Join(root, "files/root/.zshrc"), "通用 zshrc\n", 0o644)
	writeFile(t, filepath.Join(root, "devices/mt3600be/files/root/.zshrc"), "设备 zshrc\n", 0o644)
	writeFile(t, filepath.Join(root, "devices/mt3600be/files/etc/sysctl.d/99-bbr.conf"), "bbr\n", 0o644)

	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(root, testVariant(), dest); err != nil {
		t.Fatal(err)
	}

	if got := read(t, filepath.Join(dest, "etc/banner")); got != "通用\n" {
		t.Errorf("通用层文件应保留: %q", got)
	}
	if got := read(t, filepath.Join(dest, "root/.zshrc")); got != "设备 zshrc\n" {
		t.Errorf("同路径应由设备层覆盖: %q", got)
	}
	if got := read(t, filepath.Join(dest, "etc/sysctl.d/99-bbr.conf")); got != "bbr\n" {
		t.Errorf("设备层独有文件应存在: %q", got)
	}
}

func TestExecutableBitIsPreserved(t *testing.T) {
	// uci-defaults 里的脚本不可执行就等于没写：设备开机不会跑它，而固件
	// 看起来一切正常。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files/etc/uci-defaults/00-base"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(root, "files/etc/banner"), "hi\n", 0o644)

	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(root, testVariant(), dest); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dest, "etc/uci-defaults/00-base"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("可执行位丢失: %v", info.Mode())
	}
	plain, err := os.Stat(filepath.Join(dest, "etc/banner"))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Mode().Perm()&0o111 != 0 {
		t.Errorf("普通文件不该变成可执行: %v", plain.Mode())
	}
}

func TestSymlinksArePreservedNotDereferenced(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files/etc/real.conf"), "real\n", 0o644)
	if err := os.Symlink("real.conf", filepath.Join(root, "files/etc/link.conf")); err != nil {
		t.Skipf("这个文件系统不支持符号链接: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(root, testVariant(), dest); err != nil {
		t.Fatal(err)
	}

	target, err := os.Readlink(filepath.Join(dest, "etc/link.conf"))
	if err != nil {
		t.Fatalf("符号链接应当原样保留而不是被解引用: %v", err)
	}
	if target != "real.conf" {
		t.Errorf("链接目标 = %q", target)
	}
}

func TestMissingLayersAreFine(t *testing.T) {
	// 还没有任何 overlay 是合法状态。
	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(t.TempDir(), testVariant(), dest); err != nil {
		t.Fatalf("层目录不存在不该报错: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("dest 仍应被创建出来: %v", err)
	}
}

func TestNonEmptyDestIsRejected(t *testing.T) {
	// 就地叠加到一个有残留的目录，会把上一次构建的文件悄悄打进这次的固件。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files/etc/banner"), "hi\n", 0o644)

	dest := t.TempDir()
	writeFile(t, filepath.Join(dest, "leftover"), "旧的\n", 0o644)

	if err := Assemble(root, testVariant(), dest); err == nil {
		t.Fatal("dest 非空时应当报错")
	}
}

func TestGenScriptsRunInOrderAfterFilesAreInPlace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files/etc/banner"), "hi\n", 0o644)
	// 脚本按字典序执行，且执行时 overlay 已经就位——脚本的职责正是加工它。
	writeFile(t, filepath.Join(root, "files-gen/20-second.sh"),
		"#!/usr/bin/env bash\necho -n second >> \"$WRT_FILES_DIR/order\"\n", 0o755)
	writeFile(t, filepath.Join(root, "files-gen/10-first.sh"),
		"#!/usr/bin/env bash\ntest -f \"$WRT_FILES_DIR/etc/banner\" || exit 1\necho -n first >> \"$WRT_FILES_DIR/order\"\n", 0o755)

	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(root, testVariant(), dest); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dest, "order")); got != "firstsecond" {
		t.Errorf("执行顺序 = %q", got)
	}
}

func TestDeviceGenScriptsRunAfterCommonOnes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files-gen/a.sh"),
		"#!/usr/bin/env bash\necho -n common >> \"$WRT_FILES_DIR/order\"\n", 0o755)
	writeFile(t, filepath.Join(root, "devices/mt3600be/files-gen/a.sh"),
		"#!/usr/bin/env bash\necho -n device >> \"$WRT_FILES_DIR/order\"\n", 0o755)

	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(root, testVariant(), dest); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dest, "order")); got != "commondevice" {
		t.Errorf("执行顺序 = %q", got)
	}
}

func TestGenScriptsOnlyGetOverlayDir(t *testing.T) {
	// files-gen 是哑执行器：只拿到 overlay 目录，拿不到 variant 上下文。
	// 要读配置做分支的活不属于这里——那是 Go / config 的活。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files-gen/dump.sh"), `#!/usr/bin/env bash
{
  echo "files_dir=$WRT_FILES_DIR"
  echo "variant=${WRT_VARIANT:-<unset>}"
  echo "device=${WRT_DEVICE:-<unset>}"
  echo "packages=${WRT_PACKAGES:-<unset>}"
} > "$WRT_FILES_DIR/env"
`, 0o755)

	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(root, testVariant(), dest); err != nil {
		t.Fatal(err)
	}

	got := read(t, filepath.Join(dest, "env"))
	if !strings.Contains(got, "files_dir="+dest) {
		t.Errorf("WRT_FILES_DIR 应指向 overlay 目录:\n%s", got)
	}
	// variant / device / 包列表不该被注入——注入它们就是在邀请脚本做决定。
	for _, want := range []string{"variant=<unset>", "device=<unset>", "packages=<unset>"} {
		if !strings.Contains(got, want) {
			t.Errorf("variant 上下文不该出现在 files-gen 环境里，缺少 %q:\n%s", want, got)
		}
	}
}

func TestFailingGenScriptAbortsAssembly(t *testing.T) {
	// 脚本失败被吞掉，会产出一份少了内容却看着正常的 overlay。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files-gen/boom.sh"),
		"#!/usr/bin/env bash\necho 出错了 >&2\nexit 3\n", 0o755)

	dest := filepath.Join(t.TempDir(), "overlay")
	err := Assemble(root, testVariant(), dest)
	if err == nil {
		t.Fatal("脚本失败必须中断组装")
	}
	if !strings.Contains(err.Error(), "boom.sh") {
		t.Errorf("错误信息要点名是哪个脚本: %v", err)
	}
}

func TestNonShellFilesInGenDirAreIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files-gen/README.md"), "不是脚本\n", 0o644)
	dest := filepath.Join(t.TempDir(), "overlay")
	if err := Assemble(root, testVariant(), dest); err != nil {
		t.Fatalf("files-gen 目录里的非 .sh 文件应当被忽略: %v", err)
	}
}
