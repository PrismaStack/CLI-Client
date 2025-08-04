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

// --- Server Data Models ---

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
	wsConn     *websocket.Conn
}

func NewApiClient(baseURL string) *ApiClient {
	return &ApiClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

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

	// Set WebSocket scheme (ws or wss)
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}

	// Remove default ports
	if (u.Scheme == "ws" && u.Port() == "80") || (u.Scheme == "wss" && u.Port() == "443") {
		u.Host = u.Hostname()
	}

	u.Path = "/api/ws"
	q := u.Query()
	q.Set("user_id", fmt.Sprintf("%d", c.User.ID))
	u.RawQuery = q.Encode()

	// Set required headers
	headers := http.Header{}
	headers.Add("Origin", c.baseURL)
	headers.Add("Sec-WebSocket-Protocol", "chat")

	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), headers)
	if err != nil {
		program.Send(errOccurredMsg{fmt.Errorf("websocket dial error: %w", err)})
		return
	}
	defer conn.Close()

	c.wsConn = conn

	// Ping handler to keep connection alive
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})

	// Start ping loop
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					program.Send(errOccurredMsg{fmt.Errorf("websocket ping failed: %w", err)})
					return
				}
			}
		}
	}()

	// Main message loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				program.Send(errOccurredMsg{fmt.Errorf("websocket read error: %w", err)})
			}
			return
		}

		var wsMsgData WebSocketMessage
		if err := json.Unmarshal(message, &wsMsgData); err != nil {
			continue
		}
		program.Send(wsMsg{wsMsgData})
	}
}