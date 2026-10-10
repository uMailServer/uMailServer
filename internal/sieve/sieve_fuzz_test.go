package sieve

import (
	"testing"
	"time"
)

const fuzzBudget = 2 * time.Second

func fuzzTimed(t *testing.T, what string, fn func()) {
	t.Helper()
	start := time.Now()
	fn()
	if d := time.Since(start); d > fuzzBudget {
		t.Fatalf("%s took %v (> %v)", what, d, fuzzBudget)
	}
}

var sieveSeeds = []string{
	``,
	`keep;`,
	`require ["fileinto"]; if header :contains "subject" "x" { fileinto "A"; } else { discard; }`,
	`require ["vacation"]; vacation :days 1 :subject "s" "body";`,
	`if allof(anyof(true,false), not exists "x") { stop; } elsif size :over 100K { keep; }`,
	"text:\nhello\n.\n;",
	`if header :matches "from" "*a?b*" { reject "no"; }`,
	`/* unterminated`,
	`# comment` + "\n" + `if true {`,
	`"\`,
	`if header :regex "subject" "(a+)+$" { keep; }`,
	`if header :matches "subject" "*a*a*a*a*a*a*a*a*b" { keep; }`,
}

func FuzzSieveParse(f *testing.F) {
	for _, s := range sieveSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, script string) {
		fuzzTimed(t, "parse", func() {
			sc, err := NewParser(script).Parse()
			if err == nil && sc == nil {
				t.Fatal("nil script without error")
			}
			if sc != nil {
				_ = sc.String()
				_ = CheckRequires(sc)
			}
		})
	})
}

func FuzzSieveExecute(f *testing.F) {
	for _, s := range sieveSeeds {
		f.Add(s, "subject", "hello aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "body")
	}
	f.Fuzz(func(t *testing.T, script, hname, hval, body string) {
		fuzzTimed(t, "execute", func() {
			msg := &MessageContext{
				From:    "a@example.com",
				To:      []string{"b@example.com"},
				Headers: map[string][]string{hname: {hval}, "Subject": {hval}, "From": {"a@example.com"}},
				Body:    []byte(body),
				Size:    int64(len(body)),
			}
			_, _ = ExecuteScript(script, msg)
		})
	})
}
