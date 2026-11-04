package v1

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const schemaPath = "../../../../static/schemas/v1/hauler-manifest.json"

type schemaProperty struct {
	Type  string `json:"type"`
	Items *struct {
		Type string `json:"type"`
		Ref  string `json:"$ref"`
	} `json:"items"`
}

type schemaDef struct {
	Properties map[string]schemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

// schemaType is the JSON Schema type keyword expected for a Go field kind, plus the type
// its array items must carry ("$ref" for a slice of structs). An unlisted kind fails the
// test on purpose, so a new field type can't slip in untyped.
func schemaType(t *testing.T, f reflect.StructField) (typ, itemTyp string) {
	switch f.Type.Kind() {
	case reflect.String:
		return "string", ""
	case reflect.Bool:
		return "boolean", ""
	case reflect.Int, reflect.Int32, reflect.Int64:
		return "integer", ""
	case reflect.Slice:
		switch f.Type.Elem().Kind() {
		case reflect.String:
			return "array", "string"
		case reflect.Struct:
			return "array", "$ref"
		default:
			t.Fatalf("field %s is a slice of %s... teach schemaType how to map it", f.Name, f.Type.Elem().Kind())
		}
		return "", ""
	default:
		t.Fatalf("field %s has unmapped kind %s... teach schemaType how to map it", f.Name, f.Type.Kind())
		return "", ""
	}
}

// TestSchemaMatchesTypes keeps static/schemas/v1/hauler-manifest.json in sync with the
// structs in this package. Manifests are decoded non-strictly, so the published schema is
// the only thing that catches a misspelled key, and a stale schema silently stops doing that.
func TestSchemaMatchesTypes(t *testing.T) {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("reading schema: %v", err)
	}

	var schema struct {
		Defs map[string]schemaDef `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parsing schema: %v", err)
	}

	cases := []struct {
		def string
		typ any
	}{
		{"imageSpec", ImageSpec{}},
		{"image", Image{}},
		{"chartSpec", ChartSpec{}},
		{"chart", Chart{}},
		{"fileSpec", FileSpec{}},
		{"file", File{}},
		{"directorySpec", DirectorySpec{}},
		{"directory", Directory{}},
		{"gitSpec", GitSpec{}},
		{"gitRepo", GitRepo{}},
	}

	for _, c := range cases {
		def, ok := schema.Defs[c.def]
		if !ok {
			t.Errorf("schema is missing $defs/%s", c.def)
			continue
		}

		var want []string
		for k := range def.Properties {
			want = append(want, k)
		}
		var got []string
		rt := reflect.TypeOf(c.typ)
		for i := range rt.NumField() {
			f := rt.Field(i)
			tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if tag == "" || tag == "-" {
				t.Errorf("%s field %s has no json tag", rt.Name(), f.Name)
				continue
			}
			got = append(got, tag)

			prop, ok := def.Properties[tag]
			if !ok {
				continue // reported by the name comparison below
			}

			typ, itemTyp := schemaType(t, f)
			if prop.Type != typ {
				t.Errorf("%s.%s is %s but $defs/%s/properties/%s has type %q, want %q",
					rt.Name(), f.Name, f.Type, c.def, tag, prop.Type, typ)
			}
			switch {
			case itemTyp == "$ref":
				if prop.Items == nil || prop.Items.Ref == "" {
					t.Errorf("$defs/%s/properties/%s needs items with a $ref to the item schema", c.def, tag)
				}
			case itemTyp != "":
				if prop.Items == nil || prop.Items.Type != itemTyp {
					t.Errorf("$defs/%s/properties/%s needs items of type %q", c.def, tag, itemTyp)
				}
			}
		}

		// Requiredness is schema-owned: omitempty on these structs only affects
		// marshaling, which nothing does, so it says nothing about what a manifest
		// must set (Chart.Name is omitempty but a nameless chart cannot resolve,
		// Image.Key is not omitempty but is optional). Only check for typos.
		for _, r := range def.Required {
			if _, ok := def.Properties[r]; !ok {
				t.Errorf("$defs/%s requires %q, which it does not declare as a property", c.def, r)
			}
		}

		sort.Strings(want)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s fields %v do not match schema $defs/%s properties %v... update %s",
				rt.Name(), got, c.def, want, schemaPath)
		}
	}
}
