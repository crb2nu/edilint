package edilint

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// MaxGenerateCount bounds the number of claims or messages in one fixture.
const MaxGenerateCount = 1_000_000

// GenerateOptions describes a synthetic fixture. All generated identities are
// fictional and all formats declare test usage. These are structural examples,
// not implementation-guide-compliant claims or clinical messages.
type GenerateOptions struct {
	// Kind is "837p" or "835" (one X12 transaction), "hl7v2" (one ADT A08
	// batch), or "edifact" (one interchange with ORDERS message examples).
	Kind string
	// Count is the number of claims or messages, from 1 to MaxGenerateCount.
	// Zero selects one.
	Count int
	// ControlNumber identifies the interchange or HL7 file, from 1 to
	// 999999999. Zero selects one. Choose distinct values for separate files.
	ControlNumber int
	// Date is YYYY-MM-DD, from 2000 through 2099. Empty selects 2026-01-01.
	// Envelope times are always noon; generation never reads the clock.
	Date string
	// Defects selects intentional envelope errors by case-insensitive rule ID.
	// X12 supports EL3005 (SE02 mismatch), EL3006 (SE01), EL3007 (GE01), and
	// EL3008 (IEA01); HL7 supports EL6003 (BTS-1) and EL6004 (FTS-1).
	// EDIFACT supports EL7003 (first UNT-1), EL7005 (UNZ-1), and EL7006
	// (first UNT-2 mismatch). Other messages keep their correct trailers.
	// Count defects overstate the actual total by one. Each ID produces one
	// finding. Duplicate, unsupported, and cross-format IDs are errors.
	// Empty selects a clean fixture; selection order does not affect output.
	Defects []string
}

// Generate writes a deterministic synthetic fixture using bounded memory.
// X12 segments end in "~\n", EDIFACT in "'\n", and HL7 in CR. Identical options produce
// identical bytes. Options are checked before any output is written. A writer
// error may leave a partial fixture; it is returned, including flush errors.
func Generate(w io.Writer, opts GenerateOptions) error {
	if opts.Kind != "837p" && opts.Kind != "835" && opts.Kind != "hl7v2" && opts.Kind != "edifact" {
		return fmt.Errorf("gen: unknown kind %q (want 837p, 835, hl7v2, or edifact)", opts.Kind)
	}
	if opts.Count == 0 {
		opts.Count = 1
	}
	if opts.Count < 1 || opts.Count > MaxGenerateCount {
		return fmt.Errorf("gen: count must be between 1 and %d", MaxGenerateCount)
	}
	if opts.ControlNumber == 0 {
		opts.ControlNumber = 1
	}
	if opts.ControlNumber < 1 || opts.ControlNumber > 999999999 {
		return fmt.Errorf("gen: control number must be between 1 and 999999999")
	}
	if opts.Date == "" {
		opts.Date = "2026-01-01"
	}
	date, err := time.Parse("2006-01-02", opts.Date)
	if err != nil || date.Year() < 2000 || date.Year() > 2099 {
		return fmt.Errorf("gen: date must be YYYY-MM-DD between 2000-01-01 and 2099-12-31")
	}
	defects, err := validateGenerateDefects(opts.Kind, opts.Defects)
	if err != nil {
		return err
	}
	g := &fixtureWriter{w: bufio.NewWriter(w), terminator: "~\n", defects: defects}
	switch opts.Kind {
	case "hl7v2":
		g.terminator = "\r"
		generateHL7(g, opts, date)
	case "edifact":
		g.terminator = "'\n"
		generateEdifact(g, opts, date)
	default:
		generateX12(g, opts, date)
	}
	if g.err != nil {
		return fmt.Errorf("gen: write fixture: %w", g.err)
	}
	if err := g.w.Flush(); err != nil {
		return fmt.Errorf("gen: flush fixture: %w", err)
	}
	return nil
}

