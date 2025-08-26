package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/invopop/jsonschema"
)

//App wide structs

type Agent struct {
	client                *anthropic.Client
	getUserMessage        func() (string, bool)
	tools                 []ToolDefinition
	database              *Database
	currentSessionID      int
	conversation          []anthropic.MessageParam
	commandRegistry       *CommandRegistry
	isFirstQueryInSession bool
}

type ToolDefinition struct {
	Name        string                         `json:"name"`
	Description string                         `json:"description"`
	InputSchema anthropic.ToolInputSchemaParam `json:"input_schema"`
	Function    func(input json.RawMessage) (string, error)
}

//Entry point

func main() {
	client := anthropic.NewClient()

	// Initialize database
	database, err := NewDatabase("conversations.db")
	if err != nil {
		PrintError(fmt.Sprintf("Failed to initialize database: %s", err.Error()))
		os.Exit(1)
	}
	defer database.Close()

	scanner := bufio.NewScanner(os.Stdin)
	getUserMessage := func() (string, bool) {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				PrintError(fmt.Sprintf("Error reading input: %s", err.Error()))
			}
			return "", false
		}
		return scanner.Text(), true
	}

	tools := []ToolDefinition{ReadFileDefinition, ListFilesDefinition, EditFileDefinition, SearchDefinition, BashDefinition}
	agent := NewAgent(&client, getUserMessage, tools, database)
	err = agent.Run(context.TODO())
	if err != nil {
		PrintError(fmt.Sprintf("Agent error: %s", err.Error()))
		os.Exit(1)
	}
}

func NewAgent(client *anthropic.Client, getUserMessage func() (string, bool), tools []ToolDefinition, database *Database) *Agent {
	agent := &Agent{
		client:                client,
		getUserMessage:        getUserMessage,
		tools:                 tools,
		database:              database,
		conversation:          []anthropic.MessageParam{},
		commandRegistry:       NewCommandRegistry(),
		isFirstQueryInSession: true,
	}

	// Create or load default session
	session, err := database.CreateSession("")
	if err != nil {
		PrintWarning(fmt.Sprintf("Failed to create default session: %s", err.Error()))
		agent.currentSessionID = 0
	} else {
		agent.currentSessionID = session.ID
	}

	return agent
}

//Run main loop

func (a *Agent) Run(ctx context.Context) error {
	PrintWelcomeMessage()

	readUserInput := true
	for {
		if readUserInput {
			PrintPrompt()
			userInput, ok := a.getUserMessage()
			if !ok {
				break
			}

			// Rename session on first query
			a.renameSessionOnFirstQuery(userInput)

			// Check if this is a command
			isCommand, err := a.commandRegistry.ExecuteCommand(userInput, a)
			if err != nil {
				PrintError(fmt.Sprintf("Command error: %s", err.Error()))
				continue
			}
			if isCommand {
				continue // Command was executed, don't process as regular message
			}

			// Parse file mentions and include file content
			processedInput, mentionedFiles, err := parseFileMentions(userInput)
			if err != nil {
				PrintError(fmt.Sprintf("Error processing file mentions: %s", err.Error()))
				continue
			}

			// Show user which files were included
			PrintIncludedFiles(mentionedFiles)

			// Display a random tip after user input
			PrintRandomTip()

			userMessage := anthropic.NewUserMessage(anthropic.NewTextBlock(processedInput))
			a.conversation = append(a.conversation, userMessage)

			// Save user message to database
			if a.currentSessionID > 0 {
				err = a.saveMessageToDB(userMessage)
				if err != nil {
					PrintWarning(fmt.Sprintf("Failed to save user message: %s", err.Error()))
				}
			}
		}

		message, err := a.runInference(ctx, a.conversation)
		if err != nil {
			return fmt.Errorf("failed to run inference: %w", err)
		}
		a.conversation = append(a.conversation, message.ToParam())

		// Save assistant message to database
		if a.currentSessionID > 0 {
			err = a.saveMessageToDB(message.ToParam())
			if err != nil {
				PrintWarning(fmt.Sprintf("Failed to save assistant message: %s", err.Error()))
			}
		}

		toolResults := []anthropic.ContentBlockParamUnion{}
		var claudeResponse strings.Builder

		for _, content := range message.Content {
			switch content.Type {
			case "text":
				claudeResponse.WriteString(content.Text)
			case "tool_use":
				result := a.executeTool(content.ID, content.Name, content.Input)
				toolResults = append(toolResults, result)
			}
		}

		// Display Claude's response in styled box if there's text content
		if claudeResponse.Len() > 0 {
			claudeBox := NewClaudeMessageBox(claudeResponse.String())
			claudeBox.Render()
		}
		if len(toolResults) == 0 {
			readUserInput = true
			continue
		}
		readUserInput = false
		toolMessage := anthropic.NewUserMessage(toolResults...)
		a.conversation = append(a.conversation, toolMessage)

		// Save tool results to database
		if a.currentSessionID > 0 {
			err = a.saveMessageToDB(toolMessage)
			if err != nil {
				PrintWarning(fmt.Sprintf("Failed to save tool message: %s", err.Error()))
			}
		}
	}

	return nil
}

