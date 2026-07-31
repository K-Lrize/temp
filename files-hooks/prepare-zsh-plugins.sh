#!/usr/bin/env bash
#
# 构建期钩子：往 overlay 里放 zsh 插件。
#
# 插件是第三方 git 仓库，不进本仓库（否则每次更新都要提交一坨别人的代码），
# 构建时现拉。变量由 internal/files 注入，钩子不自己解析配置。

set -euo pipefail

: "${WRT_FILES_DIR:?需由 wrt files 注入}"
: "${WRT_VARIANT:?需由 wrt files 注入}"
: "${WRT_PACKAGES:?需由 wrt files 注入}"

# 没装 zsh 的设备不需要插件。包列表已经是合并后的最终结果，直接匹配即可。
case " ${WRT_PACKAGES} " in
*" zsh "*) ;;
*)
    echo "  ${WRT_VARIANT} 未启用 zsh，跳过插件准备"
    exit 0
    ;;
esac

PLUGIN_DIR="${WRT_FILES_DIR}/root/.zsh"
mkdir -p "${PLUGIN_DIR}"

clone_plugin() {
    local name="$1" url="$2"
    if [[ -d "${PLUGIN_DIR}/${name}" ]]; then
        echo "  插件已存在：${name}"
        return
    fi
    echo "  下载插件：${name}"
    git clone --depth 1 --quiet "${url}" "${PLUGIN_DIR}/${name}"
    # .git 不进固件：几 MB 的历史对设备毫无用处
    rm -rf "${PLUGIN_DIR:?}/${name}/.git"
}

clone_plugin zsh-autosuggestions https://github.com/zsh-users/zsh-autosuggestions
clone_plugin zsh-syntax-highlighting https://github.com/zsh-users/zsh-syntax-highlighting
