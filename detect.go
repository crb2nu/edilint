package edilint

import "bytes"

// candidateDelimiters are tried, in order, when detecting a delimited file.
var candidateDelimiters = []byte{'|', ',', '\t', ';', 0x1f}

// Detect infers the structural format of body. It never returns FormatAuto.
func Detect(body []byte, opts Options) Format {
	head := body[leadingSpace(body):]

	switch {
	case bytes.HasPrefix(head, []byte("ISA")):
		return FormatX12
	case bytes.HasPrefix(head, []byte("MSH")),
		bytes.HasPrefix(head, []byte("FHS")),
		bytes.HasPrefix(head, []byte("BHS")):
		return FormatHL7v2
	}

	if opts.Layout != nil {
		return FormatFixed
	}
	if opts.Delimiter != "" {
		return FormatDelimited
	}
	if detectDelimiter(body) != 0 {
		return FormatDelimited
	}
	return FormatText
}

// leadingSpace returns the offset of the first byte that is not ASCII whitespace.
func leadingSpace(body []byte) int {
	i := 0
	for i < len(body) {
		switch body[i] {
		case ' ', '\t', '\r', '\n', '\v', '\f':
			i++
		default:
			return i
		}
	}
	return i
}

// minDelimiterCoverage is the fraction of non-empty lines a candidate must
// appear on before it is accepted as the field delimiter.
const minDelimiterCoverage = 0.8

// detectDelimiter picks the field delimiter of a line-oriented file.
//
// The primary signal is coverage: a real delimiter appears on nearly every
// record. Field-count consistency is only a tie-break, because flat files that
// mix header, detail and trailer record types legitimately carry a different
// number of fields per record type.
func detectDelimiter(body []byte) byte {
	lines := sampleLines(body, 200)
	if len(lines) == 0 {
		return 0
	}

	var (
		best      byte
		bestScore float64
	)
	for _, d := range candidateDelimiters {
		counts := make([]int, 0, len(lines))
		covered := 0
		for _, ln := range lines {
			n := bytes.Count(ln, []byte{d})
			counts = append(counts, n)
			if n > 0 {
				covered++
			}
		}
		coverage := float64(covered) / float64(len(lines))
		if coverage < minDelimiterCoverage {
			continue
		}
		modal, hits := modalPositive(counts)
		consistency := float64(hits) / float64(covered)
		// Coverage dominates; consistency and then field count break ties.
		score := coverage + consistency/10 + float64(modal)/10000
		if score > bestScore {
			bestScore, best = score, d
		}
	}
	return best
}

// sampleLines returns up to limit non-empty lines from body.
func sampleLines(body []byte, limit int) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i <= len(body) && len(out) < limit; i++ {
		if i == len(body) || body[i] == '\n' || body[i] == '\r' {
			ln := bytes.TrimRight(body[start:min(i, len(body))], "\r\n")
			if len(bytes.TrimSpace(ln)) > 0 {
				out = append(out, ln)
			}
			// Skip the second byte of a CRLF pair.
			if i+1 < len(body) && body[i] == '\r' && body[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	return out
}

// modalPositive returns the most common non-zero value in xs and how often it occurs.
func modalPositive(xs []int) (value, hits int) {
	freq := map[int]int{}
	for _, x := range xs {
		if x > 0 {
			freq[x]++
		}
	}
	for v, n := range freq {
		if n > hits || (n == hits && v > value) {
			value, hits = v, n
		}
	}
	return value, hits
}
