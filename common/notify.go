package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var DebugMode bool
var WebhookMention []string

type wechatMessage struct {
	MsgType string      `json:"msgtype"`
	Text    textContent `json:"text"`
}

type textContent struct {
	Content       string   `json:"content"`
	MentionedList []string `json:"mentioned_list,omitempty"`
}

func SendWechatNotify(webhookURL, content string) {
	if webhookURL == "" {
		return
	}

	go func() {
		text := fmt.Sprintf("%s\n%s", content, time.Now().Format("2006-01-02 15:04:05"))
		msg := wechatMessage{
			MsgType: "text",
			Text:    textContent{Content: text},
		}
		if len(WebhookMention) > 0 {
			msg.Text.MentionedList = WebhookMention
		}

		body, err := json.Marshal(msg)
		if err != nil {
			LOGE("[notify] JSON marshal failed: ", err)
			return
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(body))
		if err != nil {
			LOGE("[notify] Send failed: ", err)
			return
		}
		resp.Body.Close()
		LOGI("[notify] Sent: ", content)
	}()
}
