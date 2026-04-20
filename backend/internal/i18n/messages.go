package i18n

import (
	"fmt"
	"strings"
)

func isEnglish(language string) bool {
	lang := strings.TrimSpace(strings.ToLower(language))
	return strings.HasPrefix(lang, "en")
}

// ProcessingHint 用户消息已接收，正在处理。
func ProcessingHint(language string) string {
	if isEnglish(language) {
		return "⏳ Received, processing..."
	}
	return "⏳ 收到，正在处理中..."
}

// ThinkingHint 系统正在思考。
func ThinkingHint(language string) string {
	if isEnglish(language) {
		return "💭 Thinking..."
	}
	return "💭 思考中..."
}

// CardThinkingHint 卡片中的思考提示（不带 emoji，避免卡片样式受影响）。
func CardThinkingHint(language string) string {
	if isEnglish(language) {
		return "Thinking..."
	}
	return "正在思考中..."
}

// SessionCreateFailedHint 会话创建失败提示。
func SessionCreateFailedHint(language string) string {
	if isEnglish(language) {
		return "❌ Failed to create session, please try again later"
	}
	return "❌ 创建会话失败，请稍后重试"
}

// BusyHint 队列繁忙提示。
func BusyHint(language string) string {
	if isEnglish(language) {
		return "⚠️ Message is being processed, please try again later."
	}
	return "⚠️ 消息处理中，请稍后再发。"
}

// UnsupportedMsgTypeHint 不支持的消息类型提示。
func UnsupportedMsgTypeHint(language, msgType string) string {
	if isEnglish(language) {
		return fmt.Sprintf("This message type (%s) is not supported yet. Please send text or voice.", msgType)
	}
	return fmt.Sprintf("暂不支持该消息类型（%s），请发送文字或语音。", msgType)
}
