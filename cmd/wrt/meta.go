package main

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/K-Lrize/openwrt-build/internal/artifacts"
	"github.com/K-Lrize/openwrt-build/internal/config"
	"github.com/K-Lrize/openwrt-build/internal/plan"
	"github.com/K-Lrize/openwrt-build/internal/resolve"
)

// runMeta 生成 R2 上那些元数据 JSON（名词表里的"小票"）。
//
// 职责边界：meta 只把输入拼成 JSON，不生成 id、不发网络。release_id / build_id
// 一律以参数传入——它们从哪来（plan 产还是 firmware job 产）是编排问题，不在
// 这一层决定。凡是能从 variant 与指纹推出的字段（line/device/target/commit/
// 三层指纹）都自动取，不让调用方重复传一遍再传错。
func runMeta(c ctx, args []string) error {
	if len(args) == 0 {
		return errors.New("用法: wrt meta <manifest|latest|build|current|packages> ...")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "manifest":
		return runMetaManifest(c, rest)
	case "latest":
		return runMetaLatest(c, rest)
	case "build":
		return runMetaBuild(c, rest)
	case "current":
		return runMetaCurrent(c, rest)
	case "packages":
		return runMetaPackages(c, rest)
	default:
		return fmt.Errorf("未知的 meta 子命令 %q，可用：manifest latest build current packages", sub)
	}
}

// stamp 是所有小票统一的时间戳格式：UTC、RFC3339、可排序。
func stamp(now time.Time) string { return now.UTC().Format(time.RFC3339) }

// variantFingerprints 载入配置、解析 variant、算三层指纹——meta 里凡是要写进
// 小票的 line/device/target/commit/指纹都从这一处拿，不各自再解析一遍。
func variantFingerprints(c ctx, variantID string) (resolve.Variant, plan.Fingerprints, error) {
	cfg, problems, err := config.Load(c.root)
	if err != nil {
		return resolve.Variant{}, plan.Fingerprints{}, err
	}
	if problems.HasError() {
		return resolve.Variant{}, plan.Fingerprints{}, fmt.Errorf("配置有错，先跑 wrt lint：\n%s", problems)
	}
	v, err := resolve.One(cfg, variantID)
	if err != nil {
		return resolve.Variant{}, plan.Fingerprints{}, err
	}
	fp, err := plan.NewComputer(c.root).For(cfg, v)
	if err != nil {
		return resolve.Variant{}, plan.Fingerprints{}, err
	}
	return v, fp, nil
}

func runMetaManifest(c ctx, args []string) error {
	fs := flag.NewFlagSet("meta manifest", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	releaseID := fs.String("release-id", "", "本次发布编号")
	buildID := fs.String("build-id", "", "对应的工具链构建编号（official 线留空）")
	vermagic := fs.String("vermagic", "", "内核 ABI 标识")
	ciURL := fs.String("ci-run-url", "", "本次 CI run 链接")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("用法: wrt meta manifest <device>@<line> --release-id R --vermagic V")
	}
	if *releaseID == "" || *vermagic == "" {
		return errors.New("meta manifest: --release-id 与 --vermagic 必填")
	}
	v, fp, err := variantFingerprints(c, rest[0])
	if err != nil {
		return err
	}
	return emitJSON(c.stdout, buildManifest(v, fp, *releaseID, *buildID, *vermagic, *ciURL, time.Now()))
}

func buildManifest(v resolve.Variant, fp plan.Fingerprints, releaseID, buildID, vermagic, ciURL string, now time.Time) artifacts.Manifest {
	return artifacts.Manifest{
		ReleaseID:      releaseID,
		Variant:        v.ID,
		Device:         v.Device,
		Line:           v.Line.ID,
		BuildID:        buildID,
		Vermagic:       vermagic,
		UpstreamCommit: sourceCommit(v),
		Fingerprints:   artifacts.Fingerprints{Line: fp.Line, Feed: fp.Feed, Variant: fp.Variant},
		CIRunURL:       ciURL,
		CreatedAt:      stamp(now),
	}
}

