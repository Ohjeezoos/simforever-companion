-- SavedVariables initialization, build history and bounded record retention.
local ADDON, ns = ...

ns.VERSION = "0.35.0"
ns.MAX_EVENTS = 20000
ns.MAX_SNAPSHOTS = 600

local defaults = {
	version = ns.VERSION,
	recording = true,
	character = {},
	snapshots = {},
	events = {},
	ratings = {},
	ratingReadings = {},
	items = {},
	notes = {},
	secrets = {},
	blocked = {},
	clientApi = {},
}

function ns.log(msg)
	print("|cffc9a46aSimforever|r " .. msg)
end

ns.notes = {}

function ns.note(msg)
	if type(msg) ~= "string" or msg == "" then
		return
	end
	for _, existing in ipairs(ns.notes) do
		if existing == msg then
			return
		end
	end
	ns.notes[#ns.notes + 1] = msg
	if #ns.notes > 40 then
		table.remove(ns.notes, 1)
	end
	if ns.db then
		ns.db.notes = ns.notes
	end
end

function ns.now()
	local t = ns.api("GetServerTime")
	if t then
		return t
	end
	return os and os.time and os.time() or 0
end

function ns.build()
	local version, build, _, tocversion = ns.api("GetBuildInfo")
	return { version = version, build = build, toc = tocversion }
end

local function trim(list, max)
	local excess = #list - max
	if excess <= 0 then
		return
	end
	for i = 1, #list - excess do
		list[i] = list[i + excess]
	end
	for i = #list, max + 1, -1 do
		list[i] = nil
	end
end

function ns.push(list, entry, max)
	list[#list + 1] = entry
	trim(list, max)
end

local function irreplaceable(list, i)
	local s = list[i]
	if not s then
		return false
	end
	if s.reason == "naked" then
		return true
	end
	if not (s.stats and s.stats.strength) then
		return false
	end
	for j = 1, #list do
		local other = list[j]
		local sameCharacter = (s.character and s.character.guid) == (other.character and other.character.guid)
		if j ~= i and sameCharacter and other.level == s.level and other.stats and other.stats.strength then
			return j > i
		end
	end
	return true
end

function ns.pushSnapshot(snap)
	local list = ns.db.snapshots
	list[#list + 1] = snap
	while #list > ns.MAX_SNAPSHOTS do
		local drop
		for i = 1, #list do
			if not list[i].transition and not irreplaceable(list, i) then
				drop = i
				break
			end
		end
		if not drop then
			for i = 1, #list do
				if not irreplaceable(list, i) then
					drop = i
					break
				end
			end
		end
		if not drop then
			for i = 1, #list do
				if list[i].reason ~= "naked" then
					drop = i
					break
				end
			end
		end
		table.remove(list, drop or 1)
		ns.db.snapshotsDiscarded = (ns.db.snapshotsDiscarded or 0) + 1
	end
end

function ns.nextSnapshotID()
	if not ns.db.nextSnapshotID then
		local highest = 0
		for _, snap in ipairs(ns.db.snapshots) do
			if type(snap.id) == "number" and snap.id > highest then
				highest = snap.id
			end
		end
		ns.db.nextSnapshotID = highest + 1
	end
	local id = ns.db.nextSnapshotID
	ns.db.nextSnapshotID = id + 1
	return id
end

local f = CreateFrame("Frame")
ns.listen(f, "ADDON_LOADED")
ns.listen(f, "PLAYER_LOGIN")
f:SetScript("OnEvent", function(_, event, arg1)
	if event == "ADDON_LOADED" and arg1 == ADDON then
		SimforeverProbeDB = SimforeverProbeDB or {}
		local build = ns.build()
		local previous = SimforeverProbeDB.build
		if previous and previous.version and previous.build and build.version and build.build
			and (previous.version ~= build.version or previous.build ~= build.build or previous.toc ~= build.toc) then
			local history = SimforeverProbeDB.history or {}
			SimforeverProbeDB.history = nil
			history[previous.version .. "." .. previous.build .. ":" .. tostring(previous.toc)] = SimforeverProbeDB
			SimforeverProbeDB = { history = history }
		end
		for k, v in pairs(defaults) do
			if SimforeverProbeDB[k] == nil then
				SimforeverProbeDB[k] = v
			end
		end
		SimforeverProbeDB.version = ns.VERSION
		SimforeverProbeDB.build = build
		ns.db = SimforeverProbeDB
		ns.notes = ns.db.notes
		ns.adoptBlocked()
		ns.flushBlockedEvents()
		ns.flushForbidden()
		ns.runReady()
	elseif event == "PLAYER_LOGIN" then
		ns.startSession()
		ns.ensureLogging()
		ns.db.character = ns.characterInfo()
		ns.snapshot("login")
		C_Timer.After(4, function()
			if type((ns.api("UnitStat", "player", 1))) ~= "number" then
				ns.note("this client is not answering UnitStat, so no stats are being recorded")
			end
			local status = ns.db.logging
			if status and status.on == true then
				ns.log("Logging all combat.")
			else
				ns.log("Combat logging requested; recording available character data.")
			end
		end)
	end
end)

function ns.characterInfo()
	local name = ns.api("UnitName", "player")
	local realm = ns.api("GetRealmName") or ""
	local className, classFile = ns.api("UnitClass", "player")
	local raceName, raceFile = ns.api("UnitRace", "player")
	return {
		name = name,
		realm = realm,
		guid = ns.api("UnitGUID", "player"),
		class = classFile,
		className = className,
		race = raceFile,
		raceName = raceName,
		level = ns.api("UnitLevel", "player"),
		faction = ns.api("UnitFactionGroup", "player"),
		talents = ns.talentString(),
		build = ns.build(),
		recordedAt = ns.now(),
	}
end

function ns.talentString()
	local tabs = ns.api("GetNumTalentTabs")
	if not tabs then
		return nil
	end
	local parts = {}
	for tab = 1, tabs do
		local ranks = {}
		for i = 1, (ns.api("GetNumTalents", tab) or 0) do
			local ok, rank = pcall(function() return select(5, GetTalentInfo(tab, i)) end)
			rank = ok and ns.plain(rank, "GetTalentInfo", 5) or nil
			ranks[#ranks + 1] = tostring(rank or 0)
		end
		parts[#parts + 1] = table.concat(ranks)
	end
	return table.concat(parts, "-")
end

function ns.talentRanks()
	local activeConfig = ns.lookup("C_ClassTalents.GetActiveConfigID")
	local configInfo = ns.lookup("C_Traits.GetConfigInfo")
	local treeNodes = ns.lookup("C_Traits.GetTreeNodes")
	local nodeInfo = ns.lookup("C_Traits.GetNodeInfo")
	if type(configInfo) ~= "function" or type(treeNodes) ~= "function" or type(nodeInfo) ~= "function" then
		return nil
	end
	local configID
	if type(activeConfig) == "function" then
		local ok, id = pcall(activeConfig)
		configID = ok and ns.plain(id, "C_ClassTalents.GetActiveConfigID", 1) or nil
	end
	if not configID then
		return nil
	end
	local ok, config = pcall(configInfo, configID)
	if not ok or type(config) ~= "table" or type(config.treeIDs) ~= "table" then
		return nil
	end
	local out, any = {}, false
	for _, treeID in ipairs(config.treeIDs) do
		local okNodes, nodes = pcall(treeNodes, treeID)
		if okNodes and type(nodes) == "table" then
			for _, nodeID in ipairs(nodes) do
				local okInfo, info = pcall(nodeInfo, configID, nodeID)
				if okInfo and type(info) == "table" then
					local rank = info.activeRank
					if rank == nil and type(info.activeEntry) == "table" then
						rank = info.activeEntry.rank
					end
					rank = ns.plain(rank, "C_Traits.GetNodeInfo", 1)
					if type(rank) == "number" and rank >= 0 then
						out[tostring(nodeID)] = rank
						any = true
					end
				end
			end
		end
	end
	if not any then
		return nil
	end
	return out
end

function ns.pushEvent(entry)
	local list = ns.db.events
	if #list >= ns.MAX_EVENTS then
		local count = math.max(1, math.floor(ns.MAX_EVENTS / 20))
		for i = 1, #list - count do list[i] = list[i + count] end
		for i = #list, #list - count + 1, -1 do list[i] = nil end
		ns.db.eventsDiscarded = (ns.db.eventsDiscarded or 0) + count
	end
	list[#list + 1] = entry
end
