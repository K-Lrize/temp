# openwrt-build · 设计文档

> 设计终稿。取代 `wrt-build`（P0–P6 重构后的形态）。
> R2 清空重来，不写迁移代码；密钥借此机会轮换。

---

## 0. 目标与非目标

**目标**：用 GitHub Actions 为自有的若干台 OpenWrt 设备编译并发布固件与自有软件包，
产物存 Cloudflare R2，设备通过标准 `apk` 在线更新。支持同一台设备并行多条源码线
（官方线 / 自己打补丁的线），支持自建 SDK/ImageBuilder。

**非目标**：不替代官方 buildbot；不对外提供公开包仓库；不覆盖没有实物的 target。

**这次重构要解决的**（旧仓库的实际问题，不是想象出来的）：

| 症状 | 根因 |
| --- | --- |
| 改一个字段要读五个 bash 文件 | 一份逻辑摊在 `wrt-resolve.sh` + `lib/{config,repos,plan,common}.sh` 上，进程间靠 `eval "$(jq -r '@sh ...')"` 手写反序列化 |
| schema 乱 | `schemas/*.json`、`lib/config.sh` 的 bash 断言、`lint-config.sh` 的语义规则，同一批约束写了三遍 |
| workflow 改不动、只能推 commit 调试 | `_toolchain.yml` 388 行里装的是逻辑（vermagic 探测、索引重建签名、元数据生成），不是编排 |
| 一台设备两条版本线要复制整个设备目录 | `device.channel` 是单值字段而非矩阵（`vm-armsr` / `vm-armsr-build` 就是这个复制的产物） |
| `mode` + `kmod_source` 四格矩阵只有对角线可用 | 一个布尔值被拆成两个字段 + 一条相等性断言 + 一篇 ADR |

---

## 1. 核心不变量

> **两台设备共用一条 line，当且仅当它们可以安全共用同一份 SDK/IB、同一份 kmod 仓、
> 同一份用户态包仓库。**

`line` 是"一条源码基线 + 由它产出的产物身份"。旧仓库里叫 channel，改名是为了避免与
OpenWrt 官方的 release channel（snapshot / 23.05 / 25.12）混淆 —— `25.12-mtk` 不是
任何官方概念。

推论，直接写成 lint 规则：上游版本不同 → 不同 line；源码 repo/commit 不同 → 不同 line；
`artifacts` 不同 → 不同 line（自编产物 ≠ 官方产物，即使源码一样）；有无 patch → 不同 line。

**约束挂在 line 上，不挂在 device 上** —— 一个 device 无法约束另一个 device，这是旧仓库
`vm-armsr` 与 `vm-armsr-build` 能共用同一个包仓库却基线不同的根因。

---

## 2. 三层正交生态

```
L1 自有业务包   r2://<line>/packages/<arch>/                        用户态，滚动，只增不删
L2 内核与底座   r2://<line>/targets/<t>/<s>/{builds,kmods,packages}/  vermagic 锁定
L3 官方社区源   https://downloads.openwrt.org/releases/<openwrt_version>/    luci/base/routing/... 全量借用
```

> 注：L2 的 target base 包（libc/kernel/fstools）与 L3 里那个字面上就叫 `base` 的社区
> feed 分类是两个不相关的东西，只是撞了名字——前者受下面的 `artifacts` 开关控制，
> 后者无论哪种 `artifacts` 都永远借官方（见 §5.2）。

`artifacts` 一个开关决定 L2 整层的来源：

```yaml
artifacts: official   # SDK/IB + kmod + target base 全部借官方
artifacts: self       # SDK/IB + kmod + target base 全部自己编
```

取代旧的 `mode` + `kmod_source` 两字段。旧设计的四格矩阵里只有对角线能工作，另两格各自
坏在不同地方且都只在刷机后暴露（`official+self` 路径不存在 → 设备 `apk update` 404；
`self+official` 固件能刷但 vermagic 对不上、驱动装不上），所以它本来就是一个布尔值。

L3 无论哪种都借官方，按 `openwrt_version` 键控。**硬约束**：`artifacts: self` 时
`openwrt_version` 必须与 `source.commit` 属于同一条版本线，否则借来的 luci/packages
与自编 libc 对不上。这条进 `Validate()`。

---

## 3. 配置模型

### 3.1 目录

