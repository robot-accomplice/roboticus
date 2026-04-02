package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInterviewStart(t *testing.T) {
	mgr := NewInterviewManager()
	handler := InterviewStart(mgr)
	req := httptest.NewRequest("POST", "/api/interview/start",
		strings.NewReader(`{"agent_name":"testbot"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	body := jsonBody(t, rec)
	if body["session_id"] == nil {
		t.Error("should return session_id")
	}
}

func TestInterviewTurn(t *testing.T) {
	mgr := NewInterviewManager()
	// Start first.
	startReq := httptest.NewRequest("POST", "/api/interview/start",
		strings.NewReader(`{"agent_name":"testbot"}`))
	startRec := httptest.NewRecorder()
	InterviewStart(mgr).ServeHTTP(startRec, startReq)
	startBody := jsonBody(t, startRec)
	sessionID := startBody["session_id"].(string)

	// Turn.
	turnReq := httptest.NewRequest("POST", "/api/interview/turn",
		strings.NewReader(`{"session_id":"`+sessionID+`","answer":"I am helpful"}`))
	turnRec := httptest.NewRecorder()
	InterviewTurn(mgr).ServeHTTP(turnRec, turnReq)

	if turnRec.Code != http.StatusOK {
		t.Errorf("turn status = %d", turnRec.Code)
	}
}

func TestInterviewFinish(t *testing.T) {
	mgr := NewInterviewManager()
	startReq := httptest.NewRequest("POST", "/api/interview/start",
		strings.NewReader(`{"agent_name":"testbot"}`))
	startRec := httptest.NewRecorder()
	InterviewStart(mgr).ServeHTTP(startRec, startReq)
	startBody := jsonBody(t, startRec)
	sessionID := startBody["session_id"].(string)

	finishReq := httptest.NewRequest("POST", "/api/interview/finish",
		strings.NewReader(`{"session_id":"`+sessionID+`"}`))
	finishRec := httptest.NewRecorder()
	InterviewFinish(mgr).ServeHTTP(finishRec, finishReq)

	if finishRec.Code != http.StatusOK {
		t.Errorf("finish status = %d", finishRec.Code)
	}
}
