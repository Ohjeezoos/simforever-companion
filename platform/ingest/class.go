package ingest

var classBySpec = map[int64]string{
	62: "Mage", 63: "Mage", 64: "Mage",
	65: "Paladin", 66: "Paladin", 70: "Paladin",
	71: "Warrior", 72: "Warrior", 73: "Warrior",
	102: "Druid", 103: "Druid", 104: "Druid", 105: "Druid",
	250: "DeathKnight", 251: "DeathKnight", 252: "DeathKnight",
	253: "Hunter", 254: "Hunter", 255: "Hunter",
	256: "Priest", 257: "Priest", 258: "Priest",
	259: "Rogue", 260: "Rogue", 261: "Rogue",
	262: "Shaman", 263: "Shaman", 264: "Shaman",
	265: "Warlock", 266: "Warlock", 267: "Warlock",
}

var classBySpell = map[string]string{}

func init() {
	sig := map[string][]string{
		"Mage":    {"Fireball", "Frostbolt", "Arcane Missiles", "Arcane Explosion", "Pyroblast", "Scorch", "Fire Blast", "Frost Nova", "Blizzard", "Polymorph", "Evocation", "Arcane Intellect", "Ice Barrier", "Cone of Cold", "Flamestrike", "Counterspell", "Blink"},
		"Warlock": {"Shadow Bolt", "Corruption", "Curse of Agony", "Immolate", "Drain Life", "Drain Soul", "Life Tap", "Searing Pain", "Soul Fire", "Shadowburn", "Conflagrate", "Curse of Doom", "Curse of the Elements", "Death Coil", "Siphon Life"},
		"Priest":  {"Mind Blast", "Mind Flay", "Shadow Word: Pain", "Smite", "Holy Fire", "Flash Heal", "Greater Heal", "Power Word: Shield", "Renew", "Vampiric Embrace", "Prayer of Healing", "Power Word: Fortitude", "Devouring Plague", "Shadowform", "Inner Fire"},
		"Druid":   {"Moonfire", "Starfire", "Wrath", "Rejuvenation", "Regrowth", "Healing Touch", "Shred", "Rake", "Rip", "Claw", "Maul", "Swipe", "Ferocious Bite", "Faerie Fire", "Insect Swarm", "Mark of the Wild", "Innervate", "Hurricane", "Tiger's Fury"},
		"Shaman":  {"Lightning Bolt", "Chain Lightning", "Earth Shock", "Flame Shock", "Frost Shock", "Stormstrike", "Windfury Weapon", "Healing Wave", "Lesser Healing Wave", "Chain Heal", "Windfury Totem", "Rockbiter Weapon", "Flametongue Weapon", "Lightning Shield"},
		"Paladin": {"Judgement", "Consecration", "Holy Light", "Flash of Light", "Holy Shock", "Hammer of Justice", "Seal of Righteousness", "Seal of Command", "Seal of the Crusader", "Blessing of Might", "Blessing of Kings", "Exorcism", "Holy Wrath", "Divine Shield", "Lay on Hands", "Hammer of Wrath"},
		"Warrior": {"Heroic Strike", "Bloodthirst", "Mortal Strike", "Whirlwind", "Execute", "Overpower", "Rend", "Slam", "Cleave", "Shield Slam", "Sunder Armor", "Battle Shout", "Bloodrage", "Berserker Rage", "Thunder Clap", "Revenge", "Hamstring", "Death Wish", "Recklessness"},
		"Rogue":   {"Sinister Strike", "Backstab", "Eviscerate", "Slice and Dice", "Rupture", "Ambush", "Hemorrhage", "Kidney Shot", "Cheap Shot", "Blade Flurry", "Adrenaline Rush", "Vanish", "Sprint", "Ghostly Strike", "Kick", "Gouge", "Garrote"},
		"Hunter":  {"Aimed Shot", "Multi-Shot", "Arcane Shot", "Serpent Sting", "Raptor Strike", "Auto Shot", "Steady Shot", "Hunter's Mark", "Rapid Fire", "Wing Clip", "Mongoose Bite", "Aspect of the Hawk", "Feign Death", "Concussive Shot", "Scatter Shot", "Explosive Trap"},
	}
	for class, spells := range sig {
		for _, s := range spells {
			classBySpell[s] = class
		}
	}
}

func isCast(name string) bool {
	return name == "SPELL_CAST_START" || name == "SPELL_CAST_SUCCESS"
}
