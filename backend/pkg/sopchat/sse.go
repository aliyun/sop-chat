package sopchat

import (
	"context"
	"time"

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

// SSE 读超时（毫秒）：作为 http.Client.Timeout 的兜底安全网。
// 实际超时由 NewIdleTimeoutContext 的空闲超时控制（默认 10 分钟无事件则取消）。
// 此值需大于任何可能的单次对话总时长，设为 2 小时。
const sseReadTimeoutMs = 2 * 60 * 60 * 1000 // 2 小时

// DefaultSSEIdleTimeout 是 SSE 连接的默认空闲超时：连续无任何 SSE 事件达此时长则取消。
const DefaultSSEIdleTimeout = 10 * time.Minute

// NewIdleTimeoutContext 创建一个基于空闲时间的 context。
// 返回 ctx、cancel 和 resetIdle。每次收到 SSE 事件时调用 resetIdle() 重置计时器。
// 若连续 idleTimeout 时间无 resetIdle 调用，ctx 将被取消。
func NewIdleTimeoutContext(parent context.Context, idleTimeout time.Duration) (ctx context.Context, cancel context.CancelFunc, resetIdle func()) {
	ctx, cancelFn := context.WithCancel(parent)
	timer := time.AfterFunc(idleTimeout, cancelFn)
	resetIdle = func() { timer.Reset(idleTimeout) }
	cancel = func() { timer.Stop(); cancelFn() }
	return
}

// NewSSERuntimeOptions 创建 SSE 调用的 RuntimeOptions，设置合理的超时
// ConnectTimeout: 30 秒；ReadTimeout: 31 分钟（与定时任务 / 手动触发侧一致）
func NewSSERuntimeOptions() *dara.RuntimeOptions {
	runtime := &dara.RuntimeOptions{}
	runtime.SetConnectTimeout(30000)
	runtime.SetReadTimeout(sseReadTimeoutMs)
	return runtime
}
