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
	// User message colors (enhanced gradient theme)
	UserBorderColor    = "\033[38;5;154m" // Bright yellow-green
	UserAccentColor    = "\033[38;5;148m" // Softer yellow-green
	UserTextColor      = "\033[38;5;255m" // White text for better readability
	UserTimestampColor = "\033[38;5;243m" // Gray for timestamps

	// Claude message colors (light yellow theme)
	ClaudeBorderColor = "\033[38;5;229m" // Light yellow

	// Tool execution colors
	ToolColor = "\033[38;5;208m" // Orange

	// Reset and other colors
	ResetColor   = "\033[0m"
	PromptColor  = "\033[38;5;75m"  // Light blue for prompts
	InfoColor    = "\033[38;5;244m" // Gray for info
	SuccessColor = "\033[38;5;46m"  // Green for success

	// Code styling colors
	CodeTextColor  = "\033[38;5;250m" // Light gray text (90% brightness)
	InlineCodeBg   = "\033[48;5;251m" // Lighter gray
	InlineCodeText = "\033[38;5;240m" // Medium gray text

	// Language-specific syntax colors
	KeywordColor  = "\033[38;5;207m" // Magenta for keywords
	StringColor   = "\033[38;5;222m" // Yellow for strings
	CommentColor  = "\033[38;5;244m" // Gray for comments
	FunctionColor = "\033[38;5;81m"  // Cyan for functions

	// App layout constants
	LeftMargin = "  " // Left margin for the entire app
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
	AccentColor string
	TextColor   string
	Width       int
	Timestamp   time.Time
	IsUser      bool
}

// NewUserMessageBox creates a styled box for user messages
func NewUserMessageBox(content string) *MessageBox {
	return &MessageBox{
		Content:     content,
		Sender:      "You",
		BorderColor: UserBorderColor,
		AccentColor: UserAccentColor,
		TextColor:   UserTextColor,
		Width:       85,
		Timestamp:   time.Now(),
		IsUser:      true,
	}
}

// NewClaudeMessageBox creates a styled box for Claude messages
func NewClaudeMessageBox(content string) *MessageBox {
	return &MessageBox{
		Content:     content,
		Sender:      "GoAgent",
		BorderColor: ClaudeBorderColor,
		AccentColor: ClaudeBorderColor,
		TextColor:   ResetColor,
		Width:       85,
		Timestamp:   time.Now(),
		IsUser:      false,
	}
}

// Render displays the message box with proper styling
func (mb *MessageBox) Render() {
	// Format code content first
	formattedContent := mb.formatCodeContent(mb.Content)

	// Wrap content to fit within the box
	wrappedLines := mb.wrapText(formattedContent, mb.Width-6) // Account for padding and visual indicators

	// Calculate box dimensions
	maxLineLength := 0
	for _, line := range wrappedLines {
		if length := mb.getVisibleLength(line); length > maxLineLength {
			maxLineLength = length
		}
	}

	// Format timestamp
	timestamp := mb.Timestamp.Format("3:04 PM")

	// Create header with sender name and timestamp
	headerText := mb.Sender + " • " + timestamp
	headerLength := utf8.RuneCountInString(headerText)

	// Ensure minimum width
	minWidth := headerLength + 6
	if maxLineLength+6 > minWidth {
		minWidth = maxLineLength + 6
	}

	boxWidth := minWidth

	// Visual indicator for message type
	var indicator string
	if mb.IsUser {
		indicator = "👤"
	} else {
		indicator = "🤖"
	}

	// Print top border with enhanced header
	fmt.Print(LeftMargin + mb.BorderColor)
	fmt.Print("┌─ " + indicator + " ")
	fmt.Print(mb.AccentColor + mb.Sender + ResetColor + mb.BorderColor)
	fmt.Print(" • ")
	fmt.Print(UserTimestampColor + timestamp + ResetColor + mb.BorderColor)
	fmt.Print(" ")
	remainingWidth := boxWidth - utf8.RuneCountInString(headerText) - 8
	if remainingWidth > 0 {
		fmt.Print(strings.Repeat("─", remainingWidth))
	}
	fmt.Println("┐" + ResetColor)

	// Print content lines with enhanced styling
	for i, line := range wrappedLines {
		fmt.Print(LeftMargin + mb.BorderColor + "│" + ResetColor)

		// Add extra padding for user messages
		if mb.IsUser {
			fmt.Print("  " + mb.TextColor + line + ResetColor)
		} else {
			fmt.Print(" " + line)
		}

		// Calculate padding for this specific line using visible character count
		lineLength := mb.getVisibleLength(line)
		paddingNeeded := boxWidth - lineLength - 2
		if mb.IsUser {
			paddingNeeded -= 2 // Account for extra user padding
		}

		if paddingNeeded > 0 {
			fmt.Print(strings.Repeat(" ", paddingNeeded))
		}

		fmt.Print(mb.BorderColor + "│" + ResetColor)
		fmt.Println()

		// Add subtle separator line for multi-line user messages
		if mb.IsUser && i < len(wrappedLines)-1 && len(wrappedLines) > 2 {
			fmt.Print(LeftMargin + mb.BorderColor + "│" + ResetColor)
			fmt.Print(strings.Repeat(" ", boxWidth))
			fmt.Print(mb.BorderColor + "│" + ResetColor)
			fmt.Println()
		}
	}

	// Print bottom border with subtle styling
	fmt.Print(LeftMargin + mb.BorderColor)
	fmt.Print("└")
	fmt.Print(strings.Repeat("─", boxWidth))
	fmt.Println("┘" + ResetColor)
	fmt.Println() // Add spacing between messages
}

