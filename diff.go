package edilint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// DiffSchemaVersion is the version of the `edilint diff --json` document shape.
// It follows the same discipline as SchemaVersion: it is incremented, and a new
// schema file committed, whenever a consumer written against the previous
// version could meet something it has not seen before.
const DiffSchemaVersion = 1

// Difference kinds. They are part of the diff's public contract: they appear in
// JSON output and in schema/diff.v1.schema.json.
const (
	// DiffElement is a segment element whose value differs between the files.
	DiffElement = "element"
	// DiffTerminator is a segment whose closing terminator or the whitespace
	// after it differs. Reported only under strict comparison.
	DiffTerminator = "terminator"
	// DiffSegmentAdded and DiffSegmentRemoved are segments present in only one
	// file. Added means only in the second file, removed means only in the
	// first, and the same reading applies to the other added/removed kinds.
	DiffSegmentAdded       = "segment-added"
	DiffSegmentRemoved     = "segment-removed"
	DiffTransactionAdded   = "transaction-added"
	DiffTransactionRemoved = "transaction-removed"
	DiffGroupAdded         = "group-added"
	DiffGroupRemoved       = "group-removed"
	DiffInterchangeAdded   = "interchange-added"
	DiffInterchangeRemoved = "interchange-removed"
)

// DiffOptions configures a structural comparison.
type DiffOptions struct {
	// Strict also reports the cosmetic differences the default comparison
	// ignores: element values that differ only in trailing whitespace, and
	// segment terminator or terminator-padding style.
	Strict bool
}

// DiffPath locates a difference inside the envelope hierarchy. Every index is
// 1-based and names a position, not a control number: interchange 2 is the
// second ISA in the file. Segment is the position within its enclosing unit —
// for a transaction set that is the ST-through-SE ordinal, so ST is segment 1.
// Positions refer to the first file, except that a construct present only in
// the second file is located by its position there.
type DiffPath struct {
	Interchange int `json:"interchange,omitempty"`
	Group       int `json:"group,omitempty"`
	Transaction int `json:"transaction,omitempty"`
	// TransactionType and TransactionControl repeat ST01 and ST02, so a
	// difference can be found without counting transaction sets by hand.
	TransactionType    string `json:"transaction_type,omitempty"`
	TransactionControl string `json:"transaction_control,omitempty"`
	Segment            int    `json:"segment,omitempty"`
	SegmentID          string `json:"segment_id,omitempty"`
	// Element is the X12 element position, so CLP03 is element 3.
	Element int `json:"element,omitempty"`
}

// Difference is one structural difference between two files.
type Difference struct {
	// Kind is one of the Diff* kind constants.
	Kind string `json:"kind"`
	// Cosmetic is true for differences that only strict comparison reports.
	Cosmetic bool     `json:"cosmetic,omitempty"`
	Path     DiffPath `json:"path"`
	// Designator is the conventional X12 element designator, e.g. "CLP03".
	// Set only for element differences.
	Designator string `json:"designator,omitempty"`
	// A and B carry the differing content from each file: the element values
	// for an element difference, the terminator and its padding for a
	// terminator difference, and the segment or envelope-header text for an
	// added or removed construct. The side that lacks the construct is empty.
	A string `json:"a"`
	B string `json:"b"`
	// ALine and BLine are the 1-based physical lines of the segment involved,
	// absent on the side that does not contain it.
	ALine int `json:"a_line,omitempty"`
	BLine int `json:"b_line,omitempty"`
	// Message is a one-line human-readable description carrying the path.
	Message string `json:"message"`
}

// DiffSummary counts the differences by disposition. Elements counts element
// differences, Added and Removed count the added and removed constructs of
// every scale, and Cosmetic counts the differences only strict comparison
// reports, whatever their kind.
type DiffSummary struct {
	Total    int `json:"total"`
	Elements int `json:"elements"`
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Cosmetic int `json:"cosmetic"`
}

