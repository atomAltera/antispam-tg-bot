package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAI struct {
	apiKey     string
	httpClient HTTPClient
}

func NewOpenAI(apiKey string, httpClient HTTPClient) *OpenAI {
	return &OpenAI{
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

func (c *OpenAI) GetJSONCompletion(ctx context.Context, system, user string, rf ResponseFormat, result any) (*Usage, error) {
	request := Request{
		Model: DefaultModel,
		Messages: []Message{
			{
				Role:    RoleSystem,
				Content: system,
			},
			{
				Role:    RoleUser,
				Content: user,
			},
		},
		Temperature:    0,
		ResponseFormat: rf,
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshaling body: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.openai.com/v1/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doing request: %w", err)
	}

	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		resBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected status code: %d: %s", res.StatusCode, resBody)
	}

	var response Response
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Choices) == 0 {
		return &response.Usage, fmt.Errorf("empty choices in response")
	}

	choice := response.Choices[0]

	if choice.FinishReason != FinishReasonStop {
		return &response.Usage, fmt.Errorf("unexpected finish reason: %v", choice.FinishReason)
	}

	if err = json.Unmarshal([]byte(choice.Message.Content), result); err != nil {
		return &response.Usage, fmt.Errorf("unmarshal response content: %w", err)
	}

	return &response.Usage, nil
}

type YesNoAnswer struct {
	Yes bool `json:"yes"`
}

type ResponseFormat string

func (rf ResponseFormat) MarshalJSON() ([]byte, error) {
	return []byte(rf), nil
}

var YesNoFormat ResponseFormat = `{
  "type": "json_schema",
  "json_schema": {
    "name": "yes_no_response",
    "schema": {
      "type": "object",
      "properties": {
        "yes": {
          "type": "boolean"
        }
      },
      "required": ["yes"],
      "additionalProperties": false
    },
    "strict": true
  }
}`

const DefaultModel = "gpt-4.1-nano"
