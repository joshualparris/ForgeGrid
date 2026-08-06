package coordinator

import (
	"encoding/json"
	"forgegrid/internal/agentbridge"
	"os"
)

type MessagingGateway interface {
	SendMessage(recipient, taskID string, msgType agentbridge.MessageType, body string, ttl int, idempotencyKey string) (*agentbridge.AgentMessage, error)
	GetInbox(limit, offset int, includeOutgoing bool, statuses ...agentbridge.MessageStatus) ([]agentbridge.AgentMessage, error)
	GetAgents() ([]string, error)
	GetDeliveryStatus(id string) (*agentbridge.AgentMessage, error)
	Acknowledge(id string) (*agentbridge.AgentMessage, error)
}

type LiveMessagingGateway struct {
	client *agentbridge.Client
}

func NewLiveMessagingGateway() (*LiveMessagingGateway, error) {
	path := agentbridge.GetConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		BaseURL     string `json:"url"`
		Identity    string `json:"identity"`
		Token       string `json:"token"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	client, err := agentbridge.NewClient(cfg.BaseURL, cfg.Identity, cfg.Token, cfg.Fingerprint, false)
	if err != nil {
		return nil, err
	}
	return &LiveMessagingGateway{client: client}, nil
}

func (g *LiveMessagingGateway) SendMessage(recipient, taskID string, msgType agentbridge.MessageType, body string, ttl int, idempotencyKey string) (*agentbridge.AgentMessage, error) {
	return g.client.SendMessage(recipient, taskID, msgType, body, ttl, idempotencyKey)
}

func (g *LiveMessagingGateway) GetInbox(limit, offset int, includeOutgoing bool, statuses ...agentbridge.MessageStatus) ([]agentbridge.AgentMessage, error) {
	return g.client.GetInbox(limit, offset, includeOutgoing, statuses...)
}

func (g *LiveMessagingGateway) GetAgents() ([]string, error) {
	return g.client.GetAgents()
}

func (g *LiveMessagingGateway) GetDeliveryStatus(id string) (*agentbridge.AgentMessage, error) {
	return g.client.GetDeliveryStatus(id)
}

func (g *LiveMessagingGateway) Acknowledge(id string) (*agentbridge.AgentMessage, error) {
	return g.client.Acknowledge(id)
}
