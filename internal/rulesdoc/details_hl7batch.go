package rulesdoc

// hl7BatchDetails covers EL6001-EL6006.
func hl7BatchDetails() map[string]ruleDetail {
	return map[string]ruleDetail{
		"EL6001": {
			catches:  "An FHS or BHS header that is never closed by its FTS or BTS trailer. Batch-aware readers buffer until the trailer arrives, so an unclosed batch either stalls the reader or is discarded whole once the stream ends.",
			fix:      "Resend the file. A missing trailer means the sending system did not finish writing the batch, so the messages already in it may not be the full set.",
			suppress: "There is no safe case: an incomplete batch envelope means the receiver cannot tell whether it has every message.",
			example:  hl7Example(fhs, bhs, msh, pid, "FTS|1"),
		},
		"EL6002": {
			catches:  "An FTS or BTS trailer with no matching header. Two files concatenated, or a batch split at the wrong boundary, both produce a trailer that closes nothing.",
			fix:      "Rejoin the file with its header, or split it so that each batch keeps its own header and trailer together.",
			suppress: "There is no safe case.",
			example:  hl7Example(msh, pid, "BTS|1"),
		},
		"EL6003": {
			catches:  "A BTS-1 that disagrees with the number of MSH messages in the batch. An empty BTS-1 is not checked, because the field is optional; a populated one is a promise the receiver reconciles against.",
			fix:      "Recount the MSH segments in the batch and write that number into BTS-1, after confirming no message was dropped. `edilint fix --write` rewrites BTS-1 to the recounted value.",
			suppress: "There is no safe case for a populated count. Leave BTS-1 empty instead if the sending system cannot produce a reliable one.",
			example:  hl7Example(fhs, bhs, msh, pid, "BTS|4", "FTS|1"),
		},
		"EL6004": {
			catches:  "An FTS-1 that disagrees with the number of BHS batches in the file. As with the message count, an empty FTS-1 is not checked.",
			fix:      "Recount the BHS segments and write that number into FTS-1. `edilint fix --write` rewrites it to the recounted value.",
			suppress: "There is no safe case for a populated count.",
			example:  hl7Example(fhs, bhs, msh, pid, "BTS|1", "FTS|3"),
		},
		"EL6005": {
			catches:  "Headers within one file that declare different field separators or different encoding characters, and headers whose encoding-characters field is malformed. Split-and-merge tooling parses every message in a file with the first header's separators, so a message that declares its own is read with the wrong ones and its fields come apart.",
			fix:      "Emit every FHS, BHS and MSH in a file with the same separator and encoding characters. A single message declaring something different usually means it was merged in from another source without being re-encoded.",
			suppress: "There is no safe case. The whole point of the declaration is that one file parses one way.",
			example:  hl7Example(fhs, bhs, `MSH|!~\&|NORTHGATEHEALTH|NGCLINIC|VALEMEDGROUP|VMOFFICE|20260115143000||ADT^A08|NGMSG0001|P|2.5.1`, pid, "BTS|1", "FTS|1"),
		},
		"EL6006": {
			catches:  "An MSH outside any open batch in a file that uses batch envelopes. Batch-aware readers process what is inside the envelope, so a stray message is silently skipped. Files with no BHS at all are not checked — a bare message stream is valid without an envelope.",
			fix:      "Move the message inside the batch it belongs to and update BTS-1, or give it its own BHS and BTS. Which applies depends on whether the message was meant to be part of that batch.",
			suppress: "When a partner's reader is known to process messages outside the envelope as well. It is a warning for that reason.",
			example:  hl7Example(fhs, bhs, msh, pid, "BTS|1", msh, pid, "FTS|1"),
		},
	}
}
