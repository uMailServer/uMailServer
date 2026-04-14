package spam

import (
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func TestClassifier_Initialize(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "spam_test.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	err = db.View(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(SpamBucket)) == nil {
			t.Error("expected SpamBucket to exist")
		}
		if tx.Bucket([]byte(HamBucket)) == nil {
			t.Error("expected HamBucket to exist")
		}
		if tx.Bucket([]byte(StatsBucket)) == nil {
			t.Error("expected StatsBucket to exist")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
}

func TestClassifier_Initialize_NilDB(t *testing.T) {
	classifier := NewClassifier(nil)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() with nil DB error = %v", err)
	}
}

func TestClassifier_TrainSpam(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "spam_train.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	tokens := tokenizer.Tokenize("URGENT: Click here to claim your prize!!!")
	if err := classifier.TrainSpam(tokens); err != nil {
		t.Fatalf("TrainSpam() error = %v", err)
	}

	totalHam, totalSpam, err := classifier.GetTotalCounts()
	if err != nil {
		t.Fatalf("GetTotalCounts() error = %v", err)
	}
	if totalSpam == 0 {
		t.Error("expected totalSpam > 0 after training")
	}
	t.Logf("totalHam=%d, totalSpam=%d", totalHam, totalSpam)
}

func TestClassifier_TrainHam(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ham_train.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	tokens := tokenizer.Tokenize("Hi John, can we meet tomorrow at 3pm?")
	t.Logf("Tokens: %v (count=%d)", tokens, len(tokens))

	err = classifier.TrainHam(tokens)
	if err != nil {
		t.Fatalf("TrainHam() error = %v", err)
	}

	// Verify tokens are in the bucket directly
	_ = db.View(func(tx *bbolt.Tx) error {
		hamBucket := tx.Bucket([]byte(HamBucket))
		if hamBucket == nil {
			t.Fatal("expected HamBucket to exist")
		}
		count := countAllTokens(hamBucket)
		t.Logf("HamBucket direct count = %d", count)
		if count == 0 {
			t.Error("expected count > 0 after training")
		}
		return nil
	})
}

func TestClassifier_GetTokenFrequency(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "freq.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	_ = classifier.TrainHam(tokenizer.Tokenize("hello world test email"))
	_ = classifier.TrainHam(tokenizer.Tokenize("hello world another email"))

	hamCount, spamCount, err := classifier.GetTokenFrequency("hello")
	if err != nil {
		t.Fatalf("GetTokenFrequency() error = %v", err)
	}
	if hamCount == 0 {
		t.Error("expected hamCount > 0 for trained token")
	}
	if spamCount != 0 {
		t.Error("expected spamCount = 0 for ham-only token")
	}

	hamCount, spamCount, err = classifier.GetTokenFrequency("unknowntoken123")
	if err != nil {
		t.Fatalf("GetTokenFrequency() error = %v", err)
	}
	if hamCount != 0 || spamCount != 0 {
		t.Error("expected zero counts for unknown token")
	}
}

func TestClassifier_TrainFromEmail(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "from_email.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	headers := map[string][]string{
		"Subject": {"Test Email"},
		"From":    {"sender@example.com"},
	}
	body := []byte("This is a test email body")

	err = classifier.TrainFromEmail(true, headers, body)
	if err != nil {
		t.Fatalf("TrainFromEmail() error = %v", err)
	}

	_, totalSpam, err := classifier.GetTotalCounts()
	if err != nil {
		t.Fatalf("GetTotalCounts() error = %v", err)
	}
	if totalSpam == 0 {
		t.Error("expected totalSpam > 0 after training")
	}
}

func TestClassifier_TrainFromEmail_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	err = classifier.TrainFromEmail(true, nil, nil)
	if err != nil {
		t.Fatalf("TrainFromEmail() with nil inputs error = %v", err)
	}
}

func TestClassifier_Train_DuplicateTokens(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "dup.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	_ = classifier.TrainHam(tokenizer.Tokenize("hello world"))
	_ = classifier.TrainHam(tokenizer.Tokenize("hello world"))

	hamCount, _, err := classifier.GetTokenFrequency("hello")
	if err != nil {
		t.Fatalf("GetTokenFrequency() error = %v", err)
	}
	if hamCount < 2 {
		t.Errorf("expected hamCount >= 2, got %d", hamCount)
	}
}

func TestTokenKey(t *testing.T) {
	key := tokenKey("test")
	if string(key) != "test" {
		t.Errorf("expected 'test', got %s", string(key))
	}
}

func TestUpdateStats(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "stats.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := classifier.UpdateStats(); err != nil {
		t.Fatalf("UpdateStats() error = %v", err)
	}

	totalHam, totalSpam, err := classifier.GetTotalCounts()
	if err != nil {
		t.Fatalf("GetTotalCounts() error = %v", err)
	}
	t.Logf("After UpdateStats: totalHam=%d, totalSpam=%d", totalHam, totalSpam)
}

func TestClassifier_Classify_WithTraining(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "classify.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	classifier.TrainHam(tokenizer.Tokenize("Hello John, can we meet tomorrow for the meeting?"))
	classifier.TrainSpam(tokenizer.Tokenize("URGENT! Click here to claim your prize now!!!"))

	result, err := classifier.Classify([]string{"hello", "world"})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	t.Logf("Classify result: spamProb=%.3f, isSpam=%v", result.SpamProbability, result.IsSpam)

	result2, err := classifier.Classify([]string{"urgent", "click", "prize"})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	t.Logf("Classify result2: spamProb=%.3f, isSpam=%v", result2.SpamProbability, result2.IsSpam)
}