```
lines/<id>/
├── line.yaml
├── overlay/                  rsync 覆盖进源码树（幂等），存在即生效
├── patches/                  针对 openwrt.git 自身的 quilt 系列
└── config/                   buildroot diffconfig 片段

devices/<name>/
├── device.yaml
└── files/                    本设备的 rootfs overlay

files/                        所有设备共用的 rootfs overlay（合并时排在设备层之前）
files-gen/                    合并后加工 overlay 的构建期脚本（生成/改权限/展开模板）

sets/<name>.yaml              包集

feed/
├── feeds.conf                外部 feed pin 到 commit
└── <pkg>/                    自有包源码（Makefile + files/）
```

rootfs overlay 与包清单一样是**有序层**：`files/` 在前，`devices/<name>/files/`
在后，同路径后者覆盖前者。层数将来要加就加，合并函数不该硬编码两层。

### 3.2 line.yaml

```yaml
# lines/25.12-mtk/line.yaml
id: 25.12-mtk
description: 基于 25.12.5，带 mediatek PHY 补丁
openwrt_version: 25.12.5    # L3 社区 feed 永远借这条线；artifacts:official 时 L2 也借它
artifacts: self               # official | self
source:                       # artifacts: self 时必填
  repo: https://github.com/openwrt/openwrt
  commit: f0a60eee2fe051741c643ea6118718aae1ef17fb
  ref: v25.12.5               # 只供人读，CI 检出永远用 commit；唯一的机器用途是 lint 阶段
                               # 核对它与 openwrt_version 是否同一条版本线
```

`openwrt_version` 只填完整 patch 号（`25.12.5`），域名由代码统一拼 —— 目的是杜绝"同一条版本线
不同设备各自指向 25.12.4 / 25.12.5"这种漂移。

`requires_build`（`overlay/` `patches/` `config/` 任一非空）是派生值，不是字段。
lint 规则：`requires_build && artifacts == official` → ERROR（官方不可能有你的产物）。
反向不成立 —— 官方源码自己编是合法场景（验证自建链路、只改 buildroot `.config`、
自己控制 base 包编译选项）。

### 3.3 device.yaml

```yaml
# devices/mt3600be/device.yaml
name: mt3600be
hardware:
  target: mediatek
  subtarget: filogic
  profile: glinet_gl-mt3600be
  arch: aarch64_cortex-a53

metadata:                     # 事实性硬件信息，不参与构建逻辑
  soc: MT7987a                # 市场材料写 MT7988 是错的
  wifi: MT7990

lines: [25.12, 25.12-mtk]     # ★ 出货矩阵

packages:
  include: [common, base-router, luci-zh, modem, dev-tools]
  add: [sing-box]
  remove: []

image:
  rootfs_partsize: 256

repos: []                     # 额外第三方 apk 源
```

设备只描述**硬件事实 + 装什么**，一个源码字段都没有。切换版本线 = 改 `lines` 里的一项。

### 3.4 sets/*.yaml

```yaml
# sets/base-router.yaml
name: base-router
description: 实体路由器共用的网络底座
add:
  - ip-full
  - nftables-json
  - dnsmasq-full
  - wpad-openssl
remove:
  - dnsmasq                   # 被 dnsmasq-full 取代
  - wpad-basic
  - wpad-basic-mbedtls
  - wpad-mbedtls
  - wpad-wolfssl
```

实际切分（mt3600be 原本硬列的 124 个包，M0 里拆成 11 个）：

| 包集 | 内容 |
| --- | --- |
| `common` | 每台设备（含虚拟机）都装的排障工具 |
| `luci` | LuCI 主体、默认主题与核心页面中文语言包 |
| `router-net` | DHCP/DNS、IPv6、nftables 转发、PPPoE、WireGuard |
| `router-wifi` | wpad-openssl 变体（并排掉其余 wpad） |
| `router-qos` | SQM/CAKE、BBR、中断均衡 |
| `usb-storage` | USB 存储与 ext4 挂载 |
| `modem` | USB 蜂窝模块与手机 USB 网络共享 |
| `dns-filter` | SmartDNS 分流、banIP 黑名单 |
| `ops` | 证书续期、DDNS、看门狗、定时任务、WoL、UPnP、日志轮转 |
| `monitoring` | 流量统计与 Prometheus 指标导出 |
| `dev-tools` | 上机排障用的 shell、编辑器与网络工具 |

切分按**功能**而不是按包类型——「这台设备要不要 4G 模块 / USB 存储 / QoS」才是加第二台
设备时真正要做的决定。

