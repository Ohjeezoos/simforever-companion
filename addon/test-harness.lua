-- Runs the addon outside the game against stubbed client APIs, so a syntax or
-- runtime error is caught before it reaches a play session.
local events = {}
local frames = {}
local refusedEvents = {}
-- How many times the addon asked the client for each event. Asking for a
-- refused one is what raises the warning, so a remembered refusal must bring
-- this back to zero.
local asked = {}
local secretValues = {}
local gameMessages = {}

local function stubFrame()
	local fr = {}
	fr.registered = {}
	-- The client refuses some events; a frame that was refused says so when
	-- asked, which is how the addon finds out.
	function fr:RegisterEvent(e)
		asked[e] = (asked[e] or 0) + 1
		if refusedEvents[e] then return end
		self.registered[e] = true
		events[e] = events[e] or {}
		table.insert(events[e], self)
	end
	function fr:IsEventRegistered(e) return self.registered[e] == true end
	function fr:UnregisterEvent(e) self.registered[e] = nil end
	fr.scripts = {}
	function fr:SetScript(name, fn)
		self.scripts[name] = fn
		if name == "OnEvent" then self.handler = fn end
	end
	function fr:CreateFontString() return { SetPoint = function() end, SetText = function() end } end
	local noop = function() end
	for _, m in ipairs({ "SetSize", "SetPoint", "SetMovable", "EnableMouse", "RegisterForDrag", "Show", "Hide",
		"SetMultiLine", "SetFontObject", "SetWidth", "SetAutoFocus", "SetText", "HighlightText", "SetFocus",
		"SetScrollChild", "StartMoving", "StopMovingOrSizing" }) do
		fr[m] = noop
	end
	table.insert(frames, fr)
	return fr
end

CreateFrame = function() return stubFrame() end
-- The client raises its warning by handing the addon's name and the refused
-- function to the popup, so the stub has to be hookable the way the real one is.
local function resetPopup()
	StaticPopup_Show = function() end
end
resetPopup()
hooksecurefunc = function(name, fn)
	local orig = _G[name]
	_G[name] = function(...)
		orig(...)
		fn(...)
	end
end
UIParent = {}
ChatFontNormal = {}
C_Timer = { After = function(_, fn) fn() end }
GetTime = function() return 1234.5 end
-- The client writes a combat log to disk for whoever asks. These record
-- whether the addon asked, because that file is the only per-hit data there is.
local logging, cvars, loggingCalls = false, {}, 0
IsLoggingCombat = function() return logging end
LoggingCombat = function(on) loggingCalls = loggingCalls + 1; logging = on and true or false; return logging end
GetCVar = function(name) return cvars[name] end
SetCVar = function(name, value) cvars[name] = tostring(value); return true end
time = os.time
GetServerTime = os.time
GetBuildInfo = function() return "1.60.1", "69893", "Sep 17 2026", 16001 end
UnitName = function() return "Probe" end
GetRealmName = function() return "Beta" end
UnitClass = function() return "Warrior", "WARRIOR" end
UnitRace = function() return "Orc", "Orc" end
UnitLevel = function() return 30 end
UnitGUID = function() return "Player-1-ABCDEF" end
UnitFactionGroup = function() return "Horde" end
-- base, effective, positive buff, negative buff, as the game returns them
UnitStat = function(_, i) return 10 * i, 20 * i, 12 * i, 2 * i end
UnitHealthMax = function() return 1500 end
UnitPowerMax = function() return 100 end
local playerPower = 60
UnitPower = function() playerPower = (playerPower + 7) % 100; return playerPower end
-- The skill list the character sheet shows: a header, then lines with a rank,
-- a cap and whatever a buff adds.
local skillLines = {
	{ "Weapon Skills", true },
	{ "Staves", false, nil, 42, nil, 0, 150 },
	{ "Daggers", false, nil, 150, nil, 3, 150 },
	{ "Unarmed", false, nil, 0, nil, 0, 150 },
}
-- A party of three, so the roster has somebody other than the player in it.
local group = {
	party1 = { "Yoshii", 19, "Paladin", "PALADIN", "Player-1-BBBBBB" },
	party2 = { "Deaths", 16, "Rogue", "ROGUE", "Player-1-CCCCCC" },
}
IsInRaid = function() return false end
local baseUnitGUID, baseUnitLevel, baseUnitClass, baseUnitName = UnitGUID, UnitLevel, UnitClass, UnitName
UnitGUID = function(unit) local g = group[unit]; if g then return g[5] end return baseUnitGUID and baseUnitGUID(unit) or nil end
UnitLevel = function(unit) local g = group[unit]; if g then return g[2] end return unit == "player" and baseUnitLevel and baseUnitLevel(unit) or nil end
-- An explicit if, because `return a and f(x)` keeps only the first of f's
-- values and UnitClass's second one is the whole point.
UnitClass = function(unit)
	local g = group[unit]
	if g then return g[3], g[4] end
	if baseUnitClass then return baseUnitClass(unit) end
end
UnitName = function(unit)
	local g = group[unit]
	if g then return g[1] end
	if baseUnitName then return baseUnitName(unit) end
end
GetNumSkillLines = function() return #skillLines end
GetSkillLineInfo = function(i)
	local l = skillLines[i]
	if not l then return nil end
	return l[1], l[2], l[3], l[4], l[5], l[6], l[7]