func TestClassifier_IncrementToken(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "increment.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := classifier.IncrementToken(SpamBucket, "testword", 5); err != nil {
		t.Fatalf("IncrementToken() error = %v", err)
	}

	_, spamCount, err := classifier.GetTokenFrequency("testword")
	if err != nil {
		t.Fatalf("GetTokenFrequency() error = %v", err)
	}
	if spamCount < 5 {
		t.Errorf("expected spamCount >= 5, got %d", spamCount)
	}
}

func TestClassifier_IncrementToken_Ham(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "increment_ham.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := classifier.IncrementToken(HamBucket, "hamword", 3); err != nil {
		t.Fatalf("IncrementToken() error = %v", err)
	}

	hamCount, _, err := classifier.GetTokenFrequency("hamword")
	if err != nil {
		t.Fatalf("GetTokenFrequency() error = %v", err)
	}
	if hamCount < 3 {
		t.Errorf("expected hamCount >= 3, got %d", hamCount)
	}
}

func TestClassifier_IncrementToken_NilDB(t *testing.T) {
	classifier := NewClassifier(nil)
	if err := classifier.IncrementToken(SpamBucket, "word", 1); err != nil {
		t.Fatalf("IncrementToken() with nil DB error = %v", err)
	}
}

func TestGetTotalCounts_NilDB(t *testing.T) {
	classifier := NewClassifier(nil)
	ham, spam, err := classifier.GetTotalCounts()
	if err != nil {
		t.Fatalf("GetTotalCounts() error = %v", err)
	}
	if ham != 1 || spam != 1 {
		t.Errorf("expected 1, 1 for nil DB, got %d, %d", ham, spam)
	}
}

func TestGetTotalCounts_EmptyDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty_counts.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	totalHam, totalSpam, err := classifier.GetTotalCounts()
	if err != nil {
		t.Fatalf("GetTotalCounts() error = %v", err)
	}
	t.Logf("Empty DB counts: totalHam=%d, totalSpam=%d", totalHam, totalSpam)
}

func TestExtractTokensFromBody_NilBody(t *testing.T) {
	tokens := ExtractTokensFromBody(nil)
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for nil body, got %d", len(tokens))
	}
}

func TestExtractTokensFromBody_EmptyBody(t *testing.T) {
	tokens := ExtractTokensFromBody([]byte{})
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for empty body, got %d", len(tokens))
	}
}

func TestExtractEmails_FromText(t *testing.T) {
	// The extractEmails function extracts from text in format "user@domain.com" or "<user@domain.com>"
	// It only handles single email per call
	emails := extractEmails("support@example.com")
	if len(emails) < 1 {
		t.Errorf("expected at least 1 email, got %d", len(emails))
	}

	emails2 := extractEmails("<user@company.org>")
	if len(emails2) < 1 {
		t.Errorf("expected at least 1 email from bracketed format, got %d", len(emails2))
	}
}

func TestTokenizer_Normalize(t *testing.T) {
	tokenizer := NewTokenizer()
	tests := []struct {
		input string
		want  string
	}{
		{"HELLO", "hello"},
		{"Hello", "hello"},
		{"hello", "hello"},
		{"TEST123", "test123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tokenizer.normalize(tt.input)
			if result != tt.want {
				t.Errorf("normalize(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestTokenizer_Normalize_Empty(t *testing.T) {
	tokenizer := NewTokenizer()
	result := tokenizer.normalize("")
	if result != "" {
		t.Errorf("normalize('') = %q, want ''", result)
	}
}

func TestClassifier_Classify_HighSpamScore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "highspam.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	for i := 0; i < 10; i++ {
		_ = classifier.TrainSpam(tokenizer.Tokenize("URGENT click here prize winner claim now"))
	}
	_ = classifier.TrainHam(tokenizer.Tokenize("hello meeting tomorrow"))

	result, err := classifier.Classify([]string{"urgent", "click", "prize", "winner"})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	t.Logf("SpamProbability = %.3f, isSpam = %v", result.SpamProbability, result.IsSpam)
}

func TestClassifier_Classify_HighHamScore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "highham.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	for i := 0; i < 10; i++ {
		classifier.TrainHam(tokenizer.Tokenize("Hello John, can we meet tomorrow for the meeting?"))
	}
	classifier.TrainSpam(tokenizer.Tokenize("URGENT click here prize"))

	result, err := classifier.Classify([]string{"hello", "john", "meeting"})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	t.Logf("SpamProbability = %.3f, isSpam = %v", result.SpamProbability, result.IsSpam)
}

func TestCountAllTokens(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "count.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	classifier := NewClassifier(db)
	if err := classifier.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	tokenizer := NewTokenizer()
	classifier.TrainHam(tokenizer.Tokenize("one two three four five"))

	// Access via view
	err = db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(HamBucket))
		if bucket == nil {
			t.Fatal("expected HamBucket to exist")
		}
		count := countAllTokens(bucket)
		if count == 0 {
			t.Error("expected count > 0 after training")
		}
		t.Logf("countAllTokens = %d", count)
		return nil
	})
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
}
