package provider

import (
	"context"
	"encoding/json"
	"sync"
)

// Provider abstracts a messaging bot connection (iLink, future providers, etc.).
type Provider interface {
	Name() string
	Start(ctx context.Context, opts StartOptions) error
	Stop()
	Send(ctx context.Context, msg OutboundMessage) (string, error)
	Status() string
}

// Binder is an optional interface for providers that support QR/interactive binding.
type Binder interface {
	StartBind(ctx context.Context) (*BindSession, error)
}

type BindSession struct {
	SessionID string
	QRURL     string
	// PollStatus is called repeatedly; returns final credentials on success.
	PollStatus func(ctx context.Context) (*BindPollResult, error)
}

type BindPollResult struct {
	Status      string          // "wait", "scanned", "expired", "confirmed"
	QRURL       string          // new QR URL on refresh
	Credentials json.RawMessage // set on "confirmed"
}

type StartOptions struct {
	Credentials  json.RawMessage
	SyncState    json.RawMessage
	OnMessage    func(InboundMessage)
	OnStatus     func(status string)
	OnSyncUpdate func(state json.RawMessage)
}

type InboundMessage struct {
	ExternalID   string
	Sender       string
	Timestamp    int64
	Items        []MessageItem
	ContextToken string
	SessionID    string
}

type OutboundMessage struct {
	Recipient string
	Text      string
}

type MessageItem struct {
	Type     string `json:"type"` // "text", "image", "voice", "file", "video"
	Text     string `json:"text,omitempty"`
	FileName string `json:"file_name,omitempty"`
}

// --- Registry ---

type Factory func() Provider

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

func Register(name string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = f
}

func Get(name string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	return f, ok
}