**i18n 包永远与它翻译的那个 app 待在同一个集里**，不单独成一个「中文集」：
`luci-i18n-<app>-zh-cn` 依赖 `luci-app-<app>`，一个独立的语言集会把它引用的每个 app
都当依赖装回来，等于绕过了包集的取舍。（设计初稿里那个 `luci-zh` 正是这个错误。）

**不支持 set 嵌套。** 合并函数写成接受**有序层列表**，所以将来插一层（如 `profiles/`）
是配置变更而非代码变更，但现在不建空目录。

### 3.5 包列表合并语义（定死，这是最容易出 bug 的地方）

```
layers   = include 里的 sets（按 include 顺序）+ device 自身
add      = 各层 .add 按序并集，去重
remove   = 各层 .remove 并集
PACKAGES = add ++ map("-" + ., remove)          # IB `make image PACKAGES=` 规范
```

**冲突规则**：某个包同时落进最终 `add` 与最终 `remove` → **报错**，要求显式解决。
不做"后面的层覆盖前面的" —— 那种规则在五层之后没人能预测结果。真需要 device 撤销
某个 set 的 remove 时再加 `force_add`，现在不加（YAGNI）。

---

## 4. R2 布局

两条铁律：**设备面向的路径必须永久稳定**（会被烧进 `/etc/apk/repositories.d/`，
刷机后改不了）；**CI 面向的路径必须不可变**（可回滚、可溯源）。

```
r2://<bucket>/
├── keys/<fingerprint>.pem                     公钥（同时进 git）
│
├── <line>/                                    ← 会被烧进设备，line 必须是前缀
│   ├── packages/<arch>/                       【稳定】自有包，只增不删
│   │   └── *.apk, packages.adb, build-meta.json
│   └── targets/<target>/<subtarget>/
│       ├── current.json                       【指针】本目录唯一可变文件
│       ├── builds/<build-id>/                 【不可变】一次工具链构建一个目录
│       │   ├── sdk-ib/{openwrt-sdk-*, openwrt-imagebuilder-*, sha256sums}
│       │   └── build.json
│       ├── kmods/<vermagic>/                  【稳定】设备运行期直接命中
│       └── packages/                          【稳定】target base 包
│
└── devices/<device>/<line>/                   ← 只被人和更新检查器读
    ├── latest.json                            【指针】
    └── releases/<release-id>/                 【不可变】
        └── *.img.gz, *.manifest, manifest.json, resolved.json, sha256sums(.sig)
```

**为什么 SDK/IB 不可变而 kmod/target-base 覆盖式**：SDK/IB 只被 CI 自己消费，搬进
`builds/<build-id>/` 换来"回滚 = 把 `current.json` 指回另一个 build-id"；kmod/target-base
是已刷机设备固化的 URL，搬进 build-id 之下会让在线更新失效，所以它们靠引用计数 GC
而非不可变性管理生命周期。

**为什么固件是 `devices/<device>/<line>/` 而不是 `<line>/devices/<device>/`**：
人的入口是设备（"给 mt3600be 刷机" → 一个目录下看见所有版本线并排）；GC 按设备分组，
`devices/<d>/*/releases/` 一次列举即可。反过来 line 顶层要跨所有前缀扫再聚合。
也不放回旧布局的 `<line>/targets/<t>/<s>/firmware/<device>/` —— GC 当初就是因此需要
`--max-depth 6` + 正则从路径里捞设备名。

ID 格式沿用：`build-id = <utc:%Y%m%d-%H%M%S>-<run_number>-<sha7>`，
`release-id = r` + 同样三段。三段缺一不可：纯时间戳同分钟并发会撞，纯 run_number
跨 fork 不唯一，纯 sha7 手动重跑必撞。字典序即时间序，GC 排序不需要额外字段。

**自定义域名**：R2 公网根 CNAME 到自有域名，第一天就做。这些 URL 会烧进固件，
默认域名一变所有已刷机设备同时失效，改起来的代价随设备数线性增长。

---

## 5. 解析与指纹

### 5.1 Variant

构建的最小单位是 **variant = device × line**，id 写作 `<device>@<line>`。

```
wrt resolve mt3600be@25.12-mtk
  → { device, line, hardware, packages[], repos{build,runtime}, fingerprints{...} }
```

这是全系统唯一的数据生产者。`plan` / `gc` / golden / CI 的每个 job 都消费这份对象，
没有任何一方自己去读 YAML。

