package coordinator

import (
	"forgegrid/internal/agentbridge"
)

type MessagingGateway interface {
	SendMessage(recipient, taskID string, msgType agentbridge.MessageType, body string, ttl int, idempotencyKey string) (*agentbridge.AgentMessage, error)
	GetInbox(limit, offset int, includeOutgoing bool, statuses ...agentbridge.MessageStatus) ([]agentbridge.AgentMessage, error)
	GetAgents() ([]string, error)
	GetDeliveryStatus(id string) (*agentbridge.AgentMessage, error)
	Acknowledge(id string) (*agentbridge.AgentMessage, error)
	Status() (string, bool, error) // Returns (identity, available, error)
}

type LiveMessagingGateway struct {
	client   *agentbridge.Client
	identity string
}

func NewLiveMessagingGateway() (*LiveMessagingGateway, error) {
	path := agentbridge.GetConfigPath()
	cfg, err := agentbridge.LoadClientConfig(path)
	if err != nil {
		return nil, err
	}
	client, err := agentbridge.NewClient(cfg.URL, cfg.Name, cfg.Token, cfg.Fingerprint, false)
	if err != nil {
		return nil, err
	}
	return &LiveMessagingGateway{client: client, identity: cfg.Name}, nil
}

func (g *LiveMessagingGateway) Status() (string, bool, error) {
	err := g.client.Status()
	if err != nil {
		return "", false, err
	}
	return g.identity, true, nil
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
