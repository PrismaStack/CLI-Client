package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gorilla/websocket"
)

// --- Server Data Models (unchanged) ---

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	AvatarURL string `json:"avatar_url"`
}

type Channel struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CategoryID int64  `json:"category_id"`
	Position   int    `json:"position"`
}

type ChannelCategory struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Position int       `json:"position"`
	Channels []Channel `json:"channels,omitempty"`
}

type Message struct {
	ID        int64     `json:"id"`
	ChannelID int64     `json:"channel_id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	AvatarURL string    `json:"avatar_url,omitempty"`
}

type WebSocketMessage struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

// --- API Client ---

type ApiClient struct {
	baseURL    string
	httpClient *http.Client
	User       *User
	wsConn     *websocket.Conn // The persistent WebSocket connection
}

func NewApiClient(baseURL string) *ApiClient {
	return &ApiClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Login, GetCategories, GetMessages, SendMessage are unchanged.

func (c *ApiClient) Login(username, password string) error {
	creds := map[string]string{"username": username, "password": password}
	body, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.baseURL+"/api/login", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed with status: %s", resp.Status)
	}
	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return err
	}
	c.User = &user
	return nil
}

func (c *ApiClient) GetCategories() ([]ChannelCategory, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/categories")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var categories []ChannelCategory
	if err := json.NewDecoder(resp.Body).Decode(&categories); err != nil {
		return nil, err
	}
	return categories, nil
}

func (c *ApiClient) GetMessages(channelID int64) ([]Message, error) {
	url := fmt.Sprintf("%s/api/channels/%d/messages", c.baseURL, channelID)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var messages []Message
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, err
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

func (c *ApiClient) SendMessage(channelID, userID int64, content string) error {
	msgReq := map[string]interface{}{
		"channel_id": channelID,
		"user_id":    userID,
		"content":    content,
	}
	body, err := json.Marshal(msgReq)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Post(c.baseURL+"/api/messages", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send message: %s - %s", resp.Status, string(bodyBytes))
	}
	return nil
}

// UPDATED: This method now establishes a single, long-lived connection
// and runs a loop to continuously listen for messages.
func (c *ApiClient) ConnectAndListen(program *tea.Program) {
	if c.User == nil {
		program.Send(errOccurredMsg{fmt.Errorf("must be logged in to connect")})
		return
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		program.Send(errOccurredMsg{err})
		return
	}
	u.Scheme = "ws"
	u.Path = "/api/ws"
	q := u.Query()
	q.Set("user_id", fmt.Sprintf("%d", c.User.ID))
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		program.Send(errOccurredMsg{fmt.Errorf("websocket dial error: %w", err)})
		return
	}
	c.wsConn = conn
	defer c.wsConn.Close()

	// This is the single, long-running listener loop.
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			// When the connection closes, the loop will exit.
			// We can send a final error message if we want.
			program.Send(errOccurredMsg{fmt.Errorf("websocket disconnected: %w", err)})
			return
		}

		var wsMsgData WebSocketMessage
		if err := json.Unmarshal(message, &wsMsgData); err != nil {
			continue // Ignore messages we can't parse
		}
		// Send the message to the Bubble Tea program's update loop.
		// program.Send is thread-safe.
		program.Send(wsMsg{wsMsgData})
	}
}