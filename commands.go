package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// CommandHandler defines the interface for command processors
type CommandHandler interface {
	Execute(args []string, agent *Agent) error
	Description() string
}

// CommandRegistry manages all available commands
type CommandRegistry struct {
	commands map[string]CommandHandler
}

// ClearCommand creates a new session
type ClearCommand struct{}

func (cc *ClearCommand) Description() string {
	return "Clears the current session and starts a new one."
}

func (cc *ClearCommand) Execute(args []string, agent *Agent) error {
	// Reuse the createSession logic from SessionCommand
	sc := &SessionCommand{}
	return sc.createSession("", agent)
}

func NewCommandRegistry() *CommandRegistry {
	registry := &CommandRegistry{
		commands: make(map[string]CommandHandler),
	}

	// Register built-in commands
	registry.RegisterCommand("session", &SessionCommand{})
	registry.RegisterCommand("help", &HelpCommand{registry: registry})
	registry.RegisterCommand("clear", &ClearCommand{})

	return registry
}

func (cr *CommandRegistry) RegisterCommand(name string, handler CommandHandler) {
	cr.commands[name] = handler
}

func (cr *CommandRegistry) ExecuteCommand(input string, agent *Agent) (bool, error) {
	if !strings.HasPrefix(input, "/") {
		return false, nil // Not a command
	}

	// Parse command and arguments
	parts := strings.Fields(input[1:]) // Remove leading "/"
	if len(parts) == 0 {
		return true, fmt.Errorf("empty command")
	}

	commandName := parts[0]
	args := parts[1:]

	handler, exists := cr.commands[commandName]
	if !exists {
		return true, fmt.Errorf("unknown command: %s", commandName)
	}

	return true, handler.Execute(args, agent)
}

func (cr *CommandRegistry) ListCommands() map[string]CommandHandler {
	return cr.commands
}

// SessionCommand handles session management
type SessionCommand struct{}

func (sc *SessionCommand) Description() string {
	return "Manage conversation sessions - list, switch, create, or delete sessions"
}

func (sc *SessionCommand) Execute(args []string, agent *Agent) error {
	if len(args) == 0 {
		return sc.listSessions(agent)
	}

	switch args[0] {
	case "list", "ls":
		return sc.listSessions(agent)
	case "switch", "sw":
		if len(args) < 2 {
			return fmt.Errorf("usage: /session switch <session_id>")
		}
		sessionID, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid session ID: %s", args[1])
		}
		return sc.switchSession(sessionID, agent)
	case "new", "create":
		name := ""
		if len(args) > 1 {
			name = strings.Join(args[1:], " ")
		}
		return sc.createSession(name, agent)
	case "delete", "del":
		if len(args) < 2 {
			return fmt.Errorf("usage: /session delete <session_id>")
		}
		sessionID, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid session ID: %s", args[1])
		}
		return sc.deleteSession(sessionID, agent)
	case "rename":
		if len(args) < 3 {
			return fmt.Errorf("usage: /session rename <session_id> <new_name>")
		}
		sessionID, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid session ID: %s", args[1])
		}
		newName := strings.Join(args[2:], " ")
		return sc.renameSession(sessionID, newName, agent)
	default:
		return fmt.Errorf("unknown session command: %s. Available: list, switch, new, delete, rename", args[0])
	}
}

func (sc *SessionCommand) listSessions(agent *Agent) error {
	sessions, err := agent.database.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("\u001b[93mNo sessions found. Create a new session with /session new [name]\u001b[0m")
		return nil
	}

	fmt.Println("\u001b[92m=== Available Sessions ===\u001b[0m")
	for _, session := range sessions {
		currentMarker := ""
		if agent.currentSessionID == session.ID {
			currentMarker = " \u001b[92m(current)\u001b[0m"
		}

		fmt.Printf("\u001b[94m%d\u001b[0m: %s%s\n", session.ID, session.Name, currentMarker)
		fmt.Printf("   Created: %s, Updated: %s\n",
			session.CreatedAt.Format("2006-01-02 15:04"),
			session.UpdatedAt.Format("2006-01-02 15:04"))
	}

	fmt.Println("\nUse '\u001b[94m/session switch <id>\u001b[0m' to switch to a session")
	return nil
}

func (sc *SessionCommand) switchSession(sessionID int, agent *Agent) error {
	// Verify session exists
	session, err := agent.database.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("session %d not found: %w", sessionID, err)
	}

	// Load conversation history
	messages, err := agent.database.GetSessionMessages(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session messages: %w", err)
	}

	// Convert to anthropic format
	conversation, err := ConvertDBMessagesToAnthropic(messages)
	if err != nil {
		return fmt.Errorf("failed to convert messages: %w", err)
	}

	// Switch to session
	agent.currentSessionID = sessionID
	agent.conversation = conversation
	agent.isFirstQueryInSession = true // Mark as first query for new session

	fmt.Printf("\u001b[92mSwitched to session %d: %s\u001b[0m\n", session.ID, session.Name)

	if len(messages) > 0 {
		fmt.Printf("Loaded %d messages from this session\n\n", len(messages))

		// Display conversation history
		fmt.Println("\u001b[96m=== Conversation History ===\u001b[0m")
		sc.displayConversationHistory(messages)
		fmt.Println("\u001b[96m=== End of History ===\u001b[0m")
	} else {
		fmt.Println("This is a new session with no previous messages.")
	}

	return nil
}

