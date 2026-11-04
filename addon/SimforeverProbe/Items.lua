-- Throttled, resumable item readings and observed loot sources.
local _, ns = ...

local INFLIGHT, BATCH, FEED, QUIET = 12, 2, 0.25, 20
local frame, run = CreateFrame("Frame"), nil
local automaticPasses = 0

local function available(name)
    return not ns.blocked[name] and not ns.blocked[name:match("[^.]+$")]
        and type(ns.lookup(name)) == "function"
end

local function progress(stopped)
    local rec = ns.db.itemLevelsRun
    rec.at, rec.answered = ns.now(), run.answered
    rec.missing, rec.statsAnswered = rec.asked - run.answered, run.statsAnswered
    rec.running, rec.stopped = stopped == nil, stopped
end

local function stop(why)
    frame:SetScript("OnUpdate", nil)
    frame:UnregisterEvent("GET_ITEM_INFO_RECEIVED")
    frame:UnregisterEvent("ITEM_DATA_LOAD_RESULT")
    progress(why)
    local automatic = run.automatic
    if not automatic then
        ns.log(("Items: %d of %d required levels, %d stat records. %s.")
            :format(run.answered, #ns.itemIDs, run.statsAnswered, why))
    end
    run = nil
    if automatic and why == "pass finished; missing answers can be retried" and automaticPasses < 3 then
        C_Timer.After(900, function()
            if not run and ns.db.itemLevelsRun.stopped ~= "paused by hand" then ns.sweepItemLevels(true) end
        end)
    end
end

local function record(id)
    local _, _, _, _, level = ns.api(run.info, id)
    if type(level) ~= "number" or level < 0 then return false end
    if ns.db.itemLevels[tostring(id)] == nil then run.answered = run.answered + 1 end
    ns.db.itemLevels[tostring(id)] = level
    local link = "item:" .. id
    local item = run.collectStats and ns.recordItem(link) or nil
    if item and not run.statsFound[id] then
        run.statsFound[id], run.statsAnswered = true, run.statsAnswered + 1
    end
    progress()
    return not run.collectStats or item ~= nil
end

local function onEvent(_, event, id, success)
    if (event ~= "GET_ITEM_INFO_RECEIVED" and event ~= "ITEM_DATA_LOAD_RESULT")
        or not run or not run.pending[id] then return end
    run.pending[id], run.outstanding = nil, run.outstanding - 1
    if success ~= false then record(id) end
end

local function tick(_, elapsed)
    if not run or ns.api("InCombatLockdown") then return end
    run.clock = run.clock + elapsed
    if run.clock < FEED then return end
    run.clock = 0
    local now = ns.api("GetTime") or 0
    for id, sent in pairs(run.pending) do
        if now - sent >= QUIET then
            run.pending[id], run.outstanding = nil, run.outstanding - 1
        end
    end
    local fed = 0
    while run.outstanding < INFLIGHT and run.i <= #run.queue and fed < BATCH do
        local id = run.queue[run.i]
        run.i, fed = run.i + 1, fed + 1
        if not record(id) then
            run.pending[id], run.outstanding = now, run.outstanding + 1
            if run.request then ns.api(run.request, id) end
        end
    end
    if run.i > #run.queue and run.outstanding == 0 then stop("pass finished; missing answers can be retried") end
end

function ns.sweepItemLevels(automatic)
    if run then
        if not automatic then ns.log("Item collection is already running.") end
        return
    end
    local info = available("C_Item.GetItemInfo") and "C_Item.GetItemInfo" or "GetItemInfo"
    if not available(info) then
        ns.noteApiFailure(info, "absent")
        if not automatic then ns.log("Item information is unavailable on this client.") end
        return
    end
    if not ns.itemIDs or #ns.itemIDs == 0 then return end
    ns.db.itemLevels = ns.db.itemLevels or {}
    for key, value in pairs(ns.db.itemLevels) do
        if type(key) == "number" then ns.db.itemLevels[tostring(key)], ns.db.itemLevels[key] = value, nil end
    end
    run = { queue = {}, pending = {}, i = 1, outstanding = 0, clock = 0,
        answered = 0, statsAnswered = 0, statsFound = {}, info = info,
        request = available("C_Item.RequestLoadItemDataByID") and "C_Item.RequestLoadItemDataByID" or nil,
        collectStats = available("C_Item.GetItemStats") or available("GetItemStats"), automatic = automatic }
    if automatic then automaticPasses = automaticPasses + 1 end
    for _, id in ipairs(ns.itemIDs) do
        local hasLevel = ns.db.itemLevels[tostring(id)] ~= nil
        local hasStats = ns.db.items["item:" .. id] ~= nil
        if hasLevel then run.answered = run.answered + 1 end
        if hasStats then run.statsAnswered, run.statsFound[id] = run.statsAnswered + 1, true end
        if not hasLevel or (run.collectStats and not hasStats) then run.queue[#run.queue + 1] = id end
    end
    ns.db.itemLevelsRun = { asked = #ns.itemIDs, build = ns.build() }
    progress()
    if #run.queue == 0 then stop("all candidates already recorded") return end
    local legacy = ns.listen(frame, "GET_ITEM_INFO_RECEIVED")
    local modern = ns.listen(frame, "ITEM_DATA_LOAD_RESULT")
    if not legacy and not modern then stop("item-load event unavailable") return end
    frame:SetScript("OnEvent", onEvent)
    frame:SetScript("OnUpdate", tick)
    if not automatic then ns.log(("Collecting %d missing items out of combat."):format(#run.queue)) end
end

SLASH_SIMFOREVER_ITEMS1 = "/sfpitems"
SlashCmdList.SIMFOREVER_ITEMS = function(msg)
    if msg and msg:lower():match("^%s*stop%s*$") then
        if run then
            run.automatic = false
            stop("paused by hand")
        else
            ns.db.itemLevelsRun = ns.db.itemLevelsRun or {}
            ns.db.itemLevelsRun.running, ns.db.itemLevelsRun.stopped = false, "paused by hand"
            ns.log("Item collection paused.")
        end
        return
    end
    ns.sweepItemLevels()
end

ns.onReady(function()
    C_Timer.After(8, function()
        if not ns.db.itemLevelsRun or ns.db.itemLevelsRun.stopped ~= "paused by hand" then
            ns.sweepItemLevels(true)
        end
    end)
end)

local function recordLoot()
	local n = ns.api("GetNumLootItems")
	if type(n) ~= "number" or n < 1 then
		return
	end
	for slot = 1, n do
		local link = ns.api("GetLootSlotLink", slot)
		local id = link and tonumber(link:match("item:(%d+)"))
		if id then
			local info = { ns.api("GetLootSourceInfo", slot) }
			local tName, tGuid = ns.api("UnitName", "target"), ns.api("UnitGUID", "target")
			for i = 1, #info, 2 do
				local guid = info[i]
				if type(guid) == "string" and guid ~= "" then
					local parts = {}
					for piece in guid:gmatch("[^-]+") do
						parts[#parts + 1] = piece
					end
					ns.db.loot = ns.db.loot or {}
					local key = ("%d|%s"):format(id, parts[6] or guid)
					if not ns.db.loot[key] then
						ns.db.loot[key] = {
							item = id,
							link = link,
							kind = parts[1],
							npc = tonumber(parts[6]),
							guid = guid,
							name = (tGuid == guid) and tName or nil,
							zone = ns.api("GetZoneText"),
							subzone = ns.api("GetSubZoneText"),
							at = ns.now and ns.now() or 0,
						}
					end
				end
			end
			ns.recordItem(link)
		end
	end
end

local lootFrame = CreateFrame("Frame")
ns.onReady(function()
	ns.listen(lootFrame, "LOOT_OPENED")
	ns.listen(lootFrame, "LOOT_READY")
end)
lootFrame:SetScript("OnEvent", function()
	recordLoot()
end)
