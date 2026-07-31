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

## 用法

```bash
go run ./cmd/wrt --help          # 自文档入口
go run ./cmd/wrt lint            # 校验全部配置
go run ./cmd/wrt resolve --all   # 展开全部 variant
go run ./cmd/wrt resolve mt3600be@25.12
```

装成命令：`go install ./cmd/wrt`

## 文档

- [设计文档](docs/design.md)——架构、R2 布局、编排、GC、实施阶段
