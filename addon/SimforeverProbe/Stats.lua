-- Character, gear, talent and rating snapshots with guarded client reads.
local ADDON, ns = ...

local RATINGS = {
	{ key = "meleeHit", id = CR_HIT_MELEE },
	{ key = "rangedHit", id = CR_HIT_RANGED },
	{ key = "spellHit", id = CR_HIT_SPELL },
	{ key = "meleeCrit", id = CR_CRIT_MELEE },
	{ key = "rangedCrit", id = CR_CRIT_RANGED },
	{ key = "spellCrit", id = CR_CRIT_SPELL },
	{ key = "meleeHaste", id = CR_HASTE_MELEE },
	{ key = "rangedHaste", id = CR_HASTE_RANGED },
	{ key = "spellHaste", id = CR_HASTE_SPELL },
	{ key = "expertise", id = CR_EXPERTISE },
	{ key = "defense", id = CR_DEFENSE_SKILL },
	{ key = "dodge", id = CR_DODGE },
	{ key = "parry", id = CR_PARRY },
	{ key = "block", id = CR_BLOCK },
	{ key = "armorPenetration", id = CR_ARMOR_PENETRATION },
	{ key = "mastery", id = CR_MASTERY },
}

local SCHOOLS = { "physical", "holy", "fire", "nature", "frost", "shadow", "arcane" }

local function ratings()
	local out = {}
	local named = false
	for _, r in ipairs(RATINGS) do
		if r.id then
			named = true
			local amount = ns.api("GetCombatRating", r.id)
			local percent = ns.api("GetCombatRatingBonus", r.id)
			if amount or percent then
				out[r.key] = { rating = amount, percent = percent }
			end
		end
	end
	if not named then
		for id = 1, 40 do
			local amount = ns.api("GetCombatRating", id)
			local percent = ns.api("GetCombatRatingBonus", id)
			if (amount and amount ~= 0) or (percent and percent ~= 0) then
				out["cr" .. id] = { rating = amount, percent = percent, id = id }
			end
		end
	end
	return out
end

local function spellPower()
	local out = {}
	for i, name in ipairs(SCHOOLS) do
		out[name] = ns.api("GetSpellBonusDamage", i)
	end
	out.healing = ns.api("GetSpellBonusHealing")
	return out
end

local function recordBags()
	for bag = 0, 4 do
		local slots = ns.api("C_Container.GetContainerNumSlots", bag) or ns.api("GetContainerNumSlots", bag)
		if type(slots) == "number" then
			for slot = 1, slots do
				local link = ns.api("C_Container.GetContainerItemLink", bag, slot) or
					ns.api("GetContainerItemLink", bag, slot)
				if link then
					ns.recordItem(link)
				end
			end
		end
	end
end

local function gear(unit)
	unit = unit or "player"
	local out = {}
	for slot = 1, 19 do
		local link = ns.api("GetInventoryItemLink", unit, slot)
		if link then
			local id = link:match("item:(%d+)")
			out[#out + 1] = {
				slot = slot,
				id = tonumber(id),
				link = link,
				itemString = link:match("|H(item[%-?%d:]+)|h"),
				level = ns.api("GetDetailedItemLevelInfo", link),
			}
			ns.recordItem(link)
		end
	end
	return out
end

function ns.recordItem(link)
	if not link then
		return
	end
	local itemString = link:match("|H(item[%-?%d:]+)|h") or link:match("^(item[%-?%d:]+)$")
	if not itemString or ns.db.items[itemString] then
		return itemString and ns.db.items[itemString] or nil
	end
	local raw, apiName
	if type(ns.lookup("C_Item.GetItemStats")) == "function" and not ns.blocked["C_Item.GetItemStats"] and not ns.blocked.GetItemStats then
		apiName = "C_Item.GetItemStats"
		raw = ns.api(apiName, link)
	end
	if type(raw) ~= "table" or next(raw) == nil then
		apiName = "GetItemStats"
		local fn = ns.lookup(apiName)
		if type(fn) ~= "function" or ns.blocked[apiName] then
			ns.noteApiFailure(apiName, "not available in this client")
			return
		end
		local supplied = {}
		local ok, result = pcall(fn, link, supplied)
		if not ok then
			ns.noteApiFailure(apiName, "raised: " .. tostring(result))
			return
		end
		raw = type(result) == "table" and result or supplied
	end
	if next(raw) == nil then
		ns.noteApiFailure("GetItemStats", "returned nothing for "..tostring(link))
		return
	end
	local stats, any = {}, false
	for k, v in pairs(raw) do
		local key = ns.plain(k, apiName, "key")
		if key then
			local value = ns.plain(v, apiName, key)
			if value ~= nil then stats[key], any = value, true end
		end
	end
	if not any then
		return
	end
	local infoName = type(ns.lookup("C_Item.GetItemInfo")) == "function" and "C_Item.GetItemInfo" or "GetItemInfo"
	local name, _, quality, ilvl = ns.api(infoName, link)
	ns.db.items[itemString] = { name = name, quality = quality, ilvl = ilvl, stats = stats, at = ns.now() }
	return ns.db.items[itemString]
