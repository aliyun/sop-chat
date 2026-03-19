package sopchat

import (
	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/dara"
)

// IsDoneMessage 检查 SSE 响应是否包含 done 类型的消息，表示对话已完成
func IsDoneMessage(body *cmsclient.CreateChatResponseBody) bool {
	if body == nil {
		return false
	}
	for _, msg := range body.Messages {
		if msg != nil && msg.Type != nil && *msg.Type == "done" {
			return true
		}
	}
	return false
}

// NewSSERuntimeOptions 创建 SSE 调用的 RuntimeOptions，设置合理的超时
// ConnectTimeout: 30 秒，ReadTimeout: 5 分钟
func NewSSERuntimeOptions() *dara.RuntimeOptions {
	runtime := &dara.RuntimeOptions{}
	runtime.SetConnectTimeout(30000)
	runtime.SetReadTimeout(300000)
	return runtime
}