**纯函数边界**：解析层只读仓库内文件，不发网络、不写盘。需要外部事实的字段
（kmod vermagic、R2 公网根）一律由调用方注入。旧仓库为了破一次这条线（在 repos 层
内嵌一个 curl 拉 current.json），先后长出三个环境变量后门、一张硬编码假 vermagic 表
和一个穿透三层脚本的参数。不要再往这层放 I/O。

在 Go 里 vermagic 是构造 `Repos` 时的必填参数，缺失就是构造失败 —— 不再需要旧仓库
`{VERMAGIC}` 占位符 + 下游 grep 拦截那套跨进程字符串协议。

### 5.2 三层 repositories

```
L1  <repo_base>/<line>/packages/<arch>/packages.adb
L2  kmod:         artifacts=self ? <repo_base>/<line>/... : <upstream_base>/...
                  .../targets/<t>/<s>/kmods/<vermagic>/packages.adb
    target base:  同上判定
                  .../targets/<t>/<s>/packages/packages.adb
L3  <upstream_base>/packages/<arch>/{base,luci,packages,routing,telephony}/packages.adb
extra  device.repos[]
```

产出两份：`build`（构建期，L1/L2 可替换成 `file://` 本地预同步路径）与
`runtime`（写进设备 `/etc/apk/repositories.d/99-custom.list`，**永远是在线 URL**）。
两份分开是硬要求 —— 构建期的 `file://` 路径漏进设备就是一条永久失效的软件源。

### 5.3 指纹分层

```
line_fp    = sha256(line.yaml + overlay/ + patches/ + config/ + source.commit)
feed_fp    = sha256(feed/ 树 + line_fp)
variant_fp = sha256(device.yaml + files/ + 该 device 实际 include 的 sets
                    + 最终包列表 + line_fp + feed_fp)
```

严格分层依赖，上层变化自动传导到下层，不需要显式列举"改了 A 要连带重建 B"。

**相对旧实现的修正**：旧的 firmware 指纹 hash 整个 `packages/` 树。引入 sets 之后
必须**只计入该设备实际引用到的 sets**，否则改一个没人用的包集会触发全设备重建 ——
这个洞在有了包集之后会立刻变成日常痛点。

`build.json` 里 `line_tree_sha` 与 `upstream_commit` 分两个字段存（排障时一眼区分
"源码改的"还是"我们自己配置改的"），比对时用同一种方式重新组合。

---

## 6. 代码结构

`✓` 已落地，其余按 §12 的阶段推进。

Go 源码都在 `tool/` 子目录（`go.mod` 在此，与仓库根的配置树解耦）：

```
tool/cmd/wrt/main.go         ✓
tool/internal/
├── config/      types.go = schema = 唯一校验点；load.go；validate.go；packages.go  ✓
├── resolve/     device × line → Variant                                            ✓
├── repos/       三层 URL 装配（纯函数）                                              ✓
├── files/       rootfs overlay 组装 + files-gen 脚本执行                             ✓
├── plan/        指纹计算 + 远端比对 → 三矩阵                                          ✓
├── artifacts/   R2 路径规则 + build/current/manifest/latest 类型（只有类型，无 I/O）  ✓
├── diag/        全仓库共用的诊断类型                                                  ✓
├── feed/        自有包 Makefile 校验                                                  ✓
├── publish/     S3 客户端（aws-sdk-go-v2 打 R2 端点），封装发布顺序
├── gc/          引用计数 + 熔断
├── source/      src prepare/export
└── sign/        EC P-256 签名与校验
scripts/         qemu-boot.sh、local-ib.sh（真的是 shell 的活）
```

golden 基线按 Go 惯例放在产出它的包旁边（`internal/resolve/testdata/variants/`），
不集中到仓库根——基线与产出它的代码待在一起，改代码时不会漏改基线。

**没有 Makefile。** `wrt --help` 本身就是自文档入口，再套一层 make 只是又一个要同步的
地方。qemu 冒烟和本地 IB 组装做成 `wrt boot` / `wrt build-local`，内部 exec 那两个脚本。

**JSON Schema 是生成物**：`wrt schema export > schemas/*.json` 提交进仓库供 IDE 补全，
但唯一真相是 Go 类型 + `Validate()`。绝不手写第二份。

### CLI