end

local function weaponSkills()
	local n = ns.api("GetNumSkillLines")
	if type(n) ~= "number" then
		return nil
	end
	local out
	for i = 1, n do
		local name, isHeader, _, rank, _, modifier, maxRank = ns.api("GetSkillLineInfo", i)
		if name and not isHeader and type(rank) == "number" and rank > 0 then
			out = out or {}
			out[name] = { rank = rank, max = maxRank, modifier = modifier }
		end
	end
	return out
end

local function roster()
	local out, seen = nil, {}
	local units = { "player" }
	if ns.api("IsInRaid") then
		for i = 1, 40 do
			units[#units + 1] = "raid" .. i
		end
	else
		for i = 1, 4 do
			units[#units + 1] = "party" .. i
		end
	end
	for _, unit in ipairs(units) do
		local guid = ns.api("UnitGUID", unit)
		local level = ns.api("UnitLevel", unit)
		if guid and not seen[guid] and type(level) == "number" and level > 0 then
			seen[guid] = true
			local className, classFile = ns.api("UnitClass", unit)
			local raceName, raceFile = ns.api("UnitRace", unit)
			local stat = {}
			local keys = { "strength", "agility", "stamina", "intellect", "spirit" }
			for i = 1, 5 do
				local total, effective, pos, neg = ns.api("UnitStat", unit, i)
				if type(total) == "number" then
					stat[keys[i]] = total
					stat[keys[i] .. "Effective"] = effective
					stat[keys[i] .. "Pos"] = pos
					stat[keys[i] .. "Neg"] = neg
				end
			end
			out = out or {}
			out[#out + 1] = {
				stats = next(stat) and stat or nil,
				guid = guid,
				name = ns.api("UnitName", unit),
				level = level,
				class = classFile,
				className = className,
				race = raceFile,
				raceName = raceName,
			}
		end
	end
	return out
end

local function meleeStats()
	local base, posBuff, negBuff = ns.api("UnitAttackPower", "player")
	local rbase, rpos, rneg = ns.api("UnitRangedAttackPower", "player")
	local mainSpeed, offSpeed = ns.api("UnitAttackSpeed", "player")
	local lowDmg, hiDmg, lowOff, hiOff = ns.api("UnitDamage", "player")
	local _, armor = ns.api("UnitArmor", "player")
	return {
		attackPower = ns.sum(base, posBuff, negBuff),
		rangedAttackPower = ns.sum(rbase, rpos, rneg),
		mainHandSpeed = mainSpeed,
		offHandSpeed = offSpeed,
		mainHandMin = lowDmg,
		mainHandMax = hiDmg,
		offHandMin = lowOff,
		offHandMax = hiOff,
		critChance = ns.api("GetCritChance"),
		rangedCritChance = ns.api("GetRangedCritChance"),
		spellCritChance = ns.api("GetSpellCritChance", 2),
		hitModifier = ns.api("GetHitModifier"),
		spellHitModifier = ns.api("GetSpellHitModifier"),
		meleeHaste = ns.api("GetMeleeHaste"),
		spellHaste = ns.api("UnitSpellHaste", "player"),
		expertise = ns.api("GetExpertise"),
		armor = armor,
		defense = ns.api("UnitDefense", "player"),
		dodge = ns.api("GetDodgeChance"),
		parry = ns.api("GetParryChance"),
		block = ns.api("GetBlockChance"),
	}
end

local function primaryStats()
	local out = {}
	local names = { "strength", "agility", "stamina", "intellect", "spirit" }
	for i = 1, 5 do
		local base, effective, pos, neg = ns.api("UnitStat", "player", i)
		out[names[i]] = effective
		out[names[i] .. "Base"] = base
		out[names[i] .. "Pos"] = pos
		out[names[i] .. "Neg"] = neg
	end
	out.health = ns.api("UnitHealthMax", "player")
	out.power = ns.api("UnitPowerMax", "player")
	local _, casting = ns.api("GetManaRegen")
	out.manaRegen = casting
	return out
end

local function talentsDiffer(a, b)
	if not a or not b then
		return a ~= b
	end
	for node, rank in pairs(a) do
		if b[node] ~= rank then
			return true
		end
	end
	for node, rank in pairs(b) do
		if a[node] ~= rank then
			return true
		end
	end
	return false
end

local lastTalents, toldThisSession

function ns.noteTalentChange(snap)
	local now = snap.talentRanks
    if not now then
		return
	end
	if lastTalents and talentsDiffer(lastTalents, now) and not toldThisSession then
		toldThisSession = true
		ns.note("your talents changed. The game only writes an addon's records when you "
			.. "reload or log out, so quitting another way loses this one.")
	end
	lastTalents = now
end

local ratingSeen, ratingSeenTable

local function stateKey(snap)
	local parts = { tostring(snap.level), snap.talents or "", tostring(ns.api("GetShapeshiftForm")) }
	for _, key in ipairs({ "strength", "agility", "stamina", "intellect", "spirit" }) do
		parts[#parts + 1] = tostring(snap.stats[key])
	end
	parts[#parts + 1] = snap.character.guid or "unknown"
	for _, key in ipairs({ "attackPower", "rangedAttackPower", "armor", "critChance", "spellCritChance", "meleeHaste", "spellHaste" }) do
		parts[#parts + 1] = tostring(snap.combat[key])
	end
	for _, school in ipairs(SCHOOLS) do parts[#parts + 1] = tostring(snap.spellPower[school]) end
	for _, piece in ipairs(snap.gear) do
		parts[#parts + 1] = tostring(piece.slot) .. ":" .. (piece.itemString or tostring(piece.id))
	end
	local nodes = {}
	for node in pairs(snap.talentRanks or {}) do nodes[#nodes + 1] = node end
	table.sort(nodes)
	for _, node in ipairs(nodes) do
		parts[#parts + 1] = node .. ":" .. snap.talentRanks[node]
	end
	return table.concat(parts, "|")
end

function ns.snapshot(reason)
	if not ns.db then
		return
	end
	ns.flushForbidden()
	local snap = {
		id = ns.nextSnapshotID(),
		session = ns.sessionID,
		clock = ns.api("GetTime"),
		inCombat = ns.api("InCombatLockdown"),
		form = ns.api("GetShapeshiftForm"),
		auras = ns.auraDetails("player", "HELPFUL"),
		debuffs = ns.auraDetails("player", "HARMFUL"),
		pet = ns.petContext(),
		at = ns.now(),
		build = ns.build(),
		character = ns.characterInfo(),
		reason = reason,
		level = ns.api("UnitLevel", "player"),
		zone = ns.api("GetRealZoneText"),
		stats = primaryStats(),
		combat = meleeStats(),
		spellPower = spellPower(),
		ratings = ratings(),
		gear = gear(),
		skills = weaponSkills(),

		roster = roster(),
		talents = ns.talentString(),
		talentRanks = ns.talentRanks(),
	}
	if not ns.api("InCombatLockdown") then
		local signature = stateKey(snap)
		snap.transition = ns.db.lastSnapshotState ~= signature
		ns.db.lastSnapshotState = signature
	end
	recordBags()
	if ns.harvestPlayer then
		ns.harvestPlayer()
	end
	ns.pushSnapshot(snap)
	ns.currentSnapshot = snap.id
	if not snap.inCombat then ns.lastRestSnapshot = snap.id end

	ns.noteTalentChange(snap)

	for key, r in pairs(snap.ratings) do
		if type(r.rating) == "number" and type(r.percent) == "number" and r.rating > 0 then
			local bucket = ns.db.ratings[key] or {}
			local seen = bucket[tostring(r.rating)]
			if type(snap.level) == "number" and (not seen or seen.level ~= snap.level) then
				bucket[tostring(r.rating)] = { percent = r.percent, level = snap.level, at = snap.at }
				ns.db.ratings[key] = bucket
			end
			if ratingSeenTable ~= ns.db.ratingReadings then
				ratingSeen = {}
				ratingSeenTable = ns.db.ratingReadings
				for _, reading in ipairs(ns.db.ratingReadings) do
					ratingSeen[table.concat({ reading.guid or "unknown", reading.key, reading.level or "unknown", reading.rating, reading.percent }, "|")] = true
				end
			end
			local identity = table.concat({ snap.character.guid or "unknown", key, snap.level or "unknown", r.rating, r.percent }, "|")
			if not ratingSeen[identity] then
				ratingSeen[identity] = true
				ns.db.ratingReadings[#ns.db.ratingReadings + 1] = {
					key = key, rating = r.rating, percent = r.percent,
					level = snap.level, at = snap.at, snapshot = snap.id,
					guid = snap.character.guid, class = snap.character.class, race = snap.character.race,
				}
			end
		end
	end
	return snap
end

function ns.aurasOn(unit)
	local details = ns.auraDetails(unit, "HELPFUL")
	if not details then return nil end
	local out = {}
	for _, aura in ipairs(details) do
		if type(aura.spellId) == "number" then out[#out + 1] = aura.spellId end
	end
	return out
end

local waiting, queue, queued = {}, {}, {}

local function unitFor(guid)
	local names = { "target", "mouseover", "focus" }
	for i = 1, 4 do
		names[#names + 1] = "party" .. i
	end
	for i = 1, 40 do
		names[#names + 1] = "raid" .. i
		names[#names + 1] = "nameplate" .. i
	end
	for _, u in ipairs(names) do
		if ns.api("UnitGUID", u) == guid then
			return u
		end
	end
end

local function record(row, worn)
	local sig = {}
	for _, g in ipairs(worn) do
		sig[#sig + 1] = tostring(g.id)
	end
	local key = ("%s|%d|%s"):format(row.guid, row.level, table.concat(sig, ","))
	ns.db.people = ns.db.people or {}
	if ns.db.people[key] then
		return
	end
	ns.peopleCount = (ns.peopleCount or 0) + 1
	if ns.peopleCount > 3000 then
		return
	end
	row.gear = worn
	ns.db.people[key] = row
end

function ns.inspectReady(guid)
	local row = waiting[guid]
	if not row then
		return
	end
	waiting[guid], queued[guid] = nil, nil
	local unit = unitFor(guid)
	if unit then
		record(row, gear(unit))
	end
	if ns.lookup("ClearInspectPlayer") then
		ns.api("ClearInspectPlayer")
	end
end

function ns.inspectQueued()
	local n = 0
	for _ in pairs(waiting) do
		n = n + 1
	end
	return n
end

local function pump()
	if ns.api("InCombatLockdown") then return end
	local guid = table.remove(queue, 1)
	while guid and not waiting[guid] do
		guid = table.remove(queue, 1)
	end
	if not guid then
		return
	end
	local unit = unitFor(guid)
	if not unit or not ns.api("UnitIsVisible", unit) or not ns.api("CanInspect", unit) then
		waiting[guid], queued[guid] = nil, nil
		return
	end
	ns.api("NotifyInspect", unit)
end

local function harvest(unit)
	if not ns.api("UnitIsPlayer", unit) then
		return
	end
	local guid = ns.api("UnitGUID", unit)
	local level = ns.api("UnitLevel", unit)
	if not guid or type(level) ~= "number" or level < 1 then
		return
	end
	local keys = { "strength", "agility", "stamina", "intellect", "spirit" }
	local stat, got = {}, 0
	for i = 1, 5 do
		local total, _, pos, neg = ns.api("UnitStat", unit, i)
		if type(total) == "number" and total > 0 then
			got = got + 1
			stat[keys[i]] = total
			stat[keys[i] .. "Pos"] = pos
			stat[keys[i] .. "Neg"] = neg
		end
	end
	if got < 5 then
		stat = nil
	end
	local className, classFile = ns.api("UnitClass", unit)
	local raceName, raceFile = ns.api("UnitRace", unit)
	local base, effective, _, pos, neg = ns.api("UnitArmor", unit)
	local row = {
		guid = guid,
		name = ns.api("UnitName", unit),
		level = level,
		class = classFile,
		className = className,
		race = raceFile,
		raceName = raceName,
		stats = stat,
		health = ns.api("UnitHealthMax", unit),
		power = ns.api("UnitPowerMax", unit),
		powerType = ns.api("UnitPowerType", unit),
		armor = effective,
		armorBase = base,
		armorPos = pos,
		armorNeg = neg,
		sex = ns.api("UnitSex", unit),
		at = ns.now and ns.now() or 0,
	}
	local worn = gear(unit)
	if #worn > 0 or ns.api("UnitIsUnit", unit, "player") then
		record(row, worn)
		return
	end
	if queued[guid] or #queue >= 50 then
		return
	end
	waiting[guid], queued[guid] = row, true
	queue[#queue + 1] = guid
end

function ns.harvestPlayer()
	harvest("player")
end

function ns.recordPull(reason)
	local units = {}
	if ns.api("IsInRaid") then
		for i = 1, 40 do
			units[#units + 1] = "raid" .. i
		end
	else
		units[#units + 1] = "player"
		for i = 1, 4 do
			units[#units + 1] = "party" .. i
		end
	end
	local at, rows = ns.now and ns.now() or 0, nil
	for _, unit in ipairs(units) do
		local guid = ns.api("UnitGUID", unit)
		if guid then
			harvest(unit)
			rows = rows or {}
			local className, classFile = ns.api("UnitClass", unit)
			local raceName, raceFile = ns.api("UnitRace", unit)
			rows[#rows + 1] = {
				guid = guid,
				name = ns.api("UnitName", unit),
				level = ns.api("UnitLevel", unit),
				class = classFile,
				className = className,
				race = raceFile,
				raceName = raceName,
				auras = ns.aurasOn(unit),
			}
		end
	end
	if not rows then
		return
	end
	ns.db.pulls = ns.db.pulls or {}
	if #ns.db.pulls >= 400 then
		table.remove(ns.db.pulls, 1)
	end
	ns.db.pulls[#ns.db.pulls + 1] = { at = at, reason = reason, members = rows,
		session = ns.sessionID, clock = ns.api("GetTime"), build = ns.build(), snapshot = ns.currentSnapshot }
end

local function sweep()
	for i = 1, 40 do
		harvest("nameplate" .. i)
	end
	harvest("target")
	harvest("focus")
	harvest("targettarget")
end

local f = CreateFrame("Frame")
ns.onReady(function()
	for _, e in ipairs({
		"PLAYER_TARGET_CHANGED",
		"PLAYER_FOCUS_CHANGED",
		"UNIT_TARGET",
		"PLAYER_ENTERING_WORLD",
		"ENCOUNTER_START",
		"UPDATE_MOUSEOVER_UNIT",
		"NAME_PLATE_UNIT_ADDED",
		"INSPECT_READY",
		"GROUP_ROSTER_UPDATE",
		"PLAYER_EQUIPMENT_CHANGED",
		"PLAYER_LEVEL_UP",
		"PLAYER_TALENT_UPDATE",
		"CHARACTER_POINTS_CHANGED",
		"PLAYER_REGEN_DISABLED",
		"PLAYER_REGEN_ENABLED",
		"UNIT_AURA",
	}) do
		ns.listen(f, e)
	end
	if C_Timer and C_Timer.NewTicker then
		C_Timer.NewTicker(2, pump)
		C_Timer.NewTicker(5, sweep)
	end
	ns.harvestPlayer()
end)

local pending, lastAura = nil, 0
f:SetScript("OnEvent", function(_, event, arg1)
	if event == "INSPECT_READY" then
		ns.inspectReady(arg1)
		return
	elseif event == "PLAYER_TARGET_CHANGED" then
		harvest("target")
		harvest("targettarget")
		return
	elseif event == "PLAYER_FOCUS_CHANGED" then
		harvest("focus")
		return
	elseif event == "UNIT_TARGET" then
		harvest(arg1)
		harvest((arg1 or "") .. "target")
		return
	elseif event == "PLAYER_ENTERING_WORLD" then
		ns.harvestPlayer()
		return
	elseif event == "ENCOUNTER_START" then
		ns.recordPull("encounter")
		ns.fightContext("encounter")
		return
	elseif event == "UPDATE_MOUSEOVER_UNIT" then
		harvest("mouseover")
		return
	elseif event == "NAME_PLATE_UNIT_ADDED" then
		harvest(arg1)
		return
	elseif event == "GROUP_ROSTER_UPDATE" then
		for i = 1, 40 do
			harvest("raid" .. i)
		end
		for i = 1, 4 do
			harvest("party" .. i)
		end
		harvest("player")
		return
	end
	if event == "UNIT_AURA" then
		if arg1 ~= "player" and arg1 ~= "pet" then return end
		local t = ns.api("GetTime") or 0
		if t - lastAura < 30 then
			return
		end
		lastAura = t
		ns.snapshot("aura")
		return
	end
	if pending then
		return
	end
	pending = true
	C_Timer.After(0.6, function()
		pending = nil
		ns.snapshot(event:lower())
	end)
end)

SLASH_SIMFOREVER_SNAP1 = "/sfpsnap"
SlashCmdList.SIMFOREVER_SNAP = function(msg)
	local reason = "manual"
	if type(msg) == "string" then
		local word = msg:match("^%s*([%w_]+)")
		if word then
			reason = word:lower()
		end
	end
	local s = ns.snapshot(reason)
	ns.log(("snapshot %d saved at level %s, as %s."):format(s.id, tostring(s.level or "?"), reason))
end
