package edilint

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Rule identifiers.
//
// Every rule has a stable identifier of the form EL####. The leading digit is
// the check class, so an identifier says which part of the tool produced a
// finding before you look anything up:
//
//	EL1xxx  character set and character hygiene
//	EL2xxx  record and segment terminators
//	EL3xxx  X12 envelope structure
//	EL4xxx  declared record counts and field-count consistency
//	EL5xxx  fixed-width layouts
//	EL6xxx  HL7v2 batch structure
//	EL7xxx  EDIFACT envelope structure
//
// Identifiers are permanent. A withdrawn rule keeps its number rather than
// passing it to something else, so a suppression written today cannot silently
// come to mean something different later.

// classBlocks records the identifier block each check class draws from. A rule
// whose identifier falls outside its class block is a numbering mistake, which
// TestRuleIdentifiersAreWellFormed catches.
var classBlocks = map[string]string{
	ClassCharset:    "EL1",
	ClassTerminator: "EL2",
	ClassEnvelope:   "EL3",
	ClassCounts:     "EL4",
	ClassFields:     "EL4",
	ClassLayout:     "EL5",
	ClassHL7Batch:   "EL6",
	ClassEdifact:    "EL7",
}

// reservedBlocks are allocated to formats that are specified but not yet
// implemented. Nothing shipped may use them. EL6 (HL7v2 batch) and EL7
// (EDIFACT envelope) left this table when workstream A shipped them.
var reservedBlocks = map[string]string{}

// RuleDoc describes a rule for --list-rules and for the documentation table.
type RuleDoc struct {
	// ID is the stable identifier, e.g. "EL1005".
	ID string `json:"id"`
	// Name is the stable dotted name, e.g. "charset.homoglyph".
	Name string `json:"name"`
	// Class is the leading component of Name, e.g. "charset".
	Class string `json:"class"`
	// Severity is the severity the check normally assigns. A few rules grade
	// themselves by format, and a configuration file can override any of them,
	// so this is the documented default rather than a guarantee.
	Severity Severity `json:"severity"`
	// Formats names the inputs the rule applies to.
	Formats string `json:"formats"`
	// Summary is the one-line rationale printed by --list-rules.
	Summary string `json:"summary"`
	// Acks are the acknowledgment codes a trading partner returns for the
	// defect, for X12 rules. Nil for every other rule.
	Acks []Ack `json:"acknowledgments,omitempty"`
}

// ruleEntry is one row of the catalog table: the fields a rule is declared
// with, positionally, so that each rule fits on a few lines. Rules converts
// rows to RuleDoc and attaches what is derived, such as acknowledgments.
type ruleEntry struct {
	id       string
	name     string
	class    string
	severity Severity
	formats  string
	summary  string
}

func (e ruleEntry) doc() RuleDoc {
	return RuleDoc{ID: e.id, Name: e.name, Class: e.class, Severity: e.severity, Formats: e.formats, Summary: e.summary}
}

// Rules returns the catalog of implemented rules, ordered by identifier.
func Rules() []RuleDoc {
	rows := catalog()
	rules := make([]RuleDoc, 0, len(rows))
	for _, row := range rows {
		doc := row.doc()
		doc.Acks = RuleAcks(doc.ID)
		rules = append(rules, doc)
	}
	return rules
}

