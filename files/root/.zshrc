# OpenWrt Zsh 配置

HISTFILE=~/.zsh_history
HISTSIZE=10000
SAVEHIST=10000
setopt append_history inc_append_history share_history hist_ignore_all_dups

autoload -Uz compinit && compinit -d /tmp/.zcompdump
zstyle ':completion:*' menu select

autoload -U colors && colors
PROMPT='%F{green}%m%f %(?|%F{green}➜ %f|%F{red}➜ %f) %F{cyan}%c%f '

alias c='clear'

# 加载历史命令提示
source ~/.zsh/zsh-autosuggestions/zsh-autosuggestions.zsh
# 命令高亮
source ~/.zsh/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh
