package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	// "log/slog"
	"net/http"
	"strconv"
)

type TelegramMessage struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// telegramAPIError mirrors the shape of Telegram's JSON error response,
// e.g. {"ok":false,"error_code":401,"description":"Unauthorized"}.
type telegramAPIError struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

// FIX: previously silently returned 0 on a bad chat ID string. Now returns
// an error so callers can tell "no chat configured" apart from "garbage
// value sent as chat_id 0", which Telegram would reject anyway.
func parseInt64(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("chat id %q is not a valid integer: %w", s, err)
	}
	return v, nil
}

// SendTelegramMessage posts a message to a Telegram chat and returns an
// error if either the request failed to send OR Telegram's API rejected it
// (bad token, bad chat id, bot not in the chat, etc). The previous version
// only checked for transport-level errors, so a 401/403/400 response from
// Telegram was reported as success.
func SendTelegramMessage(message string, chatID string, botToken string) error {
	if botToken == "" {
		return fmt.Errorf("telegram: empty bot token")
	}

	chatIDInt, err := parseInt64(chatID)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}

	url := "https://api.telegram.org/bot" + botToken + "/sendMessage"
	body := TelegramMessage{
		ChatID:    chatIDInt,
		Text:      message,
		ParseMode: "HTML",
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("telegram: failed to marshal message: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("telegram: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)

	// FIX: this is the core bug — a non-2xx response from Telegram was
	// previously treated as success because http.Post's err is nil for
	// any response it successfully receives, good or bad.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr telegramAPIError
		if readErr == nil {
			_ = json.Unmarshal(respBody, &apiErr)
		}
		if apiErr.Description != "" {
			return fmt.Errorf("telegram: api rejected message (%d): %s", resp.StatusCode, apiErr.Description)
		}
		return fmt.Errorf("telegram: api rejected message (%d): %s", resp.StatusCode, string(respBody))
	}

	//slog.Info("telegram message sent", "chat_id", chatIDInt, "status", resp.StatusCode)
	return nil
}
