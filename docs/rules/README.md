# Rule reference

This directory is generated from the edilint rule catalog. Run `make rulesdoc` after changing rule metadata.

| ID | Name | Class | Severity | Rationale |
|---|---|---|---|---|
| [EL1001](EL1001.md) | `charset.bom` | charset | error | File starts with a byte order mark. An error for X12, HL7v2 and fixed-width, where a BOM before ISA or MSH shifts every fixed position in the file; a warning for delimited, because spreadsheet exports emit one routinely and most CSV readers cope. |
| [EL1002](EL1002.md) | `charset.invalid-utf8` | charset | error | Byte sequence is not valid UTF-8. |
| [EL1003](EL1003.md) | `charset.nonprintable` | charset | error | Control character in record content that is not a declared separator. Tabs are reported as warnings. |
| [EL1004](EL1004.md) | `charset.zero-width` | charset | error | Zero-width or bidirectional formatting character that renders as nothing but occupies bytes. |
| [EL1005](EL1005.md) | `charset.homoglyph` | charset | error | Unicode character that is visually identical to an ASCII one, such as Cyrillic А for A. |
| [EL1006](EL1006.md) | `charset.nonascii` | charset | warning | Non-ASCII character that is not a known lookalike. |
| [EL1007](EL1007.md) | `charset.x12-basic` | charset | warning | Character outside the X12 basic character set but inside the extended set. Off by default; the default profile is extended. |
| [EL1008](EL1008.md) | `charset.x12-extended` | charset | error | Character outside the X12 extended character set, which in printable ASCII means the caret or the backtick. A caret is exempt when ISA11 declares it as the repetition separator. |
| [EL2001](EL2001.md) | `terminator.mixed` | terminator | error | File mixes CRLF, LF and CR line endings. |
| [EL2002](EL2002.md) | `terminator.missing-final` | terminator | warning | Last record has no terminator. |
| [EL2003](EL2003.md) | `terminator.x12-segment` | terminator | error | Segment is not closed by the segment terminator the ISA declared. |
| [EL2004](EL2004.md) | `terminator.x12-padding` | terminator | warning | Whitespace between segment terminators is applied inconsistently. |
| [EL2005](EL2005.md) | `terminator.x12-separator` | terminator | error | Declared separators collide with each other or are alphanumeric. |
| [EL2006](EL2006.md) | `terminator.edifact-segment` | terminator | error | Segment is not closed by the segment terminator in force, which in practice means the file was truncated mid-segment. |
| [EL3001](EL3001.md) | `envelope.isa-length` | envelope | error | ISA segment is not the fixed 106 characters, or is absent. |
| [EL3002](EL3002.md) | `envelope.nesting` | envelope | error | GS appears outside an ISA, or ST outside a GS. |
| [EL3003](EL3003.md) | `envelope.unclosed` | envelope | error | ISA, GS or ST has no matching IEA, GE or SE. |
| [EL3004](EL3004.md) | `envelope.unopened` | envelope | error | IEA, GE or SE appears without its header. |
| [EL3005](EL3005.md) | `envelope.control-number` | envelope | error | Header and trailer control numbers differ (ISA13/IEA02, GS06/GE02, ST02/SE02). A leading-zero-only difference is reported as a warning. |
| [EL3006](EL3006.md) | `envelope.segment-count` | envelope | error | SE01 does not match the recounted segments from ST through SE inclusive. |
| [EL3007](EL3007.md) | `envelope.group-count` | envelope | error | GE01 does not match the recounted transaction sets in the group. |
| [EL3008](EL3008.md) | `envelope.interchange-count` | envelope | error | IEA01 does not match the recounted functional groups in the interchange. |
| [EL3009](EL3009.md) | `envelope.duplicate-control-number` | envelope | error | Duplicate ISA13 within the file or across the files in one run, duplicate GS06 within an interchange, or duplicate ST02 within a functional group. |
| [EL3010](EL3010.md) | `envelope.datetime` | envelope | error | ISA09/GS04 dates or ISA10/GS05 times are not valid YYMMDD, CCYYMMDD or HHMM[SS[DD]] values. |
| [EL3011](EL3011.md) | `envelope.missing-control-id` | envelope | error | ISA13 interchange control number is empty. |
| [EL3012](EL3012.md) | `envelope.trailing-data` | envelope | error | Segments appear outside any interchange. |
| [EL4001](EL4001.md) | `counts.mismatch` | counts | error | A declared record count does not match the recounted total. |
| [EL4002](EL4002.md) | `counts.unparsable` | counts | error | The field a count rule points at is not an integer. |
| [EL4003](EL4003.md) | `counts.missing-field` | counts | error | The declaring record has fewer fields than the count rule reads. |
| [EL4004](EL4004.md) | `counts.no-declaring-record` | counts | warning | No record matched the count rule's declaring prefix, so nothing was verified. |
| [EL4101](EL4101.md) | `fields.count-outlier` | fields | error | A record carries a different number of fields from others of the same record type. |
| [EL5001](EL5001.md) | `layout.length` | layout | error | Record length does not match the sum of the layout's field widths. |
| [EL5002](EL5002.md) | `layout.padding` | layout | warning | A field's padding is unambiguously on the side opposite the one the layout declares. |
| [EL6001](EL6001.md) | `hl7batch.unclosed` | hl7batch | error | FHS or BHS is never closed by a matching FTS or BTS, so the batch envelope is incomplete. |
| [EL6002](EL6002.md) | `hl7batch.unopened` | hl7batch | error | FTS or BTS appears without its matching FHS or BHS header. |
| [EL6003](EL6003.md) | `hl7batch.message-count` | hl7batch | error | BTS-1 does not match the recounted MSH messages in the batch. An empty BTS-1 is not checked; the field is optional. |
| [EL6004](EL6004.md) | `hl7batch.batch-count` | hl7batch | error | FTS-1 does not match the recounted BHS batches in the file. An empty FTS-1 is not checked; the field is optional. |
| [EL6005](EL6005.md) | `hl7batch.separator` | hl7batch | error | FHS, BHS and MSH headers disagree on the field separator or the encoding characters, or a header's encoding characters are malformed. Split-and-merge tooling reads every message with the first header's separators, so a disagreeing message is misparsed. |
| [EL6006](EL6006.md) | `hl7batch.stray-message` | hl7batch | warning | MSH appears outside any open batch in a file that uses batch envelopes, so batch-aware readers will not process it. Files with no BHS at all are not checked; bare message streams are valid without an envelope. |
| [EL7001](EL7001.md) | `edifact.unclosed` | edifact | error | UNB, UNG or UNH is never closed by a matching UNZ, UNE or UNT. |
| [EL7002](EL7002.md) | `edifact.unopened` | edifact | error | UNZ, UNE or UNT appears without its matching header. |
| [EL7003](EL7003.md) | `edifact.segment-count` | edifact | error | UNT-1 does not match the recounted segments from UNH through UNT inclusive. |
| [EL7004](EL7004.md) | `edifact.group-count` | edifact | error | UNE-1 does not match the recounted messages in the functional group. |
| [EL7005](EL7005.md) | `edifact.interchange-count` | edifact | error | UNZ-1 does not match the recounted messages in the interchange, or the recounted functional groups when UNG groups are used. |
| [EL7006](EL7006.md) | `edifact.control-reference` | edifact | error | Header and trailer control references differ (UNB-5/UNZ-2, UNG-5/UNE-2, UNH-1/UNT-2), or a header's reference is empty. |
| [EL7007](EL7007.md) | `edifact.service-string` | edifact | error | UNA is not the fixed nine characters, its service characters collide or are alphanumeric, or its decimal mark is not a period or a comma. |
| [EL7008](EL7008.md) | `edifact.nesting` | edifact | error | UNG or UNH appears outside the envelope that must enclose it, or no UNB is present in a file forced to the edifact format. |
| [EL7009](EL7009.md) | `edifact.trailing-data` | edifact | error | Segments appear outside any interchange; data after UNZ is not part of a valid EDIFACT file. |
