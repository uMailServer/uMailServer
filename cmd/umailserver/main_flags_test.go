package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

// F5620: flags after the positional argument must still be parsed.
func TestParseInterspersed(t *testing.T) {
	cases := []struct {
		args     []string
		pos      []string
		config   string
		password string
	}{
		{[]string{"/backup", "--config", "c.yaml", "--password", "pw"}, []string{"/backup"}, "c.yaml", "pw"},
		{[]string{"--config", "c.yaml", "/backup", "--password", "pw"}, []string{"/backup"}, "c.yaml", "pw"},
		{[]string{"--password=pw", "/backup"}, []string{"/backup"}, "def", "pw"},
		{[]string{"a", "b", "--config", "c.yaml"}, []string{"a", "b"}, "c.yaml", ""},
		{[]string{"--", "--config", "x"}, []string{"--config", "x"}, "def", ""},
		{nil, nil, "def", ""},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		config := fs.String("config", "def", "")
		password := fs.String("password", "", "")
		pos := parseInterspersed(fs, c.args)
		if !reflect.DeepEqual(pos, c.pos) || *config != c.config || *password != c.password {
			t.Errorf("args %q: pos=%q config=%q password=%q, want %q %q %q", c.args, pos, *config, *password, c.pos, c.config, c.password)
		}
	}
}