// Helper method to rename session on first query
func (a *Agent) renameSessionOnFirstQuery(query string) {
	if a.isFirstQueryInSession && a.currentSessionID > 0 {
		words := strings.Fields(query)
		newName := strings.Join(words, " ")
		if len(words) > 15 {
			newName = strings.Join(words[:15], " ")
		}

		err := a.database.RenameSession(a.currentSessionID, newName)
		if err != nil {
			PrintWarning(fmt.Sprintf("Failed to rename session: %s", err.Error()))
		}
		a.isFirstQueryInSession = false
	}
}

// Helper method to save messages to database
func (a *Agent) saveMessageToDB(message anthropic.MessageParam) error {
	if a.currentSessionID <= 0 {
		return fmt.Errorf("no active session")
	}

	// Convert message content to JSON for storage
	contentBytes, err := json.Marshal(message.Content)
	if err != nil {
		return fmt.Errorf("failed to marshal message content: %w", err)
	}

	role := string(message.Role)
	content := string(contentBytes)

	return a.database.SaveMessage(a.currentSessionID, role, content)
}

// ------------------------------------------------------------
//Execute tool and run inference (connecting to claude)
// ------------------------------------------------------------

func (a *Agent) executeTool(id, name string, input json.RawMessage) anthropic.ContentBlockParamUnion {
	var toolDef ToolDefinition
	var found bool
	for _, tool := range a.tools {
		if tool.Name == name {
			toolDef = tool
			found = true
			break
		}
	}
	if !found {
		return anthropic.NewToolResultBlock(id, fmt.Sprintf("tool '%s' not found", name), true)
	}

	PrintToolExecution(name, string(input))

	// Start loading indicator for tool execution
	loader := NewLoadingIndicator(fmt.Sprintf("Executing %s...", name))
	loader.Start()

	response, err := toolDef.Function(input)

	// Stop loading indicator
	loader.Stop()

	if err != nil {
		return anthropic.NewToolResultBlock(id, fmt.Sprintf("tool '%s' failed: %s", name, err.Error()), true)
	}
	return anthropic.NewToolResultBlock(id, response, false)
}

func (a *Agent) runInference(ctx context.Context, conversation []anthropic.MessageParam) (*anthropic.Message, error) {
	anthropicTools := []anthropic.ToolUnionParam{}
	for _, tool := range a.tools {
		anthropicTools = append(anthropicTools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        tool.Name,
				Description: anthropic.String(tool.Description),
				InputSchema: tool.InputSchema,
			},
		})
	}

	// Start loading indicator
	loader := NewLoadingIndicator("...")
	loader.Start()

	message, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_5HaikuLatest,
		MaxTokens: int64(1024),
		Messages:  conversation,
		Tools:     anthropicTools,
	})

	// Stop loading indicator
	loader.Stop()

	return message, err
}

// ------------------------------------------------------------
// Read files
// ------------------------------------------------------------

var ReadFileDefinition = ToolDefinition{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
	InputSchema: ReadFileInputSchema,
	Function:    ReadFile,
}

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

var ReadFileInputSchema = GenerateSchema[ReadFileInput]()

