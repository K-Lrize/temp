package repos

import (
	"reflect"
	"strings"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

const (
	testVermagic = "6.12.94-1-e071ff690d47c119e0b3169c6ec92eb9"
	testRepoBase = "https://repo.example.com"
)

func variant(line string, artifacts config.Artifacts, extra ...string) resolve.Variant {
	return resolve.Variant{
		ID:     "mt3600be@" + line,
		Device: "mt3600be",
		Line: resolve.LineFacts{
			ID:               line,
			OpenWrtVersion: "25.12.5",
			Artifacts:        artifacts,
		},
		Hardware: config.Hardware{
			Target:    "mediatek",
			Subtarget: "filogic",
			Profile:   "glinet_gl-mt3600be",
			Arch:      "aarch64_cortex-a53",
		},
		ExtraRepos: extra,
	}
}

func defaultOptions() Options {
	return Options{RepoBase: testRepoBase, Vermagic: testVermagic}
}

func assemble(t *testing.T, v resolve.Variant, opt Options) Repos {
	t.Helper()
	r, err := Assemble(v, opt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOfficialLineBorrowsOpenWrtVersionForKernelLayers(t *testing.T) {
	r := assemble(t, variant("25.12", config.ArtifactsOfficial), defaultOptions())

	want := []string{
		// L1 自有业务包：无论哪条线都来自我们自己的 R2
		"https://repo.example.com/25.12/packages/aarch64_cortex-a53/packages.adb",
		// L2 内核驱动：官方线借官方，按 vermagic 键控
		"https://downloads.openwrt.org/releases/25.12.5/targets/mediatek/filogic/kmods/" + testVermagic + "/packages.adb",
		// L2 target 基础包
		"https://downloads.openwrt.org/releases/25.12.5/targets/mediatek/filogic/packages/packages.adb",
		// L3 社区 feed：按 arch 键控，五个 feed
		"https://downloads.openwrt.org/releases/25.12.5/packages/aarch64_cortex-a53/base/packages.adb",
		"https://downloads.openwrt.org/releases/25.12.5/packages/aarch64_cortex-a53/luci/packages.adb",
		"https://downloads.openwrt.org/releases/25.12.5/packages/aarch64_cortex-a53/packages/packages.adb",
		"https://downloads.openwrt.org/releases/25.12.5/packages/aarch64_cortex-a53/routing/packages.adb",
		"https://downloads.openwrt.org/releases/25.12.5/packages/aarch64_cortex-a53/telephony/packages.adb",
	}
	if !reflect.DeepEqual(r.Runtime, want) {
		t.Errorf("runtime 不符\n want: %v\n got:  %v", want, r.Runtime)
	}
	if !reflect.DeepEqual(r.Build, want) {
		t.Errorf("没有本地预同步时 build 应与 runtime 相同\n want: %v\n got:  %v", want, r.Build)
	}
}

func TestSelfLineUsesOwnR2ForKernelLayers(t *testing.T) {
	r := assemble(t, variant("25.12-mtk", config.ArtifactsSelf), defaultOptions())

	// L2 两段整体改指自有 R2：自编内核的 vermagic 含配置哈希，官方
	// kmods/ 下不会有这个目录。
	for _, want := range []string{
		"https://repo.example.com/25.12-mtk/targets/mediatek/filogic/kmods/" + testVermagic + "/packages.adb",
		"https://repo.example.com/25.12-mtk/targets/mediatek/filogic/packages/packages.adb",
	} {
		if !contains(r.Runtime, want) {
			t.Errorf("缺少 %q\n%v", want, r.Runtime)
		}
	}

	// L3 社区 feed 仍然借官方——自建的只有内核与底座，上千个通用包没有
	// 理由自己编。
	for _, url := range r.Runtime {
		if strings.Contains(url, "/packages/aarch64_cortex-a53/luci/") &&
			!strings.HasPrefix(url, "https://downloads.openwrt.org/") {
			t.Errorf("L3 应当借官方，得到 %q", url)
		}
	}
}

func TestVermagicIsRequired(t *testing.T) {
	// 上一代在这里写占位符再让下游 grep 拦，跨三个进程传一个字符串协议。
	// 现在它就是一个必填参数，缺了直接构造失败。
	opt := defaultOptions()
	opt.Vermagic = ""
	if _, err := Assemble(variant("25.12", config.ArtifactsOfficial), opt); err == nil {
		t.Fatal("vermagic 缺失必须报错——少了这一层，固件装不上任何驱动，现象要到设备上才暴露")
	}
}

func TestRepoBaseIsRequired(t *testing.T) {
	opt := defaultOptions()
	opt.RepoBase = ""
	if _, err := Assemble(variant("25.12", config.ArtifactsOfficial), opt); err == nil {
		t.Fatal("repo base 缺失必须报错")
	}
}

func TestTrailingSlashInRepoBaseIsTolerated(t *testing.T) {
	opt := defaultOptions()
	opt.RepoBase = testRepoBase + "/"
	r := assemble(t, variant("25.12", config.ArtifactsOfficial), opt)
	for _, url := range r.Runtime {
		if strings.Contains(url, "//packages") || strings.Contains(url, "com//") {
			t.Fatalf("拼出了双斜杠: %q", url)
		}
	}
}

func TestLocalMirrorsOnlyAffectBuildList(t *testing.T) {
	// 构建期从本地已预同步的索引取包（省一圈公网），但运行期列表必须是
	// 在线 URL——file:// 漏进设备的 /etc/apk/repositories.d/ 就是一条永久
	// 失效的软件源，而且只有设备联网更新时才发现。
	opt := defaultOptions()
	opt.LocalL1 = "/build/ib/custom/packages.adb"
	opt.LocalKmod = "/build/ib/kmods/packages.adb"

	r := assemble(t, variant("25.12", config.ArtifactsOfficial), opt)

	if !contains(r.Build, "file:///build/ib/custom/packages.adb") {
		t.Errorf("build 里缺少本地 L1：\n%v", r.Build)
	}
	if !contains(r.Build, "file:///build/ib/kmods/packages.adb") {
		t.Errorf("build 里缺少本地 kmod：\n%v", r.Build)
	}
	for _, url := range r.Runtime {
		if strings.HasPrefix(url, "file://") {
			t.Fatalf("runtime 里出现了 file:// —— %q", url)
		}
	}
	if len(r.Build) != len(r.Runtime) {
		t.Errorf("两份列表条目数应一致：build=%d runtime=%d", len(r.Build), len(r.Runtime))
	}
}

func TestExtraReposAppendToBothLists(t *testing.T) {
	extra := "https://third-party.example.com/custom/packages.adb"
	r := assemble(t, variant("25.12", config.ArtifactsOfficial, extra), defaultOptions())

	if got := r.Build[len(r.Build)-1]; got != extra {
		t.Errorf("额外源应追加在最后，得到 %q", got)
	}
	if got := r.Runtime[len(r.Runtime)-1]; got != extra {
		t.Errorf("额外源应追加在最后，得到 %q", got)
	}
}

func TestUpstreamRootIsOverridable(t *testing.T) {
	// 内网镜像或测试替身；不给就是官方。
	opt := defaultOptions()
	opt.UpstreamRoot = "https://mirror.internal"
	r := assemble(t, variant("25.12", config.ArtifactsOfficial), opt)

	for _, url := range r.Runtime {
		if strings.HasPrefix(url, "https://downloads.openwrt.org") {
			t.Fatalf("覆盖上游根之后不该再出现官方地址: %q", url)
		}
	}
	if !contains(r.Runtime, "https://mirror.internal/releases/25.12.5/packages/aarch64_cortex-a53/base/packages.adb") {
		t.Errorf("上游根未生效：\n%v", r.Runtime)
	}
}

func TestAssembleIsDeterministic(t *testing.T) {
	v := variant("25.12", config.ArtifactsOfficial, "https://x.example.com/a.adb")
	first := assemble(t, v, defaultOptions())
	for range 5 {
		if got := assemble(t, v, defaultOptions()); !reflect.DeepEqual(got, first) {
			t.Fatal("同样的输入必须得到逐字节相同的输出")
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
