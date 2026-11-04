local _, ns = ...

-- Session identity joins local context to the game's disk combat log.
function ns.startSession()
	if ns.sessionID then return end
	ns.db.nextSessionID = (ns.db.nextSessionID or 0) + 1
	ns.sessionID = table.concat({ tostring(ns.build().build or "unknown"),
		tostring(ns.api("UnitGUID", "player") or "unknown"), tostring(ns.now()),
		tostring(ns.api("GetTime") or 0), tostring(ns.db.nextSessionID) }, "|")
	ns.db.sessions = ns.db.sessions or {}
	ns.push(ns.db.sessions, { id = ns.sessionID, at = ns.now(),
		clock = ns.api("GetTime"), build = ns.build() }, 100)
end

function ns.auraDetails(unit, filter)
	local modern = type(ns.lookup("C_UnitAuras.GetAuraDataByIndex")) == "function"
	local name = modern and "C_UnitAuras.GetAuraDataByIndex" or
		(filter == "HARMFUL" and "UnitDebuff" or "UnitBuff")
	if type(ns.lookup(name)) ~= "function" or ns.blocked[name] then return nil end
	local out = {}
	for i = 1, 80 do
		local aura
		if modern then
			local raw = ns.api(name, unit, i, filter)
			if type(raw) ~= "table" then break end
			aura = {}
			for _, key in ipairs({ "spellId", "auraInstanceID", "name", "applications",
				"duration", "expirationTime", "sourceUnit", "dispelName", "isHelpful", "isHarmful" }) do
				aura[key] = ns.plain(raw[key], name, key)
			end
		else
			local label, _, count, dispel, duration, expires, source, _, _, id = ns.api(name, unit, i)
			if not label then break end
			aura = { name = label, spellId = id, applications = count, dispelName = dispel,
				duration = duration, expirationTime = expires, sourceUnit = source }
		end
		if type(aura.sourceUnit) == "string" then
			aura.sourceGUID = ns.api("UnitGUID", aura.sourceUnit)
		end
		out[#out + 1] = aura
	end
	return out
end

function ns.unitContext(unit)
	local guid = ns.api("UnitGUID", unit)
	if not guid then return nil end
	local kind, powerName = ns.api("UnitPowerType", unit)
	return {
		guid = guid, name = ns.api("UnitName", unit), level = ns.api("UnitLevel", unit),
		creatureType = ns.api("UnitCreatureType", unit), family = ns.api("UnitCreatureFamily", unit),
		classification = ns.api("UnitClassification", unit), health = ns.api("UnitHealthMax", unit),
		power = ns.api("UnitPowerMax", unit), powerType = kind, powerName = powerName,
		helpful = ns.auraDetails(unit, "HELPFUL"), harmful = ns.auraDetails(unit, "HARMFUL"),
	}
end

function ns.petContext()
	local pet = ns.unitContext("pet")
	if not pet then return nil end
	pet.owner = ns.api("UnitGUID", "player")
	local base, positive, negative = ns.api("UnitAttackPower", "pet")
	pet.attackPower = ns.sum(base, positive, negative)
	pet.mainHandSpeed, pet.offHandSpeed = ns.api("UnitAttackSpeed", "pet")
	pet.damageMin, pet.damageMax = ns.api("UnitDamage", "pet")
	return pet
end

function ns.fightContext(reason)
	if not ns.db or not ns.db.recording then return end
	ns.db.fights = ns.db.fights or {}
	local _, instanceType, difficulty, difficultyName, _, _, _, instanceID = ns.api("GetInstanceInfo")
	local clock = ns.api("GetTime")
	if reason == "start" then
		ns.db.nextFightID = (ns.db.nextFightID or 0) + 1
		ns.currentFight = { id = ns.db.nextFightID, clock = clock }
	end
	local row = { reason = reason, at = ns.now(), clock = clock,
		fight = ns.currentFight and ns.currentFight.id,
		startedAt = ns.currentFight and ns.currentFight.clock,
		duration = reason == "end" and clock and ns.currentFight and ns.currentFight.clock
			and clock - ns.currentFight.clock or nil,
		session = ns.sessionID, build = ns.build(), snapshot = ns.currentSnapshot,
		lastRestSnapshot = ns.lastRestSnapshot, player = ns.api("UnitGUID", "player"),
		target = ns.unitContext("target"), pet = ns.petContext(),
		zone = ns.api("GetRealZoneText"), instanceID = instanceID,
		instanceType = instanceType, difficulty = difficulty, difficultyName = difficultyName }
	ns.push(ns.db.fights, row, 400)
	if reason == "end" then ns.currentFight = nil end
end

local frame = CreateFrame("Frame")
local lastPower = {}
local spellEvents = {
	UNIT_SPELLCAST_START = true, UNIT_SPELLCAST_SUCCEEDED = true,
	UNIT_SPELLCAST_FAILED = true, UNIT_SPELLCAST_INTERRUPTED = true,
	UNIT_SPELLCAST_CHANNEL_START = true, UNIT_SPELLCAST_CHANNEL_STOP = true,
}

ns.onReady(function()
	for event in pairs(spellEvents) do ns.listen(frame, event) end
	for _, event in ipairs({ "UNIT_POWER_UPDATE", "UNIT_PET", "PLAYER_ENTERING_WORLD" }) do
		ns.listen(frame, event)
	end
end)

frame:SetScript("OnEvent", function(_, event, unit, arg2, arg3)
	if not ns.db or not ns.db.recording then return end
	if event == "PLAYER_ENTERING_WORLD" then
		ns.ensureLogging()
		return
	elseif event == "UNIT_PET" then
		if unit == "player" then ns.snapshot("pet") end
		return
	end
	unit = ns.plain(unit, event, 1)
	if unit ~= "player" and unit ~= "pet" then return end
	local now = ns.api("GetTime") or 0
	local row = { k = event, at = ns.now(), t = now, session = ns.sessionID,
		s = ns.currentSnapshot, fight = ns.currentFight and ns.currentFight.id,
		source = ns.api("UnitGUID", unit),
		owner = unit == "pet" and ns.api("UnitGUID", "player") or nil,
		p = ns.api("UnitPower", unit), powerType = ns.api("UnitPowerType", unit) }
	if event == "UNIT_POWER_UPDATE" then
		local kind = ns.plain(arg2, event, 2)
		local _, activeName = ns.api("UnitPowerType", unit)
		if activeName and kind and activeName ~= kind then return end
		local key = unit .. ":" .. tostring(kind)
		local previous = lastPower[key]
		if previous and now - previous < 0.5 then return end
		lastPower[key] = now
		row.powerName = kind
	else
		row.cast = ns.plain(arg2, event, 2)
		row.spell = ns.plain(arg3, event, 3)
	end
	ns.pushEvent(row)
end)
