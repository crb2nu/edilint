package edilint

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
)

// SARIF 2.1.0 output, the subset GitHub code scanning ingests. The document
// shape is fixed by the OASIS standard, so these structs mirror its property
// names rather than the package's own vocabulary.

const (
	sarifSchemaURI = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion   = "2.1.0"
	// sarifInformationURI is where a result's tool link points. It is the
	// repository rather than a rule page; per-rule URLs arrive with the rule
	// reference site.
	sarifInformationURI = "https://github.com/crb2nu/edilint"
)

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"`
	ShortDescription     sarifMessage       `json:"shortDescription"`
	FullDescription      *sarifMessage      `json:"fullDescription,omitempty"`
	DefaultConfiguration sarifConfiguration `json:"defaultConfiguration"`
}

type sarifConfiguration struct {
	Level string `json:"level"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	RuleIndex int             `json:"ruleIndex"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

// WriteSARIF writes the run report as a SARIF 2.1.0 document. toolVersion is
// recorded as the driver version; pass "" to omit it.
//
// Only retained findings are written, so --max-findings caps this output the
// way it caps the text form. GitHub code scanning caps results per run itself,
// so a defect-dense file is capped either way.
func (rr *RunReport) WriteSARIF(w io.Writer, toolVersion string) error {
	run := sarifRun{
		Tool: sarifTool{Driver: sarifDriver{
			Name:           "edilint",
			Version:        toolVersion,
			InformationURI: sarifInformationURI,
			Rules:          []sarifRule{},
		}},
		Results: []sarifResult{},
	}

	// The rules array carries only rules with at least one result, in order of
	// first appearance, so the document stays proportional to what was found.
	ruleAt := map[string]int{}
	for _, r := range rr.Files {
		for _, f := range r.Findings {
			id := f.ID
			if id == "" {
				// A caller-built finding for a rule outside the catalog still needs
				// a ruleId; the dotted name is the stable identifier it does have.
				id = f.Rule
			}
			idx, ok := ruleAt[id]
			if !ok {
				idx = len(run.Tool.Driver.Rules)
				ruleAt[id] = idx
				run.Tool.Driver.Rules = append(run.Tool.Driver.Rules, sarifRuleFor(id, f))
			}

			msg := f.Message
			if ctx := findingContext(f, r.Format); ctx != "" {
				msg += " (" + ctx + ")"
			}
			run.Results = append(run.Results, sarifResult{
				RuleID:    id,
				RuleIndex: idx,
				Level:     sarifLevel(f.Severity),
				Message:   sarifMessage{Text: msg},
				Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
					ArtifactLocation: sarifArtifact{URI: filepath.ToSlash(f.File)},
					Region:           sarifRegionFor(f),
				}}},
			})
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(sarifLog{
		Schema:  sarifSchemaURI,
		Version: sarifVersion,
		Runs:    []sarifRun{run},
	})
}

// sarifRuleFor builds the rule metadata entry for a result's rule, from the
// catalog when the rule is in it and from the finding itself when it is not.
func sarifRuleFor(id string, f Finding) sarifRule {
	doc, ok := ruleIndex.byName[f.Rule]
	if !ok {
		return sarifRule{
			ID:                   id,
			Name:                 f.Rule,
			ShortDescription:     sarifMessage{Text: f.Rule},
			DefaultConfiguration: sarifConfiguration{Level: sarifLevel(f.Severity)},
		}
	}
	rule := sarifRule{
		ID:                   doc.ID,
		Name:                 doc.Name,
		ShortDescription:     sarifMessage{Text: firstSentence(doc.Summary)},
		DefaultConfiguration: sarifConfiguration{Level: sarifLevel(doc.Severity)},
	}
	if full := doc.Summary; full != rule.ShortDescription.Text {
		rule.FullDescription = &sarifMessage{Text: full}
	}
	return rule
}

// sarifRegionFor converts editor coordinates to a SARIF region. A finding with
// no line has no region: SARIF requires startLine in a text region, and a
// whole-file finding genuinely has none.
func sarifRegionFor(f Finding) *sarifRegion {
	if f.Line <= 0 {
		return nil
	}
	region := &sarifRegion{StartLine: f.Line}
	if f.Column > 0 {
		region.StartColumn = f.Column
	}
	return region
}

// sarifLevel maps a severity onto the SARIF level vocabulary. Informational
// findings become "note", which GitHub renders without failing anything —
// the same contract info has everywhere else.
func sarifLevel(s Severity) string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "note"
	}
}

// firstSentence returns the first sentence of a rule summary, for the SARIF
// shortDescription, which the standard wants to be a single sentence.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}
