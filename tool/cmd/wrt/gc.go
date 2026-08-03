package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/K-Lrize/openwrt-build/internal/artifacts"
	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/gc"
	"github.com/K-Lrize/openwrt-build/internal/publish"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

// runGC 是引用计数回收。判定逻辑在 internal/gc（纯、可测），这里只做 R2 的
// 枚举/拉取/删除。默认 dry-run；单次删除超阈值即熔断。
//
// 目前覆盖 release + 工具链 build + kmod 三档。自有软件包（L1）按包名保留最新
// M 版那档还没做——它牵涉版本号语义排序（沿用旧仓库也只是 mtime 启发式），
// 单独一档，留待后续。
func runGC(c ctx, args []string) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	keep := fs.Int("keep", 3, "每 (设备,线) 保留最新 N 个 release")
	threshold := fs.Int("threshold", 30, "单次删除比例超过这个百分比就熔断")
	apply := fs.Bool("apply", false, "真的删除（默认 dry-run，只打印计划）")
	force := fs.Bool("force-over-threshold", false, "熔断后仍强制执行（须先人工核对 dry-run）")
	var pins gcPins
	fs.Var(&pins, "pin", "钉住不删的 release，形如 <device>@<line>:<release_id>，可多次")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("gc 不接受位置参数，收到 %v", rest)
	}

	cfg, problems, err := config.Load(c.root)
	if err != nil {
		return err
	}
	if problems.HasError() {
		return fmt.Errorf("配置有错，先跑 wrt lint：\n%s", problems)
	}
	variants, more := resolve.All(cfg)
	if more.HasError() {
		return fmt.Errorf("展开 variant 失败：\n%s", more)
	}

	cl, err := publish.NewClient(
		envS3Endpoint(), envS3Bucket(),
		envAWSAccessKey(), envAWSSecretKey())
	if err != nil {
		return err
	}
	ctx := context.Background()

	st := &gcState{
		liveBuilds:    map[string]map[string]bool{},
		liveVermagics: map[string]map[string]bool{},
	}
	if err := gcReleases(ctx, c, cl, variants, *keep, pins, st); err != nil {
		return err
	}
	if err := gcToolchain(ctx, cl, variants, st); err != nil {
		return err
	}

	fmt.Fprintf(c.stdout, "\n=== 汇总：现存 %d 个对象，计划清理 %d 个 ===\n", st.total, len(st.deletions))
	for _, p := range st.deletions {
		fmt.Fprintln(c.stdout, "  待清理:", p)
	}

	if gc.OverThreshold(st.total, len(st.deletions), *threshold) && !*force {
		return fmt.Errorf("熔断：计划删除 %d/%d 超过 %d%% 阈值。请人工核对上面的 dry-run，确有必要再加 --force-over-threshold",
			len(st.deletions), st.total, *threshold)
	}
	if !*apply {
		fmt.Fprintln(c.stdout, "\ndry-run 模式，未实际删除。确认无误后加 --apply。")
		return nil
	}

	deleted := 0
	for _, p := range st.deletions {
		n, err := cl.DeletePrefix(ctx, p)
		if err != nil {
			return err
		}
		deleted += n
	}
	fmt.Fprintf(c.stdout, "\nGC 完成：清理了 %d 个前缀、共 %d 个对象\n", len(st.deletions), deleted)
	return nil
}

type gcState struct {
	total         int
	deletions     []string
	liveBuilds    map[string]map[string]bool // tcKey → build_id 集合
	liveVermagics map[string]map[string]bool // tcKey → vermagic 集合
}

// gcReleases 处理固件 release：每 (device,line) 保留最新 N + 被 pin 的；顺带把
// 存活 release 引用的 build_id / vermagic 收进存活集合。
func gcReleases(ctx context.Context, c ctx, cl *publish.Client, variants []resolve.Variant, keep int, pins gcPins, st *gcState) error {
	fmt.Fprintf(c.stdout, "=== 固件 release（每设备每线保留最新 %d + 被 pin 的） ===\n", keep)
	for _, d := range distinctDeviceLines(variants) {
		relPrefix := artifacts.DeviceLineDir(d.device, d.line) + "/releases/"
		keys, err := cl.List(ctx, relPrefix)
		if err != nil {
			return err
		}
		rids := childSegments(relPrefix, keys)
		if len(rids) == 0 {
			continue
		}
		live := gc.LiveReleaseIDs(rids, keep, pins.forDeviceLine(d.device, d.line))

		existing := make([]gc.Entry, 0, len(rids))
		for _, rid := range rids {
			existing = append(existing, gc.Entry{Key: rid, Path: artifacts.ReleaseDir(d.device, d.line, rid)})
		}
		_, del := gc.Classify(existing, live)
		st.total += len(rids)
		st.deletions = append(st.deletions, del...)
		fmt.Fprintf(c.stdout, "  %s@%s: 现存 %d，清理 %d\n", d.device, d.line, len(rids), len(del))

		tcKey := d.line + "|" + d.target + "|" + d.subtarget
		for _, rid := range live {
			var m artifacts.ReleaseMeta
			ok, err := cl.GetJSON(ctx, artifacts.ReleaseMetaPath(d.device, d.line, rid), &m)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			addLive(st.liveBuilds, tcKey, m.BuildID)
			addLive(st.liveVermagics, tcKey, m.Vermagic)
		}
	}
	return nil
}

