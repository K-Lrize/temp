package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/K-Lrize/openwrt-build/internal/feed"
)

// runFeeds 就地改写 SDK 的 feeds.conf.default：外部 feed 按 feed/feeds.conf 的
// pin 钉到具体 commit，并追加一条指向本仓库 feed/ 的 src-link。
//
// 只碰 feeds.conf.default 这一份文本，不跑 scripts/feeds——后者是 SDK 自带工具的
// 调用（连同 update / install），留在 workflow step 里，与 make image 同理。这里
// 只负责「该写成什么」这段逻辑，MergeFeedsConf 有表测试兜底。
func runFeeds(c ctx, args []string) error {
	fs := flag.NewFlagSet("feeds", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	sdk := fs.String("sdk", "", "SDK 根目录（含 feeds.conf.default）")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("feeds 不接受位置参数，收到 %v", rest)
	}
	if *sdk == "" {
		return errors.New("用法: wrt feeds --sdk <sdk 目录>")
	}

	defaultPath := filepath.Join(*sdk, "feeds.conf.default")
	sdkDefault, err := os.ReadFile(defaultPath)
	if err != nil {
		return fmt.Errorf("读取 %s：%w", defaultPath, err)
	}

	// pin 列表可缺省：还没有任何外部 feed 需要钉时，只注入自有 feed 也是合法的。
	pins, err := os.ReadFile(filepath.Join(c.root, "feed", "feeds.conf"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取 feed/feeds.conf：%w", err)
	}

	feedPath, err := filepath.Abs(filepath.Join(c.root, "feed"))
	if err != nil {
		return err
	}

	merged := feed.MergeFeedsConf(string(sdkDefault), string(pins), feedPath)
	if err := os.WriteFile(defaultPath, []byte(merged), 0o644); err != nil {
		return fmt.Errorf("写回 %s：%w", defaultPath, err)
	}

	fmt.Fprintf(c.stdout, "%s 已 pin 外部 feed 并注入自有 feed（%s）\n", defaultPath, feed.CustomFeedName)
	return nil
}
