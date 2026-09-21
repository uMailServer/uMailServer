package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"testing"

	"github.com/umailserver/umailserver/internal/vacation"
)

func TestHandleGetVacation_NilConfig(t *testing.T) {
	s := &Server{
		vacationMgr: &MockVacationManager{
			GetConfigError: fmt.Errorf("user not found"),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "alice@example.com"))
	w := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v\nstack:\n%s", r, debug.Stack())
		}
	}()

	s.handleGetVacation(w, req)
}

func TestHandleGetVacation_NilConfigFields(t *testing.T) {
	s := &Server{
		vacationMgr: &MockVacationManager{
			GetConfigResult: &vacation.Config{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "alice@example.com"))
	w := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic: %v\nstack:\n%s", r, debug.Stack())
		}
	}()

	s.handleGetVacation(w, req)
}
