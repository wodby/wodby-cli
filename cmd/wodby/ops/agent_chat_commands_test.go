package ops

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAgentChatCommandsSendTheRightRequests(t *testing.T) {
	chat := map[string]interface{}{"id": 9, "orgId": 3, "title": "Deploy", "status": "running", "approvalMode": "ask"}
	runRESTRouteCases(t, []restRouteCase{
		{name: "list", command: newAgentChatCommand, args: []string{"list", "--org", "3"}, method: http.MethodGet, path: "/v1/agent-chats", query: "orgId=3",
			response: []interface{}{chat}, wantInOut: "Deploy"},
		{name: "start", command: newAgentChatCommand, args: []string{"start", "Deploy my app", "--org", "3", "--approval-mode", "never-ask"},
			method: http.MethodPost, path: "/v1/agent-chats", body: map[string]interface{}{"orgId": float64(3), "message": "Deploy my app", "approvalMode": "never_ask"},
			response: chat, wantInOut: "running"},
		{name: "start from stdin", command: newAgentChatCommand, args: []string{"start", "-", "--org", "3"}, stdin: "Why did the build fail?\n",
			method: http.MethodPost, path: "/v1/agent-chats", body: map[string]interface{}{"orgId": float64(3), "message": "Why did the build fail?"}, response: chat},
		{name: "start with raw body", command: newAgentChatCommand, args: []string{"start", "--data", `{"message":"Hi","approvalMode":"ask"}`},
			method: http.MethodPost, path: "/v1/agent-chats", body: map[string]interface{}{"message": "Hi", "approvalMode": "ask"}, response: chat},
		{name: "send", command: newAgentChatCommand, args: []string{"send", "9", "Use staging", "--org", "3"},
			method: http.MethodPost, path: "/v1/agent-chats/9/messages", query: "orgId=3", body: map[string]interface{}{"message": "Use staging"}, response: chat},
		{name: "send with raw body", command: newAgentChatCommand, args: []string{"send", "9", "--org", "3", "--data", `{"message":"Use prod"}`},
			method: http.MethodPost, path: "/v1/agent-chats/9/messages", query: "orgId=3", body: map[string]interface{}{"message": "Use prod"}, response: chat},
		{name: "approve", command: newAgentChatCommand, args: []string{"approve", "9", "--message-id", "13", "--org", "3"},
			method: http.MethodPost, path: "/v1/agent-chats/9/approvals", query: "orgId=3", body: map[string]interface{}{"messageId": float64(13), "approve": true}, response: chat},
		{name: "deny", command: newAgentChatCommand, args: []string{"approve", "9", "--message-id", "13", "--deny", "--org", "3"},
			method: http.MethodPost, path: "/v1/agent-chats/9/approvals", query: "orgId=3", body: map[string]interface{}{"messageId": float64(13), "approve": false}, response: chat},
		{name: "answer", command: newAgentChatCommand, args: []string{"answer", "9", "--message-id", "14", "--answer", "Staging (Recommended)", "--answer", "Production", "--org", "3"},
			method: http.MethodPost, path: "/v1/agent-chats/9/answers", query: "orgId=3",
			body: map[string]interface{}{"messageId": float64(14), "answers": []interface{}{"Staging (Recommended)", "Production"}}, response: chat},
		{name: "approval mode", command: newAgentChatCommand, args: []string{"approval-mode", "9", "ask", "--org", "3"},
			method: http.MethodPut, path: "/v1/agent-chats/9/approval-mode", query: "orgId=3", body: map[string]interface{}{"approvalMode": "ask"}, response: chat},
		{name: "stop", command: newAgentChatCommand, args: []string{"stop", "9", "--org", "3"},
			method: http.MethodPost, path: "/v1/agent-chats/9/stop", query: "orgId=3", noBody: true, response: chat},
		{name: "delete", command: newAgentChatCommand, args: []string{"delete", "9", "--yes", "--org", "3"},
			method: http.MethodDelete, path: "/v1/agent-chats/9", query: "orgId=3"},
	})
}

