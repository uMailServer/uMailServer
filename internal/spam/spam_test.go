package spam

import (
	"slices"
	"testing"
)

func TestTokenizer_Tokenize(t *testing.T) {
	tokenizer := NewTokenizer()

	tests := []struct {
		name  string
		input string
		want  int // minimum expected tokens
	}{
		{
			name:  "simple text",
			input: "Hello world this is a test email",
			want:  5,
		},
		{
			name:  "with numbers",
			input: "Order 12345 confirmed",
			want:  2, // 12345 is filtered as all digits
		},
		{
			name:  "short words filtered",
			input: "a b c d e f g",
			want:  0,
		},
		{
			name:  "mixed case",
			input: "Hello HELLO hello",
			want:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.Tokenize(tt.input)
			if len(tokens) < tt.want {
				t.Errorf("Tokenize(%q) = %v (len %d), want at least %d", tt.input, tokens, len(tokens), tt.want)
			}
		})
	}
}

func TestTokenizer_Tokenize_Bigrams(t *testing.T) {
	tokenizer := NewTokenizer()

	tokens := tokenizer.Tokenize("hello world test email")
	// Should have unigrams: hello, world, test, email (4 tokens, but hello/world/test might be filtered as short)
	// and bigrams: "hello world", "world test", "test email"
	if len(tokens) < 2 {
		t.Errorf("Tokenize() should produce at least 2 tokens (including bigrams), got %d", len(tokens))
	}
}

func TestTokenizer_isValidToken(t *testing.T) {
	tokenizer := NewTokenizer()

	tests := []struct {
		word string
		want bool
	}{
		{"hello", true},
		{"ab", false},  // too short
		{"a", false},   // too short
		{"123", false}, // all digits
		{"hello123", true},
		{"123hello", true},
	}

	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			if got := tokenizer.isValidToken(tt.word); got != tt.want {
				t.Errorf("isValidToken(%q) = %v, want %v", tt.word, got, tt.want)
			}
		})
	}
}

func TestExtractTokensFromHeaders(t *testing.T) {
	headers := map[string][]string{
		"Subject": {"Test Subject"},
		"From":    {"sender@example.com"},
		"To":      {"recipient@example.com"},
	}

	tokens := ExtractTokensFromHeaders(headers)

	if len(tokens) == 0 {
		t.Error("ExtractTokensFromHeaders() returned no tokens")
	}

	// Check that subject tokens are extracted
	found := false
	for _, token := range tokens {
		if token == "test" || token == "subject" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ExtractTokensFromHeaders() did not extract subject tokens")
	}
}

func TestExtractTokensFromBody(t *testing.T) {
	body := []byte("This is a test email body with some content.")

	tokens := ExtractTokensFromBody(body)

	if len(tokens) == 0 {
		t.Error("ExtractTokensFromBody() returned no tokens")
	}
}

func TestExtractTokensFromHeaders_SpecialCases(t *testing.T) {
	headers := map[string][]string{
		"Content-Type": {"text/html; charset=utf-8"},
		"Date":         {"Mon, 01 Jan 2024 12:00:00 +0000"},
	}

	tokens := ExtractTokensFromHeaders(headers)

	// Should still produce tokens from content-type and date
	if len(tokens) == 0 {
		t.Error("ExtractTokensFromHeaders() should extract tokens from Content-Type and Date")
	}
}

func TestExtractEmails(t *testing.T) {
	tests := []struct {
		input string
		want  int // expected email count
	}{
		{"user@example.com", 1},
		{"<user@example.com>", 1},
		{"", 0},
		{"invalid", 0},
		{"user@", 0},
		{"@domain.com", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractEmails(tt.input)
			if len(got) != tt.want {
				t.Errorf("extractEmails(%q) = %v (len %d), want %d", tt.input, got, len(got), tt.want)
			}
		})
	}
}

func TestTokenProbability(t *testing.T) {
	tests := []struct {
		name      string
		hamCount  uint32
		spamCount uint32
		totalHam  uint64
		totalSpam uint64
		min, max  float64 // expected range
	}{
		{
			name:     "equal counts",
			hamCount: 10, spamCount: 10,
			totalHam: 100, totalSpam: 100,
			min: 0.4, max: 0.6,
		},
		{
			name:     "more spam",
			hamCount: 5, spamCount: 20,
			totalHam: 100, totalSpam: 100,
			min: 0.6, max: 0.9,
		},
		{
			name:     "more ham",
			hamCount: 20, spamCount: 5,
			totalHam: 100, totalSpam: 100,
			min: 0.1, max: 0.4,
		},
		{
			name:     "zero counts (smoothing)",
			hamCount: 0, spamCount: 0,
			totalHam: 100, totalSpam: 100,
			min: 0.4, max: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prob := tokenProbability(tt.hamCount, tt.spamCount, tt.totalHam, tt.totalSpam)
			if prob < tt.min || prob > tt.max {
				t.Errorf("tokenProbability() = %v, want between %v and %v", prob, tt.min, tt.max)
			}
		})
	}
}

