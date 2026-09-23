package rulesdoc

// terminatorDetails covers EL2001-EL2006.
func terminatorDetails() map[string]ruleDetail {
	return map[string]ruleDetail{
		"EL2001": {
			catches:  "A file whose records do not all end the same way: CRLF on some lines, bare LF or bare CR on others. It is the signature of two generators appending to one stream, or of a file that crossed platforms through a transfer that translated only part of it.",
			fix:      "Rewrite the file with one line ending throughout, choosing the style the receiver expects. `edilint fix --write` normalizes to whichever style the file already uses for most of its records.",
			suppress: "There is no safe case. A reader that splits on one terminator will merge or truncate the records written with the other, so the inconsistency always changes what is parsed.",
			example: example{
				file:    "eligibility.psv",
				body:    psvHDR + "\r\n" + psvDTL1 + "\n" + psvDTL2 + "\r\n",
				display: psvHDR + `\r\n` + "\n" + psvDTL1 + `\n` + "\n" + psvDTL2 + `\r\n` + "\n",
			},
		},
		"EL2002": {
			catches:  "A final record with no terminator after it. Streaming readers that split on the terminator either drop the last record or hand it on as a partial one, so the row that goes missing is reliably the last — the hardest kind of loss to notice in a reconciliation.",
			fix:      "Append the file's terminator after the last record. `edilint fix --write` adds it, using the terminator the rest of the file uses.",
			suppress: "When the receiver is known to accept an unterminated final record, which some CSV readers do. It stays a warning rather than an error for that reason.",
			example: example{
				file: "eligibility.psv",
				body: psvHDR + "\n" + psvDTL1,
			},
		},
		"EL2003": {
			catches:  "An X12 segment that does not end with the segment terminator ISA declared in its final position. In a real interchange this means the file was cut short in transfer, or a generator wrote the last segment without flushing its terminator.",
			fix:      "Append the declared segment terminator to the segment. If several segments are affected, suspect a truncated transfer and resend the file rather than patching it. `edilint fix --write` closes a single unterminated final segment.",
			suppress: "There is no safe case: a segment without its terminator runs into the next one, so the parse is wrong from that point forward.",
			example: example{
				file: "example.x12",
				body: lines(isa, gs, st, bpr, se, ge) + "IEA*1*000000001",
			},
		},
		"EL2004": {
			catches:  "Whitespace between segment terminators applied to some segments but not others — a file that puts a line break after most segments and runs two of them together. Both forms are legal on their own; the mixture usually means two generators wrote into one stream.",
			fix:      "Rewrite the interchange with one convention throughout. `edilint fmt --write` produces the canonical one-segment-per-line layout, and `edilint fix --write` aligns the stragglers with the padding the file already prefers.",
			suppress: "When the receiver is indifferent to segment padding, which most are. It is a warning because it predicts a generator bug rather than a parse failure.",
			example:  x12Example(isa, gs, st+bpr, se, ge, iea),
		},
		"EL2005": {
			catches:  "An ISA whose declared separators cannot be told apart: an element separator equal to the component separator or the repetition separator, or a separator that is alphanumeric and so indistinguishable from data. Every downstream element boundary depends on these four characters being distinct.",
			fix:      "Regenerate the envelope with distinct, non-alphanumeric separators. The conventional 005010 set is `*` for elements, `:` for components, `^` for repetition and `~` for segments.",
			suppress: "There is no safe case. A colliding separator makes the element boundaries ambiguous, so two conforming parsers can read the same segment differently.",
			example: x12Example(
				"ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *260115*1430*^*00501*000000001*0*P**~",
				gs, st, bpr, se, ge, iea),
		},
		"EL2006": {
			catches:  "An EDIFACT segment that is not closed by the segment terminator in force — the apostrophe by default, or whatever UNA declared. Because EDIFACT segments are not line-oriented, an unterminated segment almost always means the file was truncated in transfer.",
			fix:      "Resend the file. A missing terminator at the end of an EDIFACT interchange means bytes are absent, and appending the terminator would hide the truncation rather than repair it.",
			suppress: "There is no safe case; the finding is evidence that the file is incomplete.",
			example: example{
				file: "order.edi",
				body: lines(una, unb, unh, bgm, unt) + "UNZ+1+NG260002",
			},
		},
	}
}
