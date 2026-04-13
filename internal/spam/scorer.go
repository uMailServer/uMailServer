package spam

import (
	"strings"
)

// FeatureExtractor extracts features from a message for Bayesian classification
type FeatureExtractor struct {
	tokenizer *Tokenizer
}

// NewFeatureExtractor creates a new feature extractor
func NewFeatureExtractor() *FeatureExtractor {
	return &FeatureExtractor{
		tokenizer: NewTokenizer(),
	}
}

// Extract extracts all features from a message
func (fe *FeatureExtractor) Extract(headers map[string][]string, body []byte) []string {
	var features []string

	// Extract from headers
	headerFeatures := ExtractTokensFromHeaders(headers)
	features = append(features, headerFeatures...)

	// Extract from body
	bodyFeatures := ExtractTokensFromBody(body)
	features = append(features, bodyFeatures...)

	// Add special features
	features = append(features, fe.addSpecialFeatures(headers, body)...)

	return features
}

// addSpecialFeatures adds special heuristic features
func (fe *FeatureExtractor) addSpecialFeatures(headers map[string][]string, body []byte) []string {
	var features []string

	// Check for suspicious patterns
	bodyStr := strings.ToLower(string(body))

	// URL features
	if strings.Contains(bodyStr, "click here") {
		features = append(features, "suspicious_phrase_click_here")
	}
	if strings.Contains(bodyStr, "act now") {
		features = append(features, "suspicious_phrase_act_now")
	}
	if strings.Contains(bodyStr, "limited time") {
		features = append(features, "suspicious_phrase_limited_time")
	}

	// Exclamation-heavy subject
	if subjects, ok := headers["Subject"]; ok && len(subjects) > 0 {
		subject := subjects[0]
		if strings.Count(subject, "!") > 2 {
			features = append(features, "excessive_exclamation")
		}
		if strings.Count(subject, "$") > 0 {
			features = append(features, "money_symbol_subject")
		}
	}

	// HTML-only email
	if contentType, ok := headers["Content-Type"]; ok {
		if len(contentType) > 0 && strings.Contains(contentType[0], "text/html") {
			features = append(features, "html_only_email")
		}
	}

	return features
}