func TestCombinedProbability(t *testing.T) {
	tests := []struct {
		name  string
		probs []float64
		min   float64
		max   float64
	}{
		{
			name:  "empty",
			probs: []float64{},
			min:   0.5, max: 0.5,
		},
		{
			name:  "all spam indicators",
			probs: []float64{0.9, 0.9, 0.8, 0.85},
			min:   0.7, max: 0.99,
		},
		{
			name:  "all ham indicators",
			probs: []float64{0.1, 0.15, 0.2, 0.1},
			min:   0.01, max: 0.3,
		},
		{
			name:  "mixed",
			probs: []float64{0.5, 0.5, 0.5, 0.5},
			min:   0.4, max: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prob := CombinedProbability(tt.probs)
			if prob < tt.min || prob > tt.max {
				t.Errorf("CombinedProbability(%v) = %v, want between %v and %v", tt.probs, prob, tt.min, tt.max)
			}
		})
	}
}

func TestCombinedProbability_EdgeCases(t *testing.T) {
	// Test extreme values
	extremeProbs := []float64{0.0001, 0.0001, 0.0001}
	prob := CombinedProbability(extremeProbs)
	if prob < 0 {
		t.Errorf("CombinedProbability() with extreme low values = %v, should not be negative", prob)
	}

	extremeHigh := []float64{0.9999, 0.9999, 0.9999}
	prob = CombinedProbability(extremeHigh)
	if prob > 1 {
		t.Errorf("CombinedProbability() with extreme high values = %v, should not exceed 1", prob)
	}
}

func TestClassifier_Classify_NilDB(t *testing.T) {
	classifier := NewClassifier(nil)

	result, err := classifier.Classify([]string{"hello", "world"})
	if err != nil {
		t.Errorf("Classify() error = %v, want nil", err)
	}
	if result.SpamProbability != 0.5 {
		t.Errorf("Classify() with nil DB SpamProbability = %v, want 0.5", result.SpamProbability)
	}
	if result.IsSpam {
		t.Error("Classify() with nil DB IsSpam = true, want false")
	}
}

func TestClassifier_Classify_EmptyTokens(t *testing.T) {
	classifier := NewClassifier(nil)

	result, err := classifier.Classify([]string{})
	if err != nil {
		t.Errorf("Classify() error = %v, want nil", err)
	}
	if result.SpamProbability != 0.5 {
		t.Errorf("Classify() with empty tokens SpamProbability = %v, want 0.5", result.SpamProbability)
	}
}

func TestFeatureExtractor_Extract(t *testing.T) {
	fe := NewFeatureExtractor()

	headers := map[string][]string{
		"Subject":      {"URGENT: Click here now!"},
		"Content-Type": {"text/html"},
	}
	body := []byte("Act now limited time offer click here")

	features := fe.Extract(headers, body)

	if len(features) == 0 {
		t.Error("Extract() returned no features")
	}

	// Check for special features
	found := false
	for _, f := range features {
		if f == "suspicious_phrase_click_here" || f == "excessive_exclamation" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Extract() did not detect special features")
	}
}

func TestFeatureExtractor_addSpecialFeatures(t *testing.T) {
	fe := NewFeatureExtractor()

	tests := []struct {
		name           string
		headers        map[string][]string
		body           []byte
		expectedTokens []string
	}{
		{
			name:           "click here phrase",
			headers:        map[string][]string{},
			body:           []byte("Please click here to claim"),
			expectedTokens: []string{"suspicious_phrase_click_here"},
		},
		{
			name:           "act now phrase",
			headers:        map[string][]string{},
			body:           []byte("Act now while supplies last"),
			expectedTokens: []string{"suspicious_phrase_act_now"},
		},
		{
			name:           "exclamation in subject",
			headers:        map[string][]string{"Subject": {"Hello!!!"}},
			body:           []byte("Test body"),
			expectedTokens: []string{"excessive_exclamation"},
		},
		{
			name:           "money symbol in subject",
			headers:        map[string][]string{"Subject": {"Your $1000 prize"}},
			body:           []byte("Test"),
			expectedTokens: []string{"money_symbol_subject"},
		},
		{
			name:           "html email",
			headers:        map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
			body:           []byte("Test body"),
			expectedTokens: []string{"html_only_email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features := fe.addSpecialFeatures(tt.headers, tt.body)
			for _, expected := range tt.expectedTokens {
				if !slices.Contains(features, expected) {
					t.Errorf("addSpecialFeatures() missing expected token %q", expected)
				}
			}
		})
	}
}

func TestNewClassifier(t *testing.T) {
	classifier := NewClassifier(nil)
	if classifier == nil {
		t.Fatal("NewClassifier() returned nil")
	}
	if classifier.tokenizer == nil {
		t.Error("NewClassifier() tokenizer is nil")
	}
}

func TestNewFeatureExtractor(t *testing.T) {
	fe := NewFeatureExtractor()
	if fe == nil {
		t.Fatal("NewFeatureExtractor() returned nil")
	}
	if fe.tokenizer == nil {
		t.Error("NewFeatureExtractor() tokenizer is nil")
	}
}
