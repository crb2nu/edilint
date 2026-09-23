package rulesdoc

// envelopeDetails covers EL3001-EL3012.
func envelopeDetails() map[string]ruleDetail {
	return map[string]ruleDetail{
		"EL3001": {
			catches:  "An ISA that is not exactly 106 characters, or a file forced to the x12 format with no ISA at all. ISA is the one X12 segment with fixed-width elements, and every element after a short or long one is read from the wrong offset — including ISA16, which declares the component separator the rest of the file is parsed with.",
			fix:      "Regenerate the ISA from the envelope data rather than editing it. Each element has a mandatory width, so padding one field by hand tends to move the error rather than clear it.",
			suppress: "There is no safe case. A malformed ISA means every field the receiver reads by offset is suspect.",
			example:  x12Example("ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP*260115*1430*^*00501*000000001*0*P*:~", gs, st, bpr, se, ge, iea),
		},
		"EL3002": {
			catches:  "An envelope segment outside the envelope that must enclose it: GS with no open ISA, or ST with no open GS. The nesting is what tells a receiver which interchange and which functional group a transaction set belongs to, so a misplaced header leaves the transaction unaddressed.",
			fix:      "Rebuild the envelope around the payload. A GS before its ISA is usually a concatenation accident — two files appended without their envelopes being merged.",
			suppress: "There is no safe case; the nesting is part of the X12 structure rather than a convention.",
			example:  x12Example(isa, st, bpr, se, iea),
		},
		"EL3003": {
			catches:  "A header with no matching trailer: ISA without IEA, GS without GE, ST without SE. A truncated transfer produces this, and so does a generator that fails partway through writing a transaction set and still ships what it wrote.",
			fix:      "Resend the file. A missing trailer means the transaction set is incomplete, and writing the trailer by hand ships a file whose contents do not match what the sender intended.",
			suppress: "There is no safe case; the finding says the interchange is unfinished.",
			example:  x12Example(isa, gs, st, bpr),
		},
		"EL3004": {
			catches:  "A trailer with no matching header: IEA, GE or SE arriving while nothing of that kind is open. The usual cause is two interchanges concatenated so that one file's trailer follows the other's, or a split that cut between a header and its trailer.",
			fix:      "Split or rejoin the file so every trailer follows its own header. If the file was produced by concatenating interchanges, concatenate the payloads and write one envelope instead.",
			suppress: "There is no safe case.",
			example:  x12Example(isa, gs, st, bpr, se, "SE*3*0001~", ge, iea),
		},
		"EL3005": {
			catches:  "A control number that differs between a header and its trailer: ISA13 against IEA02, GS06 against GE02, ST02 against SE02. The pair is how a receiver matches a trailer to its header when interchanges are being reassembled. A difference of leading zeros alone is reported as a warning, because the values compare equal numerically and many partners accept it.",
			fix:      "Copy the header's control number into the trailer. Which of the two is correct depends on what the sender recorded, so take the value from whichever the sending system considers authoritative.",
			suppress: "When a partner is known to zero-pad trailers differently and the difference is leading zeros only. Downgrade that case rather than disabling the rule, so a genuine mismatch still reports.",
			example:  x12Example(isa, gs, st, bpr, se, ge, "IEA*1*000000002~"),
		},
		"EL3006": {
			catches:  "An SE01 that disagrees with the segments actually present between ST and SE, counting both. Receivers use it as a checksum on the transaction set, so a mismatch is often the first evidence that segments were dropped or duplicated in transit.",
			fix:      "Recount the segments from ST through SE inclusive and write that number into SE01, but check first that the segments themselves are all present — a wrong count can be the symptom rather than the defect. `edilint fix --write` rewrites SE01 to the recounted value.",
			suppress: "There is no safe case; the count exists precisely to be checked.",
			example:  x12Example(isa, gs, st, bpr, "SE*9*0001~", ge, iea),
		},
		"EL3007": {
			catches:  "A GE01 that disagrees with the number of ST segments in the functional group. It usually means a transaction set was filtered out downstream of whatever wrote the group trailer.",
			fix:      "Recount the ST segments in the group and write that number into GE01. `edilint fix --write` rewrites it to the recounted value.",
			suppress: "There is no safe case.",
			example:  x12Example(isa, gs, st, bpr, se, "GE*4*1~", iea),
		},
		"EL3008": {
			catches:  "An IEA01 that disagrees with the number of GS segments in the interchange. As with the group count, it usually means a group was added or removed after the trailer was written.",
			fix:      "Recount the functional groups and write that number into IEA01. `edilint fix --write` rewrites it to the recounted value.",
			suppress: "There is no safe case.",
			example:  x12Example(isa, gs, st, bpr, se, ge, "IEA*3*000000001~"),
		},
		"EL3009": {
			catches:  "A control number reused where it must be unique: ISA13 within a file or across the files of one run, GS06 within an interchange, ST02 within a functional group. Receivers deduplicate on these, so a reused number is how a real claim silently fails to post.",
			fix:      "Assign each interchange, group and transaction set its own control number from the sender's sequence. A repeated ISA13 across a run usually means a counter was not persisted between invocations.",
			suppress: "There is no safe case. A duplicate is either a resend the receiver will discard or a distinct transaction the receiver will discard, and both are wrong.",
			example:  x12Example(isa, gs, st, bpr, se, "ST*835*0001~", bpr, "SE*3*0001~", "GE*2*1~", iea),
		},
		"EL3010": {
			catches:  "An envelope date or time that is not a valid value in its declared form: ISA09 and GS04 as YYMMDD or CCYYMMDD, ISA10 and GS05 as HHMM with optional seconds and hundredths. Impossible months and hours, and times one leading zero short, are the common shapes.",
			fix:      "Write the date and time from a formatted timestamp rather than by string arithmetic. `edilint fix --write` restores a dropped leading zero in a time, which is the one case where the intended value is unambiguous.",
			suppress: "There is no safe case; the value is either a valid timestamp or it is not.",
			example:  x12Example("ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *261315*1430*^*00501*000000001*0*P*:~", gs, st, bpr, se, ge, iea),
		},
		"EL3011": {
			catches:  "An ISA13 that is blank. The interchange control number is what a receiver acknowledges and what both sides use to trace a file after the fact; an empty one leaves the interchange unidentifiable.",
			fix:      "Populate ISA13 from the sender's interchange counter and copy the same value into IEA02. The element is fixed-width, so the value is normally zero-padded to nine characters.",
			suppress: "There is no safe case.",
			example:  x12Example("ISA*00*          *00*          *ZZ*NORTHGATEHEALTH*ZZ*VALEMEDGROUP   *260115*1430*^*00501*         *0*P*:~", gs, st, bpr, se, ge, "IEA*1*         ~"),
		},
		"EL3012": {
			catches:  "Segments sitting outside any interchange — before the first ISA or after the last IEA. Trailing data is usually a log line, a shell banner or a second file's fragment that a redirect appended to the interchange.",
			fix:      "Remove whatever follows IEA. If the extra segments are real content, they belong in their own interchange with their own envelope.",
			suppress: "There is no safe case; a receiver will either reject the file or ignore the content, and neither is what the sender intended.",
			example:  x12Example(isa, gs, st, bpr, se, ge, iea, "REF*BM*NG77001~"),
		},
	}
}
