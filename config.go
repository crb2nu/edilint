package edilint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ConfigVersion is the version of the configuration file schema this build
// understands.
const ConfigVersion = 1

// ConfigNames are the file names edilint looks for when no --config is given,
// in the order it tries them.
var ConfigNames = []string{".edilint.yml", ".edilint.yaml"}

// Config is a parsed .edilint.yml.
//
// Every setting is optional. A setting that is absent is left to the command
// line and then to the built-in default, so a configuration file states the
// conventions of one shop rather than a whole run.
//
// The file cannot switch a rule back on. Rules are on by default, and a
// configuration lists what to turn off and what to re-grade; there is no
// allowlist form, because two lists that can contradict each other need
// precedence rules nobody remembers.
type Config struct {
	// Path is the file this was read from.
	Path string
	// Version is the schema version the file declares. Zero means it declared
	// none, which is read as the current version.
	Version int

	Format        Format
	Delimiter     string
	Charset       CharsetProfile
	TypeField     int
	MaxFindings   int
	AllowWarnings bool
	// Layout is the fixed-width layout path, resolved relative to the
	// configuration file's own directory.
	Layout string
	// Disable holds rule identifiers, rule names and class names.
	Disable []string
	// Severity re-grades rules, keyed by rule name. Identifiers in the file are
	// resolved to names when it is read.
	Severity map[string]Severity
	// CountRules holds the declared-count assertions from the file.
	CountRules []CountRule

	// present records which keys the file actually set, so that a caller can
	// tell "absent" from "set to the zero value".
	present map[string]bool
}

// Has reports whether the file set the named key, e.g. "charset".
func (c *Config) Has(key string) bool {
	if c == nil {
		return false
	}
	return c.present[key]
}

// Apply layers a configuration onto a set of options, overwriting the settings
// the file specifies and appending to the cumulative ones.
//
// The command line is applied after this, so a flag always wins over the file,
// except for --disable and --count-rule, which add to what the file already
// asked for rather than replacing it. Suppressing a rule and then having a flag
// silently un-suppress it would be the more surprising rule.
//
// AllowWarnings and Layout have no counterpart in Options — one is an exit-status
// policy and the other names a file that has to be read — so a caller applies
// those two itself, from the fields of the same name.
func (c *Config) Apply(opts *Options) {
	if c == nil {
		return
	}
	if c.Has("format") {
		opts.Format = c.Format
	}
	if c.Has("delimiter") {
		opts.Delimiter = c.Delimiter
	}
	if c.Has("charset") {
		opts.X12Charset = c.Charset
	}
	if c.Has("type-field") {
		opts.TypeField = c.TypeField
	}
	if c.Has("max-findings") {
		opts.MaxFindings = c.MaxFindings
	}
	opts.Disabled = append(opts.Disabled, c.Disable...)
	opts.CountRules = append(opts.CountRules, c.CountRules...)
	if len(c.Severity) > 0 {
		if opts.Severities == nil {
			opts.Severities = map[string]Severity{}
		}
		for rule, sev := range c.Severity {
			opts.Severities[rule] = sev
		}
	}
}

// FindConfig returns the configuration file in dir, or "" if there is none.
//
// Only dir is searched: edilint does not walk up the tree. One file per working
// directory is predictable, and a run that silently picked up a configuration
// three levels above would be hard to explain when it suppressed something.
func FindConfig(dir string) string {
	for _, name := range ConfigNames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// LoadConfig reads and validates a configuration file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	doc, err := parseYAML(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	c := &Config{Path: path, present: map[string]bool{}}
	for _, key := range doc.keys {
		val := doc.values[key]
		if err := c.set(key, val); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		c.present[key] = true
	}

	if c.Version != 0 && c.Version != ConfigVersion {
		return nil, fmt.Errorf("%s: config version %d is not supported (this build understands version %d)",
			path, c.Version, ConfigVersion)
	}
	if c.Layout != "" && !filepath.IsAbs(c.Layout) {
		// A relative layout path is relative to the configuration file, which is
		// what makes a committed .edilint.yml work from any working directory.
		c.Layout = filepath.Join(filepath.Dir(path), c.Layout)
	}
	return c, nil
}

