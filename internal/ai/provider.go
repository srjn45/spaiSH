package ai

import "context"

// Message is a single turn in a conversation.
type Message struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// Provider is the interface for any AI model backend.
type Provider interface {
	// Complete sends messages to the model and streams response text chunks.
	// The returned channel is closed when the response is complete.
	Complete(ctx context.Context, messages []Message) (<-chan string, error)
	// Available returns true if this provider is configured and reachable.
	Available() bool
}
