package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/K-Lrize/openwrt-build/internal/manifest"
)

// runVerify 是发布前各道验证门禁的统一入口。第一个子命令是 manifest 回归门禁；
// 将来的签名 / 校验和验证进来时是这里的另一个子命令，而不是再开一个顶层动词。
func runVerify(c ctx, args []string) error {
	if len(args) == 0 {
		return errors.New("用法: wrt verify <manifest> ...")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "manifest":
		return runVerifyManifest(c, rest)
	default:
		return fmt.Errorf("未知的 verify 子命令 %q，可用：manifest", sub)
	}
}

// runVerifyManifest 拦「IB 漏依赖」那类坑：某个包上一个 release 里在、这次镜像里
// 没了，多半是条件依赖静默失效。有包消失就返回非零，把这次残缺的固件挡在发布前。
//
// --prev 省略即表示没有可比对的上一版（首个 release，或上一版早于门禁上线）——
// 放行，与 toolchain 首次构建跳过 kmod 下降检测同理。是否存在上一版是编排层的
// 网络事实（R2 拉没拉到），故由 workflow 决定传不传 --prev，这里只管有基线时的比对。
func runVerifyManifest(c ctx, args []string) error {
	fs := flag.NewFlagSet("verify manifest", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	prev := fs.String("prev", "", "上一版 release 的 .manifest 路径（省略即无基线，放行）")
	curr := fs.String("curr", "", "本次构建的 .manifest 路径")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	if *curr == "" {
		return errors.New("verify manifest: --curr 必填")
	}
	if *prev == "" {
		fmt.Fprintln(c.stdout, "无上一版 manifest 可比对，跳过回归门禁")
		return nil
	}

	prevBytes, err := os.ReadFile(*prev)
	if err != nil {
		return fmt.Errorf("读上一版 manifest：%w", err)
	}
	currBytes, err := os.ReadFile(*curr)
	if err != nil {
		return fmt.Errorf("读本次 manifest：%w", err)
	}

	gone := manifest.Disappeared(string(prevBytes), string(currBytes))
	if len(gone) > 0 {
		for _, name := range gone {
			fmt.Fprintf(c.stdout, "消失: %s\n", name)
		}
		return fmt.Errorf("回归门禁未通过：%d 个包相较上一版消失（多半是 IB 条件依赖失效）", len(gone))
	}
	fmt.Fprintln(c.stdout, "✓ 回归门禁通过：无包相较上一版消失")
	return nil
}
