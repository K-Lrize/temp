# sing-box Makefile for OpenWrt

include $(TOPDIR)/rules.mk

PKG_NAME:=sing-box-alpha
PKG_VERSION:=1.14.0_alpha33
PKG_SOURCE_VERSION:=1.14.0-alpha.33
PKG_RELEASE:=100

PROVIDES:=sing-box
CONFLICTS:=sing-box
DEFAULT_VARIANT:=1
PROVIDER_PRIORITY:=200

PKG_BUILD_DIR:=$(BUILD_DIR)/sing-box-$(PKG_SOURCE_VERSION)

PKG_SOURCE:=$(PKG_NAME)-$(PKG_SOURCE_VERSION).tar.gz
PKG_SOURCE_URL:=https://codeload.github.com/SagerNet/sing-box/tar.gz/v$(PKG_SOURCE_VERSION)?
PKG_HASH:=163c376128e4667ed1383750160715fedb79dc46f5e70e92c52d9aa78f6ca486

PKG_LICENSE:=GPL-3.0-or-later
PKG_LICENSE_FILE:=LICENSE
PKG_MAINTAINER:=K-Lrize

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

GO_PKG:=github.com/sagernet/sing-box
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/sing-box
GO_PKG_LDFLAGS:=-checklinkname=0 -X 'internal/godebug.defaultGODEBUG=multipathtcp=0'
GO_PKG_TAGS:=badlinkname,tfogo_checklinkname0,$(subst $(space),$(comma),$(strip \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_QUIC),with_quic) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_WIREGUARD),with_wireguard) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_DHCP),with_dhcp) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_UTLS),with_utls) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_CLASH_API),with_clash_api) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_GVISOR),with_gvisor) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_TAILSCALE),with_tailscale) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_ACME),with_acme) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_GRPC),with_grpc) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_V2RAY_API),with_v2ray_api) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_NAIVE),with_naive_outbound) \
	$(if $(CONFIG_SING_BOX_ALPHA_WITH_CLOUDFLARED),with_cloudflared) \
))

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/sing-box-alpha/config
if PACKAGE_sing-box-alpha
	config SING_BOX_ALPHA_WITH_QUIC
		bool "Enable QUIC"
		default y
	config SING_BOX_ALPHA_WITH_UTLS
		bool "Enable uTLS"
		default y
	config SING_BOX_ALPHA_WITH_GVISOR
		bool "Enable gVisor"
		default y
	config SING_BOX_ALPHA_WITH_WIREGUARD
		bool "Enable WireGuard"
		default y
	config SING_BOX_ALPHA_WITH_CLASH_API
		bool "Enable Clash API"
		default y
	config SING_BOX_ALPHA_WITH_DHCP
		bool "Enable DHCP"
		default y
	config SING_BOX_ALPHA_WITH_GRPC
		bool "Enable gRPC"
		default y
	config SING_BOX_ALPHA_WITH_ACME
		bool "Enable ACME"
		default y
	config SING_BOX_ALPHA_WITH_TAILSCALE
		bool "Enable Tailscale"
		default y
	config SING_BOX_ALPHA_WITH_NAIVE
		bool "Enable NaiveProxy"
		default n
	config SING_BOX_ALPHA_WITH_V2RAY_API
		bool "Enable V2Ray API"
		default n
	config SING_BOX_ALPHA_WITH_CLOUDFLARED
		bool "Enable Cloudflare Tunnel"
		default n
endif
endef

define Package/sing-box-alpha
  SECTION:=net
  CATEGORY:=Network
  SUBMENU:=Web Servers/Proxies
  TITLE:=The universal proxy platform
  URL:=https://sing-box.sagernet.org/
  DEPENDS:=$(GO_ARCH_DEPENDS) +ca-bundle +kmod-inet-diag +kmod-netlink-diag +SING_BOX_ALPHA_WITH_GVISOR:kmod-tun
  USERID:=sing-box=5566:sing-box=5566
endef

define Package/sing-box-alpha/description
  Universal proxy platform supporting multiple protocols
  (Shadowsocks, VMess, VLESS, Trojan, WireGuard, Tailscale, ...).
endef

define Package/sing-box-alpha/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(CP) ./files/* $(1)/
endef

$(eval $(call GoBinPackage,sing-box-alpha))
$(eval $(call BuildPackage,sing-box-alpha))
