package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles (unchanged)
var (
	headerStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	myMsgStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	otherMsgStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	systemStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Italic(true)
	tabStyle         = lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	activeTabStyle   = lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	userListStyle    = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(0, 1)
	userListHeader   = lipgloss.NewStyle().Bold(true).Underline(true)
	onlineUserStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	offlineUserStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Model & State (updated)
type viewState int

const (
	stateLoading viewState = iota
	stateChatting
	stateError
)

type model struct {
	client         *ApiClient
	state          viewState
	textInput      textinput.Model
	width, height  int
	err            error
	channels       []Channel
	messages       map[int64][]Message
	onlineUsers    map[string]bool
	activeTabIndex int

	chatViewport   viewport.Model
	viewportReady  bool
	lastChannelID  int64
}

// Messages for Tea Program (unchanged)
type initialDataLoadedMsg struct{ cats []ChannelCategory }
type historyLoadedMsg struct {
	channelID int64
	msgs      []Message
}
type wsMsg struct{ msg WebSocketMessage }
type errOccurredMsg struct{ err error }

// UPDATED: Renamed for clarity and to match `main.go` call
func InitialModel(client *ApiClient) model {
	ti := textinput.New()
	ti.Placeholder = "Type a message and press Enter..."
	ti.Focus()
	ti.CharLimit = 280
	ti.Width = 50

	m := model{
		client:      client,
		state:       stateLoading,
		textInput:   ti,
		messages:    make(map[int64][]Message),
		onlineUsers: make(map[string]bool),
	}

	return m
}

// Tea Commands (unchanged)
func fetchInitialDataCmd(client *ApiClient) tea.Cmd {
	return func() tea.Msg {
		cats, err := client.GetCategories()
		if err != nil {
			return errOccurredMsg{err}
		}
		return initialDataLoadedMsg{cats}
	}
}

func fetchHistoryCmd(client *ApiClient, channelID int64) tea.Cmd {
	return func() tea.Msg {
		msgs, err := client.GetMessages(channelID)
		if err != nil {
			return errOccurredMsg{err}
		}
		return historyLoadedMsg{channelID, msgs}
	}
}

func (m model) Init() tea.Cmd {
	return fetchInitialDataCmd(m.client)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.textInput.Width = m.width - 25

		// Update viewport size if needed
		chatWidth := m.width - 25
		chatHeight := m.height - 5
		if chatHeight < 1 {
			chatHeight = 1
		}
		if !m.viewportReady || m.chatViewport.Width != chatWidth || m.chatViewport.Height != chatHeight {
			m.chatViewport = viewport.New(chatWidth, chatHeight)
			m.viewportReady = true

			// On resize, update viewport content for current channel
			if len(m.channels) > 0 {
				activeChannel := m.channels[m.activeTabIndex]
				m.chatViewport.SetContent(m.renderMessagesContent(activeChannel.ID))
				m.chatViewport.GotoBottom()
			}
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyTab:
			if len(m.channels) > 0 {
				m.activeTabIndex = (m.activeTabIndex + 1) % len(m.channels)
				if _, ok := m.messages[m.channels[m.activeTabIndex].ID]; !ok {
					cmd = fetchHistoryCmd(m.client, m.channels[m.activeTabIndex].ID)
				} else {
					m.chatViewport.SetContent(m.renderMessagesContent(m.channels[m.activeTabIndex].ID))
					m.chatViewport.GotoBottom()
				}
			}
			return m, cmd
		case tea.KeyShiftTab:
			if len(m.channels) > 0 {
				m.activeTabIndex--
				if m.activeTabIndex < 0 {
					m.activeTabIndex = len(m.channels) - 1
				}
				if _, ok := m.messages[m.channels[m.activeTabIndex].ID]; !ok {
					cmd = fetchHistoryCmd(m.client, m.channels[m.activeTabIndex].ID)
				} else {
					m.chatViewport.SetContent(m.renderMessagesContent(m.channels[m.activeTabIndex].ID))
					m.chatViewport.GotoBottom()
				}
			}
			return m, cmd
		case tea.KeyEnter:
			content := strings.TrimSpace(m.textInput.Value())
			if content != "" && len(m.channels) > 0 {
				activeChannel := m.channels[m.activeTabIndex]
				err := m.client.SendMessage(activeChannel.ID, m.client.User.ID, content)
				if err != nil {
					m.err = fmt.Errorf("send failed: %w", err)
				}
				m.textInput.Reset()
			}
			// Always scroll to bottom after sending
			m.chatViewport.GotoBottom()
		case tea.KeyUp, tea.KeyPgUp:
			if m.viewportReady {
				m.chatViewport.LineUp(1)
			}
		case tea.KeyDown, tea.KeyPgDown:
			if m.viewportReady {
				m.chatViewport.LineDown(1)
			}
		case tea.KeyHome:
			if m.viewportReady {
				m.chatViewport.GotoTop()
			}
		case tea.KeyEnd:
			if m.viewportReady {
				m.chatViewport.GotoBottom()
			}
		}
		// Pass key to text input unless it's for viewport
		if !isViewportScrollKey(msg) {
			m.textInput, cmd = m.textInput.Update(msg)
			cmds = append(cmds, cmd)
		}

	case initialDataLoadedMsg:
		m.state = stateChatting
		for _, cat := range msg.cats {
			m.channels = append(m.channels, cat.Channels...)
		}
		sort.SliceStable(m.channels, func(i, j int) bool {
			return m.channels[i].Position < m.channels[j].Position
		})
		if len(m.channels) > 0 {
			cmds = append(cmds, fetchHistoryCmd(m.client, m.channels[0].ID))
			m.lastChannelID = m.channels[0].ID
		} else {
			m.err = fmt.Errorf("no channels found on server")
			m.state = stateError
		}

	case historyLoadedMsg:
		m.messages[msg.channelID] = msg.msgs
		m.lastChannelID = msg.channelID
		if m.viewportReady {
			m.chatViewport.SetContent(m.renderMessagesContent(msg.channelID))
			m.chatViewport.GotoBottom()
		}

	case wsMsg:
		switch msg.msg.Event {
		case "new_message":
			var newMsg Message
			if err := json.Unmarshal(msg.msg.Payload, &newMsg); err == nil {
				if _, ok := m.messages[newMsg.ChannelID]; !ok {
					m.messages[newMsg.ChannelID] = make([]Message, 0)
				}
				m.messages[newMsg.ChannelID] = append(m.messages[newMsg.ChannelID], newMsg)
				if m.viewportReady && len(m.channels) > 0 && m.channels[m.activeTabIndex].ID == newMsg.ChannelID {
					m.chatViewport.SetContent(m.renderMessagesContent(newMsg.ChannelID))
					m.chatViewport.GotoBottom()
				}
			}
		case "presence_update":
			var users []User
			if err := json.Unmarshal(msg.msg.Payload, &users); err == nil {
				newOnlineUsers := make(map[string]bool)
				for _, u := range users {
					newOnlineUsers[u.Username] = true
				}
				m.onlineUsers = newOnlineUsers
			}
		}

	case errOccurredMsg:
		m.err = msg.err
		m.state = stateError
	}

	// Always pass text input update, unless handled above
	if cmd == nil && !isViewportScrollKey(msg) {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// Utility: detect keys for viewport scrolling
func isViewportScrollKey(msg tea.Msg) bool {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch km.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		return true
	}
	return false
}

// View() and its rendering helpers

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.state {
	case stateLoading:
		return "Connecting and loading channels..."
	case stateError:
		return fmt.Sprintf("An error occurred: %v\n\nPress Esc or Ctrl+C to exit.", m.err)
	default:
		var chatContent strings.Builder
		header := headerStyle.Width(m.width).Render(fmt.Sprintf("Logged in as: %s", m.client.User.Username))
		chatContent.WriteString(header + "\n")
		tabs := m.renderTabs()
		chatContent.WriteString(tabs + "\n")
		chatPane := m.renderMessagesViewport()
		userPane := m.renderUserList()
		mainContent := lipgloss.JoinHorizontal(lipgloss.Top, chatPane, userPane)
		chatContent.WriteString(mainContent + "\n")
		chatContent.WriteString(m.textInput.View())
		return chatContent.String()
	}
}

