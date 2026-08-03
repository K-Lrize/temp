package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/plan"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

func metaVariant(withSource bool) resolve.Variant {
	v := resolve.Variant{
		ID:     "mt3600be@25.12-mtk",
		Device: "mt3600be",
		Line:   resolve.LineFacts{ID: "25.12-mtk", OpenWrtVersion: "25.12.5", Artifacts: config.ArtifactsSelf},
		Hardware: config.Hardware{
			Target: "mediatek", Subtarget: "filogic", Profile: "p", Arch: "aarch64_cortex-a53",
		},
	}
	if withSource {
		v.Line.Source = &config.Source{
			Repo:   "https://github.com/openwrt/openwrt",
			Commit: "f0a60eee2fe051741c643ea6118718aae1ef17fb",
			Ref:    "v25.12.5",
		}
	}
	return v
}

var epoch = time.Unix(0, 0)

func TestBuildReleaseMetaPullsFromVariantAndFingerprint(t *testing.T) {
	v := metaVariant(true)
	fp := plan.Fingerprints{LineTree: "lt", Line: "L", Feed: "F", Variant: "V"}
	m := buildReleaseMeta(v, fp, "r-123", "b-456", "6.12.94-1-abc", "openwrt-x.manifest", "http://ci/run/1", epoch)

	// 参数直传的
	if m.ReleaseID != "r-123" || m.BuildID != "b-456" || m.Vermagic != "6.12.94-1-abc" || m.CIRunURL != "http://ci/run/1" {
		t.Errorf("参数没原样进档案: %+v", m)
	}
	// 从 variant 自动取的
	if m.Variant != "mt3600be@25.12-mtk" || m.Device != "mt3600be" || m.Line != "25.12-mtk" {
		t.Errorf("variant 身份没自动填对: %+v", m)
	}
	if m.UpstreamCommit != v.Line.Source.Commit {
		t.Errorf("upstream_commit 应从 variant 的 source 取: %q", m.UpstreamCommit)
	}
	// 单一 variant 指纹（不再存三元组：feed⊃line、variant⊃line+feed）
	if m.Fingerprint != "V" {
		t.Errorf("fingerprint 应是 variant 指纹: %q", m.Fingerprint)
	}
	// 门禁定位官方清单的文件名透传
	if m.ManifestFile != "openwrt-x.manifest" {
		t.Errorf("manifest_file 应透传: %q", m.ManifestFile)
	}
	// 时间戳：UTC、RFC3339
	if m.CreatedAt != "1970-01-01T00:00:00Z" {
		t.Errorf("created_at 应是 UTC RFC3339: %q", m.CreatedAt)
	}
}

func TestBuildReleaseMetaOfficialHasNoCommitNoBuildID(t *testing.T) {
	// official 线没有自建工具链，没有 build_id，也没有 source commit。
	m := buildReleaseMeta(metaVariant(false), plan.Fingerprints{}, "r-1", "", "vm", "", "", epoch)
	if m.UpstreamCommit != "" {
		t.Errorf("official 线不该有 upstream_commit: %q", m.UpstreamCommit)
	}
	if m.BuildID != "" {
		t.Errorf("official 线不该有 build_id: %q", m.BuildID)
	}
}

func TestBuildBuildKeepsTreeAndCommitSeparate(t *testing.T) {
	v := metaVariant(true)
	fp := plan.Fingerprints{LineTree: "the-tree-sha", Line: "L", Feed: "F", Variant: "V"}
	b := buildBuild(v, fp, "b-1", "vm", "6.12.94", "sdksha", "ibsha", "http://ci", epoch)

	// line_tree（配置改的）与 upstream_commit（源码改的）是两个独立字段。
	if b.LineTree != "the-tree-sha" {
		t.Errorf("line_tree 应来自指纹: %q", b.LineTree)
	}
	if b.UpstreamCommit != v.Line.Source.Commit {
		t.Errorf("upstream_commit 应来自 source: %q", b.UpstreamCommit)
	}
	if b.Target != "mediatek" || b.Subtarget != "filogic" || b.Line != "25.12-mtk" {
		t.Errorf("target 坐标没自动填对: %+v", b)
	}
	if b.SDKSHA256 != "sdksha" || b.IBSHA256 != "ibsha" {
		t.Errorf("构建事实没原样进档案: %+v", b)
	}
}

func TestMetaLatestEmitsJSON(t *testing.T) {
	// meta latest 现在产固件 current.json：需要 variant 来算 variant 指纹。
	stdout, _, err := runCLI(t, atRepo(t, "meta", "latest", "vm-armsr@25.12", "--release-id", "r-abc")...)
	if err != nil {
		t.Fatal(err)
	}
	var l struct {
		Fingerprint string `json:"fingerprint"`
		ReleaseID   string `json:"release_id"`
		UpdatedAt   string `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(stdout), &l); err != nil {
		t.Fatalf("输出不是 JSON: %v\n%s", err, stdout)
	}
	if l.ReleaseID != "r-abc" {
		t.Errorf("release_id = %q", l.ReleaseID)
	}
	if l.Fingerprint == "" {
		t.Error("固件 current.json 应带 variant 指纹（plan 一跳判定的依据）")
	}
	if !strings.HasSuffix(l.UpdatedAt, "Z") {
		t.Errorf("updated_at 应是 UTC: %q", l.UpdatedAt)
	}
}

func TestMetaManifestNeedsRealVariantAndFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"缺 release-id", []string{"meta", "manifest", "vm-armsr@25.12", "--vermagic", "vm"}},
		{"缺 vermagic", []string{"meta", "manifest", "vm-armsr@25.12", "--release-id", "r-1"}},
		{"variant 不存在", []string{"meta", "manifest", "nope@25.12", "--release-id", "r-1", "--vermagic", "vm"}},
		{"未知子命令", []string{"meta", "nope"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := runCLI(t, atRepo(t, tc.args...)...); err == nil {
				t.Error("应当报错")
			}
		})
	}
}
