// Package edilint implements pre-send quality checks for healthcare interchange
// files: X12 EDI envelopes, HL7v2 messages and batches, EDIFACT envelopes,
// delimited extracts and fixed-width records.
//
// The package has no dependencies outside the Go standard library and no
// knowledge of any particular trading partner, so it can be embedded in a build
// pipeline, a send script, or a larger integration engine.
//
// The entry points are LintFile and Lint. Both return a Report holding zero or
// more Findings; a nil error means the input was read and analyzed, not that it
// was clean.
package edilint

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

// Format identifies the structural family of an input file.
type Format string

const (
	// FormatAuto asks Lint to detect the format from the file contents.
	FormatAuto Format = "auto"
	// FormatX12 is an X12 EDI interchange introduced by an ISA segment.
	FormatX12 Format = "x12"
	// FormatHL7v2 is an HL7 version 2.x message introduced by an MSH segment,
	// or a batch of them introduced by FHS or BHS.
	FormatHL7v2 Format = "hl7v2"
	// FormatEdifact is a UN/EDIFACT interchange introduced by a UNA service
	// string advice or a UNB header.
	FormatEdifact Format = "edifact"
	// FormatDelimited is a line-oriented record file with a single-character
	// field delimiter (CSV, pipe-delimited, tab-delimited).
	FormatDelimited Format = "delimited"
	// FormatFixed is a line-oriented fixed-width record file.
	FormatFixed Format = "fixed"
	// FormatText is a line-oriented file with no recognized record structure.
	// Only the character-hygiene and terminator checks apply.
	FormatText Format = "text"
)

// ParseFormat converts a user-supplied --format value into a Format.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatAuto, FormatX12, FormatHL7v2, FormatEdifact, FormatDelimited, FormatFixed, FormatText:
		return Format(s), nil
	case "":
		return FormatAuto, nil
	default:
		return "", fmt.Errorf("unknown format %q (want auto, x12, hl7v2, edifact, delimited, fixed or text)", s)
	}
}

// Options configures a lint run.
type Options struct {
	// Format forces an input format. The zero value (FormatAuto) detects it.
	Format Format

	// Delimiter overrides field-delimiter detection for delimited files.
	// It is a single character; the CLI also accepts the escapes \t, \0 and \x1f.
	Delimiter string

	// TypeField is the 1-based field index used as the record-type discriminator
	// for the field-count consistency check. Zero means field 1.
	TypeField int

	// CountRules holds declared-vs-actual record count assertions.
	CountRules []CountRule

	// Layout describes fixed-width record structure. Required for FormatFixed.
	Layout *Layout

	// X12Charset selects the X12 character-set profile enforced against X12
	// content. The zero value means CharsetExtended.
	X12Charset CharsetProfile

	// SeenISA13 enables cross-file duplicate interchange control number
	// detection. When non-nil it is read and written by every Lint call, mapping
	// an ISA13 value to the file it was first seen in. Callers linting a batch
	// should pass the same map to every call. It is not safe for concurrent use.
	//
	// Re-linting the same name is idempotent: a control number is only reported
	// as a duplicate when it was first seen under a different name. Duplicates
	// within a single input are detected independently of this map.
	SeenISA13 map[string]string

	// Disabled suppresses rules by identifier ("EL1005"), by full name
	// ("charset.homoglyph") or by dot-delimited prefix ("charset").
	Disabled []string

	// Severities overrides the severity a check assigned, keyed by rule
	// identifier or rule name. It applies to every finding of that rule,
	// including rules that otherwise grade themselves by format.
	Severities map[string]Severity

	// Baseline suppresses findings that were already recorded, so that a run
	// reports only what is new. When non-nil it is read and written by every
	// Lint call: each matched finding consumes one recorded occurrence. Callers
	// linting a batch pass the same value to every call. It is not safe for
	// concurrent use.
	Baseline *Baseline

	// MaxFindings caps the number of findings retained in a report. Zero or
	// negative means unlimited, which is the default. The cap only affects the
	// findings a report carries: Report.Summary always reflects the full set, and
	// so does Report.OK, so truncating output never changes an exit status.
	MaxFindings int
}

// maxFindings normalizes the cap: any value at or below zero means unlimited.
func (o Options) maxFindings() int {
	if o.MaxFindings < 0 {
		return 0
	}
	return o.MaxFindings
}

func (o Options) typeField() int {
	if o.TypeField <= 0 {
		return 1
	}
	return o.TypeField
}

func (o Options) charset() CharsetProfile {
	if o.X12Charset == "" {
		return CharsetExtended
	}
	return o.X12Charset
}

// LintFile reads path and lints it. A path of "-" reads standard input.
// The returned error is non-nil only for I/O problems.
func LintFile(path string, opts Options) (*Report, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return Lint("-", data, opts), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Lint(path, data, opts), nil
}

