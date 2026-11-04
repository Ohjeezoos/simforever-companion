-- Guard client reads and retain diagnostics for unavailable values.
local ADDON, ns = ...

ns.secrets = {}
ns.blocked = {}
ns.calls = {}
ns.trace = {}

local watching = false

local forbidden = {}

local function watchForbidden()
	if type(hooksecurefunc) ~= "function" or type(StaticPopup_Show) ~= "function" then
		return
	end
	hooksecurefunc("StaticPopup_Show", function(which, arg1, arg2)
		if which ~= "ADDON_ACTION_FORBIDDEN" and which ~= "ADDON_ACTION_BLOCKED" then
			return
		end
		local recent = {}
		for i, c in ipairs(ns.trace) do
			recent[i] = c.n > 1 and ("%s x%d"):format(c.name, c.n) or c.name
		end
		forbidden[#forbidden + 1] = {
			which = which,
			addon = tostring(arg1),
			func = tostring(arg2),
			after = recent,
		}
	end)
end

watchForbidden()

function ns.flushForbidden()
	if not ns.db or #forbidden == 0 then
		return
	end
	ns.db.forbidden = ns.db.forbidden or {}
	for _, entry in ipairs(forbidden) do
		entry.at = ns.now()
		ns.db.forbidden[#ns.db.forbidden + 1] = entry
	end
	forbidden = {}
end

local pendingBlocked = {}

local NEVER = {
	COMBAT_LOG_EVENT = true,
	COMBAT_LOG_EVENT_UNFILTERED = true,
}

local refused = {}

function ns.listen(frame, event)
	if NEVER[event] or refused[event] then
		return false
	end
	if not pcall(frame.RegisterEvent, frame, event) then
		refused[event] = true
		return false
	end
	local ok, registered = pcall(frame.IsEventRegistered, frame, event)
	if ok and registered == false then
		pcall(frame.UnregisterEvent, frame, event)
		refused[event] = true
		if ns.db then
			ns.db.blockedEvents = ns.db.blockedEvents or {}
			ns.db.blockedEvents[event] = ns.now and ns.now() or 0
		else
			pendingBlocked[#pendingBlocked + 1] = event
		end
		return false
	end
	return true
end

local ready = {}

function ns.onReady(fn)
	if ns.db then
		fn()
	else
		ready[#ready + 1] = fn
	end
end

function ns.runReady()
	local list = ready
	ready = {}
	for _, fn in ipairs(list) do
		fn()
	end
end

function ns.flushBlockedEvents()
	if not ns.db then
		return
	end
	for _, event in ipairs(pendingBlocked) do
		ns.db.blockedEvents = ns.db.blockedEvents or {}
		ns.db.blockedEvents[event] = ns.now and ns.now() or 0
	end
	pendingBlocked = {}
end
local isSecret = rawget(_G, "issecretvalue") or rawget(_G, "issecret") or rawget(_G, "IsSecretValue")

function ns.lookup(name)
	local dot = name:find("%.")
	if not dot then
		return rawget(_G, name)
	end
	local holder = rawget(_G, name:sub(1, dot - 1))
	if type(holder) ~= "table" then
		return nil
	end
	local ok, v = pcall(function() return holder[name:sub(dot + 1)] end)
	if not ok then
		return nil
	end
	return v
end

local apiNames = {
	"issecretvalue", "issecret", "IsSecretValue", "issecure", "securecall", "InCombatLockdown",
	"GetNumTalentTabs", "GetNumTalents", "GetTalentInfo", "GetTalentTabInfo",
	"C_Traits.GetConfigInfo", "C_Traits.GetTreeNodes", "C_Traits.GetNodeInfo",
	"C_ClassTalents.GetActiveConfigID", "C_SpecializationInfo.GetClassSpecializations",
}

local function clientApi()
	local out = {}
	for _, name in ipairs(apiNames) do
		local v = ns.lookup(name)
		if v == nil then
			out[name] = false
		else
			out[name] = type(v)
		end
	end
	return out
end

local function state()
	if InCombatLockdown and InCombatLockdown() then
		return "combat"
	end
	return "rest"
end

local function kindOf(v)
	local ok, t = pcall(type, v)
	return ok and t or "secret"
end

local function addable(v) return v + 0 end
local function catable(v) return v .. "" end

local function readable(v)
	local t = kindOf(v)
	if isSecret then
		local ok, secret = pcall(isSecret, v)
		if ok and secret then return false end
	end
	if t == "nil" or t == "boolean" or t == "table" or t == "function" then return true end
	if t == "number" then return (pcall(addable, v)) end
	if t == "string" then return (pcall(catable, v)) end
	return false
end

local function slotKey(idx)
	if type(idx) == "number" then
		return "#" .. idx
	end
	return tostring(idx)
end

local function markSecret(name, idx)
	if not name then
		return
	end
	idx = slotKey(idx or 1)
	local when = state()
	ns.secrets[name] = ns.secrets[name] or {}
	ns.secrets[name][idx] = ns.secrets[name][idx] or {}
	if ns.secrets[name][idx][when] then
		return
	end
	ns.secrets[name][idx][when] = true
	if ns.db then
		ns.db.secrets = ns.db.secrets or {}
		ns.db.secrets[name] = ns.db.secrets[name] or {}
		ns.db.secrets[name][idx] = ns.db.secrets[name][idx] or {}
		ns.db.secrets[name][idx][when] = true
	end
end

function ns.plain(v, name, idx)
	idx = idx or 1
	if readable(v) then
		return v
	end
	markSecret(name, idx)
	return nil
end

function ns.sum(...)
	local total, any = 0, false
	for i = 1, select("#", ...) do
		local v = select(i, ...)
		if type(v) == "number" then
			total, any = total + v, true
		end
	end
	if not any then
		return nil
	end
	return total
end

local function noteFailure(name, why)
	if not ns.db or not name then
		return
	end
	ns.db.apiFailed = ns.db.apiFailed or {}
	if ns.db.apiFailed[name] then
		return
	end
	ns.db.apiFailed[name] = { why = why, at = ns.now and ns.now() or 0 }
end

function ns.noteApiFailure(name, why)
	noteFailure(name, why)
end

function ns.api(name, ...)
	if ns.blocked[name] then
		return nil
	end
	local fn = ns.lookup(name)
	if type(fn) ~= "function" then
		noteFailure(name, "not a function in this client")
		return nil
	end
	ns.calls[name] = true
	local last = ns.trace[#ns.trace]
	if last and last.name == name then
		last.n = last.n + 1
	else
		ns.trace[#ns.trace + 1] = { name = name, n = 1 }
		if #ns.trace > 12 then
			table.remove(ns.trace, 1)
		end
	end
	local function pack(...) return { n = select("#", ...), ... } end
	local values = pack(pcall(fn, ...))
	if not values[1] then
		noteFailure(name, "raised: " .. tostring(values[2]))
		return nil
	end
	for i = 2, values.n do values[i - 1] = ns.plain(values[i], name, i - 1) end
	values[values.n] = nil
	return (unpack or table.unpack)(values, 1, values.n - 1)
end

local pending = {}

local function flush()
	if not ns.db then
		return
	end
	ns.db.blocked = ns.db.blocked or {}
	for _, r in ipairs(pending) do
		if not ns.db.blocked[r.fn] then
			ns.db.blocked[r.fn] = r
		end
	end
	pending = {}
end

local function record(name, by, named)
	local calls = {}
	for i, c in ipairs(ns.trace) do
		calls[i] = c.n > 1 and ("%s x%d"):format(c.name, c.n) or c.name
	end
	local entry = { at = ns.now and ns.now() or 0, by = by or "unknown", fn = name, named = named, lastCalls = calls }
	if named then
		ns.blocked[name] = true
	end
	pending[#pending + 1] = entry
	flush()
	if named then
		ns.note(("this client does not allow addons to call %s, so the probe stopped calling it"):format(name))
	else
		ns.note(("the client refused an action without naming it. Last calls: %s"):format(table.concat(calls, ", ")))
	end
end

ns.flushBlocked = flush

local function bare(fname)
	if type(fname) ~= "string" then
		return nil
	end
	fname = fname:gsub("%b()", ""):gsub("^.*[%.:]", ""):gsub("^%s+", ""):gsub("%s+$", "")
	if fname == "" then
		return nil
	end
	return fname
end

function ns.adoptBlocked()
	ns.db.clientApi = clientApi()
	ns.db.clientApi.watchesBlockedActions = watching
	flush()
	for name in pairs(ns.db.blocked or {}) do
		ns.blocked[name] = true
	end
	for name, slots in pairs(ns.db.secrets or {}) do
		ns.secrets[name] = ns.secrets[name] or {}
		if type(slots) == "table" then
			for idx, when in pairs(slots) do
				local key = slotKey(idx)
				ns.secrets[name][key] = ns.secrets[name][key] or {}
				if type(when) == "table" then
					for w in pairs(when) do
						ns.secrets[name][key][w] = true
					end
				end
			end
		end
	end
end

watching = false
