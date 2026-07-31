package plan

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/K-Lrize/openwrt-build/internal/artifacts"
)

// serve 起一个假 R2：objects 是「对象路径 -> JSON 文本」。
func serve(t *testing.T, objects map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, ok := objects[req.URL.Path[1:]]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestHTTPRemoteRecombinesToolchainFingerprint(t *testing.T) {
	// 远端只存 line_tree 与 upstream_commit 两个独立字段（排障时一眼区分
	// 是配置改的还是源码改的），比对时用与本地相同的方式重新组合。
	const tree, commit = "tree-哈希", "commit-哈希"
	base := serve(t, map[string]string{
		artifacts.CurrentPath("25.12-mtk", "mediatek", "filogic"): `{"build_id":"b1"}`,
		artifacts.BuildJSONPath("25.12-mtk", "mediatek", "filogic", "b1"): `{"line_tree":"` + tree +
			`","upstream_commit":"` + commit + `"}`,
	})

	r := NewHTTPRemote(base, nil)
	if got, want := r.ToolchainFingerprint("25.12-mtk", "mediatek", "filogic"), combine(tree, commit); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHTTPRemoteReadsPackagesAndFirmware(t *testing.T) {
	base := serve(t, map[string]string{
		artifacts.PackagesMetaPath("25.12", "aarch64_generic"): `{"feed_fingerprint":"feed-fp"}`,
		artifacts.LatestPath("vm", "25.12"):                    `{"release_id":"r1"}`,
		artifacts.ManifestPath("vm", "25.12", "r1"):            `{"fingerprints":{"variant":"variant-fp"}}`,
	})

	r := NewHTTPRemote(base, nil)
	if got := r.PackagesFingerprint("25.12", "aarch64_generic"); got != "feed-fp" {
		t.Errorf("packages = %q", got)
	}
	if got := r.FirmwareFingerprint("vm", "25.12"); got != "variant-fp" {
		t.Errorf("firmware = %q", got)
	}
}

func TestHTTPRemoteTreatsEveryFailureAsUnknown(t *testing.T) {
	// 变更检测出错的正确方向是多编一次，而不是拿一个猜出来的答案跳过构建。
	base := serve(t, map[string]string{
		// 指针在但它指向的构建元数据不在（发布到一半、或已被 GC 回收）
		artifacts.CurrentPath("l", "t", "s"): `{"build_id":"missing"}`,
		// 指针本身是坏 JSON
		artifacts.LatestPath("d", "l"): `{不是 JSON`,
		// 指针有效但没有 release_id
		artifacts.PackagesMetaPath("l", "a"): `{}`,
	})

	r := NewHTTPRemote(base, nil)
	for name, got := range map[string]string{
		"指向的构建元数据缺失": r.ToolchainFingerprint("l", "t", "s"),
		"坏 JSON":     r.FirmwareFingerprint("d", "l"),
		"字段为空":       r.PackagesFingerprint("l", "a"),
		"对象根本不存在":    r.FirmwareFingerprint("没这个设备", "l"),
	} {
		if got != "" {
			t.Errorf("%s 应当返回空串（= 不知道），得到 %q", name, got)
		}
	}
}

func TestHTTPRemoteUnreachableHostIsUnknown(t *testing.T) {
	r := NewHTTPRemote("http://127.0.0.1:1", nil)
	if got := r.FirmwareFingerprint("d", "l"); got != "" {
		t.Errorf("远端不可达时应当返回空串，得到 %q", got)
	}
}

func TestEmptyBaseFallsBackToNoRemote(t *testing.T) {
	if _, ok := NewHTTPRemote("", nil).(NoRemote); !ok {
		t.Fatal("没配 repo base 时应当退化成 NoRemote —— 无从判定即全部重建")
	}
}

func TestHTTPRemoteToleratesTrailingSlash(t *testing.T) {
	base := serve(t, map[string]string{
		artifacts.PackagesMetaPath("l", "a"): `{"feed_fingerprint":"fp"}`,
	})
	if got := NewHTTPRemote(base+"/", nil).PackagesFingerprint("l", "a"); got != "fp" {
		t.Errorf("got %q", got)
	}
}
