package edilint

import "strings"

// This file builds the envelope tree that diff and stats walk: interchanges
// holding functional groups holding transaction sets. The lint checks do not
// use it — they walk the flat segment list so that a defective envelope still
// produces findings at the right positions — but a structural comparison and a
// census both need the hierarchy itself.

// x12Seg is one segment of a parsed interchange, with the separators already
// applied.
type x12Seg struct {
	// ID is the segment identifier, e.g. "CLP".
	ID string
	// Elems holds the segment's elements. Elems[0] is the identifier as
	// written; element n of the X12 designator (CLP03 is element 3) is Elems[n].
	Elems []string
	// Raw is the segment text as it appeared, terminator excluded.
	Raw string
	// Line is the 1-based physical line the segment starts on.
	Line int
	// Term is the terminator that closed the segment, "" at end of file, and
	// Pad is the whitespace that followed it.
	Term string
	Pad  string
}

// x12Txn is one ST..SE transaction set, both envelope segments included.
type x12Txn struct {
	// Type is ST01, the transaction set identifier code, e.g. "835".
	Type string
	// Control is ST02, the transaction set control number.
	Control string
	// Segs holds every segment from ST through SE inclusive.
	Segs []x12Seg
}

// x12Group is one GS..GE functional group.
type x12Group struct {
	// Code is GS01, the functional identifier code, e.g. "HP".
	Code string
	// Control is GS06, the group control number.
	Control string
	// Frame holds the group's own segments in file order: GS, any segment that
	// sits inside the group but outside every transaction set, and GE when it
	// is present.
	Frame []x12Seg
	// Txns holds the group's transaction sets in file order.
	Txns []*x12Txn
}

// x12Interchange is one ISA..IEA interchange.
type x12Interchange struct {
	// Control is ISA13, the interchange control number.
	Control string
	// Frame holds the interchange's own segments in file order: ISA, any
	// segment outside every functional group (a TA1, say), and IEA when it is
	// present.
	Frame []x12Seg
	// Groups holds the functional groups in file order.
	Groups []*x12Group
}

// x12Doc is the envelope tree of one input.
type x12Doc struct {
	Interchanges []*x12Interchange
	// Loose holds segments outside any interchange. A well-formed file has
	// none; they appear when data precedes ISA or follows IEA.
	Loose []x12Seg
}

// parseX12Doc arranges a source's segments into the envelope tree. It is
// deliberately tolerant: an unclosed envelope is closed implicitly where its
// parent closes or the file ends, and a segment that appears outside the
// envelope that should enclose it is kept as a loose segment of whatever does
// enclose it. Reporting those defects is the linter's job; here the goal is a
// tree that still lines up for comparison and counting.
func parseX12Doc(s *source) *x12Doc {
	doc := &x12Doc{}
	var isa *x12Interchange
	var gs *x12Group
	var st *x12Txn

	closeTxn := func() {
		if st != nil {
			gs.Txns = append(gs.Txns, st)
			st = nil
		}
	}
	closeGroup := func() {
		closeTxn()
		if gs != nil {
			isa.Groups = append(isa.Groups, gs)
			gs = nil
		}
	}
	closeInterchange := func() {
		closeGroup()
		if isa != nil {
			doc.Interchanges = append(doc.Interchanges, isa)
			isa = nil
		}
	}
	// loose files a segment under the innermost open envelope.
	loose := func(seg x12Seg) {
		switch {
		case gs != nil:
			gs.Frame = append(gs.Frame, seg)
		case isa != nil:
			isa.Frame = append(isa.Frame, seg)
		default:
			doc.Loose = append(doc.Loose, seg)
		}
	}

	for i := range s.Records {
		r := s.Records[i]
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		seg := x12Seg{
			ID:    r.ID,
			Elems: s.Fields(r),
			Raw:   r.Text,
			Line:  r.Line,
			Term:  r.Term,
			Pad:   r.Pad,
		}

		switch r.ID {
		case "ISA":
			closeInterchange()
			isa = &x12Interchange{Control: strings.TrimSpace(elem(seg.Elems, 13))}
			isa.Frame = append(isa.Frame, seg)

		case "IEA":
			if isa == nil {
				loose(seg)
				continue
			}
			closeGroup()
			isa.Frame = append(isa.Frame, seg)
			closeInterchange()

		case "GS":
			if isa == nil {
				loose(seg)
				continue
			}
			closeGroup()
			gs = &x12Group{
				Code:    strings.TrimSpace(elem(seg.Elems, 1)),
				Control: strings.TrimSpace(elem(seg.Elems, 6)),
			}
			gs.Frame = append(gs.Frame, seg)

		case "GE":
			if gs == nil {
				loose(seg)
				continue
			}
			closeTxn()
			gs.Frame = append(gs.Frame, seg)
			closeGroup()

		case "ST":
			if gs == nil {
				loose(seg)
				continue
			}
			closeTxn()
			st = &x12Txn{
				Type:    strings.TrimSpace(elem(seg.Elems, 1)),
				Control: strings.TrimSpace(elem(seg.Elems, 2)),
			}
			st.Segs = append(st.Segs, seg)

		case "SE":
			if st == nil {
				loose(seg)
				continue
			}
			st.Segs = append(st.Segs, seg)
			closeTxn()

		default:
			if st != nil {
				st.Segs = append(st.Segs, seg)
				continue
			}
			loose(seg)
		}
	}
	closeInterchange()
	return doc
}