// DiffReport is the result of structurally comparing two X12 files.
type DiffReport struct {
	Version int    `json:"version"`
	A       string `json:"a"`
	B       string `json:"b"`
	Strict  bool   `json:"strict"`
	// Identical is true when no differences were found at the requested
	// strictness, which is what exit status 0 means.
	Identical   bool         `json:"identical"`
	Differences []Difference `json:"differences"`
	Summary     DiffSummary  `json:"summary"`
}

// DiffX12 structurally compares two X12 interchanges. Segments are aligned by
// their position within the envelope hierarchy — interchange, functional
// group, transaction set — never by byte offset, so files that only disagree
// about line breaks between segments compare as identical unless opts.Strict
// asks otherwise.
//
// The returned error is non-nil only when an input cannot be compared at all:
// it is not an X12 interchange, or its ISA is too malformed to yield
// separators. Files that merely have defects still diff.
func DiffX12(aName string, aData []byte, bName string, bData []byte, opts DiffOptions) (*DiffReport, error) {
	da, err := parseForDiff(aName, aData)
	if err != nil {
		return nil, err
	}
	db, err := parseForDiff(bName, bData)
	if err != nil {
		return nil, err
	}

	d := &differ{
		strict: opts.Strict,
		rep: &DiffReport{
			Version: DiffSchemaVersion,
			A:       aName,
			B:       bName,
			Strict:  opts.Strict,
		},
	}
	d.compareDocs(da, db)

	if d.rep.Differences == nil {
		d.rep.Differences = []Difference{}
	}
	d.rep.Summary.Total = len(d.rep.Differences)
	d.rep.Identical = d.rep.Summary.Total == 0
	return d.rep, nil
}

// parseForDiff turns one input into an envelope tree, or explains why it
// cannot be compared.
func parseForDiff(name string, data []byte) (*x12Doc, error) {
	body, _ := splitBOM(data)
	if share, binary := looksBinary(body); binary {
		return nil, fmt.Errorf("%s does not look like text: %.0f%% of the sample is invalid UTF-8 or NUL", name, share*100)
	}
	if f := Detect(body, Options{}); f != FormatX12 {
		return nil, fmt.Errorf("%s is not an X12 interchange (detected format %s)", name, f)
	}
	scratch := &Report{}
	src := newSource(name, body, FormatX12, Options{}, scratch)
	if !src.Delims.Declared {
		return nil, fmt.Errorf("%s: the ISA segment is missing or truncated, so no separators could be derived", name)
	}
	return parseX12Doc(src), nil
}

