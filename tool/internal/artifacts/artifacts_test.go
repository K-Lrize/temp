package artifacts

import "testing"

// R2 上的路径是设计的一部分而不是实现细节：packages/ 与 kmods/ 会被烧进
// 已刷机设备的 /etc/apk/repositories.d/，改一个字就是让那批设备的软件源
// 永久失效。这里逐条钉死，改动必须是有意的。
func TestPaths(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			"自有业务包（设备固化的地址）",
			PackagesDir("25.12", "aarch64_generic"),
			"25.12/packages/aarch64_generic",
		},
		{
			"内核驱动仓，按 vermagic 键控（设备固化的地址）",
			KmodsDir("25.12-mtk", "mediatek", "filogic", "6.12.94-1-abc"),
			"25.12-mtk/targets/mediatek/filogic/kmods/6.12.94-1-abc",
		},
		{
			"target 基础包（设备固化的地址）",
			TargetPackagesDir("25.12", "armsr", "armv8"),
			"25.12/targets/armsr/armv8/packages",
		},
		{
			"工具链指针：本目录下唯一可变的文件",
			CurrentPath("25.12-mtk", "mediatek", "filogic"),
			"25.12-mtk/targets/mediatek/filogic/current.json",
		},
		{
			"不可变的工具链构建目录",
			BuildDir("25.12-mtk", "mediatek", "filogic", "20260731-1-abc1234"),
			"25.12-mtk/targets/mediatek/filogic/builds/20260731-1-abc1234",
		},
		{
			// device 顶层、line 居中：人的入口是设备，GC 也按设备分组。
			"固件发布目录",
			ReleaseDir("mt3600be", "25.12-mtk", "r20260731-1-abc1234"),
			"devices/mt3600be/25.12-mtk/releases/r20260731-1-abc1234",
		},
		{
			"固件「当前状态」文件（可变指针，唯一可变）",
			FirmwareCurrentPath("mt3600be", "25.12"),
			"devices/mt3600be/25.12/current.json",
		},
		{
			"不可变发布目录里的档案（GC 引用 + 溯源）",
			ReleaseMetaPath("mt3600be", "25.12", "r1"),
			"devices/mt3600be/25.12/releases/r1/meta.json",
		},
		{
			"自有包「当前状态」文件（与索引同目录）",
			PackagesCurrentPath("25.12", "aarch64_generic"),
			"25.12/packages/aarch64_generic/current.json",
		},
		{
			"不可变工具链构建目录里的档案",
			BuildMetaPath("25.12-mtk", "mediatek", "filogic", "20260731-1-abc1234"),
			"25.12-mtk/targets/mediatek/filogic/builds/20260731-1-abc1234/meta.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("\n want: %s\n got:  %s", tc.want, tc.got)
			}
		})
	}
}

func TestOneDeviceTwoLinesDoNotCollide(t *testing.T) {
	// 同一台设备两条版本线的固件必须落在不同目录，否则后发的会覆盖先发的。
	// 上一代的布局里固件不带 line，这正是它只能靠复制设备目录的原因。
	a := ReleaseDir("mt3600be", "25.12", "r1")
	b := ReleaseDir("mt3600be", "25.12-mtk", "r1")
	if a == b {
		t.Fatalf("两条线的固件目录撞了: %s", a)
	}
	if FirmwareCurrentPath("mt3600be", "25.12") == FirmwareCurrentPath("mt3600be", "25.12-mtk") {
		t.Fatal("两条线的指针撞了")
	}
}

func TestAllDeviceLinesShareOnePrefix(t *testing.T) {
	// GC 要按设备一次列举出它全部版本线的发布：devices/<device>/ 下扫一次
	// 就够，不必跨所有 line 前缀扫完再聚合。
	const wantPrefix = "devices/mt3600be/"
	for _, p := range []string{
		DeviceLineDir("mt3600be", "25.12"),
		DeviceLineDir("mt3600be", "25.12-mtk"),
		ReleaseDir("mt3600be", "25.12", "r1"),
	} {
		if len(p) < len(wantPrefix) || p[:len(wantPrefix)] != wantPrefix {
			t.Errorf("%q 不在 %q 之下", p, wantPrefix)
		}
	}
}