// formatCodeContent applies syntax highlighting to code blocks and inline code
func (mb *MessageBox) formatCodeContent(text string) string {
	// Normalize backticks
	text = strings.ReplaceAll(text, "```", "```")

	// Split by triple backticks to find code blocks
	parts := strings.Split(text, "```")
	var result strings.Builder

	for i, part := range parts {
		if i%2 == 0 {
			// Regular text - handle inline code
			formattedPart := mb.formatInlineCode(part)
			result.WriteString(formattedPart)

			// Add spacing before code block if this text part ends without a newline
			// and there's a code block coming next
			if i < len(parts)-1 && !strings.HasSuffix(strings.TrimSpace(formattedPart), "\n") {
				result.WriteString("\n")
			}
		} else {
			// Code block content
			lines := strings.Split(part, "\n")
			if len(lines) > 1 {
				// Skip language identifier on first line
				codeContent := strings.Join(lines[1:], "\n")
				result.WriteString(mb.formatCodeBlock(codeContent))
			} else {
				result.WriteString(mb.formatCodeBlock(part))
			}

			// Add spacing after code block if there's more content
			if i < len(parts)-1 {
				result.WriteString("\n")
			}
		}
	}

	return result.String()
}

// formatInlineCode handles `inline code` formatting
func (mb *MessageBox) formatInlineCode(text string) string {
	parts := strings.Split(text, "`")
	var result strings.Builder

	for i, part := range parts {
		if i%2 == 0 {
			// Regular text
			result.WriteString(part)
		} else {
			// Inline code
			result.WriteString(InlineCodeBg + InlineCodeText + part + ResetColor)
		}
	}

	return result.String()
}

// formatCodeBlock formats multi-line code blocks
func (mb *MessageBox) formatCodeBlock(code string) string {
	lines := strings.Split(code, "\n")
	var result strings.Builder

	for _, line := range lines {
		result.WriteString(CodeTextColor + line + ResetColor + "\n")
	}

	return strings.TrimSuffix(result.String(), "\n")
}

