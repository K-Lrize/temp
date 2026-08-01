package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/K-Lrize/openwrt-build/internal/publish"
)

// R2 凭据统一从环境读——绝不作为命令行参数（免得进 shell 历史 / CI 日志）。
func envS3Endpoint() string   { return os.Getenv("WRT_S3_ENDPOINT") }
func envS3Bucket() string     { return os.Getenv("WRT_S3_BUCKET") }
func envAWSAccessKey() string { return os.Getenv("AWS_ACCESS_KEY_ID") }
func envAWSSecretKey() string { return os.Getenv("AWS_SECRET_ACCESS_KEY") }

// runPublish 把本地目录发布到 R2 前缀，内置内容→索引→指针的顺序。
//
// 凭据一律经环境变量注入，绝不作为参数（免得进 shell 历史 / CI 日志）：
//
//	WRT_S3_ENDPOINT        R2 的 S3 端点
//	WRT_S3_BUCKET          桶名
//	AWS_ACCESS_KEY_ID      访问密钥 ID
//	AWS_SECRET_ACCESS_KEY  机密访问密钥
func runPublish(c ctx, args []string) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	verify := fs.Bool("verify", true, "发布后重新列举一遍前缀，自证对象确实落地")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return errors.New("用法: wrt publish <本地目录> <R2 前缀>\n" +
			"凭据经环境注入：WRT_S3_ENDPOINT WRT_S3_BUCKET AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY")
	}
	src, dst := rest[0], rest[1]

	cl, err := publish.NewClient(envS3Endpoint(), envS3Bucket(), envAWSAccessKey(), envAWSSecretKey())
	if err != nil {
		return err
	}

	ctx := context.Background()
	keys, err := cl.PutDir(ctx, dst, src)
	if err != nil {
		return err
	}
	for _, k := range keys {
		fmt.Fprintln(c.stdout, "  ↑", k)
	}
	fmt.Fprintf(c.stdout, "已发布 %d 个对象（顺序：内容→索引→指针）\n", len(keys))

	if *verify {
		remote, err := cl.List(ctx, dst)
		if err != nil {
			return fmt.Errorf("发布后自证列举失败: %w", err)
		}
		fmt.Fprintf(c.stdout, "自证：%s 下现在有 %d 个对象\n", dst, len(remote))
	}
	return nil
}
