# zsh-autosuggestions Makefile for OpenWrt
#
# 纯 shell 脚本，无需编译。发行 tarball 里已含预构建的 zsh-autosuggestions.zsh。
# 更新：改 PKG_VERSION，再把新 tarball 的 sha256 填进 PKG_HASH
#   curl -fsSL 'https://codeload.github.com/zsh-users/zsh-autosuggestions/tar.gz/v<新版本>?' | sha256sum
include $(TOPDIR)/rules.mk

PKG_NAME:=zsh-autosuggestions
PKG_VERSION:=0.7.1
PKG_RELEASE:=1

PKG_SOURCE:=$(PKG_NAME)-$(PKG_VERSION).tar.gz
PKG_SOURCE_URL:=https://codeload.github.com/zsh-users/zsh-autosuggestions/tar.gz/v$(PKG_VERSION)?
PKG_HASH:=0df7affff21cd87ed298e6a3970ed08a1dd66a6efa676454ee5b091ad503badf

PKG_LICENSE:=MIT
PKG_LICENSE_FILE:=LICENSE
PKG_MAINTAINER:=K-Lrize

include $(INCLUDE_DIR)/package.mk

define Package/zsh-autosuggestions
  SECTION:=utils
  CATEGORY:=Utilities
  TITLE:=Fish-like autosuggestions for zsh
  URL:=https://github.com/zsh-users/zsh-autosuggestions
  DEPENDS:=+zsh
  PKGARCH:=all
endef

define Package/zsh-autosuggestions/description
  按历史与补全提示命令补全，键入时以灰字给出建议。
  在 ~/.zshrc 里 source /usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh 启用。
endef

# 已是预构建的 shell 脚本，跳过配置与编译。
define Build/Configure
endef

define Build/Compile
endef

define Package/zsh-autosuggestions/install
	$(INSTALL_DIR) $(1)/usr/share/zsh-autosuggestions
	$(INSTALL_DATA) $(PKG_BUILD_DIR)/zsh-autosuggestions.zsh \
		$(1)/usr/share/zsh-autosuggestions/
endef

$(eval $(call BuildPackage,zsh-autosuggestions))
