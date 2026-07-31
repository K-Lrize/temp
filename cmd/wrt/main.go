// Command wrt 是本仓库唯一的入口。
//
// 刻意不套一层 Makefile：`wrt help` 本身就是自文档，再加一层 make 目标只是
// 又一个要跟着改的地方。CI 的每个 step 也直接调这里的子命令，workflow YAML
// 里只剩编排。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/diag"
	"github.com/K-Lrize/openwrt-build/internal/feed"
	"github.com/K-Lrize/openwrt-build/internal/repos"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

type command struct {
	name    string
	summary string
	usage   string
	run     func(ctx, []string) error
}

// ctx 是每个子命令都需要的东西：仓库根与输出流。输出流走参数而不是直接用
// os.Stdout，子命令才能在测试里被完整驱动。
type ctx struct {
	root   string
	stdout io.Writer
	stderr io.Writer
}

func commands() []command {
	return []command{
		{
			name:    "lint",
			summary: "校验全部配置与自有软件包 Makefile",
			usage:   "wrt lint",
			run:     runLint,
		},
		{
			name:    "resolve",
			summary: "把 device × line 展开成 variant 并打印 JSON",
			usage:   "wrt resolve <device>@<line> | wrt resolve --all",
			run:     runResolve,
		},
		{
			name:    "repos",
			summary: "装配三层 apk 软件源地址（构建期 / 运行期两份）",
			usage:   "wrt repos <device>@<line> --repo-base <url> --vermagic <vm> [--local-l1 <path>] [--local-kmod <path>]",
			run:     runRepos,
		},
	}
}

func commandNames() []string {
	var out []string
	for _, c := range commands() {
		out = append(out, c.name)
	}
	return out
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("wrt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("C", "", "仓库根目录（默认从当前目录向上找）")
	fs.Usage = func() { printHelp(stderr) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		printHelp(stdout)
		return errors.New("缺少子命令")
	}

	name, cmdArgs := rest[0], rest[1:]
	if name == "help" || name == "-h" || name == "--help" {
		printHelp(stdout)
		return nil
	}

	for _, c := range commands() {
		if c.name != name {
			continue
		}
		resolved, err := repositoryRoot(*root)
		if err != nil {
			return err
		}
		return c.run(ctx{root: resolved, stdout: stdout, stderr: stderr}, cmdArgs)
	}
	return fmt.Errorf("未知子命令 %q，可用：%s", name, strings.Join(commandNames(), " "))
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "wrt —— openwrt-build 的构建编排入口")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "用法: wrt [-C <仓库根>] <子命令> [参数]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "子命令:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
		fmt.Fprintf(w, "  %-10s   %s\n", "", c.usage)
	}
	fmt.Fprintln(w, "  help       显示本帮助")
}

// repositoryRoot 定位仓库根。显式 -C 优先，其次 WRT_ROOT，最后从当前目录
// 向上找 go.mod——这样在仓库里任何子目录下都能直接跑。
func repositoryRoot(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if env := os.Getenv("WRT_ROOT"); env != "" {
		return filepath.Abs(env)
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("没找到仓库根（当前目录及其各级父目录都没有 go.mod）；用 -C 显式指定")
		}
		dir = parent
	}
}

// parseFlags 解析子命令的 flag，并返回位置参数。
//
// 标准库的 flag 在遇到第一个非 flag 参数时就停止解析，于是
// `wrt repos <variant> --vermagic X` 里的 --vermagic 会被当成位置参数。
// 这里先把前导的位置参数摘出去再解析，剩下的从 fs.Args() 收回来，
// 这样 flag 放在 variant 前后都能用。
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	lead := 0
	for lead < len(args) && !strings.HasPrefix(args[lead], "-") {
		lead++
	}
	positional := append([]string(nil), args[:lead]...)
	if err := fs.Parse(args[lead:]); err != nil {
		return nil, err
	}
	return append(positional, fs.Args()...), nil
}

func runLint(c ctx, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("lint 不接受参数，收到 %v", args)
	}

	cfg, problems, err := config.Load(c.root)
	if err != nil {
		return err
	}
	// 跨层的包冲突要展开 variant 才看得见（单个文件内部自洽不代表合并后自洽），
	// 所以 lint 一定要把 resolve 也跑一遍，而不是只做静态校验。
	if !problems.HasError() {
		if _, more := resolve.All(cfg); len(more) > 0 {
			problems = append(problems, more...)
		}
	}

	_, feedProblems, err := feed.Load(c.root)
	if err != nil {
		return err
	}
	problems = append(problems, feedProblems...)

	for _, p := range problems {
		fmt.Fprintln(c.stdout, p)
	}

	errCount := problems.Count(diag.SeverityError)
	warnCount := problems.Count(diag.SeverityWarn)
	fmt.Fprintf(c.stdout, "\n%d 个错误，%d 个提示\n", errCount, warnCount)
	if errCount > 0 {
		return fmt.Errorf("配置校验未通过：%d 个错误", errCount)
	}
	return nil
}

func runResolve(c ctx, args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	all := fs.Bool("all", false, "输出全部 variant（JSON 数组）")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}

	switch {
	case *all && len(rest) > 0:
		return fmt.Errorf("--all 与具体 variant 互斥，收到 %v", rest)
	case !*all && len(rest) == 0:
		return errors.New("用法: wrt resolve <device>@<line> | wrt resolve --all")
	case !*all && len(rest) > 1:
		return fmt.Errorf("一次只能解析一个 variant，收到 %v", rest)
	}

	cfg, problems, err := config.Load(c.root)
	if err != nil {
		return err
	}
	if problems.HasError() {
		return fmt.Errorf("配置有错，先跑 wrt lint：\n%s", problems)
	}

	if *all {
		variants, more := resolve.All(cfg)
		if more.HasError() {
			return fmt.Errorf("展开 variant 失败：\n%s", more)
		}
		return emitJSON(c.stdout, variants)
	}

	variant, err := resolve.One(cfg, rest[0])
	if err != nil {
		return err
	}
	return emitJSON(c.stdout, variant)
}

func runRepos(c ctx, args []string) error {
	fs := flag.NewFlagSet("repos", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	var opt repos.Options
	fs.StringVar(&opt.RepoBase, "repo-base", os.Getenv("WRT_REPO_BASE"), "自有产物的公网访问根")
	fs.StringVar(&opt.Vermagic, "vermagic", "", "本次固件对应的内核 ABI 标识")
	fs.StringVar(&opt.LocalL1, "local-l1", "", "构建机上已预同步的自有包索引（只影响构建期列表）")
	fs.StringVar(&opt.LocalKmod, "local-kmod", "", "构建机上已预同步的 kmod 索引（只影响构建期列表）")
	fs.StringVar(&opt.UpstreamRoot, "upstream-root", "", "覆盖官方发布站（内网镜像）")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("用法: wrt repos <device>@<line> --repo-base <url> --vermagic <vm>")
	}

	cfg, problems, err := config.Load(c.root)
	if err != nil {
		return err
	}
	if problems.HasError() {
		return fmt.Errorf("配置有错，先跑 wrt lint：\n%s", problems)
	}

	variant, err := resolve.One(cfg, rest[0])
	if err != nil {
		return err
	}
	assembled, err := repos.Assemble(variant, opt)
	if err != nil {
		return err
	}
	return emitJSON(c.stdout, assembled)
}

func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// 关掉 HTML 转义：URL 里的 & 会被写成 &，人读和 grep 都难受。
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
