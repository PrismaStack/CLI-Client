package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
	"prisma/tui" // CORRECTED: Use your project's module path
)

func main() {
	// --- Configuration ---
	serverBaseURL := "https://chat.sarahsforge.dev:443"
	fmt.Printf("Attempting to connect to server at %s\n", serverBaseURL)

	client := tui.NewApiClient(serverBaseURL)
	reader := bufio.NewReader(os.Stdin)

	// --- Login Loop ---
	for {
		fmt.Print("Enter username: ")
		username, _ := reader.ReadString('\n')
		username = strings.TrimSpace(username)

		fmt.Print("Enter password: ")
		bytePassword, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			log.Fatalf("Failed to read password: %v", err)
		}
		password := string(bytePassword)
		fmt.Println() // Newline after password input

		err = client.Login(username, password)
		if err != nil {
			fmt.Printf("Login failed: %v. Please try again.\n", err)
			continue
		}
		fmt.Println("Login successful! Starting chat...")
		break
	}

	// --- Launch UI ---
	p := tea.NewProgram(tui.InitialModel(client), tea.WithAltScreen(), tea.WithMouseAllMotion())

	// UPDATED: Start the WebSocket listener.
	// This goroutine runs in the background and sends messages (like new chats)
	// back to the tea.Program, which will process them in the Update function.
	go client.ConnectAndListen(p)

	if _, err := p.Run(); err != nil {
		log.Fatalf("UI Error: %v", err)
	}
	fmt.Println("Goodbye!")
}