// Lint analyzes data and returns a report. name is used only for display.
func Lint(name string, data []byte, opts Options) *Report {
	rep := &Report{
		File:       name,
		Format:     FormatText,
		disabled:   opts.Disabled,
		severities: normalizeSeverities(opts.Severities),
		baseline:   opts.Baseline,
		retain:     retentionFor(opts),
	}

	body, bom := splitBOM(data)

	format := opts.Format
	if format == "" {
		format = FormatAuto
	}
	if format == FormatAuto {
		format = Detect(body, opts)
	}
	rep.Format = format

	if bom != "" {
		checkBOM(rep, format, bom)
	}

	// A binary input would otherwise produce one finding per byte. Say so once
	// and stop: that is both a better diagnostic and the only way to keep the
	// run proportional to the input.
	if share, binary := looksBinary(body); binary {
		rep.add(Finding{
			Rule:     RuleInvalidUTF8,
			Severity: SeverityError,
			Message: fmt.Sprintf("file does not look like text: %.0f%% of the first %s is invalid UTF-8 "+
				"or NUL. It was probably picked up by a glob; no interchange checks were run.",
				share*100, describeSample(body)),
			Line: 1,
		})
		rep.finalize(opts.maxFindings())
		return rep
	}

	// Options.Layout is exported and reachable from a caller-built value, so it
	// cannot be assumed valid the way the CLI's LoadLayout guarantees.
	if opts.Layout != nil {
		if err := opts.Layout.Validate(); err != nil {
			rep.add(Finding{
				Rule:     RuleLayoutLength,
				Severity: SeverityError,
				Message:  fmt.Sprintf("layout is unusable, so no fixed-width checks were run: %v", err),
				Line:     1,
			})
			opts.Layout = nil
		}
	}

	src := newSource(name, body, format, opts, rep)

	checkCharset(src, rep)
	checkTerminators(src, rep)
	if src.Format == FormatX12 {
		checkX12Envelope(src, opts, rep)
	}
	if src.Format == FormatHL7v2 {
		checkHL7Batch(src, rep)
	}
	if src.Format == FormatEdifact {
		checkEdifactEnvelope(src, rep)
	}
	checkCountRules(src, opts, rep)
	checkFieldCounts(src, opts, rep)
	if src.Format == FormatFixed {
		checkLayout(src, opts, rep)
	}

	rep.finalize(opts.maxFindings())
	return rep
}

// checkBOM reports a leading byte order mark, graded by how much damage it does
// in the detected format.
func checkBOM(rep *Report, format Format, bom string) {
	f := Finding{
		Rule:      RuleBOM,
		Line:      1,
		Column:    1,
		CodePoint: "U+FEFF",
		Expected:  "no byte order mark",
		Actual:    bom,
	}

	if format == FormatDelimited {
		// Spreadsheet exports routinely emit a BOM and most CSV readers cope, so
		// this is a smell rather than a defect.
		f.Severity = SeverityWarning
		f.Message = fmt.Sprintf("file begins with a %s byte order mark; readers that do not strip it "+
			"see it as part of the first field of the first record", bom)
	} else {
		// A BOM shifts every byte of a fixed-position read: ISA element offsets,
		// MSH-1 and MSH-2, and every fixed-width field boundary.
		f.Severity = SeverityError
		f.Message = fmt.Sprintf("file begins with a %s byte order mark; it shifts every fixed position "+
			"in the file and is read as part of the first field", bom)
	}
	rep.add(f)
}

// splitBOM strips a leading byte order mark and names the encoding it implies.
func splitBOM(data []byte) ([]byte, string) {
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return data[3:], "UTF-8"
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return data[2:], "UTF-16LE"
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return data[2:], "UTF-16BE"
	default:
		return data, ""
	}
}

// binarySampleBytes is how much of an input is examined when deciding whether it
// is text at all.
const binarySampleBytes = 64 << 10

// binaryThreshold is the share of the sample that must be invalid UTF-8 or NUL
// before the input is treated as binary. Text that merely uses a non-UTF-8
// encoding, such as Latin-1 with a few accented names, stays far below this.
const binaryThreshold = 0.30

// looksBinary reports whether the input is dense enough in invalid UTF-8 and NUL
// bytes that treating it as an interchange file would be meaningless. It returns
// the observed share so the finding can quote it.
func looksBinary(body []byte) (float64, bool) {
	sample := body
	if len(sample) > binarySampleBytes {
		sample = sample[:binarySampleBytes]
	}
	if len(sample) == 0 {
		return 0, false
	}

	bad := 0
	for off := 0; off < len(sample); {
		r, size := utf8.DecodeRune(sample[off:])
		switch {
		case r == utf8.RuneError && size == 1:
			bad++
		case r == 0:
			bad += size
		}
		off += size
	}

	share := float64(bad) / float64(len(sample))
	return share, share > binaryThreshold
}

// describeSample names the region looksBinary examined.
func describeSample(body []byte) string {
	if len(body) > binarySampleBytes {
		return fmt.Sprintf("%d KB", binarySampleBytes>>10)
	}
	return "file"
}
