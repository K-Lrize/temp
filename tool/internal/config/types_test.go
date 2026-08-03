package config

import (
	"reflect"
	"testing"
)

const validCommit = "f0a60eee2fe051741c643ea6118718aae1ef17fb"

func officialLine() Line {
	return Line{ID: "25.12", OpenWrtVersion: "25.12.5", Artifacts: ArtifactsOfficial}
}

func selfLine() Line {
	return Line{
		ID:               "25.12-selfbuild",
		OpenWrtVersion: "25.12.5",
		Artifacts:        ArtifactsSelf,
		Source: &Source{
			Repo:   "https://github.com/openwrt/openwrt",
			Ref:    "v25.12.5",
			Commit: validCommit,
		},
	}
}

func TestLineValidate(t *testing.T) {
	tests := []struct {
		name  string
		mut   func(*Line)
		rules []string
	}{
		{"官方线最小合法配置", func(*Line) {}, nil},
		{"自建线最小合法配置", func(l *Line) { *l = selfLine() }, nil},

		{"id 为空", func(l *Line) { l.ID = "" }, []string{"line.id"}},
		{"id 含大写", func(l *Line) { l.ID = "25.12-MTK" }, []string{"line.id"}},
		{"id 以连字符开头", func(l *Line) { l.ID = "-25.12" }, []string{"line.id"}},

		{"openwrt_version 为空", func(l *Line) { l.OpenWrtVersion = "" }, []string{"line.openwrt_version"}},
		{"openwrt_version 只写到次版本", func(l *Line) { l.OpenWrtVersion = "25.12" }, []string{"line.openwrt_version"}},

		{"artifacts 为空", func(l *Line) { l.Artifacts = "" }, []string{"line.artifacts"}},
		{"artifacts 取值非法", func(l *Line) { l.Artifacts = "build" }, []string{"line.artifacts"}},

		{
			"artifacts=self 却没有 source",
			func(l *Line) { l.Artifacts = ArtifactsSelf },
			[]string{"line.source.missing"},
		},
		{
			"artifacts=official 却带了 source",
			func(l *Line) { l.Source = selfLine().Source },
			[]string{"line.source.unexpected"},
		},
		{
			"source.repo 为空",
			func(l *Line) { *l = selfLine(); l.Source.Repo = "" },
			[]string{"line.source.repo"},
		},
		{
			"source.repo 不是 http(s)",
			func(l *Line) { *l = selfLine(); l.Source.Repo = "git@github.com:openwrt/openwrt.git" },
			[]string{"line.source.repo"},
		},
		{
			"source.commit 是 tag 名而不是 40 位哈希",
			func(l *Line) { *l = selfLine(); l.Source.Commit = "v25.12.5" },
			[]string{"line.source.commit"},
		},
		{
			"source.commit 是 7 位短哈希",
			func(l *Line) { *l = selfLine(); l.Source.Commit = "f0a60ee" },
			[]string{"line.source.commit"},
		},
		{
			"source.ref 缺失时无法核对版本线",
			func(l *Line) { *l = selfLine(); l.Source.Ref = "" },
			[]string{"line.source.ref"},
		},

		// artifacts=self 时 L3 社区 feed 仍借 openwrt_version 那条线，两者版本线
		// 必须一致，否则借来的 luci/packages 与自编 libc 对不上。commit 无法离线
		// 核对，退而用只供人读的 ref 做这道检查。
		{
			"ref 与 openwrt_version 不在同一条版本线",
			func(l *Line) { *l = selfLine(); l.OpenWrtVersion = "24.10.2" },
			[]string{"line.openwrt_version-ref-mismatch"},
		},
		{
			"ref 跟踪 master 时无法核对，降级为 warn",
			func(l *Line) { *l = selfLine(); l.Source.Ref = "main" },
			[]string{"line.openwrt_version-ref-unknown"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := officialLine()
			tc.mut(&line)
			got := line.Validate().Rules()
			if !reflect.DeepEqual(got, tc.rules) {
				t.Fatalf("规则不符\n want: %v\n got:  %v\n%s", tc.rules, got, line.Validate())
			}
		})
	}
}

func TestLineRefTrackingMasterIsWarnOnly(t *testing.T) {
	line := selfLine()
	line.Source.Ref = "main"
	if line.Validate().HasError() {
		t.Fatalf("跟踪 master 是合法场景，不该阻断：\n%s", line.Validate())
	}
}

func validDevice() Device {
	return Device{
		Name: "mt3600be",
		Hardware: Hardware{
			Target:    "mediatek",
			Subtarget: "filogic",
			Profile:   "glinet_gl-mt3600be",
			Arch:      "aarch64_cortex-a53",
		},
		Lines:    []string{"25.12"},
		Packages: PackageSpec{Include: []string{"base-router"}, Add: []string{"sing-box"}},
		Image:    Image{RootfsPartsize: 256},
	}
}

