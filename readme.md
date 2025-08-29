# Coding Agent with Session Management

Barebones implementation of a coding agent with persistent conversation sessions.

## Features

### Core Agent Capabilities
- Read files
- Edit files  
- List files
- Create files
- Web search
- Add files as main context for the llm via @
- Track daily tokens used

### Session Management
- **Persistent conversations**: All conversations are automatically saved to a SQLite database
- **Session switching**: Resume any previous conversation where you left off
- **Session organization**: List, create, rename, and delete conversation sessions

## Usage

### Basic Chat
Just run the agent and start chatting:
```bash
./agent
```

### Session Commands
The agent supports several commands for managing conversation sessions:

#### List all sessions
```
/session list
```
or
```
/session ls
```

#### Switch to a specific session
```
/session switch <session_id>
```

#### Create a new session
```
/session new [optional_name]
```

#### Delete a session
```
/session delete <session_id>
```

#### Rename a session
```
/session rename <session_id> <new_name>
```

#### Get help
```
/help
```

### File Context
Include files in your conversation context using @:
```
Tell me about @main.go and @database.go
```

## Database

Conversations are stored in `conversations.db` (SQLite) with the following structure:

- **sessions**: Stores session metadata (ID, name, created/updated timestamps)
- **messages**: Stores individual messages with role (user/assistant) and content