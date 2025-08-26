package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"
)

// ANSI color codes for the improved UI
const (
	// User message colors (yellow-green theme)
	UserBorderColor = "\033[38;5;154m" // Bright yellow-green

	// Claude message colors (blue theme)
	ClaudeBorderColor = "\033[38;5;39m" // Bright blue

	// Tool execution colors
	ToolColor = "\033[38;5;208m" // Orange

	// Reset and other colors
	ResetColor   = "\033[0m"
	PromptColor  = "\033[38;5;75m"  // Light blue for prompts
	InfoColor    = "\033[38;5;244m" // Gray for info
	SuccessColor = "\033[38;5;46m"  // Green for success
)

// Tips for users - easily expandable array
var userTips = []string{
	"Use @filename to add files to your current context",
	"Use /clear often to reset context and reduce hallucinations",
	"Try /session to view your current conversation history",
	"Use Ctrl+C to quit the application anytime",
	"Fuzzy matching works with @filename - you don't need exact names",
	"Keep your prompts specific for better responses",
	"Use /help to see all available commands",
}

// MessageBox represents a styled message container
type MessageBox struct {
	Content     string
	Sender      string
	BorderColor string
	Width       int
}

// NewUserMessageBox creates a styled box for user messages
func NewUserMessageBox(content string) *MessageBox {
	return &MessageBox{
		Content:     content,
		Sender:      "You",
		BorderColor: UserBorderColor,
		Width:       80,
	}
}

// NewClaudeMessageBox creates a styled box for Claude messages
func NewClaudeMessageBox(content string) *MessageBox {
	return &MessageBox{
		Content:     content,
		Sender:      "GoAgent",
		BorderColor: ClaudeBorderColor,
		Width:       80,
	}
}

// Render displays the message box with proper styling
func (mb *MessageBox) Render() {
	// Wrap content to fit within the box
	wrappedLines := mb.wrapText(mb.Content, mb.Width-4) // Account for padding

	// Calculate box dimensions
	maxLineLength := 0
	for _, line := range wrappedLines {
		if length := utf8.RuneCountInString(line); length > maxLineLength {
			maxLineLength = length
		}
	}

	// Ensure minimum width for sender name
	senderLength := utf8.RuneCountInString(mb.Sender)
	if senderLength+4 > maxLineLength {
		maxLineLength = senderLength + 4
	}

	boxWidth := maxLineLength + 4 // Add padding

	// Print top border with sender name
	fmt.Print(mb.BorderColor)
	fmt.Print("┌─ ")
	fmt.Print(mb.Sender)
	fmt.Print(" ")
	fmt.Print(strings.Repeat("─", boxWidth-senderLength-4))
	fmt.Println("┐" + ResetColor)

	// Print content lines
	for _, line := range wrappedLines {
		fmt.Print(mb.BorderColor + "│ " + ResetColor)
		fmt.Print(line)
		// Pad line to full width
		padding := boxWidth - utf8.RuneCountInString(line) - 2
		fmt.Print(strings.Repeat(" ", padding))
		fmt.Print(mb.BorderColor + "│" + ResetColor)
		fmt.Println()
	}

	// Print bottom border
	fmt.Print(mb.BorderColor)
	fmt.Print("└")
	fmt.Print(strings.Repeat("─", boxWidth))
	fmt.Println("┘" + ResetColor)
	fmt.Println() // Add spacing between messages
}

// wrapText wraps text to fit within the specified width
func (mb *MessageBox) wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	var lines []string
	paragraphs := strings.Split(text, "\n")

	for _, paragraph := range paragraphs {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}

		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		var currentLine strings.Builder
		currentLength := 0

		for _, word := range words {
			wordLength := utf8.RuneCountInString(word)

			// If adding this word would exceed width, start new line
			if currentLength > 0 && currentLength+1+wordLength > width {
				lines = append(lines, currentLine.String())
				currentLine.Reset()
				currentLength = 0
			}

			// Add word to current line
			if currentLength > 0 {
				currentLine.WriteString(" ")
				currentLength++
			}
			currentLine.WriteString(word)
			currentLength += wordLength
		}

		// Add the last line if it has content
		if currentLine.Len() > 0 {
			lines = append(lines, currentLine.String())
		}
	}

	// Ensure we have at least one line
	if len(lines) == 0 {
		lines = append(lines, "")
	}

	return lines
}

// PrintWelcomeMessage displays the application welcome message
func PrintWelcomeMessage() {
	fmt.Println(InfoColor + "╔══════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                          Chat with GoAgent                                  ║")
	fmt.Println("║                                                                              ║")
	fmt.Println("║  " + SuccessColor + "Tips:" + InfoColor + "                                                                      ║")
	fmt.Println("║    • Use @filename to include files in your context (supports fuzzy match)  ║")
	fmt.Println("║    • Use Ctrl+C to quit                                                     ║")
	fmt.Println("║    • Commands: /session, /help                                              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════════╝" + ResetColor)
	fmt.Println()
}

// PrintPrompt displays the user input prompt
func PrintPrompt() {
	fmt.Print(PromptColor + "┌─ Your message" + ResetColor + "\n")
	fmt.Print(PromptColor + "└─► " + ResetColor)
}

// PrintRandomTip displays a random tip for the user
func PrintRandomTip() {
	rand.Seed(time.Now().UnixNano())
	randomTip := userTips[rand.Intn(len(userTips))]
	fmt.Print(InfoColor + "💡 Tip: " + randomTip + ResetColor + "\n\n")
}

// PrintToolExecution displays tool execution information
func PrintToolExecution(toolName string, input string) {
	fmt.Print(ToolColor + "🔧 " + toolName + ResetColor + InfoColor + "(" + input + ")" + ResetColor + "\n")
}

// PrintIncludedFiles displays information about included files
func PrintIncludedFiles(files []string) {
	if len(files) > 0 {
		fmt.Print(InfoColor + "📁 Included files: " + SuccessColor + strings.Join(files, ", ") + ResetColor + "\n\n")
	}
}

// PrintError displays error messages
func PrintError(message string) {
	fmt.Print("\033[38;5;196m❌ Error: " + message + ResetColor + "\n")
}

// PrintWarning displays warning messages
func PrintWarning(message string) {
	fmt.Print("\033[38;5;214m⚠️  Warning: " + message + ResetColor + "\n")
}

// PrintSuccess displays success messages
func PrintSuccess(message string) {
	fmt.Print(SuccessColor + "✅ " + message + ResetColor + "\n")
}
