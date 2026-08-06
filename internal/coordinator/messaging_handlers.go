package coordinator

import (
	"encoding/json"
	"forgegrid/internal/agentbridge"
	"net/http"
	"strconv"
	"strings"
)

func (c *Coordinator) handleMessagingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if c.MessagingGateway == nil {
		json.NewEncoder(w).Encode(map[string]bool{"available": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"available": true})
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
		http.Error(w, "Failed to get agents: "+err.Error(), http.StatusInternalServerError)
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
		// GET /api/dashboard/messages
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
			// default: pending and acknowledged
			statuses = []agentbridge.MessageStatus{agentbridge.StatusPending, agentbridge.StatusAcknowledged}
		}

		inbox, err := c.MessagingGateway.GetInbox(limit, offset, includeOutgoing, statuses...)
		if err != nil {
			http.Error(w, "Failed to get inbox: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inbox)
		return
	} else if r.Method == http.MethodPost {
		// POST /api/dashboard/messages

		// Same-Origin protection
		origin := r.Header.Get("Origin")
		if origin != "" {
			// For simplicity, enforce Origin matches Host.
			// Or check Sec-Fetch-Site == same-origin.
			if !strings.Contains(origin, r.Host) {
				http.Error(w, "Cross-Origin forbidden", http.StatusForbidden)
				return
			}
		} else if r.Header.Get("Sec-Fetch-Site") != "" && r.Header.Get("Sec-Fetch-Site") != "same-origin" {
			http.Error(w, "Cross-Origin forbidden", http.StatusForbidden)
			return
		}

		if r.Header.Get("Content-Type") != "application/json" {
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

		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		if req.Recipient == "" || req.Type == "" || req.IdempotencyKey == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		if len(req.Body) > 16384 {
			http.Error(w, "Message body too large", http.StatusRequestEntityTooLarge)
			return
		}

		msg, err := c.MessagingGateway.SendMessage(req.Recipient, req.TaskID, req.Type, req.Body, 3600, req.IdempotencyKey)
		if err != nil {
			http.Error(w, "Failed to send message: "+err.Error(), http.StatusInternalServerError)
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
			if strings.Contains(err.Error(), "Forbidden") {
				http.Error(w, "Forbidden", http.StatusForbidden)
			} else {
				http.Error(w, "Failed: "+err.Error(), http.StatusInternalServerError)
			}
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

		// Same-Origin protection
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !strings.Contains(origin, r.Host) {
				http.Error(w, "Cross-Origin forbidden", http.StatusForbidden)
				return
			}
		} else if r.Header.Get("Sec-Fetch-Site") != "" && r.Header.Get("Sec-Fetch-Site") != "same-origin" {
			http.Error(w, "Cross-Origin forbidden", http.StatusForbidden)
			return
		}

		msg, err := c.MessagingGateway.Acknowledge(id)
		if err != nil {
			http.Error(w, "Failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}
