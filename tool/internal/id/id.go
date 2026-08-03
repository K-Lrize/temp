// Package id 生成 build_id 与 release_id。
//
// 格式：<utc>-<run_number>-<sha7>，release_id 在最前多一个 "r"。
//
//	build_id   = 20260730-142104-42-f0a60ee
//	release_id = r20260730-142104-42-f0a60ee
//
// 三段缺一不可，各拦一类撞名：
//   - 纯时间戳：同一分钟内并发触发会撞（workflow 并行 job）。
//   - 纯 run_number：跨 fork / 跨仓库不唯一。
//   - 纯 sha7：手动重跑同一 commit 必然撞（workflow_dispatch 重跑）。
//
// 时间用 UTC 紧凑格式，字典序即时间序——GC 按 id 排序时不需要额外的时间字段。
//
// 纯函数：当前时间由调用方注入（now 参数），不在包内读时钟，这样它可测、
// 可复现。
package id

import (
	"fmt"
	"strings"
	"time"
)

// timeLayout 是 id 第一段的时间格式：UTC、紧凑、可排序。
const timeLayout = "20060102-150405"

// Build 拼一个 build_id。run 与 sha 都不能为空——空了就等于少一段防撞。
func Build(now time.Time, run, sha string) (string, error) {
	r, s := strings.TrimSpace(run), strings.TrimSpace(sha)
	if r == "" || s == "" {
		return "", fmt.Errorf("id: run 与 sha 都不能为空（run=%q sha=%q）", run, sha)
	}
	return now.UTC().Format(timeLayout) + "-" + r + "-" + s, nil
}

// Release 拼一个 release_id：build_id 格式加 "r" 前缀，用来一眼区分是发布还是
// 工具链构建。
func Release(now time.Time, run, sha string) (string, error) {
	b, err := Build(now, run, sha)
	if err != nil {
		return "", err
	}
	return "r" + b, nil
}

// Short 取一个 commit 哈希的前 7 位——id 第三段的常规取值。短于 7 位原样返回
// （调用方给了什么就用什么，本函数只负责截断长的）。
func Short(sha string) string {
	s := strings.TrimSpace(sha)
	if len(s) <= 7 {
		return s
	}
	return s[:7]
}
