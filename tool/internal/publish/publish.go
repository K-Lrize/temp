// Package publish 把本地产物发布到 R2（S3 兼容），用 aws-sdk-go-v2 直连，
// 不经 rclone。
//
// 不 mock、不单测：mock 出来的 S3 行为与真实 R2 的差异本身就是一类 bug 源，
// 这一层靠对真实 R2 的集成验证（`wrt publish` 的手动冒烟 / CI）。
//
// 发布顺序是硬约束——**内容 → 索引 → 指针**。设备/plan 在同步窗口内随时可能读到
// 指针（current.json），若指针先于它引用的内容/索引落地，就会拿到一个指向不存在
// 对象的指针。PutDir 内置这个顺序。
package publish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
)

// Client 是一个面向单个桶的 R2 发布器。
type Client struct {
	s3       *s3.Client
	uploader *manager.Uploader
	bucket   string
}

// NewClient 按 R2 的 S3 端点与静态凭据建一个发布器。
//
// Region 固定 "auto"（R2 不分区）；UsePathStyle=true——用 account-id 端点时
// 桶名走路径而非子域，省掉一层 DNS/证书的坑。
func NewClient(endpoint, bucket, accessKey, secretKey string) (*Client, error) {
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("publish: endpoint/bucket/access-key/secret-key 都必填（经环境变量注入）")
	}
	c := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		UsePathStyle: true,
	})
	return &Client{s3: c, uploader: manager.NewUploader(c), bucket: bucket}, nil
}

// PutFile 上传单个本地文件到 key。大文件由 uploader 自动分片。
func (c *Client) PutFile(ctx context.Context, key, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	ct := mime.TypeByExtension(filepath.Ext(localPath))
	if ct == "" {
		ct = "application/octet-stream"
	}
	_, err = c.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(ct),
	})
	if err != nil {
		return fmt.Errorf("上传 %s → %s: %w", localPath, key, err)
	}
	return nil
}

// PutDir 把 dir 下所有文件上传到 keyPrefix 下（保持相对路径），按
// 内容 → 索引 → 指针 的顺序，返回实际上传的 key（即上传顺序）。
func (c *Client) PutDir(ctx context.Context, keyPrefix, dir string) ([]string, error) {
	var content, index, pointer []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		switch classify(d.Name()) {
		case classPointer:
			pointer = append(pointer, rel)
		case classIndex:
			index = append(index, rel)
		default:
			content = append(content, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var uploaded []string
	for _, group := range [][]string{content, index, pointer} {
		sort.Strings(group)
		for _, rel := range group {
			key := path.Join(keyPrefix, filepath.ToSlash(rel))
			if err := c.PutFile(ctx, key, filepath.Join(dir, rel)); err != nil {
				return uploaded, err
			}
			uploaded = append(uploaded, key)
		}
	}
	return uploaded, nil
}

// List 列出某前缀下的全部对象 key——供发布后自证、以及将来 gc 引用计数用。
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	p := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			keys = append(keys, aws.ToString(o.Key))
		}
	}
	return keys, nil
}

// GetJSON 拉一份 JSON 小票并解码进 out。对象不存在时返回 (false, nil)——
// gc 里读不到某个 manifest/current.json 只是"这条没有信息"，不是错误。
func (c *Client) GetJSON(ctx context.Context, key string, out any) (bool, error) {
	resp, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("拉取 %s: %w", key, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("解析 %s: %w", key, err)
	}
	return true, nil
}

// DeletePrefix 删除某前缀下的全部对象，返回删除个数。R2/S3 没有真目录——删一个
// release/build/kmod "目录"就是删它前缀下的所有对象。
func (c *Client) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	keys, err := c.List(ctx, prefix)
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		if _, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(k),
		}); err != nil {
			return 0, fmt.Errorf("删除 %s: %w", k, err)
		}
	}
	return len(keys), nil
}

// isNotFound 判定一个 S3 错误是否"对象不存在"。R2 视情况回 NoSuchKey 或
// 一个 404 的通用 API 错误，两种都当"没有"。
func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}

// 发布顺序的三档。current.json 是各自目录里唯一可变的指针，必须最后落地
// （落地时它指向的内容/索引都已就位）；packages.adb 是索引，落在内容之后、
// 指针之前；meta.json 是不可变档案，属内容，随内容一起落。
const (
	classContent = iota
	classIndex
	classPointer
)

func classify(name string) int {
	switch name {
	case "current.json":
		return classPointer
	case "packages.adb":
		return classIndex
	default:
		return classContent
	}
}