func ReadFile(input json.RawMessage) (string, error) {
	readFileInput := ReadFileInput{}
	err := json.Unmarshal(input, &readFileInput)
	if err != nil {
		return "", fmt.Errorf("failed to parse read file input: %w", err)
	}

	content, err := os.ReadFile(readFileInput.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", readFileInput.Path, err)
	}
	return string(content), nil
}

func GenerateSchema[T any]() anthropic.ToolInputSchemaParam {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T

	schema := reflector.Reflect(v)

	return anthropic.ToolInputSchemaParam{
		Properties: schema.Properties,
	}
}

// ------------------------------------------------------------
// List files
// ------------------------------------------------------------

var ListFilesDefinition = ToolDefinition{
	Name:        "list_files",
	Description: "List files and directories at a given path. If no path is provided, lists files in the current directory.",
	InputSchema: ListFilesInputSchema,
	Function:    ListFiles,
}

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Optional relative path to list files from. Defaults to current directory if not provided."`
}

var ListFilesInputSchema = GenerateSchema[ListFilesInput]()

func ListFiles(input json.RawMessage) (string, error) {
	listFilesInput := ListFilesInput{}
	err := json.Unmarshal(input, &listFilesInput)
	if err != nil {
		return "", fmt.Errorf("failed to parse list files input: %w", err)
	}

	dir := "."
	if listFilesInput.Path != "" {
		dir = listFilesInput.Path
	}

	var files []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if relPath != "." {
			if info.IsDir() {
				files = append(files, relPath+"/")
			} else {
				files = append(files, relPath)
			}
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to walk directory %s: %w", dir, err)
	}

	result, err := json.Marshal(files)
	if err != nil {
		return "", fmt.Errorf("failed to marshal file list: %w", err)
	}

	return string(result), nil
}

// ------------------------------------------------------------
//Create and edit files
// ------------------------------------------------------------

var EditFileDefinition = ToolDefinition{
	Name: "edit_file",
	Description: `Make edits to a text file.

Replaces 'old_str' with 'new_str' in the given file. 'old_str' and 'new_str' MUST be different from each other.

If the file specified with path doesn't exist, it will be created.
`,
	InputSchema: EditFileInputSchema,
	Function:    EditFile,
}

type EditFileInput struct {
	Path   string `json:"path" jsonschema_description:"The path to the file"`
	OldStr string `json:"old_str" jsonschema_description:"Text to search for - must match exactly and must only have one match exactly"`
	NewStr string `json:"new_str" jsonschema_description:"Text to replace old_str with"`
}

var EditFileInputSchema = GenerateSchema[EditFileInput]()

func EditFile(input json.RawMessage) (string, error) {
	editFileInput := EditFileInput{}
	err := json.Unmarshal(input, &editFileInput)
	if err != nil {
		return "", fmt.Errorf("failed to parse edit file input: %w", err)
	}

	if editFileInput.Path == "" {
		return "", fmt.Errorf("file path cannot be empty")
	}

	if editFileInput.OldStr == editFileInput.NewStr {
		return "", fmt.Errorf("old_str and new_str cannot be identical")
	}

	content, err := os.ReadFile(editFileInput.Path)
	if err != nil {
		if os.IsNotExist(err) && editFileInput.OldStr == "" {
			return createNewFile(editFileInput.Path, editFileInput.NewStr)
		}
		return "", fmt.Errorf("failed to read file %s: %w", editFileInput.Path, err)
	}

	oldContent := string(content)
	newContent := strings.Replace(oldContent, editFileInput.OldStr, editFileInput.NewStr, -1)

	if oldContent == newContent && editFileInput.OldStr != "" {
		return "", fmt.Errorf("old_str '%s' not found in file %s", editFileInput.OldStr, editFileInput.Path)
	}

	err = os.WriteFile(editFileInput.Path, []byte(newContent), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", editFileInput.Path, err)
	}

	return "OK", nil
}

func createNewFile(filePath, content string) (string, error) {
	dir := path.Dir(filePath)
	if dir != "." {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
	}

	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}

	return fmt.Sprintf("Successfully created file %s", filePath), nil
}

// ------------------------------------------------------------
// Add files as main context of the session
// ------------------------------------------------------------

