module("luci.controller.sing-box", package.seeall)

function index()
	if not nixio.fs.access("/etc/config/sing-box") then
		return
	end

	entry({"admin", "services", "sing-box"}, firstchild(), "Sing-Box", 60).dependent = false
	entry({"admin", "services", "sing-box", "config"}, cbi("sing-box"), translate("Configuration"), 10)
	entry({"admin", "services", "sing-box", "status"}, template("sing-box/status"), translate("Status"), 20)
	entry({"admin", "services", "sing-box", "action"}, call("action_rpc"), nil).leaf = true
end

function action_rpc()
	local action = luci.http.formvalue("action")

	if action == "start" then
		luci.sys.call("/etc/init.d/sing-box start >/dev/null 2>&1")
	elseif action == "stop" then
		luci.sys.call("/etc/init.d/sing-box stop >/dev/null 2>&1")
	elseif action == "restart" then
		luci.sys.call("/etc/init.d/sing-box restart >/dev/null 2>&1")
	end

	local running = (luci.sys.call("pidof sing-box >/dev/null") == 0)
	luci.http.prepare_content("application/json")
	luci.http.write_json({ running = running })
end
