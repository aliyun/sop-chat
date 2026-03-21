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

// SSE 读超时（毫秒）：须 ≥ scheduler/queryEmployee 的 30m context，否则长对话会在约 5 分钟时出现
// context deadline exceeded (client.Timeout while reading body)，页面 trigger-task 也会失败。
const sseReadTimeoutMs = 31 * 60 * 1000 // 31 分钟

// NewSSERuntimeOptions 创建 SSE 调用的 RuntimeOptions，设置合理的超时
// ConnectTimeout: 30 秒；ReadTimeout: 31 分钟（与定时任务 / 手动触发侧一致）
func NewSSERuntimeOptions() *dara.RuntimeOptions {
	runtime := &dara.RuntimeOptions{}
	runtime.SetConnectTimeout(30000)
	runtime.SetReadTimeout(sseReadTimeoutMs)
	return runtime
}
