package claude

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
)

// Stream event type constants.
const (
	eventTypeToolUse           = "tool_use"
	eventTypeText              = "text"
	eventTypeThinking          = "thinking"
	eventTypeTextDelta         = "text_delta"
	eventTypeInputJSONDelta    = "input_json_delta"
	eventTypeThinkingDelta     = "thinking_delta"
	eventTypeContentBlockStart = "content_block_start"
	eventTypeContentBlockDelta = "content_block_delta"
	eventTypeContentBlockStop  = "content_block_stop"
	eventTypeMessageStart      = "message_start"
	eventTypeMessageDelta      = "message_delta"
	eventTypeMessageStop       = "message_stop"
)

// Session manages an agentic conversation with the Anthropic API.
type Session struct {
	client     *Client
	Events     chan Event
	ToolAnswer chan ToolAnswer
	cancel     context.CancelFunc
	messages   []anthropic.MessageParam
}

// NewSession creates a new Session tied to the given client.
func NewSession(client *Client) *Session {
	return &Session{
		client:     client,
		Events:     make(chan Event, 64),
		ToolAnswer: make(chan ToolAnswer, 1),
	}
}

// Start begins the agentic loop in a goroutine. The document is sent as the initial user message.
func (s *Session) Start(ctx context.Context, document string) {
	ctx, s.cancel = context.WithCancel(ctx)

	s.messages = []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(document)),
	}

	go s.loop(ctx)
}

// Cancel stops the session.
func (s *Session) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Session) loop(ctx context.Context) {
	defer close(s.Events)

	for {
		if ctx.Err() != nil {
			s.Events <- EventError{Err: ctx.Err()}
			return
		}

		stopReason, err := s.sendAndStream(ctx)
		if err != nil {
			s.Events <- EventError{Err: err}
			return
		}

		switch stopReason {
		case "end_turn", "max_tokens":
			s.Events <- EventDone{StopReason: stopReason}
			return
		case eventTypeToolUse:
			// Tool results are appended by sendAndStream; continue the loop.
			continue
		default:
			s.Events <- EventDone{StopReason: stopReason}
			return
		}
	}
}

// streamState holds mutable state accumulated during streaming.
type streamState struct {
	assistantBlocks  []anthropic.ContentBlockParamUnion
	toolUses         []toolUseInfo
	stopReason       string
	currentBlockType string
	currentToolUse   *toolUseInfo
	currentText      string
}

func (s *Session) sendAndStream(ctx context.Context) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(s.client.Config.Model),
		MaxTokens: s.client.Config.MaxTokens,
		Messages:  s.messages,
		Tools:     s.client.Tools,
		System: []anthropic.TextBlockParam{
			{Text: s.client.System},
		},
	}

	stream := s.client.SDK.Messages.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	st := &streamState{}

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case eventTypeMessageStart:
			// Nothing needed.
		case eventTypeMessageDelta:
			s.handleMessageDelta(event, st)
		case eventTypeContentBlockStart:
			s.handleContentBlockStart(event, st)
		case eventTypeContentBlockDelta:
			s.handleContentBlockDelta(event, st)
		case eventTypeContentBlockStop:
			s.handleContentBlockStop(st)
		case eventTypeMessageStop:
			// End of message.
		}
	}

	if err := stream.Err(); err != nil {
		return "", err
	}

	// Append assistant message.
	if len(st.assistantBlocks) > 0 {
		s.messages = append(s.messages, anthropic.MessageParam{
			Role:    anthropic.MessageParamRoleAssistant,
			Content: st.assistantBlocks,
		})
	}

	// Process tool uses.
	if len(st.toolUses) > 0 {
		if err := s.processToolUses(ctx, st); err != nil {
			return "", err
		}
	}

	return st.stopReason, nil
}

func (s *Session) handleMessageDelta(event anthropic.MessageStreamEventUnion, st *streamState) {
	delta := event.AsMessageDelta()
	st.stopReason = string(delta.Delta.StopReason)
}

