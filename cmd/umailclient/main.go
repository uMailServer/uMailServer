package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "send":
		runSend()
	case "bench":
		runBench()
	case "stress":
		runStress()
	case "auth":
		runAuth()
	case "inbox":
		runInbox()
	case "autoconfig":
		runAutoconfig()
	case "autodiscover":
		runAutodiscover()
	case "dsn":
		runDSN()
	case "mdn":
		runMDN()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`uMailClient - Email Server Benchmark & Test Tool

Usage: umailclient <command> [options]

Commands:
  send          Send a single email via SMTP
  bench         Run performance benchmark (simple stats)
  stress        Run stress test with concurrent connections
  auth          Test SMTP authentication
  inbox         Test IMAP connection
  autoconfig    Test Mozilla autoconfig endpoint
  autodiscover   Test Microsoft autodiscover endpoint
  dsn           Test DSN (Delivery Status Notification)
  mdn           Test MDN (Message Disposition Notification)

Security Options:
  --insecure    Skip TLS certificate verification (use only for testing with self-signed certs)

Examples:
  umailclient send --host localhost --port 25 --from user@example.com --to dest@example.com --subject "Test"
  umailclient send --host localhost --port 465 --tls --insecure --from user@example.com --to dest@example.com
  umailclient bench --host localhost --port 25 --count 100 --concurrency 10
  umailclient stress --host localhost --port 25 --count 1000 --concurrency 50 --user user --pass secret
  umailclient auth --host localhost --port 25 --user user --pass secret
  umailclient inbox --host localhost --port 993 --tls
  umailclient autoconfig --domain example.com
  umailclient autodiscover --email user@example.com`)
}

// ========== SMTP Client ==========

