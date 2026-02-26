package claude

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultModel is the model used when none is specified.
const DefaultModel = "claude-sonnet-4-6"

// DefaultMaxTokens is the default maximum token count.
const DefaultMaxTokens = 16384

// ClientConfig configures the API client.
type ClientConfig struct {
	Model     string
	MaxTokens int64
	RepoDir   string
}

// Client wraps the Anthropic SDK client with hydra-specific configuration.
type Client struct {
	SDK    anthropic.Client
	Config ClientConfig
	Tools  []anthropic.ToolUnionParam
	System string
}

// NewClient creates a Client from credentials and configuration.
func NewClient(creds *Credentials, cfg ClientConfig) (*Client, error) {
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}

	var opts []option.RequestOption
	if creds.APIKey != "" {
		opts = append(opts, option.WithAPIKey(creds.APIKey))
	} else if creds.AccessToken != "" {
		opts = append(opts, option.WithHeader("Authorization", "Bearer "+creds.AccessToken))
	}

	sdk := anthropic.NewClient(opts...)

	return &Client{
		SDK:    sdk,
		Config: cfg,
		Tools:  ToolDefinitions(),
		System: "You are a software engineering assistant. You have access to tools for reading, writing, and editing files, running bash commands, listing files, and searching file contents. Work within the repository directory. Be precise and make minimal changes.",
	}, nil
}
