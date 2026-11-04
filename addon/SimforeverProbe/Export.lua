-- Optional status and export commands. Collection does not require them.
local ADDON, ns = ...

local function esc(s)
	return (tostring(s):gsub('[%c"\\]', function(c)
		local map = { ['"'] = '\\"', ["\\"] = "\\\\", ["\n"] = "\\n", ["\r"] = "\\r", ["\t"] = "\\t" }
		return map[c] or string.format("\\u%04x", c:byte())
	end))
end

local function encode(v, out)
	if type(v) ~= "table" then
		v = ns.plain(v)
	end
	local t = type(v)
	if t == "nil" or (t == "number" and (v ~= v or v == math.huge or v == -math.huge)) then
		out[#out + 1] = "null"
	elseif t == "boolean" then
		out[#out + 1] = tostring(v)
	elseif t == "number" then
		out[#out + 1] = string.format("%.14g", v)
	elseif t == "string" then
		out[#out + 1] = '"' .. esc(v) .. '"'
	elseif t == "table" then
		if #v > 0 or next(v) == nil then
			out[#out + 1] = "["
			for i = 1, #v do
				if i > 1 then
					out[#out + 1] = ","
				end
				encode(v[i], out)
			end
			out[#out + 1] = "]"
		else
			out[#out + 1] = "{"
			local first = true
			for k, val in pairs(v) do
				if not first then
					out[#out + 1] = ","
				end
				first = false
				out[#out + 1] = '"' .. esc(k) .. '":'
				encode(val, out)
			end
			out[#out + 1] = "}"
		end
	else
		out[#out + 1] = "null"
	end
end

function ns.toJSON(v)
	local out = {}
	encode(v, out)
	return table.concat(out)
end

local frame
local queued

local function buildText(text, label)
	if not frame then
		frame = CreateFrame("Frame", "SimforeverProbeExport", UIParent, "BasicFrameTemplateWithInset")
		frame:SetSize(660, 420)
		frame:SetPoint("CENTER")
		frame:SetMovable(true)
		frame:EnableMouse(true)
		frame:RegisterForDrag("LeftButton")
		frame:SetScript("OnDragStart", frame.StartMoving)
		frame:SetScript("OnDragStop", frame.StopMovingOrSizing)
		frame.title = frame:CreateFontString(nil, "OVERLAY", "GameFontHighlight")
		frame.title:SetPoint("TOP", 0, -6)
		local scroll = CreateFrame("ScrollFrame", nil, frame, "UIPanelScrollFrameTemplate")
		scroll:SetPoint("TOPLEFT", 12, -34)
		scroll:SetPoint("BOTTOMRIGHT", -32, 12)
		local edit = CreateFrame("EditBox", nil, scroll)
		edit:SetMultiLine(true)
		edit:SetFontObject(ChatFontNormal)
		edit:SetWidth(600)
		edit:SetAutoFocus(false)
		edit:SetScript("OnEscapePressed", function() frame:Hide() end)
		scroll:SetScrollChild(edit)
		frame.edit = edit
	end
	frame.title:SetText(label)
	frame.edit:SetText(text)
	pcall(frame.edit.HighlightText, frame.edit)
	pcall(frame.edit.SetFocus, frame.edit)
	frame:Show()
end

local function showText(text, label)
	if InCombatLockdown and InCombatLockdown() then
		queued = { text = text, label = label }
		ns.log("your export is ready, it opens when you leave combat.")
		return
	end
	buildText(text, label)
end

local function summary()
	local db = ns.db
	ns.flushForbidden()
	local ratingLines = {}
	for key, bucket in pairs(db.ratings) do
		local n = 0
		for _ in pairs(bucket) do
			n = n + 1
		end
		ratingLines[#ratingLines + 1] = ("  %s: %d readings"):format(key, n)
	end
	table.sort(ratingLines)
	local hidden = {}
	for name, slots in pairs(db.secrets or {}) do
		local inFight, atRest = false, false
		for _, when in pairs(slots) do
			inFight = inFight or when.combat == true
			atRest = atRest or when.rest == true
		end
		hidden[#hidden + 1] = name .. ((inFight and not atRest) and " (in combat)" or "")
	end
	for _, entry in ipairs(db.forbidden or {}) do
		hidden[#hidden + 1] = ("%s (refused%s)"):format(entry.func or "?",
			(entry.after and #entry.after > 0) and (", after " .. table.concat(entry.after, ", ")) or "")
	end
	for name, entry in pairs(db.blocked or {}) do
		if type(entry) == "table" and entry.named == false then
			hidden[#hidden + 1] = ("a refused action after: %s"):format(table.concat(entry.lastCalls or {}, ", "))
		else
			hidden[#hidden + 1] = name .. " (refused)"
		end
	end
	if #hidden == 0 then
		hidden[#hidden + 1] = "nothing"
	end
	table.sort(hidden)
	local logging = "off"
	if db.logging and db.logging.on then
		logging = db.logging.advanced and "on, detailed" or "on"
	elseif db.logging then
		logging = db.logging.switchExists and "asked for; this client does not say whether it took"
			or "off, this client has no switch for it. Type /combatlog"
	end
	return ("Simforever Probe %s\nCharacter: %s, level %s\nSnapshots: %d\nCombat log: %s\nItems seen: %d\nRating readings:\n%s%s")
		:format(ns.VERSION, db.character.name or "?", tostring(db.character.level or "?"), #db.snapshots, logging,
			(function() local n = 0 for _ in pairs(db.items) do n = n + 1 end return n end)(),
			table.concat(ratingLines, "\n"),
			"\nThis client hides from addons: " .. table.concat(hidden, ", "))
end

SLASH_SIMFOREVER1 = "/sfp"
SLASH_SIMFOREVER2 = "/simforever"
SlashCmdList.SIMFOREVER = function(msg)
	msg = (msg or ""):lower():gsub("^%s+", ""):gsub("%s+$", "")
	local db = ns.db
	if msg == "export" then
		showText(ns.toJSON(db), "Simforever export, press Ctrl+C to copy")
	elseif msg == "ratings" then
		showText(ns.toJSON({ character = db.character, ratings = db.ratings, snapshots = db.snapshots }), "Rating readings, press Ctrl+C to copy")
	elseif msg == "on" then
		db.recording = true
		ns.log("combat recording on.")
	elseif msg == "off" then
		db.recording = false
		ns.log("combat recording off.")
	elseif msg == "survey" then
		local open, total = ns.surveyChannels()
		ns.log(("%d of %d channels open. Fight for a minute, then /sfp export."):format(open, total))
	elseif msg == "stats" then
		local keys = { "strength", "agility", "stamina", "intellect", "spirit" }
		local units = { "player" }
		if ns.api("IsInRaid") then
			for i = 1, 40 do units[#units + 1] = "raid" .. i end
		else
			for i = 1, 4 do units[#units + 1] = "party" .. i end
		end
		local answered, present = 0, 0
		for _, unit in ipairs(units) do
			if ns.api("UnitGUID", unit) then
				present = present + 1
				local got = 0
				for i = 1, 5 do
					if type((ns.api("UnitStat", unit, i))) == "number" then got = got + 1 end
				end
				if got == 5 then answered = answered + 1 end
				if unit == "player" then
					local line = {}
					for i = 1, 5 do
						local total, _, pos, neg = ns.api("UnitStat", unit, i)
						line[i] = ("%s %s"):format(keys[i], total and (total - (pos or 0) - (neg or 0)) or "hidden")
					end
					ns.log("you, less buffs, before gear is taken off: " .. table.concat(line, ", "))
				end
			end
		end
		local n = 0
		for _ in pairs(db.people or {}) do
			n = n + 1
		end
		local loot = 0
		for _ in pairs(db.loot or {}) do
			loot = loot + 1
		end
		ns.log(("%s %d of %d units in your group answer UnitStat. %d character(s) harvested, %d waiting on inspect, %d loot source(s) seen."):format(
			ns.VERSION, answered, present, n, ns.inspectQueued and ns.inspectQueued() or 0, loot))
	elseif msg == "notes" then
		if not ns.notes or #ns.notes == 0 then
			ns.log("nothing to report.")
		else
			for _, line in ipairs(ns.notes) do
				ns.log(line)
			end
		end
	elseif msg == "wipe" then
		db.history = nil
		db.events, db.snapshots, db.ratings, db.items, db.targets = {}, {}, {}, {}, {}
		db.ratingReadings, db.snapshotsDiscarded, db.eventsDiscarded = {}, 0, 0
		db.fights, db.sessions = {}, {}
		ns.currentFight = nil
		db.people, db.pulls, db.loot, ns.peopleCount = {}, {}, {}, 0
		ns.snapshot("wipe")
		ns.log("cleared.")
	else
		ns.log(summary())
		if ns.notes and #ns.notes > 0 then
			ns.log(("%d thing(s) worth knowing. /sfp notes to read them."):format(#ns.notes))
		end
		ns.log("commands: /sfp export, /sfp notes, /sfp stats, /sfp ratings, /sfp on, /sfp off, /sfp wipe, /sfpsnap")
	end
end

local f = CreateFrame("Frame")
ns.onReady(function()
	ns.listen(f, "PLAYER_LOGIN")
	ns.listen(f, "PLAYER_REGEN_ENABLED")
end)
f:SetScript("OnEvent", function(_, event)
	if event == "PLAYER_REGEN_ENABLED" then
		if queued then
			local q = queued
			queued = nil
			buildText(q.text, q.label)
		end
		return
	end
	if ns.db and ns.db.recording == nil then
		ns.db.recording = true
	end
end)