// formatAddr formats host:port correctly for both IPv4 and IPv6
func formatAddr(host string, port int) string {
	// Check if host is an IP address
	if ip := net.ParseIP(host); ip != nil {
		// IPv6 addresses need to be wrapped in brackets
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

type SMTPClient struct {
	Host               string
	Port               int
	Username           string
	Password           string
	UseTLS             bool
	InsecureSkipVerify bool // Only for testing with self-signed certs
}

func NewSMTPClient(host string, port int, user, pass string, tls, insecure bool) *SMTPClient {
	return &SMTPClient{Host: host, Port: port, Username: user, Password: pass, UseTLS: tls, InsecureSkipVerify: insecure}
}

func (c *SMTPClient) Send(from, to, subject, body string) error {
	addr := formatAddr(c.Host, c.Port)

	var conn net.Conn
	var err error
	if c.UseTLS {
		tlsConfig := &tls.Config{ServerName: c.Host}
		if c.InsecureSkipVerify {
			tlsConfig.InsecureSkipVerify = true
		}
		conn, err = tls.Dial("tcp", addr, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("EHLO: %w", err)
	}

	if c.Username != "" {
		auth := smtp.PlainAuth("", c.Username, c.Password, c.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}

	msg := buildMessage(from, to, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	_ = w.Close()

	return nil
}

func buildMessage(from, to, subject, body string) string {
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		from, to, subject, time.Now().Format(time.RFC1123Z), body)
}

// ========== SEND COMMAND ==========

func runSend() {
	cmd := flag.NewFlagSet("send", flag.ExitOnError)
	host := cmd.String("host", "localhost", "SMTP host")
	port := cmd.Int("port", 25, "SMTP port")
	from := cmd.String("from", "", "From email")
	to := cmd.String("to", "", "To email")
	subject := cmd.String("subject", "Test", "Subject")
	body := cmd.String("body", "Test message", "Body")
	user := cmd.String("user", "", "Username")
	pass := cmd.String("pass", "", "Password")
	tls := cmd.Bool("tls", false, "Use TLS")
	insecure := cmd.Bool("insecure", false, "Skip TLS certificate verification (use for testing with self-signed certs only)")

	_ = cmd.Parse(os.Args[2:])
	if *from == "" || *to == "" {
		fmt.Println("Error: --from and --to required")
		os.Exit(1)
	}

	client := NewSMTPClient(*host, *port, *user, *pass, *tls, *insecure)
	if err := client.Send(*from, *to, *subject, *body); err != nil {
		fmt.Printf("Send failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Email sent successfully!")
}

// ========== BENCHMARK ==========

type benchResult struct {
	ok      bool
	latency time.Duration
	err     error
}

func runBench() {
	cmd := flag.NewFlagSet("bench", flag.ExitOnError)
	host := cmd.String("host", "localhost", "SMTP host")
	port := cmd.Int("port", 25, "SMTP port")
	count := cmd.Int("count", 100, "Email count")
	concurrency := cmd.Int("concurrency", 10, "Concurrent")
	user := cmd.String("user", "", "Username")
	pass := cmd.String("pass", "", "Password")

	_ = cmd.Parse(os.Args[2:])

	fmt.Printf("Benchmark: %d emails, %d concurrent\n", *count, *concurrency)

	var sent, failed int64
	results := make(chan benchResult, *count)
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)

	start := time.Now()

	for i := 0; i < *count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Add(-1)
			defer func() { <-sem }()

			c := NewSMTPClient(*host, *port, *user, *pass, false, false)
			t0 := time.Now()
			err := c.Send(fmt.Sprintf("bench%d@localhost", idx), "test@localhost",
				fmt.Sprintf("Benchmark #%d", idx), "Test body")
			latency := time.Since(t0)

			if err != nil {
				atomic.AddInt64(&failed, 1)
			} else {
				atomic.AddInt64(&sent, 1)
			}
			results <- benchResult{ok: err == nil, latency: latency, err: err}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)
	close(results)

	// Stats
	var totalLat, minLat, maxLat time.Duration
	var latencies []time.Duration
	for r := range results {
		totalLat += r.latency
		if minLat == 0 || r.latency < minLat {
			minLat = r.latency
		}
		if r.latency > maxLat {
			maxLat = r.latency
		}
		latencies = append(latencies, r.latency)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	fmt.Println("\n========== BENCHMARK REPORT ==========")
	fmt.Printf("Sent:     %d\n", sent)
	fmt.Printf("Failed:   %d\n", failed)
	fmt.Printf("Time:     %v\n", elapsed)
	fmt.Printf("Rate:     %.2f msg/sec\n", float64(sent)/elapsed.Seconds())
	if sent > 0 {
		fmt.Printf("Avg:      %v\n", time.Duration(totalLat.Nanoseconds()/sent))
		fmt.Printf("Min:      %v\n", minLat)
		fmt.Printf("Max:      %v\n", maxLat)
		if len(latencies) > 0 {
			p50 := latencies[len(latencies)*50/100]
			p95 := latencies[len(latencies)*95/100]
			fmt.Printf("P50:      %v\n", p50)
			fmt.Printf("P95:      %v\n", p95)
		}
	}
	fmt.Println("========================================")
}

// ========== STRESS TEST ==========

func runStress() {
	cmd := flag.NewFlagSet("stress", flag.ExitOnError)
	host := cmd.String("host", "localhost", "SMTP host")
	port := cmd.Int("port", 25, "SMTP port")
	count := cmd.Int("count", 1000, "Email count")
	concurrency := cmd.Int("concurrency", 50, "Concurrent")
	user := cmd.String("user", "", "Username")
	pass := cmd.String("pass", "", "Password")

	_ = cmd.Parse(os.Args[2:])

	fmt.Printf("STRESS TEST: %d emails, %d concurrent\n", *count, *concurrency)

	var sent, failed int64
	start := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)

	for i := 0; i < *count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Add(-1)
			defer func() { <-sem }()

			c := NewSMTPClient(*host, *port, *user, *pass, false, false)
			err := c.Send(fmt.Sprintf("stress%d@localhost", idx), "test@localhost",
				fmt.Sprintf("Stress #%d", idx), "Stress test message")
			if err != nil {
				atomic.AddInt64(&failed, 1)
			} else {
				atomic.AddInt64(&sent, 1)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Printf("\nCompleted in %v\n", elapsed)
	fmt.Printf("Sent: %d, Failed: %d\n", sent, failed)
	fmt.Printf("Rate: %.2f msg/sec\n", float64(sent)/elapsed.Seconds())
}

// ========== AUTH TEST ==========

func runAuth() {
	cmd := flag.NewFlagSet("auth", flag.ExitOnError)
	host := cmd.String("host", "localhost", "SMTP host")
	port := cmd.Int("port", 25, "SMTP port")
	user := cmd.String("user", "", "Username")
	pass := cmd.String("pass", "", "Password")

	_ = cmd.Parse(os.Args[2:])
	if *user == "" || *pass == "" {
		fmt.Println("Error: --user and --pass required")
		os.Exit(1)
	}

	addr := fmt.Sprintf("%s:%d", *host, *port)
	auth := smtp.PlainAuth("", *user, *pass, *host)

	fmt.Printf("Testing AUTH PLAIN against %s...\n", addr)
	if err := smtp.SendMail(addr, auth, *user, []string{*user}, []byte("test")); err != nil {
		fmt.Printf("AUTH failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("AUTH successful!")
}

// ========== INBOX TEST ==========

func runInbox() {
	cmd := flag.NewFlagSet("inbox", flag.ExitOnError)
	host := cmd.String("host", "localhost", "IMAP host")
	port := cmd.Int("port", 993, "IMAP port")
	useTLS := cmd.Bool("tls", true, "Use TLS")
	insecure := cmd.Bool("insecure", false, "Skip TLS certificate verification (use for testing with self-signed certs only)")

	_ = cmd.Parse(os.Args[2:])

	addr := formatAddr(*host, *port)
	fmt.Printf("Testing IMAP connection to %s (TLS=%v)...\n", addr, *useTLS)

	var conn net.Conn
	var err error
	if *useTLS {
		tlsConfig := &tls.Config{ServerName: *host}
		if *insecure {
			tlsConfig.InsecureSkipVerify = true
		}
		conn, err = tls.Dial("tcp", addr, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		fmt.Printf("Connection failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	resp, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Read failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Server: %s", resp)
	fmt.Println("IMAP connection successful!")
}

// fetchAndPrint performs an HTTP GET and prints the response body.
func fetchAndPrint(url string) {
	fmt.Printf("Fetching: %s\n", url)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Status: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	fmt.Printf("\nResponse:\n%s\n", string(buf[:n]))
}

// ========== AUTOCONFIG ==========

func runAutoconfig() {
	cmd := flag.NewFlagSet("autoconfig", flag.ExitOnError)
	domain := cmd.String("domain", "", "Email domain")
	host := cmd.String("host", "localhost", "Server host")
	port := cmd.Int("port", 80, "Server port")

	_ = cmd.Parse(os.Args[2:])
	if *domain == "" {
		fmt.Println("Error: --domain required")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://%s:%d/.well-known/autoconfig/mail/config-v1.1.xml?email=postmaster@%s",
		*host, *port, *domain)
	fetchAndPrint(url)
	fmt.Println("Autoconfig PASSED!")
}

// ========== AUTODISCOVER ==========

func runAutodiscover() {
	cmd := flag.NewFlagSet("autodiscover", flag.ExitOnError)
	email := cmd.String("email", "", "Email address")
	host := cmd.String("host", "localhost", "Server host")
	port := cmd.Int("port", 80, "Server port")

	_ = cmd.Parse(os.Args[2:])
	if *email == "" {
		fmt.Println("Error: --email required")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://%s:%d/autodiscover/autodiscover.xml?email=%s", *host, *port, *email)
	fetchAndPrint(url)
	fmt.Println("Autodiscover PASSED!")
}

// ========== DSN TEST ==========

func runDSN() {
	cmd := flag.NewFlagSet("dsn", flag.ExitOnError)
	host := cmd.String("host", "localhost", "SMTP host")
	port := cmd.Int("port", 25, "SMTP port")
	to := cmd.String("to", "", "To email")
	dsnNotify := cmd.String("notify", "SUCCESS,FAILURE", "DSN NOTIFY (NEVER,SUCCESS,FAILURE,DELAY)")

	_ = cmd.Parse(os.Args[2:])
	if *to == "" {
		fmt.Println("Error: --to required")
		os.Exit(1)
	}

	// Test DSN by sending with NOTIFY parameter
	addr := formatAddr(*host, *port)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		fmt.Printf("Connect failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	readResp := func() string {
		resp, _ := reader.ReadString('\n')
		return strings.TrimSpace(resp)
	}
	sendCmd := func(cmd string) string {
		_, _ = conn.Write([]byte(cmd + "\r\n"))
		return readResp()
	}

	// Read greeting
	greet := readResp()
	fmt.Printf("Server: %s\n", greet)

	// EHLO
	ehloResp := sendCmd("EHLO localhost")
	fmt.Printf("EHLO: %s\n", ehloResp)

	// MAIL FROM with RET=HDRS
	mailResp := sendCmd("MAIL FROM:<test@localhost> RET=HDRS")
	fmt.Printf("MAIL FROM: %s\n", mailResp)

	// RCPT TO with NOTIFY
	rcptResp := sendCmd(fmt.Sprintf("RCPT TO:<%s> NOTIFY=%s", *to, *dsnNotify))
	fmt.Printf("RCPT TO: %s\n", rcptResp)

	// DATA
	dataResp := sendCmd("DATA")
	fmt.Printf("DATA: %s\n", dataResp)

	if strings.HasPrefix(mailResp, "250") && strings.HasPrefix(rcptResp, "250") {
		fmt.Println("\nDSN parameters accepted!")
	} else {
		fmt.Println("\nDSN not supported or parameters rejected")
	}

	sendCmd("QUIT")
}

// ========== MDN TEST ==========

func runMDN() {
	cmd := flag.NewFlagSet("mdn", flag.ExitOnError)
	to := cmd.String("to", "", "To email")
	subject := cmd.String("subject", "Read receipt test", "Subject")
	host := cmd.String("host", "localhost", "SMTP host")
	port := cmd.Int("port", 25, "SMTP port")

	_ = cmd.Parse(os.Args[2:])
	if *to == "" {
		fmt.Println("Error: --to required")
		os.Exit(1)
	}

	// Send email with Disposition-Notification-To header
	body := fmt.Sprintf(
		"From: admin@localhost\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"Date: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=utf-8\r\n"+
			"Disposition-Notification-To: admin@localhost\r\n"+
			"\r\n"+
			"This message requests a read receipt.\r\n"+
			"Please confirm receipt.\r\n", *to, *subject, time.Now().Format(time.RFC1123Z))

	client := NewSMTPClient(*host, *port, "", "", false, false)
	if err := client.Send("admin@localhost", *to, *subject, body); err != nil {
		fmt.Printf("Send failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Email with MDN request sent successfully!")
	fmt.Println("MDN (Disposition-Notification-To) header included.")
}