```
wrt lint                                   全部配置校验（结构 + 语义 + 跨层包冲突；feed Makefile 规则待补）  ✓
wrt resolve <variant>|--all                打印 Variant JSON                                                  ✓
wrt plan [--repo-base URL] [--all]         三矩阵                                            ✓
wrt repos <variant> --vermagic X --repo-base Y [--local-l1 PATH]                             ✓
wrt files <variant> <dest>                 组装 files overlay（合并文件层 + 跑 files-gen 脚本）  ✓
wrt src prepare --line X --target t/s      clone@commit → overlay → quilt → .config → 符号存活校验
wrt src export --line X                    内核改动写回 lines/<id>/overlay/
wrt meta build|current|manifest|latest     生成元数据 JSON
wrt publish <src> <dst> [--only NAME]      S3 发布，内置"内容 → 索引 → 指针"顺序
wrt verify <target>                        签名与校验和验证
wrt gc [--apply] [--keep N]                引用计数回收，默认 dry-run
wrt boot <variant>                         qemu 冒烟
wrt build-local <variant>                  本地 Docker IB 组装
wrt doctor                                 环境自检（等 docker/qemu 这类外部依赖进来再写，现在写只会是空壳）
wrt schema export                          导出 JSON Schema
```

**`wrt` 二进制不发 GitHub Release**：只有两个消费者 —— 本仓库 CI 和你本地
（`go run ./cmd/wrt`）。发版只会多一道"改了 wrt 要先发 release 才生效"的摩擦。

---

## 7. CI 编排

```
release.yml  (push main / workflow_dispatch)
├── plan       go build wrt → 上传 artifact；wrt plan → 三矩阵；统一算一次 release_id（M2）
├── toolchain  needs: plan          按 (line, target, subtarget)，仅 artifacts:self
├── packages   needs: [plan,toolchain]  按 (line, arch)
├── firmware   needs: [plan,packages]   按 variant
└── gc         needs: firmware
```

`needs:` 就是依赖图，从上往下读一遍就知道顺序。不用 `workflow_run` 链、不用
dispatch 回踢 —— 旧仓库那两个补丁（查 `gh run list` 探测对方是否在跑、push 事件跳过
`mode:build` 设备）本身就是"编排顺序不可知"的症状而非解法。

**`if:` 条件必须带 `!cancelled()`**：无变更时上游矩阵为空 → 该 job 被 skip，而 `needs`
里出现被 skip 的 job 会连坐让下游也被 skip。少了这句，最常见的"line 没改"情况下整条
流水线会静默什么都不干、还显示绿色。

**wrt 二进制分发**：`plan` job 里 `go build` 一次（`setup-go` 带构建缓存后 ~3s），
上传 `actions/upload-artifact`；下游 job 下载即用。一次 run 内所有 job 用的是字节相同
的二进制。

**每个 job 的 step 形状**：`checkout → 取 wrt → wrt <verb> → make → wrt publish`。
目标是 `_toolchain.yml` 从 388 行降到 80 行以内。

**runner**：`runs-on` 从仓库变量读。主路径 GitHub-hosted —— 全量编译是网络密集型
（拉几 G 源码 + 传 SDK/IB/几百个 kmod），GH runner 在数据中心里跑这两头是满速。
构建逻辑同时兼容无状态（ccache 走 `actions/cache` **按周分桶**，不按 commit —— 按
commit 分桶意味着每个 commit 写一份新的 2–3G 缓存，几个 commit 就把仓库 10GB 配额
挤爆、LRU 淘汰掉刚存的）与有状态（自建 runner，持久化 `build_dir`/`staging_dir`）。
真到每天要编好几次时，切过去是改一个变量加一个 label。

**PR 门禁**（`ci.yml`）：lint + 单测 + golden diff + 构建 `vm-armsr` 固件但不发布。
没有这条，任何设备改动只能推 main 才知道对不对，而 main 一跑就直接发布到 R2。

### 必须存在的门禁

| 门禁 | 拦什么 |
| --- | --- |
| 磁盘预检（全量编译前 ≥ 25GiB） | 编到一半 ENOSPC |
| `.config` 符号存活断言 | `make defconfig` 静默丢弃依赖不满足的符号 |
| 产物形状断言（SDK/IB 归档、kmod/target-base 数量非零、两份索引齐全、kmod 数相对上次下降超阈值） | `IGNORE_ERRORS="n m"` 放过的失败产出残缺产物 |
| 签名 `REQUIRE_KEY=1` | 静默发布未签名产物 |
| 上游归档强制校验 `sha256sums` | 上游被投毒 / 下载截断 |
| manifest 回归门禁（与上一个 release 比，有包**消失**即 fail） | IB 阶段条件依赖失效那类坑 |
| 发布顺序：内容 → 索引 → 指针 | 设备在同步窗口拿到不一致索引 |