// catalog is the rule table itself.
func catalog() []ruleEntry {
	return []ruleEntry{
		{"EL1001", RuleBOM, ClassCharset, SeverityError, "all (warning for delimited)",
			"File starts with a byte order mark. An error for X12, HL7v2 and fixed-width, where a BOM " +
				"before ISA or MSH shifts every fixed position in the file; a warning for delimited, " +
				"because spreadsheet exports emit one routinely and most CSV readers cope."},
		{"EL1002", RuleInvalidUTF8, ClassCharset, SeverityError, "all",
			"Byte sequence is not valid UTF-8."},
		{"EL1003", RuleNonPrint, ClassCharset, SeverityError, "all",
			"Control character in record content that is not a declared separator. Tabs are reported as warnings."},
		{"EL1004", RuleZeroWidth, ClassCharset, SeverityError, "all",
			"Zero-width or bidirectional formatting character that renders as nothing but occupies bytes."},
		{"EL1005", RuleHomoglyph, ClassCharset, SeverityError, "all",
			"Unicode character that is visually identical to an ASCII one, such as Cyrillic А for A."},
		{"EL1006", RuleNonASCII, ClassCharset, SeverityWarning, "all",
			"Non-ASCII character that is not a known lookalike."},
		{"EL1007", RuleX12Basic, ClassCharset, SeverityWarning, "x12 (requires --charset basic)",
			"Character outside the X12 basic character set but inside the extended set. Off by " +
				"default; the default profile is extended."},
		{"EL1008", RuleX12Extended, ClassCharset, SeverityError, "x12",
			"Character outside the X12 extended character set, which in printable ASCII means the " +
				"caret or the backtick. A caret is exempt when ISA11 declares it as the repetition " +
				"separator."},

		{"EL2001", RuleMixedTerminator, ClassTerminator, SeverityError, "hl7v2, delimited, fixed, text",
			"File mixes CRLF, LF and CR line endings."},
		{"EL2002", RuleMissingFinal, ClassTerminator, SeverityWarning, "hl7v2, delimited, fixed, text",
			"Last record has no terminator."},
		{"EL2003", RuleX12Segment, ClassTerminator, SeverityError, "x12",
			"Segment is not closed by the segment terminator the ISA declared."},
		{"EL2004", RuleX12Padding, ClassTerminator, SeverityWarning, "x12",
			"Whitespace between segment terminators is applied inconsistently."},
		{"EL2005", RuleX12Separator, ClassTerminator, SeverityError, "x12",
			"Declared separators collide with each other or are alphanumeric."},
		{"EL2006", RuleEdifactSegment, ClassTerminator, SeverityError, "edifact",
			"Segment is not closed by the segment terminator in force, which in practice means " +
				"the file was truncated mid-segment."},

		{"EL3001", RuleISALength, ClassEnvelope, SeverityError, "x12",
			"ISA segment is not the fixed 106 characters, or is absent."},
		{"EL3002", RuleEnvelopeNesting, ClassEnvelope, SeverityError, "x12",
			"GS appears outside an ISA, or ST outside a GS."},
		{"EL3003", RuleUnclosed, ClassEnvelope, SeverityError, "x12",
			"ISA, GS or ST has no matching IEA, GE or SE."},
		{"EL3004", RuleUnopened, ClassEnvelope, SeverityError, "x12",
			"IEA, GE or SE appears without its header."},
		{"EL3005", RuleControlNumber, ClassEnvelope, SeverityError, "x12",
			"Header and trailer control numbers differ (ISA13/IEA02, GS06/GE02, ST02/SE02). " +
				"A leading-zero-only difference is reported as a warning."},
		{"EL3006", RuleSegmentCount, ClassEnvelope, SeverityError, "x12",
			"SE01 does not match the recounted segments from ST through SE inclusive."},
		{"EL3007", RuleGroupCount, ClassEnvelope, SeverityError, "x12",
			"GE01 does not match the recounted transaction sets in the group."},
		{"EL3008", RuleInterchangeCount, ClassEnvelope, SeverityError, "x12",
			"IEA01 does not match the recounted functional groups in the interchange."},
		{"EL3009", RuleDupControl, ClassEnvelope, SeverityError, "x12",
			"Duplicate ISA13 within the file or across the files in one run, duplicate GS06 within an " +
				"interchange, or duplicate ST02 within a functional group."},
		{"EL3010", RuleDateTime, ClassEnvelope, SeverityError, "x12",
			"ISA09/GS04 dates or ISA10/GS05 times are not valid YYMMDD, CCYYMMDD or HHMM[SS[DD]] values."},
		{"EL3011", RuleEnvelopeMissingID, ClassEnvelope, SeverityError, "x12",
			"ISA13 interchange control number is empty."},
		{"EL3012", RuleEnvelopeTrailing, ClassEnvelope, SeverityError, "x12",
			"Segments appear outside any interchange."},

		{"EL4001", RuleCountMismatch, ClassCounts, SeverityError, "all (requires --count-rule)",
			"A declared record count does not match the recounted total."},
		{"EL4002", RuleCountUnparsable, ClassCounts, SeverityError, "all (requires --count-rule)",
			"The field a count rule points at is not an integer."},
		{"EL4003", RuleCountShortRec, ClassCounts, SeverityError, "all (requires --count-rule)",
			"The declaring record has fewer fields than the count rule reads."},
		{"EL4004", RuleCountNoDeclarer, ClassCounts, SeverityWarning, "all (requires --count-rule)",
			"No record matched the count rule's declaring prefix, so nothing was verified."},

		{"EL4101", RuleFieldOutlier, ClassFields, SeverityError, "delimited, hl7v2 (warning)",
			"A record carries a different number of fields from others of the same record type."},

		{"EL5001", RuleLayoutLength, ClassLayout, SeverityError, "fixed (requires --layout)",
			"Record length does not match the sum of the layout's field widths."},
		{"EL5002", RuleLayoutPadding, ClassLayout, SeverityWarning, "fixed (requires --layout)",
			"A field's padding is unambiguously on the side opposite the one the layout declares."},

		{"EL6001", RuleBatchUnclosed, ClassHL7Batch, SeverityError, "hl7v2",
			"FHS or BHS is never closed by a matching FTS or BTS, so the batch envelope is incomplete."},
		{"EL6002", RuleBatchUnopened, ClassHL7Batch, SeverityError, "hl7v2",
			"FTS or BTS appears without its matching FHS or BHS header."},
		{"EL6003", RuleBatchMessageCount, ClassHL7Batch, SeverityError, "hl7v2",
			"BTS-1 does not match the recounted MSH messages in the batch. An empty BTS-1 is not " +
				"checked; the field is optional."},
		{"EL6004", RuleBatchFileCount, ClassHL7Batch, SeverityError, "hl7v2",
			"FTS-1 does not match the recounted BHS batches in the file. An empty FTS-1 is not " +
				"checked; the field is optional."},
		{"EL6005", RuleBatchSeparator, ClassHL7Batch, SeverityError, "hl7v2",
			"FHS, BHS and MSH headers disagree on the field separator or the encoding characters, " +
				"or a header's encoding characters are malformed. Split-and-merge tooling reads every " +
				"message with the first header's separators, so a disagreeing message is misparsed."},
		{"EL6006", RuleBatchStrayMessage, ClassHL7Batch, SeverityWarning, "hl7v2",
			"MSH appears outside any open batch in a file that uses batch envelopes, so batch-aware " +
				"readers will not process it. Files with no BHS at all are not checked; bare message " +
				"streams are valid without an envelope."},

		{"EL7001", RuleEdifactUnclosed, ClassEdifact, SeverityError, "edifact",
			"UNB, UNG or UNH is never closed by a matching UNZ, UNE or UNT."},
		{"EL7002", RuleEdifactUnopened, ClassEdifact, SeverityError, "edifact",
			"UNZ, UNE or UNT appears without its matching header."},
		{"EL7003", RuleEdifactSegmentCount, ClassEdifact, SeverityError, "edifact",
			"UNT-1 does not match the recounted segments from UNH through UNT inclusive."},
		{"EL7004", RuleEdifactGroupCount, ClassEdifact, SeverityError, "edifact",
			"UNE-1 does not match the recounted messages in the functional group."},
		{"EL7005", RuleEdifactInterchangeCount, ClassEdifact, SeverityError, "edifact",
			"UNZ-1 does not match the recounted messages in the interchange, or the recounted " +
				"functional groups when UNG groups are used."},
		{"EL7006", RuleEdifactControlRef, ClassEdifact, SeverityError, "edifact",
			"Header and trailer control references differ (UNB-5/UNZ-2, UNG-5/UNE-2, UNH-1/UNT-2), " +
				"or a header's reference is empty."},
		{"EL7007", RuleEdifactServiceString, ClassEdifact, SeverityError, "edifact",
			"UNA is not the fixed nine characters, its service characters collide or are " +
				"alphanumeric, or its decimal mark is not a period or a comma."},
		{"EL7008", RuleEdifactNesting, ClassEdifact, SeverityError, "edifact",
			"UNG or UNH appears outside the envelope that must enclose it, or no UNB is present " +
				"in a file forced to the edifact format."},
		{"EL7009", RuleEdifactTrailing, ClassEdifact, SeverityError, "edifact",
			"Segments appear outside any interchange; data after UNZ is not part of a valid " +
				"EDIFACT file."},
	}
}