// set applies one top-level key.
func (c *Config) set(key string, val yamlValue) error {
	switch key {
	case "version":
		n, err := val.intValue(key, 1)
		if err != nil {
			return err
		}
		c.Version = n

	case "format":
		s, err := val.scalarValue(key)
		if err != nil {
			return err
		}
		format, err := ParseFormat(s)
		if err != nil {
			return fmt.Errorf("line %d: %w", val.line, err)
		}
		c.Format = format

	case "delimiter":
		s, err := val.scalarValue(key)
		if err != nil {
			return err
		}
		if _, err := ParseDelimiter(s); err != nil {
			return fmt.Errorf("line %d: %w", val.line, err)
		}
		c.Delimiter = s

	case "charset":
		s, err := val.scalarValue(key)
		if err != nil {
			return err
		}
		profile, err := ParseCharsetProfile(s)
		if err != nil {
			return fmt.Errorf("line %d: %w", val.line, err)
		}
		c.Charset = profile

	case "type-field":
		n, err := val.intValue(key, 1)
		if err != nil {
			return err
		}
		c.TypeField = n

	case "max-findings":
		n, err := val.intValue(key, 0)
		if err != nil {
			return err
		}
		c.MaxFindings = n

	case "allow-warnings":
		b, err := val.boolValue(key)
		if err != nil {
			return err
		}
		c.AllowWarnings = b

	case "layout":
		s, err := val.scalarValue(key)
		if err != nil {
			return err
		}
		c.Layout = s

	case "disable":
		items, err := val.seqValue(key)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := ValidateSelectors([]string{item.text}); err != nil {
				return fmt.Errorf("line %d: %w", item.line, err)
			}
			c.Disable = append(c.Disable, item.text)
		}

	case "count-rules":
		items, err := val.seqValue(key)
		if err != nil {
			return err
		}
		for _, item := range items {
			rule, err := ParseCountRule(item.text)
			if err != nil {
				return fmt.Errorf("line %d: %w", item.line, err)
			}
			c.CountRules = append(c.CountRules, rule)
		}

	case "severity":
		pairs, err := val.mapValue(key)
		if err != nil {
			return err
		}
		c.Severity = map[string]Severity{}
		for _, pair := range pairs {
			name := canonicalRule(pair.key)
			if name == "" {
				return fmt.Errorf("line %d: unknown rule %q: a severity can only be set on one rule "+
					"at a time, named by identifier (EL3006) or by name (envelope.segment-count)",
					pair.line, pair.key)
			}
			sev, err := ParseSeverity(pair.value)
			if err != nil {
				return fmt.Errorf("line %d: %w", pair.line, err)
			}
			c.Severity[name] = sev
		}

	default:
		return fmt.Errorf("line %d: unknown setting %q (known settings: %s)",
			val.line, key, strings.Join(configKeys(), ", "))
	}
	return nil
}

// configKeys lists every recognized setting, for the unknown-key diagnostic.
func configKeys() []string {
	keys := []string{
		"allow-warnings", "charset", "count-rules", "delimiter", "disable",
		"format", "layout", "max-findings", "severity", "type-field", "version",
	}
	sort.Strings(keys)
	return keys
}

// scalarValue reads a single value, rejecting a list or a mapping.
func (v yamlValue) scalarValue(key string) (string, error) {
	if v.kind != yamlScalar {
		return "", fmt.Errorf("line %d: %q takes a single value, not a list or a mapping", v.line, key)
	}
	return v.scalar, nil
}

// intValue reads an integer no smaller than minimum.
func (v yamlValue) intValue(key string, minimum int) (int, error) {
	s, err := v.scalarValue(key)
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(s))
	if convErr != nil {
		return 0, fmt.Errorf("line %d: %q must be a whole number, got %q", v.line, key, s)
	}
	if n < minimum {
		return 0, fmt.Errorf("line %d: %q must be at least %d, got %d", v.line, key, minimum, n)
	}
	return n, nil
}

// boolValue reads the YAML boolean spellings edilint accepts.
func (v yamlValue) boolValue(key string) (bool, error) {
	s, err := v.scalarValue(key)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("line %d: %q must be true or false, got %q", v.line, key, s)
	}
}

// seqValue reads a list of scalars.
func (v yamlValue) seqValue(key string) ([]yamlItem, error) {
	if v.kind == yamlScalar && v.scalar == "" {
		return nil, nil
	}
	if v.kind != yamlSequence {
		return nil, fmt.Errorf("line %d: %q takes a list, written as indented \"- entry\" lines", v.line, key)
	}
	return v.seq, nil
}

// mapValue reads a mapping of scalars.
func (v yamlValue) mapValue(key string) ([]yamlPair, error) {
	if v.kind == yamlScalar && v.scalar == "" {
		return nil, nil
	}
	if v.kind != yamlMapping {
		return nil, fmt.Errorf("line %d: %q takes indented \"rule: value\" entries", v.line, key)
	}
	return v.pairs, nil
}
