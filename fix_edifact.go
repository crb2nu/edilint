package edilint

import "strings"

// edifactCountFixes recounts only complete, consistently nested envelopes.
// Any ambiguous structure discards all pending count edits for this file.
// References and payload are never inferred or rewritten.
func edifactCountFixes(s *source) []edit {
	if !s.Edifact.Declared {
		return nil
	}
	var edits []edit
	var unb, ung, unh envelopeLevel
	messages, groups := 0, 0
	for _, r := range s.Records {
		if strings.TrimSpace(r.Text) == "" {
			continue // Match the linter's treatment of empty records.
		}
		if r.Term == "" {
			return nil
		}
		if unh.open {
			unh.children++
		}
		switch r.ID {
		case "UNB":
			if unb.open {
				return nil
			}
			unb.open = true
			messages, groups = 0, 0
		case "UNG":
			if !unb.open || ung.open || unh.open || (groups == 0 && messages > 0) {
				return nil
			}
			groups++
			ung = envelopeLevel{open: true}
		case "UNH":
			if !unb.open || unh.open || (groups > 0 && !ung.open) {
				return nil
			}
			messages++
			ung.children++
			unh = envelopeLevel{open: true, children: 1}
		case "UNT":
			if !unh.open {
				return nil
			}
			edits = append(edits, recountFix(s, r, RuleEdifactSegmentCount, "UNT-1", 1,
				elem(s.Fields(r), 1), unh.children, true)...)
			unh.open = false
		case "UNE":
			if !ung.open || unh.open {
				return nil
			}
			edits = append(edits, recountFix(s, r, RuleEdifactGroupCount, "UNE-1", 1,
				elem(s.Fields(r), 1), ung.children, true)...)
			ung.open = false
		case "UNZ":
			if !unb.open || ung.open || unh.open {
				return nil
			}
			count := messages
			if groups > 0 {
				count = groups
			}
			edits = append(edits, recountFix(s, r, RuleEdifactInterchangeCount, "UNZ-1", 1,
				elem(s.Fields(r), 1), count, true)...)
			unb.open = false
		case "UNA":
			return nil // Only the initial, already parsed UNA can be trusted.
		default:
			if !unh.open {
				return nil
			}
		}
	}
	if unb.open || ung.open || unh.open {
		return nil
	}
	return edits
}