func validateGenerateDefects(kind string, ids []string) (map[string]bool, error) {
	defects := make(map[string]bool)
	for _, selector := range ids {
		id := strings.ToUpper(strings.TrimSpace(selector))
		switch id {
		case "EL3005", "EL3006", "EL3007", "EL3008":
			if kind != "837p" && kind != "835" {
				return nil, fmt.Errorf("gen: defect %s is only supported for 837p or 835", id)
			}
		case "EL6003", "EL6004":
			if kind != "hl7v2" {
				return nil, fmt.Errorf("gen: defect %s is only supported for hl7v2", id)
			}
		case "EL7003", "EL7005", "EL7006":
			if kind != "edifact" {
				return nil, fmt.Errorf("gen: defect %s is only supported for edifact", id)
			}
		default:
			return nil, fmt.Errorf("gen: unsupported defect %q", selector)
		}
		if defects[id] {
			return nil, fmt.Errorf("gen: duplicate defect %s", id)
		}
		defects[id] = true
	}
	return defects, nil
}

type fixtureWriter struct {
	w          *bufio.Writer
	terminator string
	segments   int
	err        error
	defects    map[string]bool
}

func (g *fixtureWriter) count(id string, actual int) int {
	if g.defects[id] {
		return actual + 1
	}
	return actual
}

func (g *fixtureWriter) segment(format string, args ...any) {
	if g.err != nil {
		return
	}
	_, g.err = fmt.Fprintf(g.w, format+g.terminator, args...)
	g.segments++
}

func generateX12(g *fixtureWriter, opts GenerateOptions, date time.Time) {
	group, version := "HC", "005010X222A1"
	if opts.Kind == "835" {
		group, version = "HP", "005010X221A1"
	}
	g.segment("ISA*00*          *00*          *ZZ*%-15s*ZZ*%-15s*%s*1200*^*00501*%09d*0*T*:",
		"SYNTHETICSENDER", "SYNTHETICRECV", date.Format("060102"), opts.ControlNumber)
	g.segment("GS*%s*SYNTHETICSENDER*SYNTHETICRECV*%s*1200*%d*X*%s",
		group, date.Format("20060102"), opts.ControlNumber, version)
	start := g.segments
	if opts.Kind == "835" {
		generate835(g, opts, date)
	} else {
		generate837P(g, opts, date)
	}
	control := "0001"
	if g.defects["EL3005"] {
		control = "0002"
	}
	g.segment("SE*%d*%s", g.count("EL3006", g.segments-start+1), control)
	g.segment("GE*%d*%d", g.count("EL3007", 1), opts.ControlNumber)
	g.segment("IEA*%d*%09d", g.count("EL3008", 1), opts.ControlNumber)
}

// The content is based on the repository's fictional 837P examples. It supplies
// enough variety for parser, census and pipeline tests without claiming to
// validate or reproduce the licensed implementation guide.
func generate837P(g *fixtureWriter, opts GenerateOptions, date time.Time) {
	g.segment("ST*837*0001*005010X222A1")
	g.segment("BHT*0019*00*FAKEBATCH%09d*%s*1200*CH", opts.ControlNumber, date.Format("20060102"))
	g.segment("NM1*41*2*FICTIONAL CLEARINGHOUSE*****46*FAKESENDER")
	g.segment("PER*IC*TEST CONTACT*TE*8005550100")
	g.segment("NM1*40*2*FICTIONAL RECEIVER*****46*FAKERECEIVER")
	g.segment("HL*1**20*1")
	g.segment("NM1*85*2*FICTIONAL CLINIC*****XX*1999999999")
	g.segment("N3*100 SYNTHETIC STREET")
	g.segment("N4*EXAMPLEVILLE*NC*27000")
	g.segment("REF*EI*000000001")
	for i := 1; i <= opts.Count && g.err == nil; i++ {
		g.segment("HL*%d*1*22*0", i+1)
		g.segment("SBR*P*18*FAKEGROUP******CI")
		g.segment("NM1*IL*1*SAMPLE*PATIENT%06d****MI*FAKEMEMBER%06d", i, i)
		g.segment("N3*101 SYNTHETIC STREET")
		g.segment("N4*EXAMPLEVILLE*NC*27000")
		g.segment("DMG*D8*19800101*U")
		g.segment("NM1*PR*2*FICTIONAL HEALTH PLAN*****PI*FAKEPLAN")
		g.segment("CLM*FAKECLAIM%06d*125.00***11:B:1*Y*A*Y*Y", i)
		g.segment("DTP*472*D8*%s", date.Format("20060102"))
		g.segment("HI*ABK:Z0000")
		g.segment("LX*1")
		g.segment("SV1*HC:99201*125.00*UN*1***1")
	}
}

