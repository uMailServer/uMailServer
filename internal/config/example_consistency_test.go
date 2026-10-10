package config

import (
	"bytes"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// yamlPaths collects every yaml key path reachable from a struct type.
func yamlPaths(t reflect.Type, prefix string, out map[string]bool) {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if name == "-" || name == "" {
			continue
		}
		p := prefix + name
		out[p] = true
		yamlPaths(f.Type, p+".", out)
	}
}

func nodePaths(n *yaml.Node, prefix string, out map[string]bool) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			nodePaths(c, prefix, out)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			p := prefix + n.Content[i].Value
			out[p] = true
			nodePaths(n.Content[i+1], p+".", out)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			nodePaths(c, prefix, out)
		}
	}
}

// TestExampleConfigMatchesStruct fails on unknown keys in
// umailserver.yaml.example (strict decode) and on Config yaml keys that the
// example does not document (commented-out "# key:" lines count as documented).
func TestExampleConfigMatchesStruct(t *testing.T) {
	data, err := os.ReadFile("../../umailserver.yaml.example")
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("example has unknown/invalid keys: %v", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	nodePaths(&root, "", have)
	want := map[string]bool{}
	yamlPaths(reflect.TypeOf(Config{}), "", want)

	// Keys mentioned only in comments count as documented.
	text := string(data)
	var missing []string
	for p := range want {
		if have[p] {
			continue
		}
		leaf := p[strings.LastIndex(p, ".")+1:]
		if strings.Contains(text, "# "+leaf+":") || strings.Contains(text, "#"+leaf+":") {
			continue
		}
		missing = append(missing, p)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("Config keys missing from umailserver.yaml.example: %v", missing)
	}
}
