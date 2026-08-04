package agentbridge

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL    string
	AgentName  string
	Token      string
	HTTPClient *http.Client
}

func NewClient(baseURL, agentName, token string, insecure bool) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}
	return &Client{
		BaseURL:   baseURL,
		AgentName: agentName,
		Token:     token,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
	}
}

func (c *Client) doReq(method, path string, body interface{}, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.AgentName+":"+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) SendMessage(recipient, taskID string, msgType MessageType, body string, ttl int) (*AgentMessage, error) {
	req := map[string]interface{}{
		"recipient":   recipient,
		"task_id":     taskID,
		"type":        msgType,
		"body":        body,
		"ttl_seconds": ttl,
	}
	var out AgentMessage
	if err := c.doReq(http.MethodPost, "/api/v1/agent-messages", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetInbox() ([]AgentMessage, error) {
	var out []AgentMessage
	if err := c.doReq(http.MethodGet, "/api/v1/agent-messages/inbox", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Acknowledge(id string) (*AgentMessage, error) {
	var out AgentMessage
	if err := c.doReq(http.MethodPost, "/api/v1/agent-messages/"+id+"/acknowledge", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Complete(id string, result json.RawMessage) (*AgentMessage, error) {
	req := map[string]interface{}{"result": result}
	var out AgentMessage
	if err := c.doReq(http.MethodPost, "/api/v1/agent-messages/"+id+"/complete", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Fail(id string, result json.RawMessage) (*AgentMessage, error) {
	req := map[string]interface{}{"result": result}
	var out AgentMessage
	if err := c.doReq(http.MethodPost, "/api/v1/agent-messages/"+id+"/fail", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