end
UnitAttackPower = function() return 300, 50, 0 end
UnitRangedAttackPower = function() return 120, 0, 0 end
UnitAttackSpeed = function() return 2.6, nil end
UnitDamage = function() return 40, 60, 0, 0 end
UnitArmor = function() return 0, 800 end
UnitDefense = function() return 150 end
UnitSpellHaste = function() return 3.2 end
GetCritChance = function() return 12.34 end
GetRangedCritChance = function() return 9.5 end
GetSpellCritChance = function() return 5.5 end
GetHitModifier = function() return 2 end
GetSpellHitModifier = function() return 1 end
GetMeleeHaste = function() return 4.5 end
GetExpertise = function() return 3 end
GetDodgeChance = function() return 6.1 end
GetParryChance = function() return 5 end
GetBlockChance = function() return 4 end
GetManaRegen = function() return 10, 5 end
GetSpellBonusDamage = function(school) return 40 + school end
GetSpellBonusHealing = function() return 88 end
GetInventoryItemLink = function(_, slot) return slot <= 3 and ("|cffa335ee|Hitem:1264" .. slot .. "::::::::30:::::|h[Test Item]|h|r") or nil end
GetDetailedItemLevelInfo = function() return 61 end
GetItemInfo = function() return "Test Item", nil, 4, 61 end
GetItemStats = function(_, tbl) tbl.ITEM_MOD_CRIT_RATING_SHORT = 28; tbl.ITEM_MOD_STRENGTH_SHORT = 18; return tbl end
GetRealZoneText = function() return "Durotar" end
GetNumTalentTabs = function() return 3 end
GetNumTalents = function() return 4 end
GetTalentInfo = function(tab, i) return "T", nil, nil, nil, (tab + i) % 6 end
-- Ratings: this client exposes numeric ids only, which exercises the fallback.
GetCombatRating = function(id) return id == 6 and 40 or (id == 9 and 28 or 0) end
GetCombatRatingBonus = function(id) return id == 6 and 4.0 or (id == 9 and 2.0 or 0) end
CombatLogGetCurrentEventInfo = function()
	return 1234.5, "SPELL_DAMAGE", false, "Player-1-ABCDEF", "Probe", 0, 0, "Creature-0-1-2-3-4-5678", "Boar", 0, 0,
		25286, "Heroic Strike", 1, 210, 0, 1, 0, 0, 0, true
