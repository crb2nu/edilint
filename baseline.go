package edilint

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BaselineVersion is the version of the baseline document shape.
const BaselineVersion = 1

// A baseline records the findings a set of files produced at one moment, so
// that a later run can report only what is new. It is the way to adopt edilint
// on files that already exist: record what is wrong today, then fail the build
// on anything added tomorrow.
//
// A baseline entry deliberately holds no line, column or record ordinal. Those
// move whenever a segment is inserted above them, and a baseline that expired
// on the next edit would be useless. What identifies a finding instead is the
// file it is in, the rule that produced it, the record type it sits on, and the
// shape of its message — the names, record types and quoted values that say
// which defect this is, with the numbers taken out. Two findings that agree on
// all four are treated as the same finding wherever it has moved to.
//
// Identical findings are common, so an entry also carries a count. Thirty
// non-printable characters in one file record as one entry with a count of 30;
// a thirty-first is new and is reported.

// BaselineEntry is one recorded finding, or a run of identical ones.
type BaselineEntry struct {
	// File is the input path as edilint was given it, with separators
	// normalized to forward slashes.
	File string `json:"file"`
	// ID is the rule identifier, and is what a later run matches on.
	ID string `json:"id"`
	// Rule is the rule name at the time of recording. It is informational: the
	// identifier is the stable half of the pair.
	Rule string `json:"rule"`
	// Record is the segment identifier or record type the finding sits on, if
	// the rule reports one.
	Record string `json:"record,omitempty"`
	// Message is the finding's message, which is part of the match.
	Message string `json:"message"`
	// Count is how many identical findings this entry stands for.
	Count int `json:"count"`
}

// Baseline is a set of recorded findings.
//
// A Baseline passed to Lint through Options is read and written by every call:
// each matched finding consumes one of the recorded occurrences. Callers linting
// a batch pass the same value to every call, exactly as with Options.SeenISA13.
// It is not safe for concurrent use.
type Baseline struct {
	Version  int             `json:"version"`
	Findings []BaselineEntry `json:"findings"`

	// remaining tracks how many occurrences of each entry are still unmatched.
	remaining map[baselineKey]int
}

// baselineKey is the identity of a finding across runs.
type baselineKey struct {
	file    string
	id      string
	record  string
	message string
}

func keyOf(file, id, record, message string) baselineKey {
	return baselineKey{
		file:    normalizeBaselinePath(file),
		id:      id,
		record:  record,
		message: baselineMessage(message),
	}
}

