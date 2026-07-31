package plan

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/K-Lrize/openwrt-build/internal/artifacts"
)

// httpRemote 从 R2 的公网访问根读当前已发布的状态。
//
// 任何一步失败（网络不通、对象不存在、JSON 坏了）都返回空串 = 「不知道」，
// 于是那个条目被当作需要构建。变更检测出错的正确方向是多编一次，而不是
// 拿一个猜出来的答案跳过构建。
type httpRemote struct {
	base   string
	client *http.Client
}

// NewHTTPRemote 按 R2 公网根构造一个远端查询器。base 为空时返回 NoRemote：
// 没配地址就无从判定，一切都需要构建。
func NewHTTPRemote(base string, client *http.Client) Remote {
	if base == "" {
		return NoRemote{}
	}
	if client == nil {
		// 变更检测不该因为一个卡住的连接把整条流水线拖到超时。
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &httpRemote{base: strings.TrimRight(base, "/"), client: client}
}

func (r *httpRemote) ToolchainFingerprint(line, target, subtarget string) string {
	var current artifacts.Current
	if !r.fetch(artifacts.CurrentPath(line, target, subtarget), &current) || current.BuildID == "" {
		return ""
	}
	var build artifacts.Build
	if !r.fetch(artifacts.BuildJSONPath(line, target, subtarget, current.BuildID), &build) {
		return ""
	}
	if build.LineTree == "" {
		return ""
	}
	// 远端只存 line_tree 与 upstream_commit 两个独立字段，比对时用与本地
	// 相同的方式重新组合——而不是让远端也维护一份组合后的指纹字符串。
	return combine(build.LineTree, build.UpstreamCommit)
}

func (r *httpRemote) PackagesFingerprint(line, arch string) string {
	var meta artifacts.PackagesMeta
	if !r.fetch(artifacts.PackagesMetaPath(line, arch), &meta) {
		return ""
	}
	return meta.FeedFingerprint
}

func (r *httpRemote) FirmwareFingerprint(device, line string) string {
	var latest artifacts.Latest
	if !r.fetch(artifacts.LatestPath(device, line), &latest) || latest.ReleaseID == "" {
		return ""
	}
	var manifest artifacts.Manifest
	if !r.fetch(artifacts.ManifestPath(device, line, latest.ReleaseID), &manifest) {
		return ""
	}
	return manifest.Fingerprints.Variant
}

// fetch 取一份 JSON。任何失败都返回 false —— 调用方据此当作「不知道」。
func (r *httpRemote) fetch(objectPath string, out any) bool {
	resp, err := r.client.Get(r.base + "/" + objectPath)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	// 限长读取：远端返回一个巨大的对象不该把 plan 撑爆。这几份元数据都是
	// 几百字节量级。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}
	return json.Unmarshal(body, out) == nil
}
