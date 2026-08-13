package coordinator

import (
	"encoding/json"
	"forgegrid/internal/agentbridge"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func isSafeOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		expectedScheme := "http"
		if r.TLS != nil {
			expectedScheme = "https"
		}
		if u.Scheme != expectedScheme || u.Host != r.Host {
			return false
		}
		return true
	}
	secSite := r.Header.Get("Sec-Fetch-Site")
	if secSite != "" && secSite != "same-origin" {
		return false
	}
	if origin == "" && secSite == "" {
		// If both headers are missing, assume unsafe per strict modern standards for state-changing requests,
		// but usually we allow it unless they are explicitly present and wrong. Wait! The prompt says:
		// "define and test behaviour when both headers are missing".
		// Let's reject if both are missing.
		return false
	}
	return true
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "Unauthorized") || strings.Contains(msg, "authentication_failed") {
		return "authentication failed"
	}
	if strings.Contains(msg, "Forbidden") {
		return "forbidden"
	}
	if strings.Contains(msg, "Not found") {
		return "recipient not found"
	}
	if strings.Contains(msg, "timeout") {
		return "upstream timeout"
	}
	return "messaging unavailable"
}

func (c *Coordinator) handleMessagingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if c.MessagingGateway == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"available": false, "state": "unavailable"})
		return
	}
	identity, ok, err := c.MessagingGateway.Status()
	if !ok || err != nil {
		state := "unavailable"
		if err != nil && strings.Contains(err.Error(), "Unauthorized") {
			state = "authentication_failed"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"available": false, "state": state})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"available": true, "identity": identity, "state": "connected"})
}

func (c *Coordinator) handleMessagingAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c.MessagingGateway == nil {
		http.Error(w, "Messaging unavailable", http.StatusServiceUnavailable)
		return
	}

	agents, err := c.MessagingGateway.GetAgents()
	if err != nil {
		http.Error(w, sanitizeError(err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

func (c *Coordinator) handleMessages(w http.ResponseWriter, r *http.Request) {
	if c.MessagingGateway == nil {
		http.Error(w, "Messaging unavailable", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		limit := 100
		offset := 0
		if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
			limit = l
		}
		if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil {
			offset = o
		}
		includeOutgoing := r.URL.Query().Get("include_outgoing") == "true"

		var statuses []agentbridge.MessageStatus
		if st := r.URL.Query().Get("status"); st != "" {
			for _, s := range strings.Split(st, ",") {
				statuses = append(statuses, agentbridge.MessageStatus(s))
			}
		} else {
			statuses = []agentbridge.MessageStatus{agentbridge.StatusPending, agentbridge.StatusAcknowledged}
		}

		inbox, err := c.MessagingGateway.GetInbox(limit, offset, includeOutgoing, statuses...)
		if err != nil {
			http.Error(w, sanitizeError(err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inbox)
		return
	} else if r.Method == http.MethodPost {
		if !isSafeOrigin(r) {
			http.Error(w, "Cross-Origin forbidden", http.StatusForbidden)
			return
		}

		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}

		var req struct {
			Recipient      string                  `json:"recipient"`
			TaskID         string                  `json:"task_id"`
			Type           agentbridge.MessageType `json:"type"`
			Body           string                  `json:"body"`
			IdempotencyKey string                  `json:"idempotency_key"`
		}

		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384+1024))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				http.Error(w, "empty body", http.StatusBadRequest)
				return
			}
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		var dummy json.RawMessage
		if err := dec.Decode(&dummy); err != io.EOF {
			http.Error(w, "trailing JSON not allowed", http.StatusBadRequest)
			return
		}

		if req.Recipient == "" || len(req.Recipient) > 255 {
			http.Error(w, "invalid recipient", http.StatusBadRequest)
			return
		}
		if len(req.TaskID) > 255 {
			http.Error(w, "invalid task id length", http.StatusBadRequest)
			return
		}
		if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 255 {
			http.Error(w, "invalid idempotency key", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Body) == "" {
			http.Error(w, "empty or whitespace body", http.StatusBadRequest)
			return
		}
		if len(req.Body) > 16384 {
			http.Error(w, "Message body too large", http.StatusRequestEntityTooLarge)
			return
		}

		switch req.Type {
		case agentbridge.TypeInstruction, agentbridge.TypeProgress, agentbridge.TypeResult:
			if req.TaskID == "" {
				http.Error(w, "task_id required for this message type", http.StatusBadRequest)
				return
			}
		case agentbridge.TypeChat, agentbridge.TypeSystem:
		default:
			http.Error(w, "invalid message type", http.StatusBadRequest)
			return
		}

		msg, err := c.MessagingGateway.SendMessage(req.Recipient, req.TaskID, req.Type, req.Body, 3600, req.IdempotencyKey)
		if err != nil {
			http.Error(w, sanitizeError(err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (c *Coordinator) handleMessageDeliveryOrAck(w http.ResponseWriter, r *http.Request) {
	if c.MessagingGateway == nil {
		http.Error(w, "Messaging unavailable", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/dashboard/messages/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	id, action := parts[0], parts[1]

	if action == "delivery" {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		msg, err := c.MessagingGateway.GetDeliveryStatus(id)
		if err != nil {
			http.Error(w, sanitizeError(err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
		return
	} else if action == "acknowledge" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !isSafeOrigin(r) {
			http.Error(w, "Cross-Origin forbidden", http.StatusForbidden)
			return
		}

		msg, err := c.MessagingGateway.Acknowledge(id)
		if err != nil {
			http.Error(w, sanitizeError(err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}