`IGNORE_ERRORS="n m"` 保留（与官方 buildbot 一致，`CONFIG_ALL_KMODS=y` 下总有上游
本来就编不过的包，让它们打断一次三小时全量编译毫无意义）；完整性靠**发布前的产物
形状断言**保证，不靠"任何包失败就整个失败"—— 后者既拦不住真正的残缺（少一个包照样
编完），又会被上游偶发问题反复打断。

---

## 8. GC：引用计数

```
live_releases  = 每 (device, line) 最新 N 个 release ∪ pinned
live_builds    = {r.build_id ∀ live_releases} ∪ {current.json.build_id ∀ line/target}
live_vermagics = {r.vermagic ∀ live_releases} ∪ {current.json.vermagic ∀ line/target}

删：releases ∉ live_releases
    builds/<id> ∉ live_builds
    kmods/<vm>  ∉ live_vermagics
    packages/<arch>/：保留每包名最新 M 版 ∪ 被 live_release 的 .manifest 引用的版本
```

性质：**只要一台设备的固件还在保留期内，它的 kmod 仓就一定还在。** 不依赖目录名排序、
mtime、宽限期这三套并存的启发式（旧仓库三道防线并存，且 kmod 只保 Top-2 + 7 天，
三周前刷机的设备驱动仓已被删 —— 与"老设备任何时候都能 apk add"直接矛盾）。

默认 dry-run。**熔断**：单次计划删除超过 30% 对象即失败并要求人工确认 —— GC bug
删光产物是这类系统最典型的灾难。

---

## 9. 安全与签名

新仓库 = 新的 EC P-256（`prime256v1`）密钥对。私钥只存 GitHub Secrets、写
`$RUNNER_TEMP`、job 级 post 清理；公钥进 git（它本来就是公开的）并注入每台设备的
rootfs 与 IB host。**离线备份私钥** —— 私钥丢失 = 所有已刷机设备的软件源永久失信。

| 项 | 要求 |
| --- | --- |
| 签名 | fail closed，无私钥直接失败 |
| 上游归档 | 强制校验官方 `sha256sums`，结果记进 `build.json` |
| secret 传递 | 一律 `env:` + `$VAR`，不插值进 run 脚本体 |
| 外部 feed | `feed/feeds.conf` pin 到 commit（否则 golang 版本随上游漂移，同一 commit 不可复现） |
| `PKG_HASH` | lint 拒绝 `skip`/`SKIP` |
| workflow 权限 | 顶层 `permissions: contents: read` |
| 第三方 action | 按 commit SHA 固定 |
| R2 凭据 | 拆两把：CI 写入用 + 校验只读用 |
| rclone | 不用了（S3 SDK 直连），旧仓库那条 `curl \| sudo bash` 随之消失 |

APK 版本号规则（旧仓库 `docs/reference/apk-versioning.md` 的沉淀）**内化成 `wrt lint`
的校验规则**，而不是搬一份 markdown —— 文档会腐烂，校验不会。

---

## 10. 测试

**① 纯函数全覆盖**（table-driven）：repos 三层装配、包集合并、指纹、GC 集合运算、
路径规则。这几块是全系统最容易把路由器刷砖的地方，也最好测。

必测的具体行为：

```
- artifacts:official / self 各生成正确的 L1/L2/L3 URL 列表
- vermagic 缺失时构造 Repos 必须失败（不是产出缺一层的列表）
- build 期的 file:// 路径不得出现在 runtime 列表里
- set 的 remove 参与合并；add 与 remove 冲突必须报错
- include 顺序影响最终包列表顺序
- requires_build && artifacts:official → lint 报错
- artifacts:self 且 openwrt_version 与 source 版本线不符 → lint 报错
- 改一个未被任何设备 include 的 set，不改变任何 variant 指纹
```

**② golden**：`testdata/variants/<device>@<line>.json`，`go test -update` 刷新。
任何触及解析逻辑的改动在 PR 里变成逐行可读的行为 diff。纯重构类改动要求零 diff。

**③ manifest 回归门禁**（见 §7）—— 直击 IB 漏依赖那类坑。

