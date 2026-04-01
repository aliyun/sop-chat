package session

import (
	"strconv"
	"sync/atomic"
	"time"
)

var processStartUnixNano = time.Now().UnixNano()
var includeProcessStartInSessionHash atomic.Bool

func init() {
	includeProcessStartInSessionHash.Store(true)
}

// SetBindThreadToProcess 配置是否将 thread 绑定到进程生命周期。
func SetBindThreadToProcess(enabled bool) {
	includeProcessStartInSessionHash.Store(enabled)
}

// ProcessStartToken 返回当前进程启动时固定的 token。
func ProcessStartToken() string {
	return strconv.FormatInt(processStartUnixNano, 10)
}

// ProcessStartHashSuffix 返回应追加到 session hash 原文末尾的后缀。
// 关闭该特性时返回空字符串，保证 hash 输入与历史逻辑完全一致。
func ProcessStartHashSuffix() string {
	if !includeProcessStartInSessionHash.Load() {
		return ""
	}
	return "\x00" + ProcessStartToken()
}

// ProcessStartHashSuffixes 返回应该尝试的所有 hash suffix 列表，用于 thread 查找 fallback。
// 当 bindThreadToProcess=false 时，先尝试空字符串（匹配历史 thread），再尝试带进程启动时间的（fallback 匹配当前进程 thread）。
// 当 bindThreadToProcess=true 时，只返回带进程启动时间的 suffix。
func ProcessStartHashSuffixes() []string {
	if !includeProcessStartInSessionHash.Load() {
		// 关闭时：先尝试不带进程启动时间（复用历史），再 fallback 到带进程启动时间（当前进程）
		return []string{"", "\x00" + ProcessStartToken()}
	}
	// 开启时：只使用带进程启动时间的 suffix
	return []string{"\x00" + ProcessStartToken()}
}
