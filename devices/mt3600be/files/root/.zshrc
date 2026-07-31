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
alias h='history'
alias hs='history | grep'
alias hc='history -c'
alias chc='clear && history -c'

alias ls='eza --icons=auto'
alias ll='eza -lh --icons=auto --git'
alias la='eza -lah --icons=auto --git'
alias l='eza -lah --icons=auto --git'
alias lt='eza -T --icons=auto --level=2'
alias llt='eza -lT --icons=auto --git --level=2'
alias lta='eza -laT --icons=auto --git --level=2'

alias tree='eza -T --icons=auto'
alias tree1='eza -T --icons=auto --level=1'
alias tree2='eza -T --icons=auto --level=2'
alias tree3='eza -T --icons=auto --level=3'
alias tree4='eza -T --icons=auto --level=4'

alias rgi='rg -i'
alias rgl='rg -l'
alias rgc='rg -C 3'

mkcd() {
    mkdir -p "$1" && cd "$1"
}

ledon() {
    echo default-on > /sys/class/leds/blue:status/trigger
}

ledoff() {
    echo none > /sys/class/leds/blue:status/trigger
    echo 0 > /sys/class/leds/blue:status/brightness
}

# 加载历史命令提示
source ~/.zsh/zsh-autosuggestions/zsh-autosuggestions.zsh
# 命令高亮
source ~/.zsh/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh
