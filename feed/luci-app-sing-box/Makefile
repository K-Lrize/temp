include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-sing-box
PKG_VERSION:=1.0.0
PKG_RELEASE:=$(AUTORELEASE)

PKG_LICENSE:=GPL-3.0-or-later
PKG_MAINTAINER:=K-Lrize

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-sing-box
  SECTION:=luci
  CATEGORY:=LuCI
  SUBMENU:=3. Applications
  TITLE:=LuCI support for Sing-Box
  DEPENDS:=+sing-box +luci-base +luci-compat
  PKGARCH:=all
endef

define Package/luci-app-sing-box/description
  LuCI web interface for Sing-Box proxy platform.
  Provides configuration management and service control.
endef

define Build/Compile
endef

define Package/luci-app-sing-box/install
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/controller
	$(INSTALL_DATA) ./luasrc/controller/sing-box.lua \
		$(1)/usr/lib/lua/luci/controller/sing-box.lua

	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/model/cbi
	$(INSTALL_DATA) ./luasrc/model/cbi/sing-box.lua \
		$(1)/usr/lib/lua/luci/model/cbi/sing-box.lua

	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/view/sing-box
	$(INSTALL_DATA) ./luasrc/view/sing-box/status.htm \
		$(1)/usr/lib/lua/luci/view/sing-box/status.htm
endef

$(eval $(call BuildPackage,luci-app-sing-box))
