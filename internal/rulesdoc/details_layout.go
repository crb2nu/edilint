package rulesdoc

// layoutDetails covers EL5001 and EL5002.
func layoutDetails() map[string]ruleDetail {
	return map[string]ruleDetail{
		"EL5001": {
			catches:  "A fixed-width record whose length is not the sum of the layout's field widths. Every field after the point where the length went wrong is read from the wrong offset, so one short record misparses entirely rather than partially.",
			fix:      "Pad or trim the record to the declared length, after checking that the layout matches the version of the file you were sent. A whole file off by the same amount means the layout is stale, not the data.",
			suppress: "When the file carries several record types of different lengths and the layout describes only one of them. Lint those files per record type rather than suppressing the rule.",
			example:  fixedExample(lines("DTLNGH900000001RIVERA          000014400020260116", "DTLNGH900000200FAIRWEATHER     00000512002026011")),
		},
		"EL5002": {
			catches:  "A field padded on the side opposite the one the layout declares — a right-padded identifier arriving left-padded, for example. The rule only fires when the padding side is unambiguous, so a field that is full, or padded on both sides, is not reported.",
			fix:      "Pad the field on the declared side. Left-padded numerics and right-padded alphanumerics are the usual convention; check which the receiver's layout specifies before changing the generator.",
			suppress: "When the receiver is known to trim the field on both sides, which makes the padding side cosmetic. It is a warning for that reason.",
			example:  fixedExample(lines("DTLNGH900000001RIVERA          000014400020260116", "DTLNGH900000042         OKONKWO000028107520260116")),
		},
	}
}