type FileMatch struct {
	Path  string
	Score int
}

func fuzzySearchFiles(pattern string, searchDir string) ([]FileMatch, error) {
	var allFiles []string
	var matches []FileMatch

	// Walk through all files in the directory
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log but don't fail on individual file access errors
			fmt.Printf("Warning: skipping file due to error: %v\n", err)
			return nil
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(searchDir, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path for '%s': %w", path, err)
			}
			allFiles = append(allFiles, relPath)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory '%s': %w", searchDir, err)
	}

	// Score each file based on fuzzy match
	for _, file := range allFiles {
		score := calculateFuzzyScore(pattern, file)
		if score > 0 {
			matches = append(matches, FileMatch{Path: file, Score: score})
		}
	}

	// Sort by score (higher is better)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	return matches, nil
}

func calculateFuzzyScore(pattern, text string) int {
	pattern = strings.ToLower(pattern)
	text = strings.ToLower(text)

	// Exact match gets highest score
	if strings.Contains(text, pattern) {
		return 1000 + len(pattern)
	}

	// Check if all characters in pattern exist in text in order
	patternIndex := 0
	score := 0

	for _, char := range text {
		if patternIndex < len(pattern) && rune(pattern[patternIndex]) == char {
			patternIndex++
			score += 10
		}
	}

	// Only return score if we matched all pattern characters
	if patternIndex == len(pattern) {
		return score
	}

	return 0
}

func parseFileMentions(input string) (string, []string, error) {
	// Regex to find @filename patterns
	re := regexp.MustCompile(`@([^\s@]+)`)
	matches := re.FindAllStringSubmatch(input, -1)

	var filePaths []string
	var fileContents []string
	modifiedInput := input

	for _, match := range matches {
		pattern := match[1]

		// Perform fuzzy search
		searchMatches, err := fuzzySearchFiles(pattern, ".")
		if err != nil {
			return input, nil, fmt.Errorf("failed to search for files matching pattern '%s': %w", pattern, err)
		}

		if len(searchMatches) > 0 {
			// Take the best match
			bestMatch := searchMatches[0]
			filePaths = append(filePaths, bestMatch.Path)

			// Read file content
			content, err := os.ReadFile(bestMatch.Path)
			if err != nil {
				return input, nil, fmt.Errorf("failed to read file '%s' (matched pattern '%s'): %w", bestMatch.Path, pattern, err)
			}

			fileContents = append(fileContents, fmt.Sprintf("=== Content of %s ===\n%s\n=== End of %s ===\n",
				bestMatch.Path, string(content), bestMatch.Path))

			// Replace @pattern with a more descriptive reference
			modifiedInput = strings.Replace(modifiedInput, match[0], fmt.Sprintf("[%s]", bestMatch.Path), 1)
		}
	}

	// If we found files, prepend their content to the user message
	if len(fileContents) > 0 {
		contextHeader := fmt.Sprintf("Context: The following files have been included for reference:\n\n%s\n",
			strings.Join(fileContents, "\n"))
		modifiedInput = contextHeader + "User Request: " + modifiedInput
	}

	return modifiedInput, filePaths, nil
}

// ------------------------------------------------------------
// Bash command execution
// ------------------------------------------------------------

var BashDefinition = ToolDefinition{
	Name:        "bash",
	Description: "Execute a bash command and return its output. Use this to run shell commands.",
	InputSchema: BashInputSchema,
	Function:    Bash,
}

type BashInput struct {
	Command string `json:"command" jsonschema_description:"The bash command to execute"`
}

var BashInputSchema = GenerateSchema[BashInput]()

func Bash(input json.RawMessage) (string, error) {
	bashInput := BashInput{}
	err := json.Unmarshal(input, &bashInput)
	if err != nil {
		return "", err
	}

	log.Printf("Executing bash command: %s", bashInput.Command)
	cmd := exec.Command("bash", "-c", bashInput.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Bash command failed: %v", err)
		return fmt.Sprintf("Command failed with error: %s\nOutput: %s", err.Error(), string(output)), nil
	}

	log.Printf("Bash command executed successfully, output length: %d chars", len(output))
	return strings.TrimSpace(string(output)), nil
}
