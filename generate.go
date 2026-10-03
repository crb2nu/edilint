package edilint

import (
	"bufio"
	"fmt"
	"io"
	"time"
)

// MaxGenerateCount bounds the number of claims or messages in one fixture.
const MaxGenerateCount = 1_000_000

// GenerateOptions describes a synthetic fixture. All generated identities are
// fictional and both formats declare test usage. These are structural examples,
// not implementation-guide-compliant claims or clinical messages.
type GenerateOptions struct {
	// Kind is "837p" (one X12 transaction) or "hl7v2" (one ADT A08 batch).
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
}

// Generate writes a deterministic synthetic fixture using bounded memory.
// X12 segments end in "~\n"; HL7 segments end in CR. Identical options produce
// identical bytes. Options are checked before any output is written. A writer
// error may leave a partial fixture; it is returned, including flush errors.
func Generate(w io.Writer, opts GenerateOptions) error {
	if opts.Kind != "837p" && opts.Kind != "hl7v2" {
		return fmt.Errorf("gen: unknown kind %q (want 837p or hl7v2)", opts.Kind)
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
	g := &fixtureWriter{w: bufio.NewWriter(w), terminator: "~\n"}
	if opts.Kind == "837p" {
		generate837P(g, opts, date)
	} else {
		g.terminator = "\r"
		generateHL7(g, opts, date)
	}
	if g.err != nil {
		return fmt.Errorf("gen: write fixture: %w", g.err)
	}
	if err := g.w.Flush(); err != nil {
		return fmt.Errorf("gen: flush fixture: %w", err)
	}
	return nil
}

type fixtureWriter struct {
	w          *bufio.Writer
	terminator string
	segments   int
	err        error
}

func (g *fixtureWriter) segment(format string, args ...any) {
	if g.err != nil {
		return
	}
	_, g.err = fmt.Fprintf(g.w, format+g.terminator, args...)
	g.segments++
}

// The content is based on the repository's fictional 837P examples. It supplies
// enough variety for parser, census and pipeline tests without claiming to
// validate or reproduce the licensed implementation guide.
func generate837P(g *fixtureWriter, opts GenerateOptions, date time.Time) {
	g.segment("ISA*00*          *00*          *ZZ*%-15s*ZZ*%-15s*%s*1200*^*00501*%09d*0*T*:",
		"SYNTHETICSENDER", "SYNTHETICRECV", date.Format("060102"), opts.ControlNumber)
	g.segment("GS*HC*SYNTHETICSENDER*SYNTHETICRECV*%s*1200*%d*X*005010X222A1",
		date.Format("20060102"), opts.ControlNumber)
	start := g.segments
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
	g.segment("SE*%d*0001", g.segments-start+1)
	g.segment("GE*1*%d", opts.ControlNumber)
	g.segment("IEA*1*%09d", opts.ControlNumber)
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
	g.segment("BTS|%d", opts.Count)
	g.segment("FTS|1")
}
