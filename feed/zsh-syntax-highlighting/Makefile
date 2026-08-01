# zsh-syntax-highlighting Makefile for OpenWrt
#
# 纯 shell 脚本，无需编译。主脚本运行时会按相对路径加载同目录下的
# highlighters/，所以两者必须一起装、保持目录结构。
# 更新：改 PKG_VERSION，再把新 tarball 的 sha256 填进 PKG_HASH
#   curl -fsSL 'https://codeload.github.com/zsh-users/zsh-syntax-highlighting/tar.gz/<新版本>?' | sha256sum
include $(TOPDIR)/rules.mk

PKG_NAME:=zsh-syntax-highlighting
PKG_VERSION:=0.8.0
PKG_RELEASE:=1

PKG_SOURCE:=$(PKG_NAME)-$(PKG_VERSION).tar.gz
PKG_SOURCE_URL:=https://codeload.github.com/zsh-users/zsh-syntax-highlighting/tar.gz/$(PKG_VERSION)?
PKG_HASH:=5981c19ebaab027e356fe1ee5284f7a021b89d4405cc53dc84b476c3aee9cc32

PKG_LICENSE:=BSD-3-Clause
PKG_LICENSE_FILE:=LICENSE.md
PKG_MAINTAINER:=K-Lrize

include $(INCLUDE_DIR)/package.mk

define Package/zsh-syntax-highlighting
  SECTION:=utils
  CATEGORY:=Utilities
  TITLE:=Syntax highlighting for zsh
  URL:=https://github.com/zsh-users/zsh-syntax-highlighting
  DEPENDS:=+zsh
  PKGARCH:=all
endef

define Package/zsh-syntax-highlighting/description
  键入命令时实时高亮，命令合法与否一眼可辨。
  在 ~/.zshrc 里 source /usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh 启用。
endef

# 已是预构建的 shell 脚本，跳过配置与编译。
define Build/Configure
endef

define Build/Compile
endef

define Package/zsh-syntax-highlighting/install
	$(INSTALL_DIR) $(1)/usr/share/zsh-syntax-highlighting/highlighters
	$(INSTALL_DATA) $(PKG_BUILD_DIR)/zsh-syntax-highlighting.zsh \
		$(1)/usr/share/zsh-syntax-highlighting/
	$(CP) $(PKG_BUILD_DIR)/highlighters/* \
		$(1)/usr/share/zsh-syntax-highlighting/highlighters/
endef

$(eval $(call BuildPackage,zsh-syntax-highlighting))