func runMetaLatest(c ctx, args []string) error {
	fs := flag.NewFlagSet("meta latest", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	releaseID := fs.String("release-id", "", "指向的发布编号")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	if *releaseID == "" {
		return errors.New("meta latest: --release-id 必填")
	}
	return emitJSON(c.stdout, artifacts.Latest{ReleaseID: *releaseID, UpdatedAt: stamp(time.Now())})
}

func runMetaBuild(c ctx, args []string) error {
	fs := flag.NewFlagSet("meta build", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	buildID := fs.String("build-id", "", "本次工具链构建编号")
	vermagic := fs.String("vermagic", "", "内核 ABI 标识")
	kernelVersion := fs.String("kernel-version", "", "内核版本")
	sdkSHA := fs.String("sdk-sha256", "", "SDK 归档 sha256")
	ibSHA := fs.String("ib-sha256", "", "ImageBuilder 归档 sha256")
	kmodCount := fs.Int("kmod-count", 0, "本次产出的 kmod 数（供下次回归门禁比对）")
	ciURL := fs.String("ci-run-url", "", "本次 CI run 链接")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("用法: wrt meta build <device>@<line> --build-id B --vermagic V ...")
	}
	if *buildID == "" || *vermagic == "" {
		return errors.New("meta build: --build-id 与 --vermagic 必填")
	}
	v, fp, err := variantFingerprints(c, rest[0])
	if err != nil {
		return err
	}
	return emitJSON(c.stdout, buildBuild(v, fp, *buildID, *vermagic, *kernelVersion, *sdkSHA, *ibSHA, *kmodCount, *ciURL, time.Now()))
}

func buildBuild(v resolve.Variant, fp plan.Fingerprints, buildID, vermagic, kernelVersion, sdkSHA, ibSHA string, kmodCount int, ciURL string, now time.Time) artifacts.Build {
	return artifacts.Build{
		BuildID:   buildID,
		Line:      v.Line.ID,
		Target:    v.Hardware.Target,
		Subtarget: v.Hardware.Subtarget,
		// line_tree 与 upstream_commit 分两个字段：排障时一眼区分"配置改的"
		// 还是"源码改的"，plan 比对时再用同一种方式组合回 line 指纹。
		UpstreamCommit: sourceCommit(v),
		LineTree:       fp.LineTree,
		Vermagic:       vermagic,
		KernelVersion:  kernelVersion,
		SDKSHA256:      sdkSHA,
		IBSHA256:       ibSHA,
		KmodCount:      kmodCount,
		CIRunURL:       ciURL,
		CreatedAt:      stamp(now),
	}
}

func runMetaCurrent(c ctx, args []string) error {
	fs := flag.NewFlagSet("meta current", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	buildID := fs.String("build-id", "", "指向的构建编号")
	vermagic := fs.String("vermagic", "", "该构建的内核 ABI 标识")
	sdk := fs.String("sdk", "", "SDK 归档文件名")
	ib := fs.String("ib", "", "ImageBuilder 归档文件名")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	if *buildID == "" || *vermagic == "" {
		return errors.New("meta current: --build-id 与 --vermagic 必填")
	}
	return emitJSON(c.stdout, artifacts.Current{
		BuildID:             *buildID,
		Vermagic:            *vermagic,
		SDKArchive:          *sdk,
		ImageBuilderArchive: *ib,
		UpdatedAt:           stamp(time.Now()),
	})
}

func runMetaPackages(c ctx, args []string) error {
	fs := flag.NewFlagSet("meta packages", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	fp := fs.String("fingerprint", "", "plan 已算好的 packages 层指纹（CI 首选：指纹只算一次，这里只透传盖时戳）")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}

	// 两条取值路径，指向同一个 fp.Feed：
	//   --fingerprint <fp>       CI 用，plan 算好后透传，遵守「指纹只算一次」；
	//   <device>@<line>（位置参）  本地手动核对时用，就地重算（结果与 plan 一致）。
	feedFP := *fp
	switch {
	case feedFP != "" && len(rest) == 0:
	case feedFP == "" && len(rest) == 1:
		_, computed, err := variantFingerprints(c, rest[0])
		if err != nil {
			return err
		}
		feedFP = computed.Feed
	default:
		return errors.New("用法: wrt meta packages --fingerprint <fp>  或  wrt meta packages <device>@<line>")
	}

	return emitJSON(c.stdout, artifacts.PackagesMeta{FeedFingerprint: feedFP, UpdatedAt: stamp(time.Now())})
}

// sourceCommit 取 variant 的上游源码 commit，official 线没有 source 时为空。
func sourceCommit(v resolve.Variant) string {
	if v.Line.Source != nil {
		return v.Line.Source.Commit
	}
	return ""
}