func (sc *SessionCommand) displayConversationHistory(messages []Message) {
	for _, msg := range messages {
		// Display based on role
		switch msg.Role {
		case "user":
			fmt.Print("\u001b[94mYou\u001b[0m: ")
		case "assistant":
			fmt.Print("\u001b[93mClaude\u001b[0m: ")
		default:
			fmt.Printf("%s: ", msg.Role)
		}

		// Parse content as raw JSON to extract text
		var rawContent []map[string]interface{}
		err := json.Unmarshal([]byte(msg.Content), &rawContent)
		if err != nil {
			fmt.Printf("Error parsing message: %v\n", err)
			continue
		}

		// Extract and display content
		for _, block := range rawContent {
			if blockType, ok := block["type"].(string); ok {
				switch blockType {
				case "text":
					if text, ok := block["text"].(string); ok {
						fmt.Println(text)
					}
				case "tool_use":
					if name, ok := block["name"].(string); ok {
						fmt.Printf("[Used tool: %s]\n", name)
					}
				case "tool_result":
					if content, ok := block["content"].(string); ok {
						if isError, ok := block["is_error"].(bool); ok && isError {
							fmt.Printf("[Tool error: %s]\n", content)
						} else {
							fmt.Printf("[Tool result: %s]\n", content)
						}
					}
				}
			}
		}
	}
}

func (sc *SessionCommand) createSession(name string, agent *Agent) error {
	session, err := agent.database.CreateSession(name)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	// Switch to new session
	agent.currentSessionID = session.ID
	agent.conversation = []anthropic.MessageParam{} // Start fresh
	agent.isFirstQueryInSession = true              // Mark as first query for new session

	fmt.Printf("\u001b[92mCreated and switched to new session %d: %s\u001b[0m\n", session.ID, session.Name)
	return nil
}

func (sc *SessionCommand) deleteSession(sessionID int, agent *Agent) error {
	// Verify session exists
	session, err := agent.database.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("session %d not found: %w", sessionID, err)
	}

	// Confirm deletion
	fmt.Printf("Are you sure you want to delete session %d: %s? (y/N): ", session.ID, session.Name)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("failed to read confirmation")
	}

	response := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if response != "y" && response != "yes" {
		fmt.Println("Deletion cancelled")
		return nil
	}

	err = agent.database.DeleteSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	// If we deleted the current session, create a new one
	if agent.currentSessionID == sessionID {
		newSession, err := agent.database.CreateSession("")
		if err != nil {
			return fmt.Errorf("failed to create replacement session: %w", err)
		}
		agent.currentSessionID = newSession.ID
		agent.conversation = []anthropic.MessageParam{}
		fmt.Printf("Deleted session and created new session %d\n", newSession.ID)
	} else {
		fmt.Printf("Deleted session %d: %s\n", session.ID, session.Name)
	}

	return nil
}

func (sc *SessionCommand) renameSession(sessionID int, newName string, agent *Agent) error {
	// Verify session exists
	_, err := agent.database.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("session %d not found: %w", sessionID, err)
	}

	err = agent.database.RenameSession(sessionID, newName)
	if err != nil {
		return fmt.Errorf("failed to rename session: %w", err)
	}

	fmt.Printf("\u001b[92mRenamed session %d to: %s\u001b[0m\n", sessionID, newName)
	return nil
}

// HelpCommand shows available commands
type HelpCommand struct {
	registry *CommandRegistry
}

func (hc *HelpCommand) Description() string {
	return "Show available commands and their descriptions"
}

func (hc *HelpCommand) Execute(args []string, agent *Agent) error {
	fmt.Println("\u001b[92m=== Available Commands ===\u001b[0m")

	for name, handler := range hc.registry.commands {
		fmt.Printf("\u001b[94m/%s\u001b[0m - %s\n", name, handler.Description())
	}

	fmt.Println("\nSession management:")
	fmt.Println("  \u001b[94m/session list\u001b[0m     - List all sessions")
	fmt.Println("  \u001b[94m/session switch <id>\u001b[0m - Switch to session")
	fmt.Println("  \u001b[94m/session new [name]\u001b[0m - Create new session")
	fmt.Println("  \u001b[94m/session delete <id>\u001b[0m - Delete session")
	fmt.Println("  \u001b[94m/session rename <id> <name>\u001b[0m - Rename session")
	fmt.Println("  \u001b[94m/clear\u001b[0m - Clears the current session and starts a new one.")

	return nil
}
