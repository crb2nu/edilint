package rulesdoc

// charsetDetails covers EL1001-EL1008.
func charsetDetails() map[string]ruleDetail {
	return map[string]ruleDetail{
		"EL1001": {
			catches:  "The first bytes of the file, before format detection runs. UTF-8, UTF-16 and UTF-32 marks are all recognized, and the mark is reported once for the file rather than once per record. A mark ahead of ISA or MSH shifts every byte offset in the interchange, so a partner reading by position reads the wrong field.",
			fix:      "Re-save the file as UTF-8 without a byte order mark. Editors usually offer that encoding separately from \"UTF-8 with BOM\"; in a pipeline, drop a leading EF BB BF.",
			suppress: "Only for a delimited extract whose reader is known to strip the mark. Leave it on for X12, HL7v2 and fixed-width, where the shifted offsets are a real parse failure rather than a cosmetic one.",
			example: example{
				file:    "example.x12",
				body:    "\ufeff" + lines(isa, gs, st, bpr, se, ge, iea),
				display: `\ufeff` + lines(isa, gs, st, bpr, se, ge, iea),
			},
		},
		"EL1002": {
			catches:  "Any byte sequence that is not well-formed UTF-8, which in practice means a file encoded in a single-byte code page — Latin-1 or CP1252 — that was passed on as if it were UTF-8. When the input is dense enough in invalid bytes and NULs not to be text at all, the rule reports once and the interchange checks are skipped.",
			fix:      "Transcode the file from its real encoding rather than editing the offending bytes. `iconv -f windows-1252 -t utf-8` covers the usual case; fix the generator afterwards so the next file is emitted as UTF-8.",
			suppress: "There is no safe case. A file that is not valid UTF-8 cannot be read consistently by two different parsers, so the finding always describes a genuine ambiguity.",
			example: example{
				file:    "eligibility.psv",
				body:    lines(psvHDR, "DTL|NGH900000001|RIVERA\xff|DANA|19840322|F|PPO-GOLD|20260101|20261231"),
				display: lines(psvHDR, `DTL|NGH900000001|RIVERA\xff|DANA|19840322|F|PPO-GOLD|20260101|20261231`),
			},
		},
		"EL1003": {
			catches:  "C0 and C1 control characters inside record content, excluding the separators and terminators the file has declared. These arrive from copy-and-paste out of a terminal or from a field that captured a form feed. Tabs are graded a warning because they are common in hand-maintained extracts and rarely break a reader.",
			fix:      "Strip the control character from the source record. If it is meaningful — a tab inside a free-text note, say — escape it the way the receiver's specification requires instead of passing the raw byte.",
			suppress: "When the receiver's specification allows a specific control character in a specific field and the sending system depends on it. Suppress by rule for the affected file, not globally.",
			example: example{
				file:    "eligibility.psv",
				body:    lines(psvHDR, "DTL|NGH900000001|RIVERA\x07|DANA|19840322|F|PPO-GOLD|20260101|20261231"),
				display: lines(psvHDR, `DTL|NGH900000001|RIVERA\x07|DANA|19840322|F|PPO-GOLD|20260101|20261231`),
			},
		},
		"EL1004": {
			catches:  "Zero-width spaces, zero-width joiners and the bidirectional formatting controls. They render as nothing, so a field looks correct in every editor while comparing unequal to the same field without them — the reason an identifier match fails on one side of an integration and not the other.",
			fix:      "Delete the character. Because it is invisible, delete it by code point rather than by eye: match the reported column, or filter the file for the code point the finding names.",
			suppress: "There is no routine case. A zero-width character is never load-bearing in an interchange field, so a finding here is a defect in whatever produced the file.",
			example: example{
				file:    "eligibility.psv",
				body:    lines(psvHDR, "DTL|NGH9000\u200b00001|RIVERA|DANA|19840322|F|PPO-GOLD|20260101|20261231"),
				display: lines(psvHDR, `DTL|NGH9000\u200b00001|RIVERA|DANA|19840322|F|PPO-GOLD|20260101|20261231`),
			},
		},
		"EL1005": {
			catches:  "Unicode characters that render identically to an ASCII one: Cyrillic А for A, Greek Ο for O, a full-width digit for its ASCII form. They come from data typed in a localized editor or pasted out of a document, and they make an identifier compare unequal while looking right in every report.",
			fix:      "Replace the character with its ASCII lookalike. `edilint fix --unsafe` does the substitution, and because it rewrites content bytes on a visual judgment it is in the unsafe tier and always shows a diff.",
			suppress: "When the field legitimately carries non-Latin text — a patient name in Cyrillic, for instance — and the receiver accepts it. Restrict the suppression to that file; a homoglyph in a control number is always a defect.",
			example: example{
				file:    "example.x12",
				body:    lines(isa, gs, st, "N1*PR*NORTHG\u0410TE HEALTH~", "SE*3*0001~", ge, iea),
				display: lines(isa, gs, st, `N1*PR*NORTHG\u0410TE HEALTH~`, "SE*3*0001~", ge, iea),
			},
		},
		"EL1006": {
			catches:  "Non-ASCII characters that are not known lookalikes: accented letters, currency symbols, typographic quotes and dashes. These are legal in many interchanges and illegal in others, which is why the rule is a warning — it exists so the character is a decision rather than a surprise at the receiver.",
			fix:      "Transliterate the character to ASCII when the receiver's character set is restricted, or confirm the receiver accepts it and leave it. Typographic quotes and en dashes pasted from a word processor should normally be replaced.",
			suppress: "When the trading partner's specification accepts the full character set the file uses. Disabling `charset.nonascii` for that partner's files is the usual arrangement.",
			example: example{
				file: "eligibility.psv",
				body: lines(psvHDR, "DTL|NGH900000001|RIVERA|DAN\u00c9|19840322|F|PPO-GOLD|20260101|20261231"),
			},
		},
		"EL1007": {
			catches:  "Characters legal in the X12 extended character set but outside the basic set: lower-case letters and the punctuation the extended set adds. The check is off by default because the default profile is extended; it runs under `--charset basic` for partners whose specification still names the basic set.",
			fix:      "Upper-case the affected content, or drop the punctuation the basic set omits. If the partner in fact accepts the extended set, run without `--charset basic` rather than editing the file.",
			suppress: "The rule only runs when it is asked for, so suppression is a matter of not passing `--charset basic`. Do that once the partner confirms the extended set.",
			example: example{
				file: "example.x12",
				body: lines(isa, gs, st, "N1*PR*Northgate Health~", "SE*3*0001~", ge, iea),
				args: "--charset basic",
				opts: basicCharsetOpts,
			},
		},
		"EL1008": {
			catches:  "Characters outside the X12 extended character set, which among printable ASCII means the caret and the backtick. A caret is exempt when ISA11 declares it as the repetition separator, so the rule does not fire on the 005010 envelopes that use it for exactly that.",
			fix:      "Remove the character from the content. A backtick is almost always a stray keystroke or a shell quoting accident in whatever generated the segment.",
			suppress: "When the partner has agreed to a character set wider than the X12 extended set. That is rare enough to be worth recording next to the suppression.",
			example:  x12Example(isa, gs, st, "REF*BM*NG`77001~", "SE*3*0001~", ge, iea),
		},
	}
}
