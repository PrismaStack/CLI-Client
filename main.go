package main

import (
	"bufio"
	"flag"
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
	// --- Command-line flags ---
	var (
		flagUsername string
		flagPassword string
	)
	flag.StringVar(&flagUsername, "username", "", "Username to log in with")
	flag.StringVar(&flagPassword, "password", "", "Password to log in with")
	flag.Parse()

//	serverBaseURL := "https://chat.sarahsforge.dev:443"
	serverBaseURL := "http://localhost:8081"
	fmt.Printf("Attempting to connect to server at %s\n", serverBaseURL)

	client := tui.NewApiClient(serverBaseURL)
	reader := bufio.NewReader(os.Stdin)

	username := strings.TrimSpace(flagUsername)
	password := flagPassword

	// --- Login Loop ---
	for {
		if username == "" {
			fmt.Print("Enter username: ")
			rawUser, _ := reader.ReadString('\n')
			username = strings.TrimSpace(rawUser)
		}

		if password == "" {
			fmt.Print("Enter password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				log.Fatalf("Failed to read password: %v", err)
			}
			password = string(bytePassword)
			fmt.Println()
		}

		err := client.Login(username, password)
		if err != nil {
			fmt.Printf("Login failed: %v. Please try again.\n", err)
			// Reset password so it is prompted again if login fails.
			password = ""
			continue
		}
		fmt.Println("Login successful! Starting chat...")
		break
	}

	// --- Launch UI ---
	p := tea.NewProgram(tui.InitialModel(client), tea.WithAltScreen(), tea.WithMouseAllMotion())

	// Start the WebSocket listener.
	go client.ConnectAndListen(p)

	if _, err := p.Run(); err != nil {
		log.Fatalf("UI Error: %v", err)
	}
	fmt.Println("Goodbye!")
}