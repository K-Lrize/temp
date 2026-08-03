# openwrt-build

用 GitHub Actions 为自有的若干台 OpenWrt 设备编译并发布固件与自有软件包，产物存
Cloudflare R2，设备通过标准 `apk` 在线更新。

同一台设备可以并行多条**源码线**（line）——官方线、自己打补丁的线——各自出一份固件、
各自有独立的包仓库。

## 核心概念

| 概念 | 是什么 | 在哪 |
| --- | --- | --- |
| **line** | 一条源码基线 + 由它产出的产物身份 | `lines/<id>/line.yaml` |
| **device** | 一台硬件：target/profile/arch + 装什么包 | `devices/<name>/device.yaml` |
| **set** | 可复用的包清单 | `sets/<name>.yaml` |
| **variant** | 构建的最小单位 = device × line，写作 `<device>@<line>` | 由前三者展开 |

不变量：**两台设备共用一条 line，当且仅当它们可以安全共用同一份 SDK/IB、同一份 kmod
仓、同一份用户态包仓库。**

## 目录

```
lines/<id>/line.yaml          源码基线 + 产物身份（overlay/ patches/ config/ 存在即生效）
devices/<name>/device.yaml    硬件事实 + 装什么包 + 出货到哪几条 line
sets/<name>.yaml              可复用的包清单
files/                        所有设备共用的 rootfs overlay
files-gen/                    合并后加工 overlay 的构建期脚本（哑执行器，不 fetch）
feed/                         自有软件包源码 + pin 到 commit 的外部 feed
tool/                         wrt 本体（Go 源码：cmd/ internal/ + go.mod）
```

> Go 相关命令都在 `tool/` 子目录下运行（`go.mod` 在那里）。wrt 会向上找含
> `lines/` 与 `sets/` 的目录当仓库根，所以编好的二进制在仓库任意位置都能直接跑。

## 用法

```bash
cd tool
go run ./cmd/wrt help            # 自文档入口
go run ./cmd/wrt lint            # 校验全部配置
go run ./cmd/wrt resolve --all   # 展开全部 variant
go run ./cmd/wrt resolve mt3600be@25.12

# 这次改动到底要不要重新构建
go run ./cmd/wrt plan [--repo-base https://repo.example.com] [--all]

# 三层软件源地址（构建期 / 运行期两份）
go run ./cmd/wrt repos mt3600be@25.12 --repo-base https://repo.example.com --vermagic 6.12.94-1-...

# 组装 rootfs overlay
go run ./cmd/wrt files mt3600be@25.12 /tmp/overlay
```

装成命令：`go install ./cmd/wrt`

## 开发

```bash
cd tool
go test -race ./...                       # 全部测试
go test ./internal/resolve -update        # 刷新 variant golden 基线
```

改了解析逻辑就会在 golden 基线上显示成逐行 diff。纯重构要求零 diff；
行为确实要变时刷新基线，并在 commit message 里说清楚为什么变。

## 文档

- [设计文档](docs/design.md)——架构、R2 布局、编排、GC、实施阶段
