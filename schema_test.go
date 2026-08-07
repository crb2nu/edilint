package edilint

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These tests hold schema/report.v3.schema.json to the structs that produce
// the --json document. The schema file is the committed contract; a field
// added, removed or renamed in the structs without a matching schema change —
// and a version bump — fails here rather than in a consumer.

// schemaPath is the committed schema for the current SchemaVersion.
const schemaPath = "schema/report.v3.schema.json"

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", schemaPath, err)
	}
	return doc
}

// schemaObject navigates to a nested object by key path.
func schemaObject(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := doc
	for _, key := range path {
		next, ok := cur[key].(map[string]any)
		if !ok {
			t.Fatalf("schema has no object at %v (stopped at %q)", path, key)
		}
		cur = next
	}
	return cur
}

// jsonTagSets returns the JSON property names a struct marshals, and the
// subset that is always present because the field has no omitempty.
func jsonTagSets(t *testing.T, typ reflect.Type) (all, alwaysPresent map[string]bool) {
	t.Helper()
	all, alwaysPresent = map[string]bool{}, map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue // unexported fields never marshal
		}
		tag := field.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf("%s.%s has no usable json tag %q", typ.Name(), field.Name, tag)
		}
		all[name] = true
		if !strings.Contains(opts, "omitempty") {
			alwaysPresent[name] = true
		}
	}
	return all, alwaysPresent
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestSchemaMatchesStructs(t *testing.T) {
	doc := loadSchema(t)

	tests := []struct {
		name string
		typ  reflect.Type
		obj  map[string]any
	}{
		{"RunReport", reflect.TypeOf(RunReport{}), doc},
		{"Report", reflect.TypeOf(Report{}), schemaObject(t, doc, "$defs", "report")},
		{"Finding", reflect.TypeOf(Finding{}), schemaObject(t, doc, "$defs", "finding")},
		{"Summary", reflect.TypeOf(Summary{}), schemaObject(t, doc, "$defs", "summary")},
		{"RunSummary", reflect.TypeOf(RunSummary{}), schemaObject(t, doc, "$defs", "runSummary")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			all, alwaysPresent := jsonTagSets(t, tt.typ)

			props := map[string]bool{}
			for key := range schemaObject(t, tt.obj, "properties") {
				props[key] = true
			}
			if !reflect.DeepEqual(props, all) {
				t.Errorf("schema properties %v\n!= struct fields %v", sortedKeys(props), sortedKeys(all))
			}

			required := map[string]bool{}
			rawRequired, ok := tt.obj["required"].([]any)
			if !ok {
				t.Fatal("schema object has no required array")
			}
			for _, r := range rawRequired {
				required[r.(string)] = true
			}
			// A field without omitempty is always emitted, so the schema must
			// require it; a field with omitempty must not be required.
			if !reflect.DeepEqual(required, alwaysPresent) {
				t.Errorf("schema required %v\n!= always-present struct fields %v",
					sortedKeys(required), sortedKeys(alwaysPresent))
			}

			if strict, ok := tt.obj["additionalProperties"].(bool); !ok || strict {
				t.Error("additionalProperties must be false, or the version discipline is unenforceable")
			}
		})
	}
}

func TestSchemaVersionConstMatchesCode(t *testing.T) {
	doc := loadSchema(t)
	got, ok := schemaObject(t, doc, "properties", "version")["const"].(float64)
	if !ok || int(got) != SchemaVersion {
		t.Errorf("schema version const %v, want SchemaVersion %d; a new version needs a new schema file",
			schemaObject(t, doc, "properties", "version")["const"], SchemaVersion)
	}
	if !strings.Contains(schemaPath, "v3") {
		t.Errorf("schema file name %q should carry the version", schemaPath)
	}
}

func TestSchemaEnumsMatchConstants(t *testing.T) {
	doc := loadSchema(t)

	formats := schemaObject(t, doc, "$defs", "report", "properties", "format")["enum"].([]any)
	wantFormats := []string{
		string(FormatX12), string(FormatHL7v2), string(FormatEdifact),
		string(FormatDelimited), string(FormatFixed), string(FormatText),
	}
	if got := toStrings(formats); !sameSet(got, wantFormats) {
		t.Errorf("format enum %v != emitted formats %v", got, wantFormats)
	}

	severities := schemaObject(t, doc, "$defs", "finding", "properties", "severity")["enum"].([]any)
	wantSeverities := []string{string(SeverityError), string(SeverityWarning), string(SeverityInfo)}
	if got := toStrings(severities); !sameSet(got, wantSeverities) {
		t.Errorf("severity enum %v != severity constants %v", got, wantSeverities)
	}
}

func TestSchemaIDPatternMatchesCatalog(t *testing.T) {
	doc := loadSchema(t)
	pattern, ok := schemaObject(t, doc, "$defs", "finding", "properties", "id")["pattern"].(string)
	if !ok {
		t.Fatal("finding.id has no pattern")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("finding.id pattern does not compile: %v", err)
	}
	for _, rule := range Rules() {
		if !re.MatchString(rule.ID) {
			t.Errorf("catalog identifier %s does not match the schema pattern %s", rule.ID, pattern)
		}
	}
}

// TestSchemaAcceptsRealOutput type-checks a real document against the schema's
// property lists: every key the writer emits must be declared, at every level.
// It is not a full JSON Schema validation — that would need a dependency — but
// with additionalProperties pinned false and the property lists verified
// against the structs, an undeclared key is the drift that matters.
func TestSchemaAcceptsRealOutput(t *testing.T) {
	rr := NewRunReport()
	rr.Add(lintFixture(t, "835_envelope_broken.x12", Options{}))
	rr.Add(lintFixture(t, "835_clean.x12", Options{}))

	var buf strings.Builder
	if err := rr.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var emitted map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &emitted); err != nil {
		t.Fatalf("WriteJSON output is not valid JSON: %v", err)
	}

	doc := loadSchema(t)
	requireDeclared(t, "$", emitted, schemaObject(t, doc, "properties"), doc)
}

// requireDeclared walks an emitted object and fails on any key the schema's
// corresponding properties object does not declare.
func requireDeclared(t *testing.T, at string, emitted map[string]any, props map[string]any, doc map[string]any) {
	t.Helper()
	for key, value := range emitted {
		spec, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("%s.%s is emitted but not declared in the schema", at, key)
			continue
		}
		spec = resolveRef(t, spec, doc)

		switch v := value.(type) {
		case map[string]any:
			if childProps, ok := spec["properties"].(map[string]any); ok {
				requireDeclared(t, at+"."+key, v, childProps, doc)
			}
		case []any:
			items, ok := spec["items"].(map[string]any)
			if !ok {
				continue
			}
			items = resolveRef(t, items, doc)
			childProps, ok := items["properties"].(map[string]any)
			if !ok {
				continue
			}
			for i, elem := range v {
				if obj, ok := elem.(map[string]any); ok {
					requireDeclared(t, at+"."+key+"["+strconv.Itoa(i)+"]", obj, childProps, doc)
				}
			}
		}
	}
}

// resolveRef follows a local $ref of the form "#/$defs/name".
func resolveRef(t *testing.T, spec, doc map[string]any) map[string]any {
	t.Helper()
	ref, ok := spec["$ref"].(string)
	if !ok {
		return spec
	}
	name, ok := strings.CutPrefix(ref, "#/$defs/")
	if !ok {
		t.Fatalf("unsupported $ref %q", ref)
	}
	return schemaObject(t, doc, "$defs", name)
}

func toStrings(in []any) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.(string)
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}
