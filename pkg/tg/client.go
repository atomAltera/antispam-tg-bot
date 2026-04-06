package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const apiBase = "https://api.telegram.org"

// Client is a minimal Telegram Bot API client.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a new Telegram Bot API client.
func NewClient(token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{token: token, httpClient: httpClient}
}

// GetMe returns basic information about the bot.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var user User
	err := c.call(ctx, "getMe", nil, &user)
	return user, err
}

// GetUpdates fetches incoming updates using long polling.
func (c *Client) GetUpdates(ctx context.Context, offset int, timeout int) ([]Update, error) {
	params := url.Values{
		"offset":  {strconv.Itoa(offset)},
		"timeout": {strconv.Itoa(timeout)},
	}
	var updates []Update
	err := c.call(ctx, "getUpdates", params, &updates)
	return updates, err
}

// DeleteMessage deletes a message.
func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	params := url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"message_id": {strconv.Itoa(messageID)},
	}
	return c.call(ctx, "deleteMessage", params, nil)
}

// BanChatMember bans a user in a chat.
func (c *Client) BanChatMember(ctx context.Context, chatID int64, userID int64) error {
	params := url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"user_id": {strconv.FormatInt(userID, 10)},
	}
	return c.call(ctx, "banChatMember", params, nil)
}

// SendMessage sends a text message.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	params := url.Values{
		"chat_id":                  {strconv.FormatInt(chatID, 10)},
		"text":                     {text},
		"parse_mode":               {"HTML"},
		"disable_web_page_preview": {"true"},
	}
	return c.call(ctx, "sendMessage", params, nil)
}

// GetFile gets basic info about a file and prepares it for download.
func (c *Client) GetFile(ctx context.Context, fileID string) (File, error) {
	params := url.Values{
		"file_id": {fileID},
	}
	var file File
	err := c.call(ctx, "getFile", params, &file)
	return file, err
}

// DownloadFile downloads a file by its file ID.
func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	file, err := c.GetFile(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("getting file info: %w", err)
	}

	fileURL := fmt.Sprintf("%s/file/bot%s/%s", apiBase, c.token, file.FilePath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// FileSize returns the file size from getFile, without downloading content.
func (c *Client) FileSize(ctx context.Context, fileID string) (int64, error) {
	file, err := c.GetFile(ctx, fileID)
	if err != nil {
		return 0, err
	}
	return int64(file.FileSize), nil
}

func (c *Client) call(ctx context.Context, method string, params url.Values, result any) error {
	u := fmt.Sprintf("%s/bot%s/%s", apiBase, c.token, method)
	if params != nil {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var raw Response[json.RawMessage]
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	if !raw.OK {
		return fmt.Errorf("telegram api error %d: %s", raw.ErrorCode, raw.Description)
	}

	if result != nil {
		if err := json.Unmarshal(raw.Result, result); err != nil {
			return fmt.Errorf("decoding result: %w", err)
		}
	}

	return nil
}