// WriteJSON writes the diff report as a single indented JSON document.
func (r *DiffReport) WriteJSON(w io.Writer) error {
	if r.Differences == nil {
		r.Differences = []Difference{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText writes one line per difference followed by a summary. Identical
// files write nothing, mirroring how a clean lint run is silent.
func (r *DiffReport) WriteText(w io.Writer) error {
	if len(r.Differences) == 0 {
		return nil
	}
	for _, d := range r.Differences {
		if _, err := fmt.Fprintln(w, d.Message); err != nil {
			return err
		}
	}
	tally := fmt.Sprintf("%d element, %d added, %d removed",
		r.Summary.Elements, r.Summary.Added, r.Summary.Removed)
	if r.Summary.Cosmetic > 0 {
		tally += fmt.Sprintf(", %d cosmetic", r.Summary.Cosmetic)
	}
	_, err := fmt.Fprintf(w, "\n%s (%s)\n", plural(r.Summary.Total, "difference"), tally)
	return err
}

// differ carries the comparison state: the strictness, the report being built,
// and nothing else.
type differ struct {
	strict bool
	rep    *DiffReport
}

// record appends a difference and folds it into the summary.
func (d *differ) record(diff Difference) {
	switch diff.Kind {
	case DiffElement:
		d.rep.Summary.Elements++
	case DiffSegmentAdded, DiffTransactionAdded, DiffGroupAdded, DiffInterchangeAdded:
		d.rep.Summary.Added++
	case DiffSegmentRemoved, DiffTransactionRemoved, DiffGroupRemoved, DiffInterchangeRemoved:
		d.rep.Summary.Removed++
	}
	if diff.Cosmetic {
		d.rep.Summary.Cosmetic++
	}
	d.rep.Differences = append(d.rep.Differences, diff)
}

// compareDocs aligns the two envelope trees. Interchanges pair by position:
// the first ISA of one file against the first ISA of the other.
func (d *differ) compareDocs(a, b *x12Doc) {
	n := min(len(a.Interchanges), len(b.Interchanges))
	for i := 0; i < n; i++ {
		d.compareInterchanges(i+1, a.Interchanges[i], b.Interchanges[i])
	}
	for i := n; i < len(a.Interchanges); i++ {
		d.envelopeOnly(DiffInterchangeRemoved, DiffPath{Interchange: i + 1},
			"this interchange", describeInterchange(a.Interchanges[i]), headSeg(a.Interchanges[i].Frame), true)
	}
	for i := n; i < len(b.Interchanges); i++ {
		d.envelopeOnly(DiffInterchangeAdded, DiffPath{Interchange: i + 1},
			"this interchange", describeInterchange(b.Interchanges[i]), headSeg(b.Interchanges[i].Frame), false)
	}
	d.alignSegments(a.Loose, b.Loose, DiffPath{})
}

func (d *differ) compareInterchanges(idx int, a, b *x12Interchange) {
	path := DiffPath{Interchange: idx}
	d.alignSegments(a.Frame, b.Frame, path)

	n := min(len(a.Groups), len(b.Groups))
	for i := 0; i < n; i++ {
		d.compareGroups(path, i+1, a.Groups[i], b.Groups[i])
	}
	for i := n; i < len(a.Groups); i++ {
		p := path
		p.Group = i + 1
		d.envelopeOnly(DiffGroupRemoved, p, "this functional group",
			describeGroup(a.Groups[i]), headSeg(a.Groups[i].Frame), true)
	}
	for i := n; i < len(b.Groups); i++ {
		p := path
		p.Group = i + 1
		d.envelopeOnly(DiffGroupAdded, p, "this functional group",
			describeGroup(b.Groups[i]), headSeg(b.Groups[i].Frame), false)
	}
}

func (d *differ) compareGroups(base DiffPath, idx int, a, b *x12Group) {
	path := base
	path.Group = idx
	d.alignSegments(a.Frame, b.Frame, path)
	d.alignTransactions(path, a.Txns, b.Txns)
}

// alignTransactions pairs the transaction sets of one aligned group pair.
// Identical transaction sets anchor the alignment; between anchors the
// leftovers pair by position when their ST01 types agree, and anything still
// unpaired is reported as an added or removed transaction set.
func (d *differ) alignTransactions(base DiffPath, a, b []*x12Txn) {
	akeys := make([]string, len(a))
	for i := range a {
		akeys[i] = txnKey(a[i], d.strict)
	}
	bkeys := make([]string, len(b))
	for i := range b {
		bkeys[i] = txnKey(b[i], d.strict)
	}
	anchors, exact := lcsPairs(len(a), len(b), func(i, j int) bool { return akeys[i] == bkeys[j] })
	if !exact {
		anchors = nil
	}

	ai, bi := 0, 0
	step := func(nextA, nextB int) {
		ga, gb := a[ai:nextA], b[bi:nextB]
		n := min(len(ga), len(gb))
		for i := 0; i < n; i++ {
			ta, tb := ga[i], gb[i]
			if ta.Type != tb.Type {
				d.transactionOnly(DiffTransactionRemoved, base, ai+i+1, ta, true)
				d.transactionOnly(DiffTransactionAdded, base, bi+i+1, tb, false)
				continue
			}
			d.compareTransactions(base, ai+i+1, ta, tb)
		}
		for i := n; i < len(ga); i++ {
			d.transactionOnly(DiffTransactionRemoved, base, ai+i+1, ga[i], true)
		}
		for i := n; i < len(gb); i++ {
			d.transactionOnly(DiffTransactionAdded, base, bi+i+1, gb[i], false)
		}
	}
	for _, pair := range anchors {
		step(pair[0], pair[1])
		ai, bi = pair[0]+1, pair[1]+1
	}
	step(len(a), len(b))
}

func (d *differ) compareTransactions(base DiffPath, idx int, a, b *x12Txn) {
	path := base
	path.Transaction = idx
	path.TransactionType = a.Type
	path.TransactionControl = a.Control
	d.alignSegments(a.Segs, b.Segs, path)
}

// alignSegments compares two segment lists that occupy the same position in
// the envelope hierarchy: the segments of one transaction set, or the framing
// segments of one interchange or group. Segments that compare equal anchor the
// alignment; between anchors, segments pair by identifier, and the rest are
// reported as added or removed.
func (d *differ) alignSegments(a, b []x12Seg, base DiffPath) {
	akeys := make([]string, len(a))
	for i := range a {
		akeys[i] = segKey(a[i], d.strict)
	}
	bkeys := make([]string, len(b))
	for i := range b {
		bkeys[i] = segKey(b[i], d.strict)
	}
	anchors, exact := lcsPairs(len(a), len(b), func(i, j int) bool { return akeys[i] == bkeys[j] })
	if !exact {
		anchors = nil
	}

	ai, bi := 0, 0
	gap := func(nextA, nextB int) {
		d.alignGap(a[ai:nextA], b[bi:nextB], ai, bi, base)
	}
	for _, pair := range anchors {
		gap(pair[0], pair[1])
		ai, bi = pair[0]+1, pair[1]+1
	}
	gap(len(a), len(b))
}

// alignGap handles one run of segments between anchors. aOff and bOff are the
// absolute positions of the run within each unit, for 1-based path ordinals.
func (d *differ) alignGap(a, b []x12Seg, aOff, bOff int, base DiffPath) {
	pairs, _ := lcsPairs(len(a), len(b), func(i, j int) bool { return a[i].ID == b[j].ID })

	ai, bi := 0, 0
	emit := func(nextA, nextB int) {
		for i := ai; i < nextA; i++ {
			d.segmentOnly(DiffSegmentRemoved, base, aOff+i+1, a[i], true)
		}
		for j := bi; j < nextB; j++ {
			d.segmentOnly(DiffSegmentAdded, base, bOff+j+1, b[j], false)
		}
	}
	for _, pair := range pairs {
		emit(pair[0], pair[1])
		pa, pb := a[pair[0]], b[pair[1]]
		if pa.ID == pb.ID {
			d.compareSegments(base, aOff+pair[0]+1, pa, pb)
		} else {
			// A positional fallback pair (the run was too large for alignment
			// by identifier) can hold different segments; report both sides.
			d.segmentOnly(DiffSegmentRemoved, base, aOff+pair[0]+1, pa, true)
			d.segmentOnly(DiffSegmentAdded, base, bOff+pair[1]+1, pb, false)
		}
		ai, bi = pair[0]+1, pair[1]+1
	}
	emit(len(a), len(b))
}

// compareSegments reports the element-level differences of one aligned pair.
func (d *differ) compareSegments(base DiffPath, ordinal int, a, b x12Seg) {
	path := base
	path.Segment = ordinal
	path.SegmentID = a.ID

	ae := normElems(a.Elems, d.strict)
	be := normElems(b.Elems, d.strict)
	n := max(len(ae), len(be))
	for e := 1; e < n; e++ {
		av, bv := elem(ae, e), elem(be, e)
		if av == bv {
			continue
		}
		p := path
		p.Element = e
		diff := Difference{
			Kind:       DiffElement,
			Path:       p,
			Designator: fmt.Sprintf("%s%02d", a.ID, e),
			A:          av,
			B:          bv,
			ALine:      a.Line,
			BLine:      b.Line,
		}
		note := ""
		if strings.TrimRight(av, " \t") == strings.TrimRight(bv, " \t") {
			diff.Cosmetic = true
			note = "; the values differ only in trailing whitespace"
		}
		diff.Message = fmt.Sprintf("%s: %s is %q in %s (line %d) but %q in %s (line %d)%s",
			pathString(p), diff.Designator, av, d.rep.A, a.Line, bv, d.rep.B, b.Line, note)
		d.record(diff)
	}

	if d.strict && a.Term+a.Pad != b.Term+b.Pad {
		d.record(Difference{
			Kind:     DiffTerminator,
			Cosmetic: true,
			Path:     path,
			A:        a.Term + a.Pad,
			B:        b.Term + b.Pad,
			ALine:    a.Line,
			BLine:    b.Line,
			Message: fmt.Sprintf("%s: the segment is closed by %q in %s (line %d) but %q in %s (line %d)",
				pathString(path), a.Term+a.Pad, d.rep.A, a.Line, b.Term+b.Pad, d.rep.B, b.Line),
		})
	}
}

// segmentOnly reports a segment present in only one file.
func (d *differ) segmentOnly(kind string, base DiffPath, ordinal int, s x12Seg, inA bool) {
	path := base
	path.Segment = ordinal
	path.SegmentID = s.ID
	diff := Difference{Kind: kind, Path: path}
	side := d.rep.B
	if inA {
		side = d.rep.A
		diff.A, diff.ALine = s.Raw, s.Line
	} else {
		diff.B, diff.BLine = s.Raw, s.Line
	}
	diff.Message = fmt.Sprintf("%s: this segment is only in %s (line %d): %s",
		pathString(path), side, s.Line, shortRaw(s.Raw))
	d.record(diff)
}

// transactionOnly reports a transaction set present in only one file.
func (d *differ) transactionOnly(kind string, base DiffPath, idx int, t *x12Txn, inA bool) {
	path := base
	path.Transaction = idx
	path.TransactionType = t.Type
	path.TransactionControl = t.Control
	head := headSeg(t.Segs)
	d.only(kind, path, "this transaction set", head, inA)
}

// envelopeOnly reports an interchange or functional group present in only one
// file. desc names the construct and detail identifies it by its control
// values, since the path alone carries only positions.
func (d *differ) envelopeOnly(kind string, path DiffPath, desc, detail string, head x12Seg, inA bool) {
	d.only(kind, path, desc+detail, head, inA)
}

// only is the shared shape of every construct-in-one-file-only difference.
func (d *differ) only(kind string, path DiffPath, what string, head x12Seg, inA bool) {
	diff := Difference{Kind: kind, Path: path}
	side := d.rep.B
	if inA {
		side = d.rep.A
		diff.A, diff.ALine = head.Raw, head.Line
	} else {
		diff.B, diff.BLine = head.Raw, head.Line
	}
	diff.Message = fmt.Sprintf("%s: %s is only in %s (line %d)",
		pathString(path), what, side, head.Line)
	d.record(diff)
}

// describeInterchange and describeGroup render the control values that
// identify an envelope, for messages about whole envelopes.
func describeInterchange(i *x12Interchange) string {
	if i.Control == "" {
		return ""
	}
	return fmt.Sprintf(" (ISA13 %s)", i.Control)
}

func describeGroup(g *x12Group) string {
	parts := []string{}
	if g.Code != "" {
		parts = append(parts, "code "+g.Code)
	}
	if g.Control != "" {
		parts = append(parts, "GS06 "+g.Control)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// headSeg returns the first segment of a unit, which identifies it in
// messages. Units are only created when their header segment is seen, so the
// slice is never empty in practice; the zero value keeps a defect here from
// panicking.
func headSeg(segs []x12Seg) x12Seg {
	if len(segs) == 0 {
		return x12Seg{}
	}
	return segs[0]
}

// pathString renders a DiffPath the way the report's messages speak:
// "interchange 1, group 1, transaction 2 (835, control 0002), segment 8 (CLP)".
func pathString(p DiffPath) string {
	var parts []string
	if p.Interchange > 0 {
		parts = append(parts, fmt.Sprintf("interchange %d", p.Interchange))
	}
	if p.Group > 0 {
		parts = append(parts, fmt.Sprintf("group %d", p.Group))
	}
	if p.Transaction > 0 {
		t := fmt.Sprintf("transaction %d", p.Transaction)
		var detail []string
		if p.TransactionType != "" {
			detail = append(detail, p.TransactionType)
		}
		if p.TransactionControl != "" {
			detail = append(detail, "control "+p.TransactionControl)
		}
		if len(detail) > 0 {
			t += " (" + strings.Join(detail, ", ") + ")"
		}
		parts = append(parts, t)
	}
	if p.SegmentID != "" {
		if p.Segment > 0 {
			parts = append(parts, fmt.Sprintf("segment %d (%s)", p.Segment, p.SegmentID))
		} else {
			parts = append(parts, "segment "+p.SegmentID)
		}
	}
	if len(parts) == 0 {
		return "outside any interchange"
	}
	return strings.Join(parts, ", ")
}

// normElems prepares a segment's elements for comparison. Trailing whitespace
// within each element is removed unless the comparison is strict, and trailing
// empty elements are dropped in either mode: CLP*A*B* and CLP*A*B carry the
// same content, and generators disagree about emitting the empty tail.
func normElems(elems []string, strict bool) []string {
	out := make([]string, len(elems))
	copy(out, elems)
	if !strict {
		for i := range out {
			out[i] = strings.TrimRight(out[i], " \t")
		}
	}
	n := len(out)
	for n > 1 && out[n-1] == "" {
		n--
	}
	return out[:n]
}

// segKey renders a segment into a comparison key so that alignment and
// pairwise comparison cannot disagree about equality. Elements are
// length-prefixed, which makes the encoding injective.
func segKey(s x12Seg, strict bool) string {
	var b strings.Builder
	for _, e := range normElems(s.Elems, strict) {
		fmt.Fprintf(&b, "%d:", len(e))
		b.WriteString(e)
	}
	if strict {
		b.WriteString("|")
		b.WriteString(s.Term)
		b.WriteString(s.Pad)
	}
	return b.String()
}

// txnKey renders a whole transaction set into a comparison key.
func txnKey(t *x12Txn, strict bool) string {
	var b strings.Builder
	for _, s := range t.Segs {
		k := segKey(s, strict)
		fmt.Fprintf(&b, "%d:", len(k))
		b.WriteString(k)
	}
	return b.String()
}

// lcsCellBudget bounds the dynamic-programming table of one alignment. A
// transaction set large enough to exceed it falls back to pairing by position,
// which keeps the diff proportional to the input instead of quadratic.
const lcsCellBudget = 4 << 20

// lcsPairs returns the index pairs of a longest common subsequence of two
// sequences, under the given equality predicate. The boolean reports whether
// the result is an exact subsequence: when the table would exceed the cell
// budget it returns position-paired indexes instead, which are not guaranteed
// equal under eq.
func lcsPairs(la, lb int, eq func(i, j int) bool) ([][2]int, bool) {
	if la == 0 || lb == 0 {
		return nil, true
	}
	if la*lb > lcsCellBudget {
		n := min(la, lb)
		pairs := make([][2]int, n)
		for i := range pairs {
			pairs[i] = [2]int{i, i}
		}
		return pairs, false
	}

	w := lb + 1
	dp := make([]int, (la+1)*w)
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if eq(i-1, j-1) {
				dp[i*w+j] = dp[(i-1)*w+j-1] + 1
				continue
			}
			dp[i*w+j] = max(dp[(i-1)*w+j], dp[i*w+j-1])
		}
	}

	pairs := make([][2]int, 0, dp[la*w+lb])
	for i, j := la, lb; i > 0 && j > 0; {
		switch {
		case eq(i-1, j-1) && dp[i*w+j] == dp[(i-1)*w+j-1]+1:
			pairs = append(pairs, [2]int{i - 1, j - 1})
			i, j = i-1, j-1
		case dp[(i-1)*w+j] >= dp[i*w+j-1]:
			i--
		default:
			j--
		}
	}
	// The backtrack found them last-to-first.
	for i, j := 0, len(pairs)-1; i < j; i, j = i+1, j-1 {
		pairs[i], pairs[j] = pairs[j], pairs[i]
	}
	return pairs, true
}

// shortRawLimit bounds how much of a segment a one-line message quotes.
const shortRawLimit = 80

// shortRaw truncates a segment for display without splitting a UTF-8 sequence.
func shortRaw(s string) string {
	if len(s) <= shortRawLimit {
		return s
	}
	cut := s[:shortRawLimit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "..."
}