func TestDeviceValidate(t *testing.T) {
	tests := []struct {
		name  string
		mut   func(*Device)
		rules []string
	}{
		{"最小合法配置", func(*Device) {}, nil},

		{"name 为空", func(d *Device) { d.Name = "" }, []string{"device.name"}},
		{"name 含大写", func(d *Device) { d.Name = "MT3600BE" }, []string{"device.name"}},

		{"target 为空", func(d *Device) { d.Hardware.Target = "" }, []string{"device.arch", "device.hardware"}},
		{"profile 为空", func(d *Device) { d.Hardware.Profile = "" }, []string{"device.hardware"}},
		{"arch 为空", func(d *Device) { d.Hardware.Arch = "" }, []string{"device.arch", "device.hardware"}},

		// arch 打错一个字母 -> 从错误架构的仓库拉包 -> 不可开机，而其余校验全绿。
		// 这是收录表存在的全部理由。
		{
			"已收录组合的 arch 填错",
			func(d *Device) { d.Hardware.Arch = "aarch64_generic" },
			[]string{"device.arch"},
		},
		{
			"未收录的 target 组合无法核对，降级为 warn",
			func(d *Device) {
				d.Hardware.Target = "ipq807x"
				d.Hardware.Subtarget = "generic"
				d.Hardware.Arch = "aarch64_cortex-a53"
			},
			[]string{"device.arch"},
		},

		{"lines 为空", func(d *Device) { d.Lines = nil }, []string{"device.lines"}},
		{"lines 有重复", func(d *Device) { d.Lines = []string{"25.12", "25.12"} }, []string{"device.lines"}},

		{
			"同一个包同时出现在 add 与 remove",
			func(d *Device) { d.Packages.Remove = []string{"sing-box"} },
			[]string{"packages.conflict"},
		},
		{
			"remove 里误写了 IB 的 - 前缀",
			func(d *Device) { d.Packages.Remove = []string{"-dnsmasq"} },
			[]string{"packages.name"},
		},
		{
			"包名含空格",
			func(d *Device) { d.Packages.Add = []string{"luci app firewall"} },
			[]string{"packages.name"},
		},

		{"rootfs_partsize 为负", func(d *Device) { d.Image.RootfsPartsize = -1 }, []string{"device.image"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dev := validDevice()
			tc.mut(&dev)
			got := dev.Validate().Rules()
			if !reflect.DeepEqual(got, tc.rules) {
				t.Fatalf("规则不符\n want: %v\n got:  %v\n%s", tc.rules, got, dev.Validate())
			}
		})
	}
}

func TestDeviceUnknownTargetIsWarnOnly(t *testing.T) {
	dev := validDevice()
	dev.Hardware.Target = "ipq807x"
	dev.Hardware.Subtarget = "generic"
	if dev.Validate().HasError() {
		t.Fatalf("未收录的 target 只该 warn，不该阻断接入新硬件：\n%s", dev.Validate())
	}
}

func TestSetValidate(t *testing.T) {
	tests := []struct {
		name  string
		set   Set
		rules []string
	}{
		{
			"最小合法配置",
			Set{Name: "base-router", Add: []string{"ip-full"}, Remove: []string{"dnsmasq"}},
			nil,
		},
		{"name 为空", Set{Add: []string{"ip-full"}}, []string{"set.name"}},
		{"add 与 remove 都为空", Set{Name: "empty"}, []string{"set.empty"}},
		{
			"同一个包同时 add 与 remove",
			Set{Name: "x", Add: []string{"dnsmasq"}, Remove: []string{"dnsmasq"}},
			[]string{"packages.conflict"},
		},
		{
			"包名带 - 前缀",
			Set{Name: "x", Remove: []string{"-dnsmasq"}},
			[]string{"packages.name"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.set.Validate().Rules()
			if !reflect.DeepEqual(got, tc.rules) {
				t.Fatalf("规则不符\n want: %v\n got:  %v\n%s", tc.rules, got, tc.set.Validate())
			}
		})
	}
}

func TestLineRequiresBuildIsDerivedNotDeclared(t *testing.T) {
	// requires_build 是「lines/<id>/ 下有没有 overlay/patches/config」的派生结论，
	// 由载入层填。类型上必须没有对应的 YAML 字段，否则就能手写一个与磁盘事实
	// 不符的值。
	f, ok := reflect.TypeOf(Line{}).FieldByName("RequiresBuild")
	if !ok {
		t.Fatal("Line 缺少 RequiresBuild 字段")
	}
	if tag := f.Tag.Get("yaml"); tag != "-" {
		t.Fatalf("RequiresBuild 不该可从 YAML 写入，当前 tag 为 %q", tag)
	}
}