// baselineMessage reduces a message to the part that says which defect this is,
// rather than what the file happens to look like today. Every run of digits
// becomes "#".
//
// Several messages quote a statistic over the whole file: the field-count rule
// says "9 in 2 of 3 record(s) of this type", which changes as soon as an
// unrelated record is added. Keying on the literal text would make a baseline go
// stale on an edit that had nothing to do with the defect, and a gate that cries
// wolf after every edit is one people stop believing. Names, record types and
// quoted values survive, so two different defects do not collapse into one, and
// the occurrence count still catches one more of the same defect.
func baselineMessage(message string) string {
	var b strings.Builder
	b.Grow(len(message))
	inDigits := false
	for i := 0; i < len(message); i++ {
		if c := message[i]; c >= '0' && c <= '9' {
			if !inDigits {
				b.WriteByte('#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		b.WriteByte(message[i])
	}
	return b.String()
}

// normalizeBaselinePath makes the recorded path independent of how the shell
// spelled it, so that "./out/a.x12" and "out/a.x12" are the same file. Paths
// stay relative, so a baseline is only portable between runs started from the
// same directory, which is what committing one alongside the files implies.
func normalizeBaselinePath(name string) string {
	if name == "" || name == "-" {
		return name
	}
	return filepath.ToSlash(filepath.Clean(name))
}

// NewBaseline records every finding in a run report.
//
// Findings that a retention ceiling dropped cannot be recorded, so a truncated
// report yields an incomplete baseline. Callers should check
// RunReport.Summary.Truncated and say so.
func NewBaseline(rr *RunReport) *Baseline {
	counts := map[baselineKey]int{}
	var order []baselineKey

	for _, rep := range rr.Files {
		for _, f := range rep.Findings {
			k := keyOf(f.File, f.ID, f.Record, f.Message)
			if counts[k] == 0 {
				order = append(order, k)
			}
			counts[k]++
		}
	}

	b := &Baseline{Version: BaselineVersion}
	for _, k := range order {
		b.Findings = append(b.Findings, BaselineEntry{
			File:    k.file,
			ID:      k.id,
			Rule:    RuleName(k.id),
			Record:  k.record,
			Message: k.message,
			Count:   counts[k],
		})
	}
	b.sortEntries()
	b.index()
	return b
}

// sortEntries orders the document so that recording an unchanged set of
// findings twice produces byte-identical files. A baseline is committed and
// reviewed in diffs; churn from map iteration order would make it unreadable.
func (b *Baseline) sortEntries() {
	sort.SliceStable(b.Findings, func(i, j int) bool {
		x, y := b.Findings[i], b.Findings[j]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.ID != y.ID {
			return x.ID < y.ID
		}
		if x.Record != y.Record {
			return x.Record < y.Record
		}
		return x.Message < y.Message
	})
}

// index rebuilds the unmatched-occurrence counters from the entries.
func (b *Baseline) index() {
	b.remaining = make(map[baselineKey]int, len(b.Findings))
	for _, e := range b.Findings {
		n := e.Count
		if n < 1 {
			n = 1
		}
		b.remaining[keyOf(e.File, e.ID, e.Record, e.Message)] += n
	}
}

// Reset restores every entry's unmatched count, so the baseline can be applied
// to a second run.
func (b *Baseline) Reset() {
	if b == nil {
		return
	}
	b.index()
}

// Total is how many individual findings the baseline records.
func (b *Baseline) Total() int {
	if b == nil {
		return 0
	}
	n := 0
	for _, e := range b.Findings {
		if e.Count < 1 {
			n++
			continue
		}
		n += e.Count
	}
	return n
}

// accountsFor reports whether the baseline already holds this finding, and
// consumes one of the recorded occurrences when it does. A nil baseline
// accounts for nothing, which is what makes an ordinary run cost nothing.
func (b *Baseline) accountsFor(f Finding) bool {
	if b == nil || len(b.remaining) == 0 {
		return false
	}
	k := keyOf(f.File, f.ID, f.Record, f.Message)
	if b.remaining[k] <= 0 {
		return false
	}
	b.remaining[k]--
	return true
}

// Unmatched returns the entries the run did not encounter, with Count set to
// the number of occurrences left over. They are entries for defects that were
// fixed, or for files that were not linted this time; a baseline full of them
// is stale and worth re-recording.
//
// Reporting an entry clears it, so calling this twice does not count the same
// leftover twice. Reset restores the tally.
func (b *Baseline) Unmatched() []BaselineEntry {
	if b == nil {
		return nil
	}
	var out []BaselineEntry
	for _, e := range b.Findings {
		k := keyOf(e.File, e.ID, e.Record, e.Message)
		if n := b.remaining[k]; n > 0 {
			left := e
			left.Count = n
			out = append(out, left)
			// An entry is emitted once even when several share a key.
			b.remaining[k] = 0
		}
	}
	return out
}

// ReadBaseline decodes a baseline document.
func ReadBaseline(r io.Reader) (*Baseline, error) {
	var b Baseline
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("baseline is not a valid edilint baseline document: %w", err)
	}
	if b.Version != BaselineVersion {
		return nil, fmt.Errorf("baseline version %d is not supported (this build writes version %d); "+
			"re-record it with --write-baseline", b.Version, BaselineVersion)
	}
	for i, e := range b.Findings {
		if e.ID == "" {
			return nil, fmt.Errorf("baseline entry %d has no rule id", i+1)
		}
		if e.File == "" {
			return nil, fmt.Errorf("baseline entry %d has no file", i+1)
		}
	}
	b.index()
	return &b, nil
}

// LoadBaseline reads a baseline from a file.
func LoadBaseline(path string) (*Baseline, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("baseline %s does not exist; record one with --write-baseline %s", path, path)
		}
		return nil, fmt.Errorf("read baseline %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	b, err := ReadBaseline(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

// WriteJSON writes the baseline as an indented JSON document with a trailing
// newline, so it can be committed and diffed.
func (b *Baseline) WriteJSON(w io.Writer) error {
	doc := *b
	if doc.Version == 0 {
		doc.Version = BaselineVersion
	}
	if doc.Findings == nil {
		doc.Findings = []BaselineEntry{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// WriteFile writes the baseline to path, replacing any existing file.
func (b *Baseline) WriteFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	if err := b.WriteJSON(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	return nil
}