// Remittance examples keep amounts internally consistent: each $125 charge
// has a $100 payment and $25 patient adjustment. Integer cents avoid rounding
// and remain safe at the maximum claim count on 32-bit platforms.
func generate835(g *fixtureWriter, opts GenerateOptions, date time.Time) {
	day := date.Format("20060102")
	paymentCents := int64(opts.Count) * 10000
	g.segment("ST*835*0001")
	g.segment("BPR*I*%d.%02d*C*CHK", paymentCents/100, paymentCents%100)
	g.segment("TRN*1*FAKETRACE%09d*0000000000", opts.ControlNumber)
	g.segment("DTM*405*%s", day)
	g.segment("N1*PR*FICTIONAL HEALTH PLAN")
	g.segment("N3*100 SYNTHETIC STREET")
	g.segment("N4*EXAMPLEVILLE*NC*27000")
	g.segment("N1*PE*FICTIONAL CLINIC*XX*1999999999")
	g.segment("N3*101 SYNTHETIC STREET")
	g.segment("N4*EXAMPLEVILLE*NC*27000")
	g.segment("REF*TJ*000000001")
	for i := 1; i <= opts.Count && g.err == nil; i++ {
		g.segment("LX*%d", i)
		g.segment("CLP*FAKECLAIM%06d*1*125.00*100.00*25.00*12*FAKEPAYER%06d*11", i, i)
		g.segment("NM1*QC*1*SAMPLE*PATIENT%06d****MI*FAKEMEMBER%06d", i, i)
		g.segment("SVC*HC:99201*125.00*100.00**1")
		g.segment("DTM*472*%s", day)
		g.segment("CAS*PR*2*25.00")
		g.segment("AMT*B6*125.00")
	}
}

func generateHL7(g *fixtureWriter, opts GenerateOptions, date time.Time) {
	stamp := date.Format("20060102") + "120000"
	g.segment("FHS|^~\\&|SYNTHETIC|TESTFACILITY|RECEIVER|TESTFACILITY|%s||SYNTHETIC TEST DATA|FILE%09d", stamp, opts.ControlNumber)
	g.segment("BHS|^~\\&|SYNTHETIC|TESTFACILITY|RECEIVER|TESTFACILITY|%s|||BATCH%09d", stamp, opts.ControlNumber)
	for i := 1; i <= opts.Count && g.err == nil; i++ {
		g.segment("MSH|^~\\&|SYNTHETIC|TESTFACILITY|RECEIVER|TESTFACILITY|%s||ADT^A08|TEST%09d-%06d|T|2.5.1", stamp, opts.ControlNumber, i)
		g.segment("EVN|A08|%s", stamp)
		g.segment("PID|1||FAKEPATIENT%06d^^^SYNTHETIC^MR||SAMPLE^PATIENT%06d", i, i)
	}
	g.segment("BTS|%d", g.count("EL6003", opts.Count))
	g.segment("FTS|%d", g.count("EL6004", 1))
}

// These narrow ORDERS examples follow testdata/edifact_clean.edi. UNB-11
// declares a test interchange; no functional groups or partner guide rules
// are modeled. Message defects affect only the first trailer so each selected
// ID still produces exactly one finding regardless of the message count.
func generateEdifact(g *fixtureWriter, opts GenerateOptions, date time.Time) {
	g.segment("UNA:+.? ")
	g.segment("UNB+UNOA:3+SYNTHETICSENDER+SYNTHETICRECV+%s:1200+%09d++++++1",
		date.Format("060102"), opts.ControlNumber)
	stamp := date.Format("20060102") + "1200"
	for i := 1; i <= opts.Count && g.err == nil; i++ {
		start := g.segments
		ref := fmt.Sprintf("FAKE%07d", i)
		g.segment("UNH+%s+ORDERS:D:96A:UN", ref)
		g.segment("BGM+220+FAKEORDER%07d+9", i)
		g.segment("DTM+137:%s:203", stamp)
		count := g.segments - start + 1
		if i == 1 {
			count = g.count("EL7003", count)
			if g.defects["EL7006"] {
				ref = "WRONG0000001"
			}
		}
		g.segment("UNT+%d+%s", count, ref)
	}
	g.segment("UNZ+%d+%09d", g.count("EL7005", opts.Count), opts.ControlNumber)
}
