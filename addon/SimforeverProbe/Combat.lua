-- Enable advanced disk logging; retain optional local event diagnostics.
local ADDON, ns = ...

function ns.ensureLogging()
	local exists = type(rawget(_G, "LoggingCombat")) == "function"
	local readable = type(rawget(_G, "IsLoggingCombat")) == "function"
	local get = type(ns.lookup("C_CVar.GetCVar")) == "function" and "C_CVar.GetCVar" or "GetCVar"
	local set = type(ns.lookup("C_CVar.SetCVar")) == "function" and "C_CVar.SetCVar" or "SetCVar"
	local advanced = ns.api(get, "advancedCombatLogging")
	if advanced ~= "1" then
		ns.api(set, "advancedCombatLogging", "1")
		advanced = ns.api(get, "advancedCombatLogging")
	end
	local before = ns.api("IsLoggingCombat")
	local returned = ns.api("LoggingCombat", true)
	local on = ns.api("IsLoggingCombat")
	if type(on) ~= "boolean" and type(returned) == "boolean" then on = returned end
	if ns.db then
		ns.db.logging = {
			on = on,
			requested = exists,
			advanced = advanced == "1",
			at = ns.now(),
			switchExists = exists,
			stateReadable = readable,
			before = before,
			returned = returned,
			after = on,
			beforeKind = type(before),
			afterKind = type(on),
		}
	end
	return on == true
end

local f = CreateFrame("Frame")
ns.onReady(function()
	ns.listen(f, "PLAYER_REGEN_DISABLED")
	ns.listen(f, "PLAYER_REGEN_ENABLED")
	ns.ensureLogging()
end)

f:SetScript("OnEvent", function(_, event)
	if event == "PLAYER_REGEN_DISABLED" then
		ns.ensureLogging()
		ns.fightContext("start")
		if ns.db.recording then ns.recordPull("combat") end
	elseif event == "PLAYER_REGEN_ENABLED" then
		ns.fightContext("end")
	end
end)

local CANDIDATES = {
	"UNIT_COMBAT",
	"UNIT_SPELLCAST_SUCCEEDED",
	"UNIT_SPELLCAST_START",
	"UNIT_HEALTH",
	"UNIT_POWER_UPDATE",
	"COMBAT_TEXT_UPDATE",
	"CHAT_MSG_COMBAT_SELF_HITS",
	"CHAT_MSG_SPELL_SELF_DAMAGE",
	"CHAT_MSG_SPELL_PERIODIC_SELF_DAMAGE",
	"CHAT_MSG_COMBAT_SELF_MISSES",
}

local MAX_SAMPLES = 12
local survey

function ns.surveyChannels()
	ns.db.channels = ns.db.channels or {}
	if not survey then
		survey = CreateFrame("Frame")
		survey:SetScript("OnEvent", function(_, event, ...)
			local rec = ns.db.channels[event]
			if not rec then
				return
			end
			rec.fired = rec.fired + 1
			if #rec.samples >= MAX_SAMPLES then
				return
			end
			local args = { t = ns.api("GetTime") }
			for i = 1, select("#", ...) do
				args[i] = ns.plain((select(i, ...)), event, i)
			end
			rec.samples[#rec.samples + 1] = args
		end)
	end
	local open = 0
	for _, event in ipairs(CANDIDATES) do
		if ns.listen(survey, event) then
			ns.db.channels[event] = ns.db.channels[event] or { fired = 0, samples = {} }
			open = open + 1
		end
	end
	return open, #CANDIDATES
end
