package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

type schemaSample struct {
	Name    string   `json:"name" jsonschema:"required,description=The name"`
	Count   int      `json:"count"`
	Ratio   float64  `json:"ratio"`
	Verbose bool     `json:"verbose"`
	Tags    []string `json:"tags"`
	Nested  struct {
		Path string `json:"path" jsonschema:"required"`
	} `json:"nested"`
}

func TestSchemaFor(t *testing.T) {
	s := MustSchemaFor(&schemaSample{})
	if s.Type != "object" {
		t.Fatalf("type = %q", s.Type)
	}
	if len(s.Required) != 1 || s.Required[0] != "name" {
		t.Fatalf("required = %v", s.Required)
	}
	name := s.Properties["name"]
	if name.Type != "string" || name.Description != "The name" {
		t.Fatalf("name property = %+v", name)
	}
	if s.Properties["count"].Type != "integer" {
		t.Fatalf("count property = %+v", s.Properties["count"])
	}
	if s.Properties["ratio"].Type != "number" {
		t.Fatalf("ratio property = %+v", s.Properties["ratio"])
	}
	if s.Properties["verbose"].Type != "boolean" {
		t.Fatalf("verbose property = %+v", s.Properties["verbose"])
	}
	tags := s.Properties["tags"]
	if tags.Type != "array" || tags.Items.Type != "string" {
		t.Fatalf("tags property = %+v", tags)
	}
	nested := s.Properties["nested"]
	if nested.Type != "object" || len(nested.Required) != 1 || nested.Required[0] != "path" {
		t.Fatalf("nested property = %+v", nested)
	}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"type":"object"`, `"required":["name"]`, `"items":{"type":"string"}`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("marshaled schema missing %s: %s", want, raw)
		}
	}
}

func TestSchemaForRejectsUnsupported(t *testing.T) {
	type bad struct {
		Ch chan int `json:"ch"`
	}
	if _, err := SchemaFor(&bad{}); err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("want unsupported-type error, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	s := MustSchemaFor(&schemaSample{})

	cases := []struct {
		name string
		args string
		ok   bool
	}{
		{"valid", `{"name":"x","nested":{"path":"a"}}`, true},
		{"extra property allowed", `{"name":"x","nested":{"path":"a"},"extra":1}`, true},
		{"empty args means empty object", ``, false},
		{"missing required", `{"nested":{"path":"a"}}`, false},
		{"wrong scalar type", `{"name":5,"nested":{"path":"a"}}`, false},
		{"integer given string", `{"name":"x","count":"3","nested":{"path":"a"}}`, false},
		{"float as integer", `{"name":"x","count":1.5,"nested":{"path":"a"}}`, false},
		{"nested missing required", `{"name":"x","nested":{}}`, false},
		{"array item type", `{"name":"x","nested":{"path":"a"},"tags":[1,2]}`, false},
		{"array valid", `{"name":"x","nested":{"path":"a"},"tags":["a","b"]}`, true},
		{"invalid json", `{`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(s, json.RawMessage(tc.args))
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
