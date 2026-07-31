local m, s

m = Map("sing-box", translate("Sing-Box"), translate("Universal proxy platform"))

s = m:section(NamedSection, "main", "sing-box", translate("Basic Settings"))
s.addremove = false

s:option(Flag, "enabled", translate("Enable"))

local cf = s:option(Value, "conffile", translate("Config File"))
cf.default = "/etc/sing-box/config.json"

local wd = s:option(Value, "workdir", translate("Work Directory"))
wd.default = "/etc/sing-box"

local sf = s:option(Value, "scriptfile", translate("Script File"))
sf.default = "/etc/sing-box/script.sh"

return m
