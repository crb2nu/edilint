package edilint

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// American English is a standing constraint in ROADMAP.md. User-facing strings
// have drifted from it before, so the rule is enforced here rather than left to
// review.

// iseStems are verb stems whose British form takes -ise, -ised, -ises, -ising
// or -isation. A suffix is required, which is what keeps American nouns built on
// the same stem — "analysis", "organism", "realistic", "specialist", "optimism",
// "emphasis" — from being flagged.
var iseStems = []string{
	"analys", "apologis", "authoris", "capitalis", "categoris", "centralis",
	"characteris", "customis", "emphasis", "finalis", "formalis", "generalis",
	"initialis", "itemis", "maximis", "minimis", "modernis", "neutralis",
	"normalis", "optimis", "organis", "parameteris", "prioritis", "realis",
	"recognis", "sanitis", "serialis", "specialis", "standardis", "summaris",
	"synchronis", "tokenis", "utilis", "visualis",
}

// iseSuffix is the set of endings that make an iseStem a British spelling.
const iseSuffix = `(e|ing|ation)`

// otherBritish are British forms that are unambiguous on their own.
var otherBritish = map[string]string{
	// -our
	"honour": "honor", "colour": "color", "behaviour": "behavior",
	"favour": "favor", "flavour": "flavor", "labour": "labor",
	"neighbour": "neighbor", "endeavour": "endeavor", "rumour": "rumor",
	"humour": "humor", "armour": "armor", "vapour": "vapor", "odour": "odor",
	"harbour": "harbor", "parlour": "parlor", "saviour": "savior",
	"valour": "valor", "vigour": "vigor", "rigour": "rigor",
	"splendour": "splendor", "demeanour": "demeanor",

	// Doubled consonant before a suffix.
	"labelling": "labeling", "labelled": "labeled",
	"cancelling": "canceling", "cancelled": "canceled",
	"modelling": "modeling", "modelled": "modeled",
	"signalling": "signaling", "signalled": "signaled",
	"travelling": "traveling", "travelled": "traveled",
	"fuelling": "fueling", "fuelled": "fueled",
	"totalling": "totaling", "totalled": "totaled",
	"marvellous": "marvelous",

	// -re, -ce and assorted others.
	"centre": "center", "metre": "meter", "litre": "liter",
	"theatre": "theater", "fibre": "fiber", "calibre": "caliber",
	"defence": "defense", "offence": "offense", "pretence": "pretense",
	"licence": "license", "practise": "practice",
	"catalogue": "catalog", "analogue": "analog", "programme": "program",
	"grey": "gray", "whilst": "while", "amongst": "among",
	"learnt": "learned", "spelt": "spelled",
	"fulfil": "fulfill", "instalment": "installment", "enrolment": "enrollment",
	"skilful": "skillful", "wilful": "willful",
	"artefact": "artifact", "judgement": "judgment", "ageing": "aging",
	"storey": "story", "aluminium": "aluminum", "draught": "draft",
	"manoeuvre": "maneuver", "sceptic": "skeptic", "cheque": "check",

	// Medical spellings, since the domain invites them.
	"paediatric": "pediatric", "foetal": "fetal", "anaemia": "anemia",
	"diarrhoea": "diarrhea", "haemo": "hemo", "orthopaedic": "orthopedic",
	"oesophag": "esophag",
}

// spellingExempt lists file names the check does not read. LICENSE is canonical
// third-party text that must not be edited, and this file necessarily contains
// every British form it looks for.
var spellingExempt = map[string]bool{
	"LICENSE":          true,
	"spelling_test.go": true,
}

// spellingExts are the file types checked.
var spellingExts = map[string]bool{
	".go": true, ".md": true, ".yml": true, ".yaml": true,
}

func TestNoBritishSpellings(t *testing.T) {
	type rule struct {
		re       *regexp.Regexp
		american string
	}
	var rules []rule
	for _, stem := range iseStems {
		rules = append(rules, rule{
			re:       regexp.MustCompile(`(?i)\b` + stem + iseSuffix),
			american: stem[:len(stem)-1] + "ze",
		})
	}
	for british, american := range otherBritish {
		rules = append(rules, rule{
			re:       regexp.MustCompile(`(?i)\b` + british),
			american: american,
		})
	}

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip everything that is not this repository's own text: the
			// git dir, tool caches CI places inside the checkout (the shared
			// Go template sets GOPATH and GOCACHE to .go/ and .go-build/,
			// which the first GitLab pipeline walked for ten minutes before
			// timing out), linked worktrees, build output and vendored code.
			// .github stays in: its workflow files are ours.
			name := d.Name()
			if path != "." && ((strings.HasPrefix(name, ".") && name != ".github") ||
				name == "vendor" || name == "bin" || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if spellingExempt[filepath.Base(path)] || !spellingExts[filepath.Ext(path)] {
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(body), "\n") {
			for _, r := range rules {
				if loc := r.re.FindStringIndex(line); loc != nil {
					t.Errorf("%s:%d: British spelling %q (use the American form, e.g. %q)\n\t%s",
						path, i+1, line[loc[0]:loc[1]], r.american, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

func TestSpellingCheckCatchesKnownForms(t *testing.T) {
	// The check is only useful if it actually matches, and only safe if it
	// leaves American words alone. Both directions are pinned here.
	shouldMatch := []string{
		"tokenised", "tokenising", "tokenisation", "normalise", "organisation",
		"honoured", "labelling", "cancelled", "centre", "defence", "catalogue",
		"analyse", "analysing", "emphasise", "realise", "specialised",
	}
	shouldNotMatch := []string{
		"analysis", "organism", "realistic", "specialist", "optimism",
		"emphasis", "tokenize", "tokenized", "honor", "labeling", "center",
		"defense", "catalog", "otherwise", "wise", "precise", "concise",
		"license", "practice", "program", "story", "check", "metering",
	}

	var rules []*regexp.Regexp
	for _, stem := range iseStems {
		rules = append(rules, regexp.MustCompile(`(?i)\b`+stem+iseSuffix))
	}
	for british := range otherBritish {
		rules = append(rules, regexp.MustCompile(`(?i)\b`+british))
	}
	matches := func(s string) bool {
		for _, re := range rules {
			if re.MatchString(s) {
				return true
			}
		}
		return false
	}

	for _, w := range shouldMatch {
		if !matches(w) {
			t.Errorf("%q should be flagged as a British spelling", w)
		}
	}
	for _, w := range shouldNotMatch {
		if matches(w) {
			t.Errorf("%q is American English and must not be flagged", w)
		}
	}
}
