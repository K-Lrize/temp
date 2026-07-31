# packages/hello-world/Makefile
include $(TOPDIR)/rules.mk

PKG_NAME:=hello-world
PKG_VERSION:=1.0.0
PKG_RELEASE:=1

PKG_LICENSE:=GPL-2.0-or-later
PKG_MAINTAINER:=K-Lrize

PKG_BUILD_PARALLEL:=1

include $(INCLUDE_DIR)/package.mk

define Package/hello-world
  SECTION:=utils
  CATEGORY:=Utilities
  TITLE:=Hello world — feed validation package
  DEPENDS:=
endef

define Package/hello-world/description
  Minimal package used to validate the custom feed build pipeline.
  Install and run: hello-world
endef

define Build/Compile
	$(TARGET_CC) $(TARGET_CFLAGS) $(TARGET_LDFLAGS) \
		-o $(PKG_BUILD_DIR)/hello-world \
		$(PKG_BUILD_DIR)/hello-world.c
endef

define Package/hello-world/install
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/hello-world $(1)/usr/bin/
endef

$(eval $(call BuildPackage,hello-world))
