# 名词表

系统里反复出现的名词,按"它到底干嘛的"分三类。任何一个词不确定,翻这里。

---

## A · 身份类 —— 给东西起名字

| 名词 | 是什么 | 例子 | 谁给的 |
|---|---|---|---|
| **variant** | 一份固件的身份 = device × line | `myrouter@25.12-mtk` | 你的配置组合出来 |
| **vermagic** | 内核 ABI 标签(内核版本 + 配置哈希)。驱动包和它对不上就装不上 | `6.12.94-1-ba5ca7f3e736...` | **OpenWrt 给的**,不是我们造的 |
| **build_id** | 一次"编 SDK/IB"的编号(只有 self 线有) | `20260730-142104-42-f0a60ee` | toolchain 构建时生成 |
| **release_id** | 一次"发固件"的编号 | `r20260730-142104-42-f0a60ee` | 每发一次固件生成 |

`build_id` / `release_id` 三段 = `<时间>-<CI跑号>-<commit前7位>`。三段缺一不可:同分钟并发会撞、跨 fork 跑号不唯一、手动重跑同 commit 会撞。

**release_id 不是指纹。** 它记的是"哪次 push、哪次 CI、什么时候",跟"内容变没变"无关。内容的身份是指纹(见 B),单独存。两个轴,别混。

---

## B · 判定类 —— 回答"变了没,要不要重造"

**指纹就是这个,别的都不是。** 三层,每层是一串 sha256:

| 指纹 | = 哈希(什么) | 它一变,触发 |
|---|---|---|
| **line 指纹** | line.yaml + overlay/patches/config + 源码 commit | 重编 SDK / 底座 |
| **feed 指纹** | `feed/` 整棵树 + line 指纹 | 重编自有包 |
| **variant 指纹** | device.yaml + files + 实际用到的 sets + 最终包列表 + 上两层 | 重组这份固件 |

分层的意义:**上层一变自动往下传**,不用手写"改了 A 要连带重建 B"。

一句话钉死区别:**指纹回答"变没变"**(内容一样 → 指纹永远一样,哪怕重跑十次);**release_id 回答"这是第几次发的"**(每发都是新号,哪怕内容一模一样)。

---

## C · 小票 / 指针类 —— R2 上那些 JSON

两种角色,全线同名同形:**`current.json`** = 每个被查询路径上唯一可变的**指针**,自带本层指纹(plan 一次读它就够)+ 取物句柄;**`meta.json`** = 不可变目录里的**档案**,记 GC 引用 + 溯源,plan 不读。

| 文件 | 角色 | 是什么 | 住哪 |
|---|---|---|---|
| **current.json**(工具链) | **指针** | line 指纹(plan 一跳)+ build_id/vermagic/SDK·IB 文件名 + kmod_count(门禁一跳)。回滚=改它指别的 | `<line>/targets/<t>/<s>/current.json` |
| **current.json**(包) | **指针** | 这批自有包对应的 feed 指纹 | `<line>/packages/<arch>/current.json` |
| **current.json**(固件) | **指针** | variant 指纹(plan 一跳)+ 指向的 release_id | `devices/<d>/<line>/current.json` |
| **meta.json**(工具链) | 档案 | 一次 SDK/IB 构建的溯源(line_tree/commit 分列、vermagic、sha256、内核版本) | `.../builds/<build_id>/meta.json` |
| **meta.json**(固件) | 档案 | 一份固件的记录:build_id/vermagic(GC 引用计数)+ manifest_file(门禁锚点)+ variant 指纹 | `.../releases/<release_id>/meta.json` |

为什么分指针和不可变:每次构建单独存一份、永不覆盖 → 能溯源、能回滚;**回滚 = 只改指针那一个字,不动任何产物**。指纹存进指针本身,plan 判定"要不要重造"永远只读一次。

---

## 把它们串起来 —— 为什么全都存在

只为一件事:让 `plan` 能回答"要不要重造":

```
plan 读该层指针 current.json
  → 直接取出里面存的指纹（无需第二跳）
  → 和本地现算的指纹比
     一样   → 跳过,不重造
     不一样 → 重造 → 生成新 build_id/release_id → 写新小票 → 改指针指向它
```

所以这一整套名词 —— 指纹、build_id/release_id、小票、指针 —— **全是为"增量构建"这一个目的服务的配件**。M3 自建(几小时的编译)之前用不上;现在存在只是因为按设计提前建了。