end
print = function(...)
    local message = table.concat({ ... }, " ")
    gameMessages[#gameMessages + 1] = message
    io.write("[game] ", message, "\n")
end
SlashCmdList = {}
local inCombat = false
local combatOnlySecret = false
InCombatLockdown = function() return inCombat end
-- Forever can hide a value from addons: it still looks like a number, but the
-- client refuses to let addon code use it.
local secretChecks = 0
local checkedValue = {}
issecretvalue = function(v)
	secretChecks = secretChecks + 1
	checkedValue[v] = (checkedValue[v] or 0) + 1
	if secretValues[v] ~= true then
		return false
	end
	return not combatOnlySecret or inCombat
end

local ADDON = "SimforeverProbe"
local ns

local function fire(e, ...)
	for _, fr in ipairs(events[e] or {}) do
		if fr.handler then fr.handler(fr, e, ...) end
	end
end

local function run(during, seed, passive)
	events, frames, ns, SimforeverProbeDB, asked, gameMessages = {}, {}, {}, seed, {}, {}
	resetPopup()
	logging, cvars, loggingCalls = false, {}, 0
	for _, file in ipairs({ "Secret.lua", "Core.lua", "Stats.lua", "ItemIDs.lua", "Items.lua", "Collection.lua", "Combat.lua", "Export.lua" }) do
		assert(loadfile("SimforeverProbe/" .. file))(ADDON, ns)
	end
	fire("ADDON_LOADED", ADDON)
	fire("PLAYER_LOGIN")
	fire("PLAYER_EQUIPMENT_CHANGED")
	if during then during() end
	inCombat = true
	fire("PLAYER_REGEN_DISABLED")
	for _ = 1, 3 do fire("COMBAT_LOG_EVENT_UNFILTERED") end
	inCombat = false
	fire("PLAYER_REGEN_ENABLED")
	if not passive then
		SlashCmdList.SIMFOREVER_SNAP()
		SlashCmdList.SIMFOREVER("")
	end
	return ns, SimforeverProbeDB
end

local db
ns, db = run()
assert(db.character.class == "WARRIOR", "character not captured")
assert(#db.snapshots >= 2, "snapshots missing")
-- This client hands no addon the combat log, so a fight leaves no per-hit
-- record. What a session is worth is its snapshots.
assert(#db.events == 0, "events recorded from a log this client does not give: " .. #db.events)
local snap = db.snapshots[1]
assert(snap.stats.strength == 20 and snap.combat.attackPower == 350, "stats wrong")
assert(snap.build.version == "1.60.1" and snap.build.build == "69893", "snapshot build missing")
-- The base and the two buff sides are what a base stat is worked out from,
-- so losing them silently would cost the level 20 board its numbers.
assert(snap.stats.strengthBase == 10, "base stat not recorded")
assert(snap.stats.strengthPos == 12 and snap.stats.strengthNeg == 2, "stat buffs not recorded")
assert(snap.stats.intellectBase == 40, "base stat wrong for a later index")
assert(snap.spellPower.fire and snap.gear[1].id == 12641, "gear or spell power wrong")
-- Weapon skill against the target's defence is what the melee hit table is
-- built on, so a swing whose skill nobody wrote down says nothing about it.
assert(snap.skills, "weapon skills not recorded")
assert(snap.skills.Staves.rank == 42 and snap.skills.Staves.max == 150, "staff skill wrong")
assert(snap.skills.Daggers.modifier == 3, "a skill's buff was dropped")
assert(snap.skills["Weapon Skills"] == nil, "a header was recorded as a skill")
assert(snap.skills.Unarmed == nil, "a skill nobody has was recorded")
assert(next(db.ratings), "no rating readings captured")
assert(db.ratings.cr6 and db.ratings.cr6["40"].percent == 4.0, "rating pair wrong")
assert(next(db.items), "item stats not captured")
local json = ns.toJSON({ character = db.character, ratings = db.ratings, snapshots = db.snapshots, events = db.events })
assert(json:sub(1, 1) == "{" and #json > 500, "json too small")
assert(not json:find("nan") and not json:find("inf"), "json has bad numbers")
local f = io.open("/tmp/probe-export.json", "w"); f:write(json); f:close()
print(("ok: %d snapshots, %d events, %d rating keys, json %d bytes"):format(#db.snapshots, #db.events,
	(function() local n = 0 for _ in pairs(db.ratings) do n = n + 1 end return n end)(), #json))

-- Second pass: the client hides the attack power reading and refuses to run
-- GetCombatRating at all. The probe has to keep going and say what it lost.
secretValues[300] = true
local ns2, db2 = run(nil, { blocked = { GetCombatRating = { at = 1, by = "SimforeverProbe", fn = "GetCombatRating", named = true } } })
assert(#db2.snapshots >= 2, "snapshots lost when a value is hidden")
assert(db2.secrets.UnitAttackPower, "hidden value not reported")
local snap2 = db2.snapshots[#db2.snapshots]
assert(snap2.combat.attackPower == 50, "hidden value not dropped: " .. tostring(snap2.combat.attackPower))
assert(snap2.combat.critChance == 12.34, "readable values lost")
assert(db2.blocked.GetCombatRating, "refused call not remembered")
for key, r in pairs(snap2.ratings) do
	assert(r.rating == nil, "kept calling a refused function for " .. key)
end
assert(#db2.events == 0, "events recorded from a log this client does not give")
local json2 = ns2.toJSON(db2)
assert(json2:sub(1, 1) == "{" and #json2 > 500, "json broken in the hidden value pass")
assert(db2.clientApi.issecretvalue == "function", "client capabilities not recorded")

-- Third pass: a reading the client hid before is tested again anyway.
--
-- This used to assert the opposite, that a hidden reading is never tested
-- twice. That memo was restored from the saved variables at login, so one
-- hidden UnitStat blacklisted it for every later session and every character,
-- and three days of snapshots carried nothing but health and mana. The test
-- agreed with the bug, so it could not catch it.
--
-- The test is a pcall on an arithmetic op and costs nothing, so it runs every
-- time. What the client hides is still written down; it is a record of this
-- client's behaviour, never a reason to stop looking.
local seed = { secrets = db2.secrets, blocked = db2.blocked }
secretChecks, checkedValue = 0, {}
local _, db3 = run(nil, seed)
local snap3 = db3.snapshots[#db3.snapshots]
assert(snap3.combat.attackPower == 50, "hidden value came back")
for key, r in pairs(snap3.ratings) do
	assert(r.rating == nil, "called a function this client already refused: " .. key)
end
assert(checkedValue[300] ~= nil, "a reading hidden in an earlier session was never tested again, "
	.. "which is the blacklist that cost three days of data")
-- A call the client refuses outright is different: that raises at the client
-- and is still remembered, because asking again raises again.
assert(db3.blocked.GetCombatRating, "a refused call was forgotten")
print(("ok: a hidden reading is tested again rather than assumed, %d checks in the pass, %d on it")
	:format(secretChecks, checkedValue[300]))

local old = { build = { version = "1.60.1", build = "70235", toc = 16001 },
	blocked = db2.blocked, snapshots = { { level = 20, stats = { strength = 99 } } },
	ratings = { old = {} }, items = { old = {} }, apiFailed = { old = {} } }
local _, upgraded = run(nil, old)
local archived = upgraded.history["1.60.1.70235:16001"]
assert(archived == old and archived.snapshots[1].stats.strength == 99, "old build data lost")
assert(archived.history == nil and upgraded.history ~= old, "build history is recursive")
assert(not upgraded.blocked.GetCombatRating and upgraded.ratings.cr6, "old API refusal survived a build change")
assert(upgraded.ratings.old == nil and upgraded.items.old == nil and upgraded.apiFailed.old == nil, "build aggregates mixed")
local firstCount = #upgraded.snapshots
local _, reloaded = run(nil, upgraded)
assert(#reloaded.snapshots > firstCount and reloaded.history["1.60.1.70235:16001"] == archived, "reload discarded current build data")
local savedBuildInfo = GetBuildInfo
GetBuildInfo = function() return "1.60.1", "70245", "Oct 6 2026", 16001 end
local _, nextBuild = run(nil, reloaded)
assert(nextBuild.history["1.60.1.69893:16001"] == reloaded, "second build not archived")
assert(nextBuild.history["1.60.1.70235:16001"] == archived and reloaded.history == nil, "earlier history lost or nested")
assert(nextBuild.snapshots[1].build.build == "70245", "new build snapshot mislabeled")
SlashCmdList.SIMFOREVER("wipe")
assert(nextBuild.history == nil and #nextBuild.snapshots == 1, "wipe retained archived recordings")
GetBuildInfo = savedBuildInfo
print("ok: build changes preserve archives, reset API refusals and isolate measurements; reload preserves the current build")

-- Fourth pass: Forever hides these values during a fight only, so the reading
-- has to come back once the fight is over.
combatOnlySecret = true
local _, db4 = run()
local slot = db4.secrets.UnitAttackPower["#1"]
assert(slot.combat and not slot.rest, "hidden in a fight was remembered as hidden everywhere")
local login, fight, after = db4.snapshots[1], nil, db4.snapshots[#db4.snapshots]
for _, s in ipairs(db4.snapshots) do
	if s.reason == "player_regen_disabled" then fight = s end
end
assert(login.combat.attackPower == 350, "reading lost out of combat: " .. tostring(login.combat.attackPower))
assert(fight and fight.combat.attackPower == 50, "hidden reading kept during the fight")
assert(after.combat.attackPower == 350, "reading not recovered after the fight")
print("ok: hidden in a fight, read again once it ends")

-- Fifth pass: the addon must not ask to watch for blocked actions. Registering
-- that event is itself an action the client refuses, which is what raised the
-- warning on every login and cost the combat log its registration.
local watcher = io.open("SimforeverProbe/Secret.lua"):read("a")
assert(not watcher:find("RegisterEvent, f, \"ADDON_ACTION"), "the addon still asks to watch blocked actions")
local _, db5 = run()
assert(db5.clientApi.watchesBlockedActions == false, "the addon still claims to watch blocked actions")
print("ok: the addon no longer asks for the event that was refusing it")

-- Sixth pass: the combat log is refused outright by this client, and asking is
-- what raises the warning. Writing the refusal down cannot help, because the
-- client writes an addon's saved data and never reads it back: every session
-- starts knowing nothing. So it must never be asked for at all, on a fresh
-- database, on a client that would happily have allowed it.
local _, db6 = run(nil, nil)
assert((asked.COMBAT_LOG_EVENT_UNFILTERED or 0) == 0,
  "the combat log was asked for " .. tostring(asked.COMBAT_LOG_EVENT_UNFILTERED) .. " times on a fresh database")
assert((asked.COMBAT_LOG_EVENT or 0) == 0, "the other form of the combat log was asked for")
assert(db6.blockedEvents == nil, "an event that is never asked for was reported as measured")
-- Anything else the client refuses is still found by asking, and the ask is
-- guarded so an event it does not know cannot take the rest of the file down.
refusedEvents["UNIT_AURA"] = true
local _, db6b = run()
assert(db6b.blockedEvents and db6b.blockedEvents.UNIT_AURA, "a refused event was not written down")
refusedEvents = {}
print("ok: the combat log is never asked for, other refusals are still measured")

-- Seventh pass: with the combat log refused, the addon has to find out which
-- of the remaining channels this client still allows, and what they carry.
refusedEvents["COMBAT_LOG_EVENT_UNFILTERED"] = true
refusedEvents["CHAT_MSG_SPELL_SELF_DAMAGE"] = true
local ns8, db8
do
  local n, d = run(function()
    SlashCmdList.SIMFOREVER("survey")
    fire("UNIT_COMBAT", "target", "WOUND", "CRITICAL", 142, 1)
    fire("UNIT_COMBAT", "target", "WOUND", "", 71, 1)
  end)
  ns8, db8 = n, d
end
assert(db8.channels, "the survey wrote nothing down")
assert(db8.channels.UNIT_COMBAT, "an allowed channel was not kept")
assert(db8.channels.UNIT_COMBAT.fired == 2, "fires not counted: " .. tostring(db8.channels.UNIT_COMBAT.fired))
assert(db8.channels.UNIT_COMBAT.samples[1][4] == 142, "the payload was not kept")
assert(db8.channels.CHAT_MSG_SPELL_SELF_DAMAGE == nil, "a refused channel was kept as if it answered")
assert(db8.blockedEvents.CHAT_MSG_SPELL_SELF_DAMAGE, "a refused channel was not written down")
refusedEvents = {}
-- Eighth pass: when the client refuses something it names the addon and the
-- function on the popup. That name is the whole diagnosis, so it has to be
-- kept rather than left on screen for the player to read out.
local _, db9 = run(function()
  StaticPopup_Show("ADDON_ACTION_FORBIDDEN", "SimforeverProbe", "CastSpellByName()")
end)
assert(db9.forbidden and db9.forbidden[1], "the refused action was not written down")
assert(db9.forbidden[1].func == "CastSpellByName()",
  "the refused function was lost: " .. tostring(db9.forbidden[1].func))
assert(db9.forbidden[1].addon == "SimforeverProbe", "the named addon was lost")
print("ok: a refused action is caught by name, " .. db9.forbidden[1].func)

-- Ninth pass: the addon turns the client's own combat logging on, because it
-- is the only per-hit record this client allows anyone to have, and asks for
-- the detailed form, which puts the attacker's power on every line.
--
-- And it must never reach for the toggle behind /combatlog. On a client that
-- does not answer whether it is logging, a toggle turns it off every time the
-- addon runs, which is what happened: the logging worked and the addon kept
-- switching it back.
do
  local probe = io.open("SimforeverProbe/Combat.lua"):read("a")
  assert(not probe:find("SlashCmdList"),
    "the addon reaches for a toggle to set a state")
end

-- A client that answers nothing about whether it is logging must still be
-- asked to log, rather than read as already logging or as hopeless.
do
  local saved = IsLoggingCombat
  IsLoggingCombat = function() return nil end
  loggingCalls = 0
  local _, dbq = run()
  assert(loggingCalls > 0, "a client that says nothing was never asked to log")
  assert(dbq.logging and dbq.logging.switchExists, "the switch was not recorded as being there")
  IsLoggingCombat = saved
  print("ok: a client that will not say whether it is logging is still asked to")
end
local _, db10 = run()
assert(logging == true, "combat logging was not turned on")
assert(cvars.advancedCombatLogging == "1", "detailed logging was not asked for")
assert(db10.logging and db10.logging.on and db10.logging.advanced, "logging state not recorded: "
  .. tostring(db10.logging and db10.logging.on))
-- Already-on logging must not be toggled: turning it off and on again would
-- start a new file and lose the fight in progress.
local before = loggingCalls
local _, db11 = run(function() end)
assert(db11.logging.on, "logging lost")
local perSession = loggingCalls - before
assert(perSession <= 1, "combat logging was toggled " .. perSession .. " times in one session")
print("ok: the client's combat log is switched on once per session, detailed")

print(("ok: the survey found %d channels and kept what they carried"):format(
  (function() local n = 0 for _ in pairs(db8.channels) do n = n + 1 end return n end)()))
print(("ok: hidden values survived, %d snapshots, %d events, refused %s"):format(#db2.snapshots, #db2.events,
	(function() local n = {} for k in pairs(db2.blocked) do n[#n + 1] = k end return table.concat(n, ",") end)()))

-- Talents. This client counts tabs, so that is what the old reader answers
-- with, and the node reader must say nothing rather than invent a tree.
do
  local _, dbt = run()
  local snap = dbt.snapshots[#dbt.snapshots]
  assert(type(snap.talents) == "string" and #snap.talents > 0,
    "the tab reader said nothing on a client that counts tabs")
  assert(snap.talentRanks == nil, "a tree of nodes was read from a client that has none")
  assert(dbt.clientApi["C_Traits.GetNodeInfo"] == false,
    "an absent function was not recorded as absent: " .. tostring(dbt.clientApi["C_Traits.GetNodeInfo"]))
  assert(dbt.clientApi["GetNumTalentTabs"] == "function", "a present function was not recorded")
  print("ok: an absent talent api is recorded as absent, not left out")
end

-- A client built on a newer base keeps a tree of nodes instead. The points are
-- read by node, because a node names the talent and a position does not.
do
  local savedTraits, savedClass = C_Traits, C_ClassTalents
  local ranks = { [105939] = 5, [105940] = 2, [105941] = 0 }
  C_ClassTalents = { GetActiveConfigID = function() return 77 end }
  C_Traits = {
    GetConfigInfo = function(id) return id == 77 and { treeIDs = { 800 } } or nil end,
    GetTreeNodes = function(tree) return tree == 800 and { 105939, 105940, 105941 } or {} end,
    GetNodeInfo = function(_, node) return { activeRank = ranks[node] } end,
  }
  local _, dbn = run()
  local snap = dbn.snapshots[#dbn.snapshots]
  assert(type(snap.talentRanks) == "table", "the node reader said nothing on a client with a tree")
  assert(snap.talentRanks["105939"] == 5, "wrong rank read: " .. tostring(snap.talentRanks["105939"]))
  assert(snap.talentRanks["105940"] == 2, "wrong rank read")
  assert(snap.talentRanks["105941"] == 0,
    "a talent with no points was left out, which makes the first point spent unreadable")
  assert(dbn.clientApi["C_Traits.GetNodeInfo"] == "function", "a present function was not recorded")
  C_Traits, C_ClassTalents = savedTraits, savedClass
  print("ok: talents are read by node where the client keeps a tree of them")
end

-- A tree the client will not open must leave nothing behind, rather than a
-- half-read one that would look like a respec to whatever reads it next.
do
  local savedTraits, savedClass = C_Traits, C_ClassTalents
  C_ClassTalents = { GetActiveConfigID = function() return nil end }
  C_Traits = {
    GetConfigInfo = function() error("refused") end,
    GetTreeNodes = function() error("refused") end,
    GetNodeInfo = function() error("refused") end,
  }
  local _, dbr = run()
  local snap = dbr.snapshots[#dbr.snapshots]
  assert(snap.talentRanks == nil, "a refused tree left something behind")
  C_Traits, C_ClassTalents = savedTraits, savedClass
  print("ok: a tree the client refuses leaves nothing behind")
end

-- A client that opens its tree but will not say which one is active must not
-- be read as though any tree would do.
do
  local savedTraits, savedClass = C_Traits, C_ClassTalents
  C_ClassTalents = { GetActiveConfigID = function() return nil end }
  C_Traits = {
    GetConfigInfo = function() return { treeIDs = { 800 } } end,
    GetTreeNodes = function() return { 105939 } end,
    GetNodeInfo = function() return { activeRank = 5 } end,
  }
  local _, dbc = run()
  assert(dbc.snapshots[#dbc.snapshots].talentRanks == nil,
    "a tree was read without the client saying which one is active")
  C_Traits, C_ClassTalents = savedTraits, savedClass
  print("ok: no active tree means no reading, not any tree")
end

-- A client that names its tree and then refuses to open it must be survived,
-- not thrown out of: an error here would abandon the rest of the snapshot.
do
  local savedTraits, savedClass = C_Traits, C_ClassTalents
  for _, which in ipairs({ "GetConfigInfo", "GetTreeNodes", "GetNodeInfo" }) do
    C_ClassTalents = { GetActiveConfigID = function() return 77 end }
    C_Traits = {
      GetConfigInfo = function() return { treeIDs = { 800 } } end,
      GetTreeNodes = function() return { 105939 } end,
      GetNodeInfo = function() return { activeRank = 5 } end,
    }
    C_Traits[which] = function() error("refused") end
    local okRun, _, dbx = pcall(run)
    assert(okRun, which .. " threw and took the snapshot with it")
    assert(dbx.snapshots[#dbx.snapshots].talentRanks == nil,
      which .. " refused and something was recorded anyway")
    assert(#dbx.snapshots > 0, which .. " cost us the rest of the snapshot")
  end
  C_Traits, C_ClassTalents = savedTraits, savedClass
  print("ok: a refusal part way through the tree is survived, not thrown out of")
end

-- A talent point spent is the one measurement the client never gives us, and
-- the game only writes an addon's records on a clean exit. If the session ends
-- any other way the measurement goes with it, so the addon has to say so at
-- the moment it is worth saying, and only then.
do
  local savedTraits, savedClass = C_Traits, C_ClassTalents
  local ranks = { [105939] = 0, [105940] = 0 }
  C_ClassTalents = { GetActiveConfigID = function() return 77 end }
  C_Traits = {
    GetConfigInfo = function() return { treeIDs = { 800 } } end,
    GetTreeNodes = function() return { 105939, 105940 } end,
    GetNodeInfo = function(_, node) return { activeRank = ranks[node] } end,
  }
  local said = {}
  local realPrint = print
  print = function(msg) said[#said + 1] = tostring(msg); realPrint(msg) end

  local nsT = select(2, (function() local n = {} ; return nil, n end)())
  local _ = nsT
  local nsLocal, dbLocal = run(function()
    -- nothing spent yet
  end)
  -- Spending a point and snapshotting again is what has to be noticed. It is
  -- written down rather than said: the addon is going on CurseForge and one
  -- that interrupts play gets uninstalled, and losing a measurement is our
  -- problem, not something the player asked about. /sfp notes reads it.
  local before = #said
  ranks[105939] = 1
  SlashCmdList.SIMFOREVER_SNAP()
  local noted = 0
  for _, line in ipairs(nsLocal.notes or {}) do
    if line:find("talents changed") then noted = noted + 1 end
  end
  assert(noted == 1, "spending a talent point was not written down")
  for i = before + 1, #said do
    assert(not said[i]:find("talents changed"), "the talent warning was printed at the player")
  end

  -- And it is recorded once, not once a point. Somebody respeccing spends a
  -- dozen in a row and ns.note keeps one of each line.
  ranks[105940] = 1
  SlashCmdList.SIMFOREVER_SNAP()
  SlashCmdList.SIMFOREVER_SNAP()
  ranks[105939] = 2
  SlashCmdList.SIMFOREVER_SNAP()
  SlashCmdList.SIMFOREVER_SNAP()
  local again = 0
  for _, line in ipairs(nsLocal.notes or {}) do
    if line:find("talents changed") then again = again + 1 end
  end
  assert(again == 1, "the talent note was recorded more than once")

  print = realPrint
  C_Traits, C_ClassTalents = savedTraits, savedClass
  local _ = dbLocal
  realPrint("ok: a talent point spent is written down, not said, and only once")
end

-- A whole session's snapshots once carried no stats while health and mana came
-- through, and nothing recorded whether the reading had stopped existing, had
-- raised, or had been hidden. Three different problems, identical from outside.
do
	local before = ns.db.apiFailed and ns.db.apiFailed.NotARealFunction
	assert(ns.api("NotARealFunction") == nil, "a missing function returned something")
	assert(ns.db.apiFailed and ns.db.apiFailed.NotARealFunction, "a missing reading was not written down")
	assert(before == nil, "the harness was not starting clean")
	Exploder = function() error("boom") end
	assert(ns.api("Exploder") == nil, "a raising function returned something")
	assert(ns.db.apiFailed.Exploder and ns.db.apiFailed.Exploder.why:find("boom"),
		"a raising reading did not say what it raised")
	print("ok: a reading that comes back with nothing says why")
end

-- The snapshot list is full of one level and the levels before it cannot be
-- visited again, so what gets dropped is a reading something else covers.
do
	local keep = ns.MAX_SNAPSHOTS
	-- Four slots for three levels that cannot be revisited plus one of the new
	-- one. Three slots could not hold four levels and no rule would help.
	ns.MAX_SNAPSHOTS = 4
	ns.db.snapshots = {
		{ level = 11, stats = { strength = 20 } },
		{ level = 12, stats = { strength = 22 } },
		{ level = 13, reason = "naked", stats = { strength = 24 } },
	}
	for i = 1, 20 do
		ns.pushSnapshot({ level = 14, stats = { strength = 26 } })
	end
	local levels = {}
	for _, s in ipairs(ns.db.snapshots) do
		levels[s.level] = (levels[s.level] or 0) + 1
	end
	assert(levels[11] == 1, "the only reading of level 11 was dropped")
	assert(levels[12] == 1, "the only reading of level 12 was dropped")
	assert(levels[13] == 1, "the naked reading was dropped")
	assert(#ns.db.snapshots <= 4, "the list grew without limit: " .. #ns.db.snapshots)
	ns.MAX_SNAPSHOTS = keep
	print("ok: a level that cannot be revisited survives a full list")
end

-- Every rate in the melee hit table belongs to a level difference, and a log
-- states the creature's level and not the player's. One character's swings were
-- usable and the four other people in the dungeon swinging at the same creatures
-- were not, so the roster carries the level the log never says.
do
	local s = ns.snapshot("roster-check")
	assert(s.roster, "no roster recorded")
	local by = {}
	for _, m in ipairs(s.roster) do
		by[m.name] = m
	end
	assert(by.Probe and by.Probe.level == 30, "the player is not in the roster at its own level")
	assert(by.Yoshii and by.Yoshii.level == 19 and by.Yoshii.class == "PALADIN",
		"a party member's level or class is missing")
	assert(by.Deaths and by.Deaths.guid == "Player-1-CCCCCC", "a party member's guid is missing")
	assert(#s.roster == 3, "the roster has " .. #s.roster .. " entries, want 3")
	print("ok: the roster names everyone in the group and the level each one is")
end

-- The bags are twenty more readings of what the game says an item is worth,
-- against the client database every published number is derived from. They cost
-- nothing to ask for, and the items table was empty for as long as it existed
-- because the one reading it needed returned without a word when it was missing.
do
  local bag = {
    [0] = { "|cffffffff|Hitem:12640::::::::60:::::::::|h[Lionheart Helm]|h|r" },
    [1] = { "|cffffffff|Hitem:19019::::::::60:::::::::|h[Thunderfury]|h|r" },
  }
  GetContainerNumSlots = function(b) return bag[b] and #bag[b] or 0 end
  GetContainerItemLink = function(b, s) return bag[b] and bag[b][s] or nil end
  GetItemStats = function(_, out) out.ITEM_MOD_STRENGTH_SHORT = 18; return out end
  ns.db.items = {}
  ns.snapshot("bags-check")
  local n = 0
  for _ in pairs(ns.db.items) do n = n + 1 end
  assert(n >= 2, "the bags were not read: " .. n .. " item(s)")
  print(("ok: %d item(s) recorded from what is worn and carried"):format(n))
end

-- A reading that is simply not in this client has to say so, even where it
-- cannot go through ns.api because it fills a table it is handed.
do
  GetItemStats = nil
  ns.db.apiFailed = ns.db.apiFailed or {}
  ns.db.apiFailed.GetItemStats = nil
  ns.recordItem("|cffffffff|Hitem:99999::::::::60:::::::::|h[Nothing]|h|r")
  assert(ns.db.apiFailed.GetItemStats, "a missing item reading was not written down")
  print("ok: a missing item reading says so instead of returning quietly")
end

-- A full buffer cannot be its own sequence counter. Existing ambiguous IDs
-- stay untouched, while every new reading is unique across trimming/reload.
do
    local oldLevel = UnitLevel
    local engine, d = run()
    engine.MAX_SNAPSHOTS = 12
    local seen, highest = {}, 0
    for i = 1, 650 do
        local s = engine.snapshot('sequence')
        assert(not seen[s.id] and s.id > highest, 'snapshot identity reused')
        seen[s.id], highest = true, s.id
    end
    assert(#d.snapshots == 12 and d.snapshotsDiscarded > 0, 'buffer retention not bounded/reported')
    local reloaded, same = run(nil, d)
    assert(same.snapshots[#same.snapshots].id > highest, 'sequence reset on reload')
    local before = reloaded.snapshot('level-before')
    UnitLevel = function(unit) return unit == 'player' and 31 or oldLevel(unit) end
    local after = reloaded.snapshot('level-after')
    assert(after.transition, 'level transition not protected')
    local levels = {}
    for _, row in ipairs(same.ratingReadings) do
        if row.key == 'cr6' and row.rating == 40 then levels[row.level] = true end
    end
    assert(levels[30] and levels[31], 'equal ratings at different levels conflated')
    assert(same.ratings.cr6['40'].level == 31, 'legacy latest-level aggregate stale')
    SlashCmdList.SIMFOREVER('wipe')
    assert(#same.ratingReadings == 2, 'wipe did not reset distinct rating readings')
    UnitLevel = function(unit) if unit ~= 'player' then return oldLevel(unit) end end
    local hiddenLevel = reloaded.snapshot('unknown-level')
    assert(hiddenLevel.level == nil and same.ratingReadings[#same.ratingReadings].level == nil,
        'an unavailable level was guessed or broke rating collection')
    assert(same.ratings.cr6['40'].level == 31, 'unknown level replaced a usable legacy aggregate')
    UnitLevel = oldLevel
    print('ok: snapshot IDs survive trimming/reload; level-specific ratings retained')
end

-- Modern item APIs return a table. Partial sweeps must survive reload without
-- losing existing answers, must pause in combat, and must ignore stray events.
do
    local oldItem, oldInfo, oldTime, oldTimer, oldStats = C_Item, GetItemInfo, GetTime, C_Timer, GetItemStats
    local clock, ready, requests = 0, {[101] = true}, {}
    C_Item = {
        GetItemInfo = function(id)
            if type(id) == 'string' then id = tonumber(id:match('item:(%d+)')) end
            if ready[id] then return 'Test', nil, 2, 10, id == 101 and 0 or 20 end
        end,
        GetItemStats = function(link)
            local id = tonumber(link:match('item:(%d+)'))
            if ready[id] then return { ITEM_MOD_STRENGTH_SHORT = id } end
        end,
        RequestLoadItemDataByID = function(id) requests[#requests + 1] = id end,
    }
    GetItemStats, GetTime = nil, function() return clock end
    C_Timer = { After = function(delay, fn) if delay ~= 8 then fn() end end }
    local engine, d = run()
    engine.itemIDs = {101, 102, 103}
    local function advance()
        clock = clock + 0.3
        for _, f in ipairs(frames) do if f.scripts.OnUpdate then f.scripts.OnUpdate(f, 0.3) end end
    end
    engine.sweepItemLevels()
    advance()
    assert(d.itemLevels['101'] == 0 and d.items['item:101'], 'modern API/cache answer missing')
    assert(d.itemLevelsRun.running and d.itemLevelsRun.answered == 1, 'partial results not persisted')
    fire('GET_ITEM_INFO_RECEIVED', 999, true)
    assert(d.itemLevels['999'] == nil, 'unrequested item included')
    inCombat = true
    local count = #requests
    advance()
    assert(#requests == count, 'item sweep requested data in combat')
    inCombat = false
    engine, d = run(nil, d)
    engine.itemIDs = {101, 102, 103}
    engine.sweepItemLevels()
    advance()
    assert(d.itemLevels['101'] == 0, 'reload lost an existing answer')
    ready[102], ready[103] = true, true
    fire('GET_ITEM_INFO_RECEIVED', 102, true)
    advance()
    fire('GET_ITEM_INFO_RECEIVED', 103, true)
    advance()
    assert(d.itemLevelsRun.answered == 3 and d.itemLevelsRun.statsAnswered == 3, 'async answers lost/double-counted')
    assert(not d.itemLevelsRun.running, 'completed sweep did not stop')
    local answered = d.itemLevelsRun.answered
    fire('GET_ITEM_INFO_RECEIVED', 103, true)
    assert(d.itemLevelsRun.answered == answered, 'duplicate event counted twice')
    C_Item = {GetItemStats = function() return { safe = 3, hidden = 9009 } end}
    combatOnlySecret = false
    secretValues[9009] = true
    assert(engine.recordItem('item:104').stats.hidden == nil, 'hidden item stat persisted')
    secretValues[9009] = nil
    C_Item, GetItemInfo, GetTime, C_Timer, GetItemStats = oldItem, oldInfo, oldTime, oldTimer, oldStats
    print('ok: namespace item stats, partial sweep, combat pause, async answers and hidden values')
end

-- A normal installation collects without commands and prints only its login notice.
do
    local oldItem, oldTimer, oldTime, oldGUID, oldType = C_Item, C_Timer, GetTime, UnitGUID, UnitPowerType
    local timers, clock, ready, requests = {}, 0, {[101] = true}, {}
    C_Timer = { After = function(delay, fn)
        if delay == 8 or delay == 900 then timers[#timers + 1] = {delay, fn} else fn() end
    end }
    GetTime = function() return clock end
    C_Item = {
        GetItemInfo = function(id)
            if type(id) == 'string' then id = tonumber(id:match('item:(%d+)')) end
            if ready[id] then return 'Test', nil, 2, 10, 20 end
        end,
        GetItemStats = function(link)
            local id = tonumber(link:match('item:(%d+)'))
            if ready[id] then return {ITEM_MOD_STRENGTH_SHORT = id} end
        end,
        RequestLoadItemDataByID = function(id) requests[#requests + 1] = id end,
    }
    UnitGUID = function(unit)
        if unit == 'pet' then return 'Pet-1-AAAAAA' end
        if unit == 'target' then return 'Creature-0-1-2-3-5678-AAAAAA' end
        return oldGUID(unit)
    end
    UnitPowerType = function() return 3, 'ENERGY' end
    local engine, d = run(nil, nil, true)
    assert(#gameMessages == 1 and gameMessages[1]:find('Logging all combat.', 1, true), 'passive login chat is noisy')
    assert(d.recording and #d.fights == 2 and #d.pulls == 1, 'ordinary fights need no user command')
    assert(d.fights[1].target.guid:find('Creature', 1, true) and d.fights[1].pet.owner == oldGUID('player'), 'fight target/pet ownership lost')
    assert(d.fights[1].lastRestSnapshot and d.sessions[1].id == d.fights[1].session, 'resting/session context missing')
    engine.itemIDs = {101, 102, 103}
    timers[1][2]()
    local function tick()
        clock = clock + 0.3
        for _, f in ipairs(frames) do if f.scripts.OnUpdate then f.scripts.OnUpdate(f, 0.3) end end
    end
    tick()
    assert(d.itemLevelsRun.running and d.itemLevels['101'] == 20, 'automatic item scan did not start')
    assert(#requests <= 2, 'item requests exceeded the per-tick bound')
    inCombat = true
    local count = #requests
    tick()
    assert(#requests == count, 'automatic item scan requested in combat')
    fire('UNIT_SPELLCAST_SUCCEEDED', 'player', 'Cast-123', 1752)
    fire('UNIT_SPELLCAST_CHANNEL_START', 'pet', 'Cast-456', 1234)
    fire('UNIT_POWER_UPDATE', 'player', 'ENERGY')
    fire('UNIT_POWER_UPDATE', 'player', 'ENERGY')
    fire('UNIT_POWER_UPDATE', 'player', 'MANA')
    assert(#d.events == 3 and d.events[1].spell == 1752 and d.events[2].owner == oldGUID('player'), 'passive cast/pet/resource events incorrect')
    inCombat = false
    ready[102], ready[103] = true, true
    tick()
    fire('ITEM_DATA_LOAD_RESULT', 102, true)
    fire('GET_ITEM_INFO_RECEIVED', 103, true)
    tick()
    assert(d.itemLevelsRun.answered == 3 and not d.itemLevelsRun.running, 'automatic async scan did not complete')
    assert(#gameMessages == 1, 'automatic collection printed progress or fight messages')
    engine.MAX_EVENTS = 20
    for i = 1, 100 do engine.pushEvent({k='fixture', sequence=i}) end
    assert(#d.events <= 20 and d.events[#d.events].sequence == 100 and d.eventsDiscarded > 0, 'event retention is unbounded or stale')
    SlashCmdList.SIMFOREVER_ITEMS('stop')
    local _, paused = run(nil, d, true)
    timers[#timers][2]()
    assert(not paused.itemLevelsRun.running and paused.itemLevelsRun.stopped == 'paused by hand', 'reload ignored an explicit pause')
    C_Item, C_Timer, GetTime, UnitGUID, UnitPowerType = oldItem, oldTimer, oldTime, oldGUID, oldType
    print('ok: zero-command collection, one login notice, ordinary fights, pets, casts, resources and bounded retention')
end

-- Aura IDs come from named fields or the documented legacy position, never a guessed number.
do
    local oldAuras, oldBuff, oldDebuff = C_UnitAuras, UnitBuff, UnitDebuff
    C_UnitAuras = {GetAuraDataByIndex = function(_, index, filter)
        if index == 1 then return {spellId=filter == 'HELPFUL' and 123 or 456, applications=2,
            duration=30, expirationTime=9009, sourceUnit='player'} end
    end}
    secretValues[9009] = true
    local engine, d = run(nil, nil, true)
    local s = d.snapshots[1]
    assert(s.auras[1].spellId == 123 and s.auras[1].applications == 2 and s.debuffs[1].spellId == 456, 'aura details missing')
    assert(s.auras[1].expirationTime == nil, 'hidden aura value leaked')
    C_UnitAuras = nil
    UnitBuff = function(_, index)
        if index == 1 then return 'Aura', 999, 2, nil, 30, 40, 'player', false, false, 789, true, false, 99999 end
    end
    local ids = engine.aurasOn('player')
    assert(ids[1] == 789 and #ids == 1, 'legacy aura ID was truncated or guessed')
    secretValues[9009] = nil
    C_UnitAuras, UnitBuff, UnitDebuff = oldAuras, oldBuff, oldDebuff
    print('ok: aura stacks, source and duration preserved; secrets omitted; legacy IDs use position ten')
end

-- Leave synthetic current-build data for the companion's offline round-trip check.
do
    local oldBuild, oldGUID, oldAuras = GetBuildInfo, UnitGUID, C_UnitAuras
    GetBuildInfo = function() return '1.60.1', '70338', 'Oct 10 2026', 16001 end
    UnitGUID = function(unit)
        if unit == 'pet' then return 'Pet-1-FIXTURE' end
        if unit == 'target' then return 'Creature-0-1-2-3-5678-FIXTURE' end
        return oldGUID(unit)
    end
    C_UnitAuras = {GetAuraDataByIndex = function(_, index)
        if index == 1 then return {spellId=123, applications=2, duration=30, sourceUnit='player'} end
    end}
    local engine, d = run(nil, nil, true)
    fire('UNIT_SPELLCAST_SUCCEEDED', 'player', 'Cast-FIXTURE', 1752)
    local function literal(value)
        if type(value) == 'table' then
            local fields = {}
            for key, entry in pairs(value) do
                fields[#fields + 1] = '[' .. literal(key) .. ']=' .. literal(entry)
            end
            return '{' .. table.concat(fields, ',') .. '}'
        elseif type(value) == 'string' then return string.format('%q', value)
        else return tostring(value) end
    end
    local file = assert(io.open('/tmp/simforever-addon-034-fixture.lua', 'w'))
    file:write('SimforeverProbeDB=', literal(d))
    file:close()
    GetBuildInfo, UnitGUID, C_UnitAuras = oldBuild, oldGUID, oldAuras
    print('ok: synthetic 70338 recording includes sessions, fights, pets, auras and casts')
end

do
    local oldBuild, oldCVars, oldGet, oldSet = GetBuildInfo, C_CVar, GetCVar, SetCVar
    GetBuildInfo = function() return '1.60.1', '70707', 'Oct 30 2026', 16001 end
    C_CVar = { GetCVar = oldGet, SetCVar = oldSet }
    GetCVar, SetCVar = nil, nil
    local engine, d = run(nil, nil, true)
    assert(d.build.build == '70707' and d.build.toc == 16001, 'launch build identity missing')
    assert(d.logging.on and d.logging.advanced, 'namespace-only CVars did not enable advanced logging')
    assert(d.recording and #d.snapshots > 0 and #d.fights == 2, 'launch passive collection failed')
    assert(#gameMessages == 1 and gameMessages[1]:find('Logging all combat.', 1, true), 'launch login chat is noisy')
    assert(engine.toJSON(d):find('70707', 1, true), 'launch export lost client build')
    GetBuildInfo, C_CVar, GetCVar, SetCVar = oldBuild, oldCVars, oldGet, oldSet
    print('ok: launch 70707 identity, namespace-only CVars, passive collection and one login notice')
end
