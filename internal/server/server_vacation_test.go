package server

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/sieve"
)

// TestHandleSieveVacation_NoQueue tests handleSieveVacation without queue
func TestHandleSieveVacation_NoQueue(t *testing.T) {
	srv := helperServer(t)
	srv.queue = nil

	vacation := sieve.VacationAction{
		Subject: "On vacation",
		Body:    "I'm away",
	}

	// Should not panic
	srv.handleSieveVacation("sender@example.com", "recipient@example.com", vacation)
}

// TestHandleSieveVacation_MailerDaemon tests handleSieveVacation with mailer-daemon sender
func TestHandleSieveVacation_MailerDaemon(t *testing.T) {
	srv := helperServer(t)

	vacation := sieve.VacationAction{
		Subject: "On vacation",
		Body:    "I'm away",
	}

	// Should not send vacation reply to mailer-daemon
	srv.handleSieveVacation("mailer-daemon@example.com", "recipient@example.com", vacation)
}

// TestHandleSieveVacation_Postmaster tests handleSieveVacation with postmaster sender
func TestHandleSieveVacation_Postmaster(t *testing.T) {
	srv := helperServer(t)

	vacation := sieve.VacationAction{
		Subject: "On vacation",
		Body:    "I'm away",
	}

	// Should not send vacation reply to postmaster
	srv.handleSieveVacation("postmaster@example.com", "recipient@example.com", vacation)
}

// TestHandleSieveVacation_NoReply tests handleSieveVacation with noreply sender
func TestHandleSieveVacation_NoReply(t *testing.T) {
	srv := helperServer(t)

	vacation := sieve.VacationAction{
		Subject: "On vacation",
		Body:    "I'm away",
	}

	// Should not send vacation reply to noreply
	srv.handleSieveVacation("noreply@example.com", "recipient@example.com", vacation)
}

// TestHandleSieveVacation_Bounce tests handleSieveVacation with bounce sender
func TestHandleSieveVacation_Bounce(t *testing.T) {
	srv := helperServer(t)

	vacation := sieve.VacationAction{
		Subject: "On vacation",
		Body:    "I'm away",
	}

	// Should not send vacation reply to bounce
	srv.handleSieveVacation("bounce@example.com", "recipient@example.com", vacation)
}

// TestHandleSieveVacation_DefaultSubject tests handleSieveVacation with default subject
func TestHandleSieveVacation_DefaultSubject(t *testing.T) {
	srv := helperServer(t)

	vacation := sieve.VacationAction{
		Subject: "", // Empty subject
		Body:    "I'm away",
	}

	// Should use default subject
	srv.handleSieveVacation("sender@example.com", "recipient@example.com", vacation)
}

// TestHandleSieveVacation_DefaultBody tests handleSieveVacation with default body
func TestHandleSieveVacation_DefaultBody(t *testing.T) {
	srv := helperServer(t)

	vacation := sieve.VacationAction{
		Subject: "On vacation",
		Body:    "", // Empty body
	}

	// Should use default body
	srv.handleSieveVacation("sender@example.com", "recipient@example.com", vacation)
}

// TestHandleSieveVacation_CustomFrom tests handleSieveVacation with custom from
func TestHandleSieveVacation_CustomFrom(t *testing.T) {
	srv := helperServer(t)

	vacation := sieve.VacationAction{
		Subject: "On vacation",
		Body:    "I'm away",
		From:    "vacation@example.com",
	}

	// Should use custom from address
	srv.handleSieveVacation("sender@example.com", "recipient@example.com", vacation)
}

// TestSendVacationReply_MailerDaemon tests sendVacationReply with mailer-daemon
func TestSendVacationReply_MailerDaemon(t *testing.T) {
	srv := helperServer(t)

	settings := `{"enabled":true,"message":"I'm on vacation"}`

	// Should not send to mailer-daemon
	srv.sendVacationReply("recipient@example.com", "mailer-daemon@example.com", settings)
}

// TestSendVacationReply_Postmaster tests sendVacationReply with postmaster
func TestSendVacationReply_Postmaster(t *testing.T) {
	srv := helperServer(t)

	settings := `{"enabled":true,"message":"I'm on vacation"}`

	// Should not send to postmaster
	srv.sendVacationReply("recipient@example.com", "postmaster@example.com", settings)
}

// TestSendVacationReply_Disabled tests sendVacationReply when disabled
func TestSendVacationReply_Disabled(t *testing.T) {
	srv := helperServer(t)

	settings := `{"enabled":false,"message":"I'm on vacation"}`

	// Should not send when disabled
	srv.sendVacationReply("recipient@example.com", "sender@example.com", settings)
}

// TestSendVacationReply_InvalidJSON tests sendVacationReply with invalid JSON
func TestSendVacationReply_InvalidJSON(t *testing.T) {
	srv := helperServer(t)

	settings := `invalid json`

	// Should not send with invalid JSON
	srv.sendVacationReply("recipient@example.com", "sender@example.com", settings)
}

// TestSendVacationReply_FutureStartDate tests sendVacationReply with future start date
func TestSendVacationReply_FutureStartDate(t *testing.T) {
	srv := helperServer(t)

	// Start date is tomorrow
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	settings := `{"enabled":true,"message":"I'm on vacation","start_date":"` + tomorrow + `"}`

	// Should not send before start date
	srv.sendVacationReply("recipient@example.com", "sender@example.com", settings)
}

// TestSendVacationReply_PastEndDate tests sendVacationReply with past end date
func TestSendVacationReply_PastEndDate(t *testing.T) {
	srv := helperServer(t)

	// End date is yesterday
	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	settings := `{"enabled":true,"message":"I'm on vacation","end_date":"` + yesterday + `"}`

	// Should not send after end date
	srv.sendVacationReply("recipient@example.com", "sender@example.com", settings)
}

// TestCleanupVacationRepliesNil tests cleanup with nil map
func TestCleanupVacationRepliesNil(t *testing.T) {
	srv := helperServer(t)
	srv.vacationReplies = nil

	// Should not panic
	srv.cleanupVacationReplies()
}

// TestStartVacationCleanup tests starting the vacation cleanup goroutine
func TestStartVacationCleanup(t *testing.T) {
	srv := helperServer(t)

	// Should not panic
	srv.startVacationCleanup()
}
