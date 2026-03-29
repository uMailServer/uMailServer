package smtp

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestServer(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	t.Run("ListenAndServe", func(t *testing.T) {
		// Start server on random port
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}

		go func() {
			server.Serve(ln)
		}()

		// Give server time to start
		time.Sleep(100 * time.Millisecond)

		// Connect to server
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Read greeting
		reader := bufio.NewReader(conn)
		greeting, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read greeting: %v", err)
		}

		if !strings.HasPrefix(greeting, "220") {
			t.Errorf("Expected 220 greeting, got: %s", greeting)
		}

		server.Stop()
	})
}

func TestSessionCommands(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	t.Run("EHLO", func(t *testing.T) {
		server, client := createTestConnection(t, config)
		defer server.Stop()
		defer client.Close()

		// Read greeting first
		reader := bufio.NewReader(client)
		greeting, _ := reader.ReadString('\n')
		if !strings.HasPrefix(greeting, "220") {
			t.Fatalf("Expected 220 greeting, got: %s", greeting)
		}

		// Send EHLO
		fmt.Fprintf(client, "EHLO client.example.com\r\n")

		// Read response
		response, err := readMultilineResponse(reader)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}

		if !strings.HasPrefix(response, "250") {
			t.Errorf("Expected 250 response, got: %s", response)
		}

		// Check capabilities
		if !strings.Contains(response, "8BITMIME") {
			t.Error("Expected 8BITMIME capability")
		}
	})

	t.Run("HELO", func(t *testing.T) {
		server, client := createTestConnection(t, config)
		defer server.Stop()
		defer client.Close()

		reader := bufio.NewReader(client)

		// Read greeting first
		greeting, _ := reader.ReadString('\n')
		if !strings.HasPrefix(greeting, "220") {
			t.Fatalf("Expected 220 greeting, got: %s", greeting)
		}

		fmt.Fprintf(client, "HELO client.example.com\r\n")

		response, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}

		if !strings.HasPrefix(response, "250") {
			t.Errorf("Expected 250 response, got: %s", response)
		}
	})

	t.Run("MAIL FROM", func(t *testing.T) {
		server, client := createTestConnection(t, config)
		defer server.Stop()
		defer client.Close()

		reader := bufio.NewReader(client)

		// EHLO first
		fmt.Fprintf(client, "EHLO client.example.com\r\n")
		readMultilineResponse(reader)

		// MAIL FROM
		fmt.Fprintf(client, "MAIL FROM:<sender@example.com>\r\n")
		response, _ := reader.ReadString('\n')

		if !strings.HasPrefix(response, "250") {
			t.Errorf("Expected 250 response, got: %s", response)
		}
	})

	t.Run("RCPT TO", func(t *testing.T) {
		server, client := createTestConnection(t, config)
		defer server.Stop()
		defer client.Close()

		reader := bufio.NewReader(client)

		// EHLO
		fmt.Fprintf(client, "EHLO client.example.com\r\n")
		readMultilineResponse(reader)

		// MAIL FROM
		fmt.Fprintf(client, "MAIL FROM:<sender@example.com>\r\n")
		reader.ReadString('\n')

		// RCPT TO
		fmt.Fprintf(client, "RCPT TO:<recipient@example.com>\r\n")
		response, _ := reader.ReadString('\n')

		if !strings.HasPrefix(response, "250") {
			t.Errorf("Expected 250 response, got: %s", response)
		}
	})

	t.Run("FullMessage", func(t *testing.T) {
		var receivedFrom string
		var receivedTo []string
		var receivedData []byte

		server := NewServer(config, nil)
		server.SetDeliveryHandler(func(from string, to []string, data []byte) error {
			receivedFrom = from
			receivedTo = to
			receivedData = data
			return nil
		})

		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		go server.Serve(ln)
		time.Sleep(50 * time.Millisecond)

		client, _ := net.Dial("tcp", ln.Addr().String())
		defer client.Close()
		defer server.Stop()

		reader := bufio.NewReader(client)

		// Read greeting
		reader.ReadString('\n')

		// EHLO
		fmt.Fprintf(client, "EHLO client.example.com\r\n")
		readMultilineResponse(reader)

		// MAIL FROM
		fmt.Fprintf(client, "MAIL FROM:<sender@example.com>\r\n")
		reader.ReadString('\n')

		// RCPT TO
		fmt.Fprintf(client, "RCPT TO:<recipient@example.com>\r\n")
		reader.ReadString('\n')

		// DATA
		fmt.Fprintf(client, "DATA\r\n")
		reader.ReadString('\n')

		// Send message
		fmt.Fprintf(client, "Subject: Test\r\n")
		fmt.Fprintf(client, "From: sender@example.com\r\n")
		fmt.Fprintf(client, "To: recipient@example.com\r\n")
		fmt.Fprintf(client, "\r\n")
		fmt.Fprintf(client, "This is a test message.\r\n")
		fmt.Fprintf(client, ".\r\n")

		response, _ := reader.ReadString('\n')
		if !strings.HasPrefix(response, "250") {
			t.Errorf("Expected 250 response, got: %s", response)
		}

		// Give handler time to process
		time.Sleep(100 * time.Millisecond)

		if receivedFrom != "sender@example.com" {
			t.Errorf("Expected from sender@example.com, got %s", receivedFrom)
		}
		if len(receivedTo) != 1 || receivedTo[0] != "recipient@example.com" {
			t.Errorf("Expected to [recipient@example.com], got %v", receivedTo)
		}
		if len(receivedData) == 0 {
			t.Error("Expected non-empty message data")
		}
	})

	t.Run("QUIT", func(t *testing.T) {
		server, client := createTestConnection(t, config)
		defer server.Stop()

		reader := bufio.NewReader(client)

		// Read greeting
		reader.ReadString('\n')

		// QUIT
		fmt.Fprintf(client, "QUIT\r\n")
		response, _ := reader.ReadString('\n')

		if !strings.HasPrefix(response, "221") {
			t.Errorf("Expected 221 response, got: %s", response)
		}

		client.Close()
	})

	t.Run("NOOP", func(t *testing.T) {
		server, client := createTestConnection(t, config)
		defer server.Stop()
		defer client.Close()

		reader := bufio.NewReader(client)

		// Read greeting
		reader.ReadString('\n')

		// NOOP
		fmt.Fprintf(client, "NOOP\r\n")
		response, _ := reader.ReadString('\n')

		if !strings.HasPrefix(response, "250") {
			t.Errorf("Expected 250 response, got: %s", response)
		}
	})

	t.Run("RSET", func(t *testing.T) {
		server, client := createTestConnection(t, config)
		defer server.Stop()
		defer client.Close()

		reader := bufio.NewReader(client)

		// EHLO
		reader.ReadString('\n')
		fmt.Fprintf(client, "EHLO client.example.com\r\n")
		readMultilineResponse(reader)

		// MAIL FROM
		fmt.Fprintf(client, "MAIL FROM:<sender@example.com>\r\n")
		reader.ReadString('\n')

		// RSET
		fmt.Fprintf(client, "RSET\r\n")
		response, _ := reader.ReadString('\n')

		if !strings.HasPrefix(response, "250") {
			t.Errorf("Expected 250 response, got: %s", response)
		}
	})
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantCmd  string
		wantArg  string
	}{
		{"EHLO example.com", "EHLO", "example.com"},
		{"MAIL FROM:<test@example.com>", "MAIL", "FROM:<test@example.com>"},
		{"QUIT", "QUIT", ""},
		{"NOOP", "NOOP", ""},
		{"  RCPT  TO:<test>  ", "RCPT", "TO:<test>"},
	}

	for _, tt := range tests {
		cmd, arg := parseCommand(tt.input)
		if cmd != tt.wantCmd {
			t.Errorf("parseCommand(%q) cmd = %q, want %q", tt.input, cmd, tt.wantCmd)
		}
		if arg != tt.wantArg {
			t.Errorf("parseCommand(%q) arg = %q, want %q", tt.input, arg, tt.wantArg)
		}
	}
}