// ruleIndex holds the catalog keyed both ways, built once at startup.
type ruleLookup struct {
	byName map[string]RuleDoc
	byID   map[string]RuleDoc
	// classes lists every distinct class, sorted, for diagnostics.
	classes []string
}

var ruleIndex = buildRuleIndex()

func buildRuleIndex() ruleLookup {
	idx := ruleLookup{
		byName: map[string]RuleDoc{},
		byID:   map[string]RuleDoc{},
	}
	seen := map[string]bool{}
	// The index is built from the bare table: ruleAcks is keyed through RuleID,
	// which reads this index, so Rules() cannot be called until it exists.
	for _, row := range catalog() {
		r := row.doc()
		idx.byName[r.Name] = r
		idx.byID[r.ID] = r
		if !seen[r.Class] {
			seen[r.Class] = true
			idx.classes = append(idx.classes, r.Class)
		}
	}
	sort.Strings(idx.classes)
	return idx
}

// RuleID returns the stable identifier for a rule name, or "" if the name is
// not in the catalog.
func RuleID(name string) string {
	return ruleIndex.byName[name].ID
}

// RuleName returns the rule name for an identifier, or "" if the identifier is
// not in the catalog. Identifier matching is case-insensitive, so "el3006" and
// "EL3006" name the same rule.
func RuleName(id string) string {
	return ruleIndex.byID[strings.ToUpper(strings.TrimSpace(id))].Name
}

