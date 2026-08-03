package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/K-Lrize/openwrt-build/internal/id"
)

// runID 从 CI 环境生成 build_id 或 release_id，供 workflow 一处取值、下游共享。
//
// 放在 CLI 而不是 plan 里：id 要读 CI 环境（GITHUB_RUN_NUMBER / GITHUB_SHA）与
// 当前时钟，是 I/O；单独成命令，internal/id 与 plan 都保持纯。本地手动跑（无 CI
// 环境变量）时退化为 0 / 0000000，仅供试跑，不作正式产物 id。
func runID(c ctx, args []string) error {
	if len(args) != 1 {
		return errors.New("用法: wrt id <build|release>")
	}
	run := os.Getenv("GITHUB_RUN_NUMBER")
	if run == "" {
		run = "0"
	}
	sha := id.Short(os.Getenv("GITHUB_SHA"))
	if sha == "" {
		sha = "0000000"
	}

	now := time.Now()
	var (
		out string
		err error
	)
	switch args[0] {
	case "build":
		out, err = id.Build(now, run, sha)
	case "release":
		out, err = id.Release(now, run, sha)
	default:
		return fmt.Errorf("未知的 id 类型 %q，应为 build 或 release", args[0])
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, out)
	return nil
}