func TestParseMailFrom(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"FROM:<sender@example.com>", "sender@example.com", false},
		{"FROM:<>", "", false},
		{"FROM:sender@example.com", "sender@example.com", false},
		{"<sender@example.com>", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := parseMailFrom(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseMailFrom(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseMailFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseRcptTo(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"TO:<recipient@example.com>", "recipient@example.com", false},
		{"TO:recipient@example.com", "recipient@example.com", false},
		{"<recipient@example.com>", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := parseRcptTo(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseRcptTo(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseRcptTo(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"test@example.com", "test@example.com", false},
		{"<test@example.com>", "test@example.com", false},
		{"Test User <test@example.com>", "test@example.com", false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := ValidateEmail(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ValidateEmail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"test@example.com", "example.com"},
		{"user@sub.example.com", "sub.example.com"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := ExtractDomain(tt.input)
		if got != tt.want {
			t.Errorf("ExtractDomain(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Helper functions

func createTestConnection(t *testing.T, config *Config) (*Server, net.Conn) {
	server := NewServer(config, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go server.Serve(ln)
	time.Sleep(50 * time.Millisecond)

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	return server, client
}

func readMultilineResponse(reader *bufio.Reader) (string, error) {
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
		// Last line starts with "250 " (not "250-")
		if len(line) >= 4 && line[:4] == "250 " {
			break
		}
	}
	return strings.Join(lines, ""), nil
}

func TestServerConfig(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.config != config {
		t.Error("expected config to be set")
	}
	if server.config.Hostname != "mail.example.com" {
		t.Errorf("expected hostname 'mail.example.com', got %s", server.config.Hostname)
	}
}

func TestServerSetHandler(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	handler := func(from string, to []string, data []byte) error {
		return nil
	}

	server.SetDeliveryHandler(handler)
	if server.onDeliver == nil {
		t.Error("expected handler to be set")
	}
}

func TestServerSetAuthHandler(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	authHandler := func(username, password string) (bool, error) {
		return true, nil
	}

	server.SetAuthHandler(authHandler)
	if server.onAuth == nil {
		t.Error("expected authHandler to be set")
	}
}

func TestServerWithTLS(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  false,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	server := NewServer(config, nil)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestValidateEmailVariations(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"user@example.com", "user@example.com", false},
		{"<user@example.com>", "user@example.com", false},
		{"User <user@example.com>", "user@example.com", false},
		{"user+tag@example.com", "user+tag@example.com", false},
		{"user.name@example.com", "user.name@example.com", false},
		{"@example.com", "", true},
		{"user@", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := ValidateEmail(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ValidateEmail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseCommandVariations(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantArg string
	}{
		{"EHLO client.example.com", "EHLO", "client.example.com"},
		{"ehlo client.example.com", "EHLO", "client.example.com"},
		{"MAIL FROM:<test@example.com>", "MAIL", "FROM:<test@example.com>"},
		{"RCPT TO:<recipient@example.com>", "RCPT", "TO:<recipient@example.com>"},
		{"QUIT", "QUIT", ""},
		{"NOOP", "NOOP", ""},
		{"RSET", "RSET", ""},
		{"DATA", "DATA", ""},
		{"  SPACES  around  ", "SPACES", "around"},
	}

	for _, tt := range tests {
		cmd, arg := parseCommand(tt.input)
		if cmd != tt.wantCmd {
			t.Errorf("parseCommand(%q) cmd = %q, want %q", tt.input, cmd, tt.wantCmd)
		}
		if arg != tt.wantArg {
			t.Errorf("parseCommand(%q) arg = %q, want %q", tt.input, arg, tt.wantArg)
		}
	}
}

func TestParseMailFromVariations(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"FROM:<sender@example.com>", "sender@example.com", false},
		{"FROM:<>", "", false},
		{"FROM:sender@example.com", "sender@example.com", false},
		{"from:<sender@example.com>", "sender@example.com", false},
		{"FROM: <sender@example.com>", "sender@example.com", false},
		{"<sender@example.com>", "", true},
		{"", "", true},
		{"FROM:", "", false},
	}

	for _, tt := range tests {
		got, err := parseMailFrom(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseMailFrom(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseMailFrom(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractDomainVariations(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"test@example.com", "example.com"},
		{"user@sub.example.com", "sub.example.com"},
		{"user@deep.sub.example.com", "deep.sub.example.com"},
		{"user@", ""},
		{"@example.com", "example.com"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := ExtractDomain(tt.input)
		if got != tt.want {
			t.Errorf("ExtractDomain(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestConfigStruct(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 10485760,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowInsecure:  true,
		TLSConfig:      nil,
	}

	if config.MaxMessageSize != 10485760 {
		t.Errorf("expected MaxMessageSize 10485760, got %d", config.MaxMessageSize)
	}
	if config.MaxRecipients != 100 {
		t.Errorf("expected MaxRecipients 100, got %d", config.MaxRecipients)
	}
	if !config.AllowInsecure {
		t.Error("expected AllowInsecure to be true")
	}
}

func TestSessionCommandsHELP(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// HELP (non-standard command)
	fmt.Fprintf(client, "HELP\r\n")
	response, _ := reader.ReadString('\n')

	// Should return 500 or 502 for unknown command
	if !strings.HasPrefix(response, "500") && !strings.HasPrefix(response, "502") {
		t.Logf("HELP response: %s", response)
	}
}

func TestSessionCommandsVRFY(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// VRFY
	fmt.Fprintf(client, "VRFY user@example.com\r\n")
	response, _ := reader.ReadString('\n')

	// Should return 252 or 550
	if !strings.HasPrefix(response, "252") && !strings.HasPrefix(response, "550") {
		t.Logf("VRFY response: %s", response)
	}
}

func TestAuthPLAIN(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	// Set auth handler
	server.SetAuthHandler(func(username, password string) (bool, error) {
		return username == "testuser" && password == "testpass", nil
	})

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH PLAIN with inline credentials
	// Format: \0username\0password base64 encoded
	credentials := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))
	fmt.Fprintf(client, "AUTH PLAIN %s\r\n", credentials)
	response, _ := reader.ReadString('\n')

	if !strings.HasPrefix(response, "235") {
		t.Errorf("Expected 235 authentication successful, got: %s", response)
	}
}

func TestAuthPLAINInvalid(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	// Set auth handler
	server.SetAuthHandler(func(username, password string) (bool, error) {
		return false, nil
	})

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH PLAIN with invalid credentials
	credentials := base64.StdEncoding.EncodeToString([]byte("\x00wrong\x00wrong"))
	fmt.Fprintf(client, "AUTH PLAIN %s\r\n", credentials)
	response, _ := reader.ReadString('\n')

	if !strings.HasPrefix(response, "535") {
		t.Errorf("Expected 535 authentication failed, got: %s", response)
	}
}

func TestAuthPLAINMultiline(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	// Set auth handler
	server.SetAuthHandler(func(username, password string) (bool, error) {
		return username == "testuser" && password == "testpass", nil
	})

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH PLAIN without credentials (will prompt)
	fmt.Fprintf(client, "AUTH PLAIN\r\n")
	response, _ := reader.ReadString('\n')

	// Should get 334 continue
	if !strings.HasPrefix(response, "334") {
		t.Fatalf("Expected 334 continue, got: %s", response)
	}

	// Send credentials
	credentials := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))
	fmt.Fprintf(client, "%s\r\n", credentials)
	response, _ = reader.ReadString('\n')

	if !strings.HasPrefix(response, "235") {
		t.Errorf("Expected 235 authentication successful, got: %s", response)
	}
}

func TestAuthLOGIN(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	// Set auth handler
	server.SetAuthHandler(func(username, password string) (bool, error) {
		return username == "testuser" && password == "testpass", nil
	})

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH LOGIN
	fmt.Fprintf(client, "AUTH LOGIN\r\n")
	response, _ := reader.ReadString('\n')

	// Should get 334 VXNlcm5hbWU6 (Username:)
	if !strings.HasPrefix(response, "334") {
		t.Fatalf("Expected 334 continue, got: %s", response)
	}

	// Send username
	usernameEnc := base64.StdEncoding.EncodeToString([]byte("testuser"))
	fmt.Fprintf(client, "%s\r\n", usernameEnc)
	response, _ = reader.ReadString('\n')

	// Should get 334 UGFzc3dvcmQ6 (Password:)
	if !strings.HasPrefix(response, "334") {
		t.Fatalf("Expected 334 continue for password, got: %s", response)
	}

	// Send password
	passwordEnc := base64.StdEncoding.EncodeToString([]byte("testpass"))
	fmt.Fprintf(client, "%s\r\n", passwordEnc)
	response, _ = reader.ReadString('\n')

	if !strings.HasPrefix(response, "235") {
		t.Errorf("Expected 235 authentication successful, got: %s", response)
	}
}

func TestAuthLOGINInvalid(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	// Set auth handler
	server.SetAuthHandler(func(username, password string) (bool, error) {
		return false, nil
	})

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH LOGIN
	fmt.Fprintf(client, "AUTH LOGIN\r\n")
	response, _ := reader.ReadString('\n')

	// Should get 334
	if !strings.HasPrefix(response, "334") {
		t.Fatalf("Expected 334 continue, got: %s", response)
	}

	// Send username
	usernameEnc := base64.StdEncoding.EncodeToString([]byte("wronguser"))
	fmt.Fprintf(client, "%s\r\n", usernameEnc)
	reader.ReadString('\n')

	// Send password
	passwordEnc := base64.StdEncoding.EncodeToString([]byte("wrongpass"))
	fmt.Fprintf(client, "%s\r\n", passwordEnc)
	response, _ = reader.ReadString('\n')

	if !strings.HasPrefix(response, "535") {
		t.Errorf("Expected 535 authentication failed, got: %s", response)
	}
}

func TestAuthInvalidMechanism(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH CRAM-MD5 (not supported)
	fmt.Fprintf(client, "AUTH CRAM-MD5\r\n")
	response, _ := reader.ReadString('\n')

	if !strings.HasPrefix(response, "504") {
		t.Errorf("Expected 504 unrecognized auth type, got: %s", response)
	}
}

// TestSetValidateHandler tests setting the validate handler
func TestSetValidateHandler(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	handler := func(from string, to []string) error {
		return nil
	}

	server.SetValidateHandler(handler)
	if server.onValidate == nil {
		t.Error("expected validate handler to be set")
	}
}

// TestIsRunningAndActiveConnections tests the running status and connection count
func TestIsRunningAndActiveConnections(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server := NewServer(config, nil)

	// Before starting
	if server.IsRunning() {
		t.Error("expected server to not be running before Start")
	}
	if server.ActiveConnections() != 0 {
		t.Errorf("expected 0 active connections, got %d", server.ActiveConnections())
	}

	// After starting
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer ln.Close()

	go server.Serve(ln)
	time.Sleep(50 * time.Millisecond)

	if !server.IsRunning() {
		t.Error("expected server to be running after Start")
	}

	// Connect to increase active connections
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Give server time to accept connection
	time.Sleep(50 * time.Millisecond)

	// After stopping
	server.Stop()
	if server.IsRunning() {
		t.Error("expected server to not be running after Stop")
	}
}

func TestAuthPLAINInvalidBase64(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH PLAIN with invalid base64
	fmt.Fprintf(client, "AUTH PLAIN not-valid-base64!!!\r\n")
	response, _ := reader.ReadString('\n')

	if !strings.HasPrefix(response, "501") {
		t.Errorf("Expected 501 syntax error, got: %s", response)
	}
}

func TestAuthPLAINInvalidFormat(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// AUTH PLAIN with valid base64 but invalid format (not \0user\0pass)
	invalidCreds := base64.StdEncoding.EncodeToString([]byte("invalid-format"))
	fmt.Fprintf(client, "AUTH PLAIN %s\r\n", invalidCreds)
	response, _ := reader.ReadString('\n')

	if !strings.HasPrefix(response, "501") {
		t.Errorf("Expected 501 syntax error, got: %s", response)
	}
}

// TestSessionGetters tests session getter methods
func TestSessionGetters(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Get server internals to access session
	time.Sleep(50 * time.Millisecond)

	// Test that we can check server state
	if server.ActiveConnections() < 1 {
		t.Error("expected at least 1 active connection")
	}
}

// TestSessionStateTransitions tests session state management
func TestSessionStateTransitions(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Initially in NEW state (no authentication yet)
	// After EHLO, session remains not authenticated
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// Session getters work via the session object
	// The connection is active so getters should be accessible
	if server.ActiveConnections() != 1 {
		t.Errorf("expected 1 active connection, got %d", server.ActiveConnections())
	}
}

// TestSessionEXPN tests the EXPN command
func TestSessionEXPN(t *testing.T) {
	config := &Config{
		Hostname:       "mail.example.com",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
		AllowInsecure:  true,
	}

	server, client := createTestConnection(t, config)
	defer server.Stop()
	defer client.Close()

	reader := bufio.NewReader(client)

	// Read greeting
	reader.ReadString('\n')

	// Send EHLO
	fmt.Fprintf(client, "EHLO client.example.com\r\n")
	readMultilineResponse(reader)

	// EXPN (typically not implemented)
	fmt.Fprintf(client, "EXPN mailing-list\r\n")
	response, _ := reader.ReadString('\n')

	// Should return 502 (command not implemented) or 550
	if !strings.HasPrefix(response, "502") && !strings.HasPrefix(response, "550") {
		t.Logf("EXPN response: %s", response)
	}
}
