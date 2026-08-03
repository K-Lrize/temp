package feed

import "testing"

func TestMergeFeedsConf(t *testing.T) {
	// SDK 自带的 feeds.conf.default：外部 feed 只 pin 到分支。
	sdk := `src-git packages https://git.openwrt.org/feed/packages.git;openwrt-25.12
src-git luci https://git.openwrt.org/project/luci.git;openwrt-25.12
src-git routing https://git.openwrt.org/feed/routing.git;openwrt-25.12
src-git telephony https://git.openwrt.org/feed/telephony.git;openwrt-25.12`

	// 仓库的 pin 列表：把 packages/luci 钉到具体 commit，routing/telephony 不动。
	pins := `# 注释行应被忽略
src-git packages https://git.openwrt.org/feed/packages.git^3dc6fa0d
src-git luci https://git.openwrt.org/project/luci.git^37279809`

	got := MergeFeedsConf(sdk, pins, "/ws/feed")

	want := `src-git packages https://git.openwrt.org/feed/packages.git^3dc6fa0d
src-git luci https://git.openwrt.org/project/luci.git^37279809
src-git routing https://git.openwrt.org/feed/routing.git;openwrt-25.12
src-git telephony https://git.openwrt.org/feed/telephony.git;openwrt-25.12
src-link custom_feed /ws/feed
`
	if got != want {
		t.Fatalf("合并结果不符：\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMergeFeedsConf_AppendsWhenAbsent(t *testing.T) {
	// pin 列表里出现了 SDK 没有的 feed，应追加而非丢弃。
	sdk := `src-git packages https://git.openwrt.org/feed/packages.git;openwrt-25.12`
	pins := `src-git extra https://example.org/extra.git^deadbeef`

	got := MergeFeedsConf(sdk, pins, "/ws/feed")
	want := `src-git packages https://git.openwrt.org/feed/packages.git;openwrt-25.12
src-git extra https://example.org/extra.git^deadbeef
src-link custom_feed /ws/feed
`
	if got != want {
		t.Fatalf("追加缺失 feed 结果不符：\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMergeFeedsConf_ReplacesExistingCustomLink(t *testing.T) {
	// 重跑（feeds.conf.default 已含 custom_feed）时应整行覆盖，不重复追加。
	sdk := `src-git packages https://git.openwrt.org/feed/packages.git;openwrt-25.12
src-link custom_feed /old/path`
	got := MergeFeedsConf(sdk, "", "/new/path")
	want := `src-git packages https://git.openwrt.org/feed/packages.git;openwrt-25.12
src-link custom_feed /new/path
`
	if got != want {
		t.Fatalf("覆盖既有 custom_feed 结果不符：\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
