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

// 三层同形：指纹就存在该层的 current.json 里，plan 一次 GET 直接读到，不再翻第二跳。
func TestHTTPRemoteReadsFingerprintFromCurrent(t *testing.T) {
	base := serve(t, map[string]string{
		artifacts.CurrentPath("25.12-mtk", "mediatek", "filogic"): `{"fingerprint":"tc-fp"}`,
		artifacts.PackagesCurrentPath("25.12", "aarch64_generic"): `{"fingerprint":"feed-fp"}`,
		artifacts.FirmwareCurrentPath("vm", "25.12"):              `{"fingerprint":"variant-fp","release_id":"r1"}`,
	})

	r := NewHTTPRemote(base, nil)
	if got := r.ToolchainFingerprint("25.12-mtk", "mediatek", "filogic"); got != "tc-fp" {
		t.Errorf("toolchain = %q", got)
	}
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
		// 指针是坏 JSON
		artifacts.FirmwareCurrentPath("d", "l"): `{不是 JSON`,
		// 指针有效但没有 fingerprint 字段
		artifacts.PackagesCurrentPath("l", "a"): `{}`,
	})

	r := NewHTTPRemote(base, nil)
	for name, got := range map[string]string{
		"坏 JSON":  r.FirmwareFingerprint("d", "l"),
		"字段为空":    r.PackagesFingerprint("l", "a"),
		"对象根本不存在": r.ToolchainFingerprint("l", "t", "s"),
		"设备根本不存在": r.FirmwareFingerprint("没这个设备", "l"),
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
		artifacts.PackagesCurrentPath("l", "a"): `{"fingerprint":"fp"}`,
	})
	if got := NewHTTPRemote(base+"/", nil).PackagesFingerprint("l", "a"); got != "fp" {
		t.Errorf("got %q", got)
	}
}
