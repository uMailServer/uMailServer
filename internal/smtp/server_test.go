package smtp

import (
	"bufio"
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