func TestAgentChatCommandsRejectBadInput(t *testing.T) {
	configureTestAPI(t, "http://127.0.0.1:1/v1")
	for _, args := range [][]string{
		{"start", "Hi", "--approval-mode", "sometimes", "--org", "3"},
		{"approve", "9", "--org", "3"},
		{"answer", "9", "--message-id", "14", "--org", "3"},
		{"approval-mode", "9", "--org", "3"},
		{"send", "9", "--org", "3"},
	} {
		var out bytes.Buffer
		cmd := newAgentChatCommand()
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("Execute(%v) succeeded, want an input error", args)
		}
	}
}

// agentChatDetailResponse is a chat waiting for approval of message 13, with the user's message and a tool call.
func agentChatDetailResponse(status string) map[string]interface{} {
	return map[string]interface{}{
		"chat": map[string]interface{}{"id": 9, "orgId": 3, "title": "Deploy", "status": status, "error": "", "approvalMode": "ask"},
		"messages": []interface{}{
			map[string]interface{}{"id": 12, "role": "user", "content": "Deploy my app", "approval": nil, "toolDestructive": false, "toolTargets": []interface{}{}},
			map[string]interface{}{"id": 13, "role": "tool", "tool": "deploy", "content": "", "approval": "pending", "toolDestructive": true,
				"toolArguments": map[string]interface{}{"environment_id": 5},
				"toolTargets":   []interface{}{map[string]interface{}{"kind": "Environment", "name": "Production", "argument": "environment_id"}}},
		},
	}
}

func TestAgentChatStartWaitsForTheAgentAndShowsTheTranscript(t *testing.T) {
	previous := agentChatPollInterval
	agentChatPollInterval = time.Millisecond
	t.Cleanup(func() { agentChatPollInterval = previous })

	var mu sync.Mutex
	reads := 0
	var started map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs":
			_ = json.NewEncoder(w).Encode([]interface{}{map[string]interface{}{"id": 3, "name": "acme"}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-chats":
			_ = json.NewDecoder(r.Body).Decode(&started)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 9, "orgId": 3, "title": "Deploy", "status": "running"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agent-chats/9":
			if r.URL.Query().Get("orgId") != "3" {
				t.Errorf("orgId = %q, want 3", r.URL.Query().Get("orgId"))
			}
			reads++
			status := "running"
			if reads > 1 {
				status = "waiting"
			}
			_ = json.NewEncoder(w).Encode(agentChatDetailResponse(status))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newAgentChatCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"start", "Deploy my app", "--wait"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, out.String())
	}
	if want := map[string]interface{}{"orgId": float64(3), "message": "Deploy my app"}; !reflect.DeepEqual(started, want) {
		t.Fatalf("start body = %#v, want %#v", started, want)
	}
	if reads != 2 {
		t.Fatalf("chat reads = %d, want 2", reads)
	}
	for _, want := range []string{
		"waiting",
		"[12] user",
		"  Deploy my app",
		"[13] tool deploy · waits for approval · destructive",
		`Target: Environment "Production" (environment_id)`,
		`Arguments: {"environment_id":5}`,
		"agent-chat approve 9 --message-id 13",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output misses %q:\n%s", want, out.String())
		}
	}
}

func TestAgentChatGetPrintsJSONAsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/agent-chats/9" || r.URL.RawQuery != "orgId=4" {
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(agentChatDetailResponse("waiting"))
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newAgentChatCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"get", "9", "--org", "4", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, out.String())
	}
	var detail map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &detail); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if messages, _ := detail["messages"].([]interface{}); len(messages) != 2 {
		t.Fatalf("messages = %#v, want 2", detail["messages"])
	}
}

func TestAgentChatWaitFailsWhenTheRunStopsWithAnError(t *testing.T) {
	previous := agentChatPollInterval
	agentChatPollInterval = time.Millisecond
	t.Cleanup(func() { agentChatPollInterval = previous })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agent-chats/9/messages":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 9, "status": "running"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agent-chats/9":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"chat":     map[string]interface{}{"id": 9, "status": "idle", "error": "AI credits are used up"},
				"messages": []interface{}{},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTestAPI(t, server.URL+"/v1")

	var out bytes.Buffer
	cmd := newAgentChatCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"send", "9", "Continue", "--org", "3", "--wait"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "AI credits are used up") {
		t.Fatalf("Execute() error = %v, want the run's error\n%s", err, out.String())
	}
}