// gcToolchain 处理工具链 build 与 kmod 仓（仅 self 线）。current.json 指向的
// 目标永远存活，即使还没有任何 release 引用它。
func gcToolchain(ctx context.Context, cl *publish.Client, variants []resolve.Variant, st *gcState) error {
	for _, d := range distinctSelfTargets(variants) {
		tcKey := d.line + "|" + d.target + "|" + d.subtarget

		var cur artifacts.Current
		ok, err := cl.GetJSON(ctx, artifacts.CurrentPath(d.line, d.target, d.subtarget), &cur)
		if err != nil {
			return err
		}
		if ok {
			addLive(st.liveBuilds, tcKey, cur.BuildID)
			addLive(st.liveVermagics, tcKey, cur.Vermagic)
		}

		base := artifacts.TargetDir(d.line, d.target, d.subtarget)
		if err := gcTargetGroup(ctx, cl, base+"/builds/", liveList(st.liveBuilds, tcKey), st,
			func(id string) string { return artifacts.BuildDir(d.line, d.target, d.subtarget, id) }); err != nil {
			return err
		}
		if err := gcTargetGroup(ctx, cl, base+"/kmods/", liveList(st.liveVermagics, tcKey), st,
			func(vm string) string { return artifacts.KmodsDir(d.line, d.target, d.subtarget, vm) }); err != nil {
			return err
		}
	}
	return nil
}

// gcTargetGroup 枚举 prefix 下的子目录（build_id / vermagic），凡不在 live 里的
// 计入删除。
func gcTargetGroup(ctx context.Context, cl *publish.Client, prefix string, live []string, st *gcState, pathOf func(string) string) error {
	keys, err := cl.List(ctx, prefix)
	if err != nil {
		return err
	}
	segs := childSegments(prefix, keys)
	if len(segs) == 0 {
		return nil
	}
	existing := make([]gc.Entry, 0, len(segs))
	for _, s := range segs {
		existing = append(existing, gc.Entry{Key: s, Path: pathOf(s)})
	}
	_, del := gc.Classify(existing, live)
	st.total += len(segs)
	st.deletions = append(st.deletions, del...)
	return nil
}

// ── 小工具 ──

type deviceLine struct{ device, line, target, subtarget string }

func distinctDeviceLines(variants []resolve.Variant) []deviceLine {
	seen := map[string]deviceLine{}
	for _, v := range variants {
		seen[v.Device+"@"+v.Line.ID] = deviceLine{v.Device, v.Line.ID, v.Hardware.Target, v.Hardware.Subtarget}
	}
	return sortedByKey(seen)
}

func distinctSelfTargets(variants []resolve.Variant) []deviceLine {
	seen := map[string]deviceLine{}
	for _, v := range variants {
		if v.Line.Artifacts != config.ArtifactsSelf {
			continue
		}
		seen[v.Line.ID+"|"+v.Hardware.Target+"|"+v.Hardware.Subtarget] = deviceLine{
			line: v.Line.ID, target: v.Hardware.Target, subtarget: v.Hardware.Subtarget}
	}
	return sortedByKey(seen)
}

func sortedByKey(m map[string]deviceLine) []deviceLine {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]deviceLine, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

// childSegments 从 prefix 下的 key 列表里取紧邻 prefix 的那一段目录名（去重、有序）。
func childSegments(prefix string, keys []string) []string {
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	seen := map[string]bool{}
	var out []string
	for _, k := range keys {
		rest := strings.TrimPrefix(k, prefix)
		if rest == k {
			continue
		}
		seg := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			seg = rest[:i]
		}
		if seg != "" && !seen[seg] {
			seen[seg] = true
			out = append(out, seg)
		}
	}
	sort.Strings(out)
	return out
}

func addLive(m map[string]map[string]bool, tcKey, id string) {
	if id == "" {
		return
	}
	if m[tcKey] == nil {
		m[tcKey] = map[string]bool{}
	}
	m[tcKey][id] = true
}

func liveList(m map[string]map[string]bool, tcKey string) []string {
	out := make([]string, 0, len(m[tcKey]))
	for id := range m[tcKey] {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// gcPins 累积 --pin，形如 <device>@<line>:<release_id>。
type gcPins []string

func (p *gcPins) String() string { return strings.Join(*p, ",") }
func (p *gcPins) Set(v string) error {
	if !strings.Contains(v, ":") {
		return errors.New("pin 格式应为 <device>@<line>:<release_id>")
	}
	*p = append(*p, v)
	return nil
}
func (p gcPins) forDeviceLine(device, line string) []string {
	prefix := device + "@" + line + ":"
	var out []string
	for _, raw := range p {
		if strings.HasPrefix(raw, prefix) {
			out = append(out, strings.TrimPrefix(raw, prefix))
		}
	}
	return out
}