**④ qemu 冒烟**：`armsr/armv8` 固件在 CI 里起来，断言 开机拿 IP → `apk update` 对
**真实 R2 源**成功且**无 UNTRUSTED** → `apk add sing-box` 成功 → 服务起得来。
**这一条的价值超过前三条加起来** —— 它验证的是设备视角，不是构建视角。

不刻意凑总覆盖率数字。`publish` / `gc` 的 I/O 层不 mock S3 —— mock 出来的 S3 行为
与真实 R2 的差异本身就是一类 bug 源，那层靠 CI 集成验证。

---

## 11. 从旧仓库搬什么

**搬**：

- `packages/` 的自有包源码 → `feed/`（sing-box、sing-box-alpha、luci-app-sing-box、hello-world 的 Makefile 与 files/）
- `packages/feeds.conf` → `feed/feeds.conf`（pin 到 commit 的那两行）
- 设备配置：mt3600be 的 ~120 个包**拆成 sets/**，`hardware`/`metadata`/`image` 直接迁
- `devices/_common/files/` 与 `devices/mt3600be/files/`：uci-defaults 脚本、zsh 配置、dropbear authorized_keys、sysctl BBR

**不搬**：旧 bash 脚本、workflow、schemas、docs、tests、密钥、R2 上的一切。
`docs/reference/apk-versioning.md` 的规则内化进 `wrt lint`（见 §9）。

**旧仓库处理**：归档保留，不删 —— 排障时"当初为什么这么做"还得回去查 git 历史。

---

## 12. 实施阶段

| 阶段 | 内容 | 出口标准 |
| --- | --- | --- |
| **M0** ✓ | 仓库骨架；`config` 类型 + `Validate()`；`wrt lint` / `resolve`；sets 拆分 | ✓ 对同一份配置，`wrt resolve` 与旧仓库 `expected/resolved.json` 语义等价（`TestMigrationPreservesPackageSets` 逐包核对 124 项） |
| **M1** ✓ | `repos` / `files` / `plan` + 纯函数单测 | ✓ 判定逻辑已就位并有离线测试覆盖；「无变更时三个矩阵为空」要等 M2 真的往 R2 发布之后才能端到端验证 |
| **M2** ⌁ | `artifacts` / `publish` / `meta`；`release.yml` + `_firmware.yml`；先跑通 `artifacts: official` 的 vm-armsr | 一次 push 产出一份可刷的固件，落到新 R2 布局 |
| **M3** ⌁ | `_toolchain.yml`；跑通 `artifacts: self` | **自建 SDK/IB 落地**；`25.12-selfbuild` line 产出可用的 SDK/IB/kmod/target-base |
| **M4** ◐ | `gc` + `verify` + qemu 冒烟 + PR 门禁 `ci.yml` | qemu 里 `apk update` 无 UNTRUSTED |
| **M5** | mt3600be 接入；`25.12-mtk` line + 第一个内核补丁 | 一台设备两条线并行出货 |

标记：`✓` 出口标准已验证；`⌁` 代码就绪、等 CI 实测才能验出口（本地无从跑全量编译 /
真发布）；`◐` 部分落地。M3 的源码准备（clone@commit + `wrt feeds`）内联在 `_toolchain.yml`
里——那是 git 克隆而非决策逻辑，没必要单开 `wrt src` 动词；要到 M5 引入内核补丁（quilt +
符号存活校验，才是真逻辑）时再落 `wrt src prepare/export`。

M3 是这次重构真正的目的地 —— 前面几步都是为了让"自己编 SDK/IB"这件事有个能承载它的
结构。

---

## 13. 进度与交接

> 截至 2026-08-02，M0/M1 已验证完成，M2/M3 代码就绪（待 CI 实测），M4 已落 `ci.yml`
> PR 门禁、`gc`（dry-run）与 manifest 回归门禁（`wrt verify manifest` + `_firmware.yml`
> 收/比对 `.manifest`）。全部 commit 在本地 `main` 分支（未 push）。
>
> M4 剩余、且属「纯代码、不碰真实基建」可继续推进的：`wrt verify` 的签名/校验和
> 验证（现只有 `manifest` 子命令）、`gc --apply` 熔断切换。`qemu 冒烟` 与「PR 里真
> 构建 vm-armsr 固件」要碰真实 R2/密钥/官方下载站，归入「真实测试」，另行开启。

### 已落地

| 包 | 职责 | 覆盖率 |
| --- | --- | --- |
| `internal/config` | 类型即 schema，载入 + 全部配置校验 | 94.1% |
| `internal/diag` | 全仓库共用的诊断类型 | 100% |
| `internal/resolve` | device × line → variant | 90.7% |
| `internal/feed` | 自有包 Makefile 校验 | 97.3% |
| `internal/repos` | 三层软件源装配 | 100% |
| `internal/files` | overlay 有序层合并 + files-gen 脚本执行 | 81.0% |
| `internal/plan` | 三层指纹 + 三矩阵 + 远端比对 | 86.8% |
| `internal/artifacts` | R2 路径规则与元数据类型（只有类型，无 I/O） | 76.9% |

CLI：`wrt lint | resolve | plan | repos | files`（`go test -race ./...` 全绿）。

### 实现期定下、初稿里没有的约定

- **rootfs overlay 两层** —— 仓库根 `files/` + `devices/<name>/files/`，合并后的加工
  脚本落 `files-gen/`（同样两层）。脚本是**哑执行器**：只注入 `WRT_FILES_DIR`
  （overlay 目录，加 `WRT_` 前缀避免与 shell 通用名撞车），cwd 为仓库根按相对路径
  读输入，**拿不到 variant / 包列表**——要按这些做分支就是在做本该在 Go 里做的
  决定。**不在脚本里 fetch 外部东西**：要往 rootfs 放「有版本、要追更新」的第三方
  代码（如 zsh 插件），打成 `feed/` 的 apk，由 `PKG_HASH` 钉死、走 feed 指纹，
  而不是构建期拉取绕过内容寻址。
- **golden 基线放在产出它的包旁边**（`internal/resolve/testdata/`），不集中到仓库根。
- **`internal/artifacts` 提前到 M1** —— plan 的远端比对需要读 R2 上那几份元数据，
  与将来发布侧共用一份定义，避免两边各写一遍再漂移。目前只有类型与路径，没有 I/O。
- **`wrt doctor` 暂不实现** —— 现阶段没有外部工具可检（docker/qemu/密钥要到 M2/M3
  才进来），现在写只会是一份空壳。
- **`feed/Makefile`（feed 根的那个）保留** —— 旧仓库的注释自己也说不确定是否必需，
  现阶段没有能验证它的构建链路，等 M2 的软件包流水线跑通再决定删不删。

### 下一步（M2）要先定的两件事

1. **R2 的自定义域名。** 这些 URL 会被烧进固件的 `/etc/apk/repositories.d/`，刷机之后
   改不了。第一天定最便宜，改起来的代价随已刷机设备数线性增长。
2. **新密钥对什么时候生成。** 新仓库 = 新的 EC P-256 对（旧密钥不搬，借这次轮换）。
   私钥进 GitHub Secrets + 离线备份，公钥进 git。发布链路一旦跑起来就不能再换。

### 仍然未决

1. ~~新仓库名~~ — 已定 `openwrt-build`
2. ~~sets 的切分粒度~~ — M0 已切成 11 个，见 §3.4
3. **第二台实体设备是什么** —— 会影响 `sets/` 是否需要再分层，M5 之后再看

---

## 附：一句话对照

| 旧仓库 | 新设计 |
| --- | --- |
| `channel` | `line`（避免与官方 release channel 混淆） |
| `device.channel` 单值 | `device.lines[]`，variant = `<device>@<line>` |
| `mode` + `kmod_source` + 相等性断言 | `artifacts: official \| self` |
| `packages/`（自有包源码） | `feed/` |
| 设备各自硬列全部包 | `sets/*.yaml` + `include` |
| `schemas/*.json` 手写 | Go 类型 + `Validate()`，schema 是生成物 |
| 20 个 bash 脚本 ~1.9k 行 | 一个 Go 二进制 + 2 个 shell |
| `eval "$(jq -r '@sh ...')"` | 结构体 |
| `{VERMAGIC}` 占位符 + 下游 grep | 构造 `Repos` 的必填参数 |
| `publish-r2` + `setup-rclone` action + `curl \| sudo bash` | `wrt publish`（S3 SDK） |
| `Makefile` + `make help` | `wrt --help` |
| `devices/<d>/releases/` | `devices/<d>/<line>/releases/` |
| firmware 指纹 hash 整个 `packages/` 树 | 只计入该设备 include 的 sets |
