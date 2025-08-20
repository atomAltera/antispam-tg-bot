package ai

import (
	"net/http"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Request struct {
	Model           string          `json:"model"`
	Messages        []Message       `json:"messages"`
	ReasoningEffort ReasoningEffort `json:"reasoning_effort,omitempty"`
	ResponseFormat  any             `json:"response_format"`
}

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ReasoningEffort string

const (
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
)

type Response struct {
	Index   int      `json:"index"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Choice struct {
	Index        int          `json:"index"`
	Message      Message      `json:"message"`
	FinishReason FinishReason `json:"finish_reason"`
}

type FinishReason string

const (
	FinishReasonStop         FinishReason = "stop"
	FinishReasonLength       FinishReason = "length"
	FinishReasonFunctionCall FinishReason = "function_call"
	FinishReasonNull         FinishReason = ""
)