// RuleClasses lists the check classes, sorted. Each is a valid rule selector.
func RuleClasses() []string {
	out := make([]string, len(ruleIndex.classes))
	copy(out, ruleIndex.classes)
	return out
}

// canonicalRule resolves a selector that names exactly one rule — an identifier
// or a full rule name — to that rule's name. It returns "" for a class name or
// for anything unknown.
func canonicalRule(selector string) string {
	selector = strings.TrimSpace(selector)
	if _, ok := ruleIndex.byName[selector]; ok {
		return selector
	}
	return RuleName(selector)
}

// matchesSelector reports whether one --disable entry suppresses a rule.
//
// An entry matches a rule identifier (case-insensitively), the full rule name,
// or any dot-delimited prefix of the name, so "charset" suppresses every
// charset.* rule while "charset.x12" suppresses nothing, because there is no
// rule at that name and no dot boundary after it.
func matchesSelector(rule, id, selector string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return false
	}
	if selector == rule || strings.HasPrefix(rule, selector+".") {
		return true
	}
	return id != "" && strings.EqualFold(selector, id)
}

// ruleDisabled reports whether rule is suppressed by any entry in disabled.
func ruleDisabled(rule string, disabled []string) bool {
	id := RuleID(rule)
	for _, d := range disabled {
		if matchesSelector(rule, id, d) {
			return true
		}
	}
	return false
}

// ValidateSelectors reports the first entry that names neither a rule
// identifier, a rule name, nor a check class.
//
// The library ignores an unrecognized selector, because a caller may be
// suppressing a rule from a newer version. The command line and the
// configuration file validate instead: there, a typo that silently suppresses
// nothing is worse than an error.
func ValidateSelectors(selectors []string) error {
	for _, s := range selectors {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if canonicalRule(s) != "" {
			continue
		}
		if _, ok := classBlocks[s]; ok {
			continue
		}
		return fmt.Errorf("unknown rule %q: expected an identifier (EL3006), a rule name "+
			"(envelope.segment-count) or a class (%s)", s, strings.Join(RuleClasses(), ", "))
	}
	return nil
}

// normalizeSeverities resolves override keys, which may be identifiers or rule
// names, to rule names. An unknown key is dropped: as with Disabled, the
// library tolerates rules it does not know and the command line rejects them.
func normalizeSeverities(in map[string]Severity) map[string]Severity {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]Severity, len(in))
	for key, sev := range in {
		name := canonicalRule(key)
		if name == "" {
			continue
		}
		out[name] = sev
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// WriteRules renders the rule catalog as an aligned table of identifier, name,
// default severity and summary.
func WriteRules(w io.Writer) error {
	rules := Rules()
	idWidth, nameWidth := 0, 0
	for _, r := range rules {
		if len(r.ID) > idWidth {
			idWidth = len(r.ID)
		}
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	for _, r := range rules {
		if _, err := fmt.Fprintf(w, "%-*s  %-*s  %-7s  %s\n",
			idWidth, r.ID, nameWidth, r.Name, r.Severity, r.Summary); err != nil {
			return err
		}
	}
	return nil
}