func (s *Session) handleContentBlockStart(event anthropic.MessageStreamEventUnion, st *streamState) {
	startEvt := event.AsContentBlockStart()
	cb := startEvt.ContentBlock
	switch cb.Type {
	case eventTypeText:
		st.currentBlockType = eventTypeText
		st.currentText = ""
	case eventTypeToolUse:
		st.currentBlockType = eventTypeToolUse
		st.currentToolUse = &toolUseInfo{
			ID:   cb.ID,
			Name: cb.Name,
		}
	case eventTypeThinking:
		st.currentBlockType = eventTypeThinking
	default:
		st.currentBlockType = cb.Type
	}
}

func (s *Session) handleContentBlockDelta(event anthropic.MessageStreamEventUnion, st *streamState) {
	deltaEvt := event.AsContentBlockDelta()
	switch st.currentBlockType {
	case eventTypeText:
		if deltaEvt.Delta.Type == eventTypeTextDelta {
			st.currentText += deltaEvt.Delta.Text
			s.Events <- EventText{Text: deltaEvt.Delta.Text}
		}
	case eventTypeToolUse:
		if deltaEvt.Delta.Type == eventTypeInputJSONDelta && st.currentToolUse != nil {
			st.currentToolUse.inputJSON += deltaEvt.Delta.PartialJSON
		}
	case eventTypeThinking:
		if deltaEvt.Delta.Type == eventTypeThinkingDelta {
			s.Events <- EventThinking{Text: deltaEvt.Delta.Thinking}
		}
	}
}

func (s *Session) handleContentBlockStop(st *streamState) {
	switch st.currentBlockType {
	case eventTypeText:
		if st.currentText != "" {
			st.assistantBlocks = append(st.assistantBlocks, anthropic.NewTextBlock(st.currentText))
		}
		st.currentText = ""
	case eventTypeToolUse:
		if st.currentToolUse != nil {
			st.toolUses = append(st.toolUses, *st.currentToolUse)
			st.assistantBlocks = append(st.assistantBlocks,
				anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    st.currentToolUse.ID,
						Name:  st.currentToolUse.Name,
						Input: json.RawMessage(st.currentToolUse.inputJSON),
					},
				})
			st.currentToolUse = nil
		}
	}
	st.currentBlockType = ""
}

func (s *Session) processToolUses(ctx context.Context, st *streamState) error {
	var toolResultBlocks []anthropic.ContentBlockParamUnion

	for _, tu := range st.toolUses {
		inputRaw := json.RawMessage(tu.inputJSON)
		meta := PrepareMeta(s.client.Config.RepoDir, tu.Name, inputRaw)

		if NeedsApproval(tu.Name) {
			s.Events <- EventToolRequest{
				ID:    tu.ID,
				Name:  tu.Name,
				Input: inputRaw,
				Meta:  meta,
			}

			// Wait for approval.
			select {
			case answer := <-s.ToolAnswer:
				if !answer.Approved {
					toolResultBlocks = append(toolResultBlocks,
						anthropic.NewToolResultBlock(tu.ID, "Tool execution was rejected by the user.", true))
					s.Events <- EventToolResult{
						ID:      tu.ID,
						Content: "Rejected by user",
						IsError: true,
					}
					continue
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Execute the tool.
		result, err := ExecuteTool(s.client.Config.RepoDir, tu.Name, inputRaw)
		isError := err != nil
		content := result
		if err != nil {
			content = err.Error()
		}

		toolResultBlocks = append(toolResultBlocks,
			anthropic.NewToolResultBlock(tu.ID, content, isError))

		s.Events <- EventToolResult{
			ID:      tu.ID,
			Content: content,
			IsError: isError,
		}
	}

	// Append user message with tool results.
	s.messages = append(s.messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleUser,
		Content: toolResultBlocks,
	})

	return nil
}

type toolUseInfo struct {
	ID        string
	Name      string
	inputJSON string
}
