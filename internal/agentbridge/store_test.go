package agentbridge

import (
	"fmt"
	"testing"
	"time"
)

func TestStore_GetInbox_PaginationAndAllAgents(t *testing.T) {
	store, _ := NewStore()
	store.dataDir = t.TempDir()
	store.messages = make(map[string]AgentMessage) // clear the store

	msg1 := AgentMessage{ID: "msg1", Recipient: "test-agent", CreatedAt: time.Now().Add(-1 * time.Hour), ExpiresAt: time.Now().Add(1 * time.Hour)}
	msg2 := AgentMessage{ID: "msg2", Recipient: "#all-agents", CreatedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(1 * time.Hour)}
	msg3 := AgentMessage{ID: "msg3", Recipient: "other-agent", CreatedAt: time.Now().Add(-3 * time.Hour), ExpiresAt: time.Now().Add(1 * time.Hour)}
	
	store.AddMessage(msg1)
	store.AddMessage(msg2)
	store.AddMessage(msg3)

	inbox := store.GetInbox("test-agent", 100, 0)
	if len(inbox) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(inbox))
	}
	if inbox[0].ID != "msg1" || inbox[1].ID != "msg2" {
		t.Errorf("Expected msg1 then msg2, got %s then %s", inbox[0].ID, inbox[1].ID)
	}

	inboxPage2 := store.GetInbox("test-agent", 1, 1)
	if len(inboxPage2) != 1 || inboxPage2[0].ID != "msg2" {
		t.Errorf("Pagination failed: expected msg2, got %v", inboxPage2)
	}
}

func TestStore_EnforceRetention(t *testing.T) {
	store, _ := NewStore()
	store.dataDir = t.TempDir()
	store.messages = make(map[string]AgentMessage) // clear the store

	for i := 0; i < 5; i++ {
		msg := AgentMessage{
			ID:        fmt.Sprintf("msg%d", i),
			CreatedAt: time.Now().Add(time.Duration(i) * time.Hour),
		}
		store.AddMessage(msg)
	}

	if len(store.messages) != 5 {
		t.Fatalf("Expected 5 messages, got %d", len(store.messages))
	}

	if err := store.EnforceRetention(3); err != nil {
		t.Fatalf("EnforceRetention error: %v", err)
	}

	if len(store.messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(store.messages))
	}

	for i := 0; i < 2; i++ {
		if _, ok := store.messages[fmt.Sprintf("msg%d", i)]; ok {
			t.Errorf("Expected msg%d to be deleted", i)
		}
	}
	for i := 2; i < 5; i++ {
		if _, ok := store.messages[fmt.Sprintf("msg%d", i)]; !ok {
			t.Errorf("Expected msg%d to be retained", i)
		}
	}
}