// getVisibleLength returns the visible character count of a string, ignoring ANSI escape codes
func (mb *MessageBox) getVisibleLength(text string) int {
	// Remove ANSI escape codes for accurate length calculation
	cleanText := text
	for {
		start := strings.Index(cleanText, "\033[")
		if start == -1 {
			break
		}
		end := strings.Index(cleanText[start:], "m")
		if end == -1 {
			break
		}
		cleanText = cleanText[:start] + cleanText[start+end+1:]
	}
	return utf8.RuneCountInString(cleanText)
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
	fmt.Println(LeftMargin + InfoColor + "╔══════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println(LeftMargin + "║                          Chat with GoAgent                                  ║")
	fmt.Println(LeftMargin + "║                                                                              ║")
	fmt.Println(LeftMargin + "║  " + SuccessColor + "Tips:" + InfoColor + "                                                                      ║")
	fmt.Println(LeftMargin + "║    • Use @filename to include files in your context (supports fuzzy match)  ║")
	fmt.Println(LeftMargin + "║    • Use Ctrl+C to quit                                                     ║")
	fmt.Println(LeftMargin + "║    • Commands: /session, /help                                              ║")
	fmt.Println(LeftMargin + "╚══════════════════════════════════════════════════════════════════════════════╝" + ResetColor)
	fmt.Println()
}

// PrintPrompt displays the user input prompt
func PrintPrompt() {
	fmt.Print(LeftMargin + PromptColor + "┌─ Your message" + ResetColor + "\n")
	fmt.Print(LeftMargin + PromptColor + "└─► " + ResetColor)
}

// PrintRandomTip displays a random tip for the user
func PrintRandomTip() {
	rand.Seed(time.Now().UnixNano())
	randomTip := userTips[rand.Intn(len(userTips))]
	fmt.Print(LeftMargin + InfoColor + "💡 Tip: " + randomTip + ResetColor + "\n\n")
}

// PrintToolExecution displays tool execution information
func PrintToolExecution(toolName string, input string) {
	fmt.Print(LeftMargin + ToolColor + "🔧 " + toolName + ResetColor + InfoColor + "(" + input + ")" + ResetColor + "\n")
}

// PrintIncludedFiles displays information about included files
func PrintIncludedFiles(files []string) {
	if len(files) > 0 {
		fmt.Print(LeftMargin + InfoColor + "📁 Included files: " + SuccessColor + strings.Join(files, ", ") + ResetColor + "\n\n")
	}
}

// PrintError displays error messages
func PrintError(message string) {
	fmt.Print(LeftMargin + "\033[38;5;196m❌ Error: " + message + ResetColor + "\n")
}

// PrintWarning displays warning messages
func PrintWarning(message string) {
	fmt.Print(LeftMargin + "\033[38;5;214m⚠️  Warning: " + message + ResetColor + "\n")
}

// PrintSuccess displays success messages
func PrintSuccess(message string) {
	fmt.Print(LeftMargin + SuccessColor + "✅ " + message + ResetColor + "\n")
}

// LoadingIndicator represents a loading animation
type LoadingIndicator struct {
	message string
	frames  []string
	stop    chan bool
	done    chan bool
}

// NewLoadingIndicator creates a new loading indicator
func NewLoadingIndicator(message string) *LoadingIndicator {
	return &LoadingIndicator{
		message: message,
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stop:    make(chan bool),
		done:    make(chan bool),
	}
}

// Start begins the loading animation
func (li *LoadingIndicator) Start() {
	go func() {
		defer close(li.done)
		frameIndex := 0

		for {
			select {
			case <-li.stop:
				// Clear the loading line
				fmt.Print("\r" + strings.Repeat(" ", len(li.message)+10) + "\r")
				return
			default:
				// Print current frame
				fmt.Printf("\r%s%s%s %s%s", LeftMargin, ClaudeBorderColor, li.frames[frameIndex], li.message, ResetColor)
				frameIndex = (frameIndex + 1) % len(li.frames)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

// Stop ends the loading animation
func (li *LoadingIndicator) Stop() {
	close(li.stop)
	<-li.done // Wait for goroutine to finish
}

// applySyntaxHighlighting applies basic syntax highlighting to code lines
func (mb *MessageBox) applySyntaxHighlighting(line string) string {
	// Handle strings first (simple approach for "..." and '...')
	line = mb.highlightStrings(line)

	// Handle comments (// and /* */)
	line = mb.highlightComments(line)

	// Handle common keywords
	keywords := []string{
		"func", "var", "const", "if", "else", "for", "while", "return", "import", "package",
		"class", "def", "async", "await", "function", "let", "const", "export", "default",
		"public", "private", "static", "void", "int", "string", "bool", "interface", "struct",
	}

	for _, keyword := range keywords {
		line = strings.ReplaceAll(line, keyword, KeywordColor+keyword+CodeTextColor)
	}

	return CodeTextColor + line
}

// highlightStrings highlights string literals
func (mb *MessageBox) highlightStrings(line string) string {
	// Simple string highlighting for "..." and '...'
	inDoubleQuote := false
	inSingleQuote := false
	var result strings.Builder

	for _, char := range line {
		switch char {
		case '"':
			if !inSingleQuote {
				if !inDoubleQuote {
					result.WriteString(StringColor + `"`)
					inDoubleQuote = true
				} else {
					result.WriteString(`"` + CodeTextColor)
					inDoubleQuote = false
				}
			} else {
				result.WriteRune(char)
			}
		case '\'':
			if !inDoubleQuote {
				if !inSingleQuote {
					result.WriteString(StringColor + "'")
					inSingleQuote = true
				} else {
					result.WriteString("'" + CodeTextColor)
					inSingleQuote = false
				}
			} else {
				result.WriteRune(char)
			}
		default:
			result.WriteRune(char)
		}
	}

	return result.String()
}

// highlightComments highlights code comments
func (mb *MessageBox) highlightComments(line string) string {
	// Handle // comments
	if idx := strings.Index(line, "//"); idx != -1 {
		beforeComment := line[:idx]
		comment := line[idx:]
		return beforeComment + CommentColor + comment + CodeTextColor
	}

	// Handle /* */ comments (simple single-line case)
	if strings.Contains(line, "/*") && strings.Contains(line, "*/") {
		startIdx := strings.Index(line, "/*")
		endIdx := strings.Index(line, "*/") + 2
		if startIdx < endIdx {
			before := line[:startIdx]
			comment := line[startIdx:endIdx]
			after := line[endIdx:]
			return before + CommentColor + comment + CodeTextColor + after
		}
	}

	return line
}
