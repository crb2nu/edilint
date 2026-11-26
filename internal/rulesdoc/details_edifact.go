package rulesdoc

// edifactDetails covers EL7001-EL7010.
func edifactDetails() map[string]ruleDetail {
	return map[string]ruleDetail{
		"EL7001": {
			catches:  "A UNB, UNG or UNH that is never closed by its UNZ, UNE or UNT. A truncated transfer is the usual cause, and because EDIFACT is not line-oriented the truncation can fall anywhere.",
			fix:      "Resend the file. A missing trailer means content is absent, so writing the trailer would ship an interchange that does not match what the sender assembled.",
			suppress: "There is no safe case; the interchange is incomplete.",
			example:  edifactExample(una, unb, unh, bgm),
		},
		"EL7002": {
			catches:  "A UNZ, UNE or UNT arriving with no matching header open. Concatenated interchanges and mis-split files both produce a trailer that closes nothing.",
			fix:      "Split or rejoin the file so each trailer follows its own header.",
			suppress: "There is no safe case.",
			example:  edifactExample(una, unb, unh, bgm, unt, "UNT+3+NG0001'", unz),
		},
		"EL7003": {
			catches:  "A UNT-1 that disagrees with the segments actually present from UNH through UNT, counting both. It is EDIFACT's per-message checksum and the first place a dropped segment shows up.",
			fix:      "After confirming no segment was lost, run `edilint fix --write` to recount UNT-1 from UNH through UNT inclusive. Count edits are withheld if service characters, nesting, trailing data, or truncation make the file ambiguous.",
			suppress: "There is no safe case.",
			example:  edifactExample(una, unb, unh, bgm, "UNT+9+NG0001'", unz),
		},
		"EL7004": {
			catches:  "A UNE-1 that disagrees with the number of UNH messages in the functional group. Groups are optional in EDIFACT, so this only applies to interchanges that use UNG.",
			fix:      "After confirming all intended messages are present, run `edilint fix --write` to recount UNE-1. Count edits require complete, consistently nested envelopes and valid service characters.",
			suppress: "There is no safe case.",
			example: edifactExample(una, unb, "UNG+ORDERS+NORTHGATEHEALTH+VALEMEDGROUP+260115:1430+1+UN+D:96A'",
				unh, bgm, unt, "UNE+4+1'", "UNZ+1+NG260002'"),
		},
		"EL7005": {
			catches:  "A UNZ-1 that disagrees with the interchange's contents: the number of messages when no UNG groups are used, or the number of functional groups when they are.",
			fix:      "After confirming all intended content is present, run `edilint fix --write` to recount UNZ-1: messages without groups, otherwise groups. Ambiguous structure, including mixed grouped and ungrouped messages in one interchange, prevents count edits.",
			suppress: "There is no safe case.",
			example:  edifactExample(una, unb, unh, bgm, unt, "UNZ+5+NG260002'"),
		},
		"EL7006": {
			catches:  "A control reference that differs between a header and its trailer — UNB-5 against UNZ-2, UNG-5 against UNE-2, UNH-1 against UNT-2 — or a header whose reference is empty. The pair is how a receiver matches a trailer to its header and how both sides trace the interchange afterwards.",
			fix:      "Copy the header's reference into the trailer, taking the value from whichever the sending system considers authoritative.",
			suppress: "There is no safe case.",
			example:  edifactExample(una, unb, unh, bgm, unt, "UNZ+1+NG260099'"),
		},
		"EL7007": {
			catches:  "A UNA that is not the fixed nine characters, declares service characters that collide with each other, or names a decimal mark that is neither a period nor a comma. UNA declares the characters the rest of the interchange is parsed with, so a malformed one makes every later segment boundary uncertain.",
			fix:      "Emit the conventional service string advice, `UNA:+.? '`, unless the partner has specified different characters — in which case emit theirs, in the same fixed nine-character form.",
			suppress: "There is no safe case.",
			example:  edifactExample("UNA:+/? '", unb, unh, bgm, unt, unz),
		},
		"EL7008": {
			catches:  "A UNG or UNH outside the envelope that must enclose it, an interchange that mixes UNG functional groups with messages outside any group, and a file forced to the edifact format with no UNB at all. As in X12, the nesting is what tells a receiver which interchange a message belongs to. An interchange carries either messages or functional groups at its top level, not both; the first record that mixes them is reported once per interchange.",
			fix:      "Wrap the messages in a UNB and UNZ. A file that starts at UNH is usually a payload that lost its envelope in an intermediate step. In a mixed interchange, move the ungrouped messages into a UNG..UNE group, or split them into their own interchange.",
			suppress: "There is no safe case.",
			example: example{
				file: "order.edi",
				body: lines(unh, bgm, unt),
				args: "--format edifact",
				opts: edifactFormatOpts,
			},
		},
		"EL7009": {
			catches:  "Segments outside any interchange — before UNB or after UNZ. Data after UNZ is not part of a valid EDIFACT file, and it usually arrives from a redirect that appended a second file or a log line.",
			fix:      "Remove whatever follows UNZ, or give it its own UNB and UNZ if it is real content.",
			suppress: "There is no safe case.",
			example:  edifactExample(una, unb, unh, bgm, unt, unz, "FTX+AAI+++THANK YOU'"),
		},
		"EL7010": {
			catches:  "A control reference repeated within its scope: a UNB-5 interchange reference used twice in one file, a UNG-5 group reference used twice in one interchange, or a UNH-1 message reference used twice in one functional group, or in one interchange when it has no groups. Receivers use these references to detect resent data, so a repeat is rejected as a duplicate (CONTRL code 26) or silently dropped. The finding is on the repeating header and names the record that first used the reference.",
			fix:      "Have the sending system assign a fresh reference to every interchange, group and message, and keep the trailer reference matched to its header.",
			suppress: "A sender that restarts UNH-1 at 1 in every interchange or group is valid and is not reported, because the scope ends at the enclosing interchange or group. Within one scope there is no safe case.",
			example:  edifactExample(una, unb, unh, bgm, unt, unh, "BGM+220+NG77002+9'", unt, "UNZ+2+NG260002'"),
		},
	}
}
