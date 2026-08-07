package edilint

import "fmt"

// OutputFormat identifies how a run report is rendered. It is distinct from
// Format, which identifies the structure of an input file: a run reads x12 and
// may write sarif.
type OutputFormat string

const (
	// OutputText is the default: one grep-parseable diagnostic line per finding.
	OutputText OutputFormat = "text"
	// OutputJSON is the versioned JSON document described by schema/report.v3.schema.json.
	OutputJSON OutputFormat = "json"
	// OutputSARIF is a SARIF 2.1.0 document, the format GitHub code scanning ingests.
	OutputSARIF OutputFormat = "sarif"
	// OutputJUnit is JUnit XML, the format CI test panels ingest.
	OutputJUnit OutputFormat = "junit"
	// OutputGitHub is GitHub Actions workflow commands, one annotation per finding.
	OutputGitHub OutputFormat = "github"
)

// ParseOutputFormat converts a user-supplied --output value into an OutputFormat.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch OutputFormat(s) {
	case OutputText, OutputJSON, OutputSARIF, OutputJUnit, OutputGitHub:
		return OutputFormat(s), nil
	case "":
		return OutputText, nil
	default:
		return "", fmt.Errorf("unknown output format %q (want text, json, sarif, junit or github)", s)
	}
}
