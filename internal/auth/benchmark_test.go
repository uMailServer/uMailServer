package auth

import (
	"testing"
)

func BenchmarkParseSequenceSet(b *testing.B) {
	sets := []string{
		"1",
		"1:10",
		"1,3,5",
		"1:5,10:15,20",
		"*",
	}

	for i := 0; i < b.N; i++ {
		for _, set := range sets {
			_, _ = ParseSequenceSet(set)
		}
	}
}

func BenchmarkDKIMResultString(b *testing.B) {
	results := []DKIMResult{DKIMNone, DKIMPass, DKIMFail, DKIMPERMError, DKIMTempError}

	for i := 0; i < b.N; i++ {
		for _, r := range results {
			_ = r.String()
		}
	}
}

func BenchmarkSPFResultString(b *testing.B) {
	results := []SPFResult{SPFNone, SPFPass, SPFFail, SPFSoftFail, SPFTempError, SPFPermError}

	for i := 0; i < b.N; i++ {
		for _, r := range results {
			_ = r.String()
		}
	}
}

func BenchmarkDMARCResultString(b *testing.B) {
	results := []DMARCResult{DMARCNone, DMARCPass, DMARCFail}

	for i := 0; i < b.N; i++ {
		for _, r := range results {
			_ = r.String()
		}
	}
}