func (m model) renderTabs() string {
	var renderedTabs []string
	for i, ch := range m.channels {
		style := tabStyle
		if i == m.activeTabIndex {
			style = activeTabStyle
		}
		renderedTabs = append(renderedTabs, style.Render("#"+ch.Name))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
}

// NEW: Use viewport for messages
func (m model) renderMessagesViewport() string {
	if !m.viewportReady {
		return lipgloss.NewStyle().Width(m.width-25).Height(m.height-5).Render("Loading...")
	}
	return m.chatViewport.View()
}

// Helper: render messages content for viewport
func (m model) renderMessagesContent(channelID int64) string {
	chatWidth := m.width - 25
	if chatWidth < 1 {
		chatWidth = 1
	}
	var sb strings.Builder
	msgs := m.messages[channelID]
	for _, msg := range msgs {
		timeStr := msg.CreatedAt.Format("15:04")
		prefix := fmt.Sprintf("[%s] %s:", timeStr, msg.Username)
		style := otherMsgStyle
		if msg.Username == m.client.User.Username {
			style = myMsgStyle
		}
		line := fmt.Sprintf("%s %s", prefix, msg.Content)
		sb.WriteString(style.Render(line) + "\n")
	}
	return sb.String()
}

func (m model) renderUserList() string {
	listWidth := 20
	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	var sb strings.Builder
	sb.WriteString(userListHeader.Render("Users Online") + "\n")
	var userNames []string
	for u := range m.onlineUsers {
		userNames = append(userNames, u)
	}
	sort.Strings(userNames)

	for _, u := range userNames {
		sb.WriteString(onlineUserStyle.Render("• "+u) + "\n")
	}

	return userListStyle.Width(listWidth).Height(listHeight).Render(sb.String())
}