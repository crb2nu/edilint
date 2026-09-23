package rulesdoc

// countDetails covers EL4001-EL4004 and EL4101.
func countDetails() map[string]ruleDetail {
	return map[string]ruleDetail{
		"EL4001": {
			catches:  "A trailer that declares a record count the file does not contain. The check is driven by `--count-rule recordPrefix:fieldIndex:countedPrefix`, so it applies to any line-oriented extract with a self-describing trailer, not only to a particular partner's layout.",
			fix:      "Find out which side is wrong before editing either. A count one short of the records present usually means a record was appended after the trailer was written; a count too high usually means a record was filtered out afterwards.",
			suppress: "When the declared field counts something other than what the rule points at — a total of dollars rather than of records, say. Correct the count rule rather than suppressing the finding.",
			example:  psvExample(psvHDR, psvDTL1, psvDTL2, "TRL|4"),
		},
		"EL4002": {
			catches:  "A count field that is not an integer: a decimal total, a label, or a field that has drifted one position because the record gained a column. The rule reports the value it read, which is usually enough to see which of those happened.",
			fix:      "If the field holds a number in another form, point the count rule at the field that holds the record count. If the record gained a column, update the field index everywhere the layout is configured.",
			suppress: "When the extract genuinely has no integer record count. Drop the count rule for that file rather than suppressing the rule.",
			example:  psvExample(psvHDR, psvDTL1, psvDTL2, "TRL|two"),
		},
		"EL4003": {
			catches:  "A declaring record with fewer fields than the count rule reads — the trailer exists but stops before the field index the rule points at. A trailer written without its optional fields produces this, as does a count rule configured against a newer layout than the file.",
			fix:      "Emit the trailer with all of its fields, or point the count rule at a field the trailer actually carries.",
			suppress: "When the trailer's count field is genuinely optional and its absence is not an error for that partner. Removing the count rule for that file says the same thing more clearly.",
			example:  psvExample(psvHDR, psvDTL1, psvDTL2, "TRL"),
		},
		"EL4004": {
			catches:  "A count rule that matched nothing: no record in the file began with the declaring prefix, so no count was verified. The finding exists so that a configured check cannot quietly stop running — without it, a missing trailer looks the same as a clean file.",
			fix:      "Confirm whether the trailer should be there. If the extract legitimately has no trailer, remove the count rule; if it should have one, the missing trailer is the defect the rule just found.",
			suppress: "When one count rule covers a set of files and only some of them carry that trailer. It is a warning rather than an error for exactly that case.",
			example:  psvExample(psvHDR, psvDTL1, psvDTL2),
		},
		"EL4101": {
			catches:  "A record carrying a different number of fields from the other records of its type. The record type is taken from the first field by default, or from `--type-field n`. An unescaped delimiter inside a free-text field is the usual cause, and it shifts every field after it.",
			fix:      "Quote or escape the delimiter inside the offending field, or choose a delimiter the data cannot contain. Counting the fields of the reported record against a correct one shows where the shift starts.",
			suppress: "When a record type legitimately has optional trailing fields and the extract omits them. Disabling `fields.count-outlier` for that file is the usual arrangement.",
			example: psvExample(psvHDR, psvDTL1,
				"DTL|NGH900000042|OKONKWO|TERESA|19770915|F|PPO|GOLD|20260101|20261231",
				"DTL|NGH900000108|BRENNAN|MICHAEL|19910204|M|HMO-CORE|20260201|20261231",
				"TRL|3"),
		},
	}
}
