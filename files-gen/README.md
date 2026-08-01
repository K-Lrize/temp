# files-gen — 构建期 overlay 加工脚本

`wrt files` 把静态的 `files/` 层合并进 overlay 之后，会执行这里的 `*.sh`
（先本目录、再 `devices/<device>/files-gen/`，各自按文件名字典序）。

用途：对**已经合并好**的 overlay 做纯加工——生成文件、改权限、按模板展开。

约定，别破坏：

- **脚本是「已提交输入」的确定函数**：自己的脚本 + 它读的仓库内文件。产物由
  这些输入唯一决定，`plan` 才能靠指纹判断要不要重建。
- **只有一个环境变量 `WRT_FILES_DIR`**（overlay 目录）。cwd 是仓库根，按相对
  路径读仓库内文件。拿不到 variant / device / 包列表——要按这些做分支，说明
  这段逻辑该在 Go / config 里，不在这里。
- **不 fetch 外部东西**。要往 rootfs 放「有版本、要追更新」的第三方代码，打成
  `feed/` 里的 apk（例：`feed/zsh-autosuggestions`）。构建期拉取会绕过内容
  寻址，让固件在指纹不变的情况下悄悄漂移。
