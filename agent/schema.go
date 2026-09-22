package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
)

// Schema is the JSON schema subset generated from struct tags. It supports
// object, string, integer, number, boolean, and array-of-T plus nested
// structs — enough for tool arguments, deliberately not the full
// specification.
type Schema struct {
	Type        string             `json:"type"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
}

// SchemaFor builds the schema of one arguments struct. Field names come
// from `json` tags; the `jsonschema` tag accepts `required` and
// `description=...` tokens, comma-separated. `description=` must come last:
// everything after it, commas included, is the description text.
func SchemaFor(v any) (*Schema, error) {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("agent: schema source must be a struct, got %s", t)
	}
	return schemaForStruct(t)
}

// MustSchemaFor is SchemaFor for statically known argument structs; it
// panics on programmer error the way regexp.MustCompile does.
func MustSchemaFor(v any) *Schema {
	s, err := SchemaFor(v)
	if err != nil {
		panic(err)
	}
	return s
}

func schemaForStruct(t reflect.Type) (*Schema, error) {
	s := &Schema{Type: "object", Properties: map[string]*Schema{}}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			head := strings.Split(tag, ",")[0]
			if head == "-" {
				continue
			}
			if head != "" {
				name = head
			}
		}
		fs, err := schemaForType(f.Type)
		if err != nil {
			return nil, err
		}
		spec := f.Tag.Get("jsonschema")
		if idx := strings.Index(spec, "description="); idx >= 0 {
			fs.Description = strings.TrimSpace(spec[idx+len("description="):])
			spec = spec[:idx]
		}
		for _, tok := range strings.Split(spec, ",") {
			tok = strings.TrimSpace(tok)
			switch tok {
			case "":
			case "required":
				s.Required = append(s.Required, name)
			default:
				return nil, fmt.Errorf("agent: field %s: unknown jsonschema token %q", f.Name, tok)
			}
		}
		s.Properties[name] = fs
	}
	return s, nil
}

func schemaForType(t reflect.Type) (*Schema, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}, nil
	case reflect.Bool:
		return &Schema{Type: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}, nil
	case reflect.Slice, reflect.Array:
		items, err := schemaForType(t.Elem())
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "array", Items: items}, nil
	case reflect.Struct:
		return schemaForStruct(t)
	case reflect.Map:
		return &Schema{Type: "object"}, nil
	default:
		return nil, fmt.Errorf("agent: unsupported type %s", t)
	}
}

// Validate checks one arguments document against a schema. Empty input is
// treated as an empty object.
func Validate(schema *Schema, args json.RawMessage) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return validateValue(schema, v, "")
}

func validateValue(s *Schema, v any, path string) error {
	if s == nil {
		return nil
	}
	where := path
	if where == "" {
		where = "arguments"
	}
	switch s.Type {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want object", where)
		}
		for _, req := range s.Required {
			if _, ok := m[req]; !ok {
				return fmt.Errorf("%s: missing required property %q", where, req)
			}
		}
		for name, sub := range s.Properties {
			if mv, ok := m[name]; ok {
				if err := validateValue(sub, mv, join(path, name)); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s: want string", where)
		}
	case "integer":
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) {
			return fmt.Errorf("%s: want integer", where)
		}
	case "number":
		if _, ok := v.(float64); !ok {
			return fmt.Errorf("%s: want number", where)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: want boolean", where)
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: want array", where)
		}
		for i, item := range arr {
			if err := validateValue(s.Items, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
