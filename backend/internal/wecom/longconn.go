package wecom

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/gorilla/websocket"

	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"
)

const (
	defaultLongConnURL       = "wss://openws.work.weixin.qq.com"
	defaultPingInterval      = 30 * time.Second
	defaultReconnectDelay    = 5 * time.Second
	defaultMaxReconnectDelay = 60 * time.Second

	cmdSubscribe = "aibot_subscribe"
	cmdPing      = "ping"
	cmdResponse  = "aibot_respond_msg"
	cmdCallback  = "aibot_msg_callback"
)

// longConnFrame WebSocket 帧结构
type longConnFrame struct {
	Cmd     string            `json:"cmd"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	ErrCode *int              `json:"errcode,omitempty"`
	ErrMsg  string            `json:"errmsg,omitempty"`
}

// longConnCallbackBody 回调消息体
type longConnCallbackBody struct {
	MsgType     string                 `json:"msgtype"`
	MsgID       string                 `json:"msgid"`
	ChatType    string                 `json:"chattype"`
	ChatID      string                 `json:"chatid"`
	From        *longConnFrom          `json:"from,omitempty"`
	Text        *longConnTextContent   `json:"text,omitempty"`
	ResponseURL string                 `json:"response_url,omitempty"`
	Stream      *longConnStreamContent `json:"stream,omitempty"`
}

type longConnFrom struct {
	UserID string `json:"userid"`
}

type longConnTextContent struct {
	Content string `json:"content"`
}

type longConnStreamContent struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Finish  bool   `json:"finish"`
}

// longConnStreamReplyBody 流式回复消息体
type longConnStreamReplyBody struct {
	MsgType string                `json:"msgtype"`
	Stream  longConnStreamContent `json:"stream"`
}

// LongConnBot 企业微信 AI 助手群机器人长连接管理器
type LongConnBot struct {
	mu        sync.RWMutex
	cfg       *config.WeComBotConfig
	cmsConfig *config.ClientConfig

	conn              *websocket.Conn
	connected         bool
	shouldRun         bool
	reconnectAttempts int
	subscribeReqID    string
	lastPingReqID     string
	missedHeartbeats  int

	// 会话 -> 线程 ID 的映射
	threadStore sync.Map

	// key -> chan func()，每个 key 对应一个串行 worker
	workerQueues sync.Map

	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewLongConnBot 创建长连接机器人实例
func NewLongConnBot(wbConfig *config.WeComBotConfig, cmsConfig *config.ClientConfig) *LongConnBot {
	return &LongConnBot{
		cfg:       wbConfig,
		cmsConfig: cmsConfig,
		stopCh:    make(chan struct{}),
	}
}

// Config 返回当前配置快照
func (b *LongConnBot) Config() *config.WeComBotConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cfg
}

// UpdateConfig 热更新配置
func (b *LongConnBot) UpdateConfig(newCfg *config.WeComBotConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cfg = newCfg
	log.Printf("[WeCom-LongConn] 配置已热更新: botId=%s employee=%s",
		newCfg.BotID, newCfg.EmployeeName)
}

// Start 启动长连接
func (b *LongConnBot) Start() {
	b.shouldRun = true
	go b.connectLoop()
	log.Printf("[WeCom-LongConn] 启动: botId=%s employee=%s",
		b.cfg.BotID, b.cfg.EmployeeName)
}

// Stop 停止长连接
func (b *LongConnBot) Stop() {
	b.stopOnce.Do(func() {
		b.shouldRun = false
		close(b.stopCh)

		// 使用 recover 防止 conn.Close() 时 goroutine 仍在写入导致的 panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[WeCom-LongConn] Stop() recovered from panic: %v", r)
				}
			}()
			b.mu.Lock()
			if b.conn != nil {
				b.conn.Close()
				b.conn = nil
			}
			b.connected = false
			b.mu.Unlock()
		}()

		log.Printf("[WeCom-LongConn] 已停止")
	})
}

// connectLoop 连接循环（含重连逻辑）
func (b *LongConnBot) connectLoop() {
	for b.shouldRun {
		select {
		case <-b.stopCh:
			return
		default:
		}

		err := b.connect()
		if err != nil {
			log.Printf("[WeCom-LongConn] 连接失败: %v", err)
		}

		if !b.shouldRun {
			return
		}

		delay := b.reconnectDelay()
		log.Printf("[WeCom-LongConn] %s 后重连 (attempt=%d)", delay, b.reconnectAttempts)
		b.reconnectAttempts++

		select {
		case <-b.stopCh:
			return
		case <-time.After(delay):
		}
	}
}

// reconnectDelay 计算重连延迟（指数退避）
func (b *LongConnBot) reconnectDelay() time.Duration {
	cfg := b.Config()
	base := defaultReconnectDelay
	if cfg.ReconnectDelaySec > 0 {
		base = time.Duration(cfg.ReconnectDelaySec) * time.Second
	}
	maxDelay := defaultMaxReconnectDelay
	if cfg.MaxReconnectDelaySec > 0 {
		maxDelay = time.Duration(cfg.MaxReconnectDelaySec) * time.Second
	}
	delay := time.Duration(float64(base) * math.Pow(2, float64(b.reconnectAttempts)))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// connect 建立 WebSocket 连接并进入消息循环
func (b *LongConnBot) connect() error {
	cfg := b.Config()

	wsURL := cfg.URL
	if wsURL == "" {
		wsURL = defaultLongConnURL
	}

	log.Printf("[WeCom-LongConn] 正在连接 %s ...", wsURL)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 连接失败: %w", err)
	}

	b.mu.Lock()
	b.conn = conn
	b.connected = false
	b.missedHeartbeats = 0
	b.mu.Unlock()

	// 发送订阅命令
	reqID := fmt.Sprintf("%s_%d", cmdSubscribe, time.Now().UnixMilli())
	b.subscribeReqID = reqID
	subscribeBody, _ := json.Marshal(map[string]string{
		"bot_id": cfg.BotID,
		"secret": cfg.BotSecret,
	})
	err = b.sendFrame(&longConnFrame{
		Cmd:     cmdSubscribe,
		Headers: map[string]string{"req_id": reqID},
		Body:    subscribeBody,
	})
	if err != nil {
		conn.Close()
		return fmt.Errorf("发送订阅命令失败: %w", err)
	}
	log.Printf("[WeCom-LongConn] 已发送订阅命令 reqId=%s", reqID)

	// 启动心跳
	pingDone := make(chan struct{})
	go b.pingLoop(pingDone)

	// 消息读取循环
	defer func() {
		close(pingDone)
		b.mu.Lock()
		b.connected = false
		if b.conn != nil {
			b.conn.Close()
			b.conn = nil
		}
		b.mu.Unlock()
	}()

	for {
		select {
		case <-b.stopCh:
			return nil
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[WeCom-LongConn] 连接正常关闭")
				return nil
			}
			return fmt.Errorf("读取消息失败: %w", err)
		}

		var frame longConnFrame
		if err := json.Unmarshal(message, &frame); err != nil {
			log.Printf("[WeCom-LongConn] 解析帧失败: %v, raw=%s", err, string(message))
			continue
		}

		b.handleFrame(&frame)
	}
}

// sendFrame 发送 WebSocket 帧
func (b *LongConnBot) sendFrame(frame *longConnFrame) error {
	b.mu.RLock()
	conn := b.conn
	b.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("序列化帧失败: %w", err)
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

// pingLoop 心跳循环
func (b *LongConnBot) pingLoop(done chan struct{}) {
	cfg := b.Config()
	interval := defaultPingInterval
	if cfg.PingIntervalSec > 0 {
		interval = time.Duration(cfg.PingIntervalSec) * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.mu.RLock()
			missed := b.missedHeartbeats
			b.mu.RUnlock()

			if missed >= 2 {
				log.Printf("[WeCom-LongConn] 心跳超时 (missed=%d)，强制断开重连", missed)
				b.mu.RLock()
				conn := b.conn
				b.mu.RUnlock()
				if conn != nil {
					conn.Close()
				}
				return
			}

			b.mu.Lock()
			b.missedHeartbeats++
			b.mu.Unlock()

			reqID := fmt.Sprintf("%s_%d", cmdPing, time.Now().UnixMilli())
			b.lastPingReqID = reqID
			err := b.sendFrame(&longConnFrame{
				Cmd:     cmdPing,
				Headers: map[string]string{"req_id": reqID},
			})
			if err != nil {
				log.Printf("[WeCom-LongConn] 发送心跳失败: %v", err)
			}
		}
	}
}

// handleFrame 处理收到的 WebSocket 帧
func (b *LongConnBot) handleFrame(frame *longConnFrame) {
	cmd := strings.ToLower(strings.TrimSpace(frame.Cmd))
	reqID := ""
	if frame.Headers != nil {
		reqID = frame.Headers["req_id"]
	}

	// 处理 pong 响应
	if cmd == "pong" {
		b.mu.Lock()
		b.missedHeartbeats = 0
		b.mu.Unlock()
		return
	}

	// 处理订阅响应（errcode 字段）
	if frame.ErrCode != nil {
		errCode := *frame.ErrCode
		if reqID == b.subscribeReqID || strings.HasPrefix(reqID, cmdSubscribe+"_") {
			if errCode == 0 {
				b.mu.Lock()
				b.connected = true
				b.reconnectAttempts = 0
				b.missedHeartbeats = 0
				b.mu.Unlock()
				log.Printf("[WeCom-LongConn] 订阅成功，已连接")
			} else {
				log.Printf("[WeCom-LongConn] 订阅失败: errcode=%d errmsg=%s", errCode, frame.ErrMsg)
				b.mu.RLock()
				conn := b.conn
				b.mu.RUnlock()
				if conn != nil {
					conn.Close()
				}
			}
			return
		}
		// ping 响应
		if reqID == b.lastPingReqID || strings.HasPrefix(reqID, cmdPing+"_") {
			if errCode == 0 {
				b.mu.Lock()
				b.missedHeartbeats = 0
				b.mu.Unlock()
			} else {
				log.Printf("[WeCom-LongConn] ping 被拒绝: errcode=%d errmsg=%s", errCode, frame.ErrMsg)
			}
			return
		}
		if errCode != 0 {
			log.Printf("[WeCom-LongConn] 命令被拒绝: reqId=%s errcode=%d errmsg=%s", reqID, errCode, frame.ErrMsg)
		}
		return
	}

	// 处理消息回调
	if cmd == cmdCallback || cmd == "aibot_event_callback" {
		b.handleCallback(frame)
		return
	}

	if cmd != "" && cmd != cmdPing {
		log.Printf("[WeCom-LongConn] 忽略未知命令: cmd=%s", cmd)
	}
}

// handleCallback 处理消息回调
func (b *LongConnBot) handleCallback(frame *longConnFrame) {
	reqID := ""
	if frame.Headers != nil {
		reqID = frame.Headers["req_id"]
	}

	var body longConnCallbackBody
	if err := json.Unmarshal(frame.Body, &body); err != nil {
		log.Printf("[WeCom-LongConn] 解析回调消息体失败: %v", err)
		return
	}

	fromUser := ""
	if body.From != nil {
		fromUser = body.From.UserID
	}

	log.Printf("[WeCom-LongConn] 收到消息 msgType=%s from=%s chatId=%s msgId=%s",
		body.MsgType, fromUser, body.ChatID, body.MsgID)

	// 只处理文本消息
	if body.MsgType != "text" {
		log.Printf("[WeCom-LongConn] 不支持的消息类型: %s", body.MsgType)
		return
	}

	if body.Text == nil || strings.TrimSpace(body.Text.Content) == "" {
		return
	}
	if fromUser == "" {
		log.Printf("[WeCom-LongConn] 消息缺少发送者信息，忽略")
		return
	}

	userMessage := strings.TrimSpace(body.Text.Content)
	cfg := b.Config()
	isGroupChat := body.ChatType == "group" || body.ChatID != ""

	// 生成 streamId 用于流式回复
	streamID := fmt.Sprintf("stream_%d", time.Now().UnixNano())

	key := longConnThreadKey(fromUser, cfg.EmployeeName, body.ChatID)

	// worker queue key 不含 variable，保证同一会话的消息串行处理
	queueKey := key

	work := func() {
		workCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// 立即发送"思考中"提示，让用户知道消息已收到
		b.sendStreamReply(reqID, streamID, "💭 思考中...", false)

		threadID, err := b.getOrCreateThreadID(fromUser, cfg.EmployeeName, body.ChatID)
		if err != nil {
			log.Printf("[WeCom-LongConn] 创建线程失败: %v", err)
			b.sendStreamReply(reqID, streamID, "❌ 创建会话失败，请稍后重试", true)
			return
		}

		log.Printf("[WeCom-LongConn] 调用数字员工 employee=%s threadId=%s isGroup=%v",
			cfg.EmployeeName, threadID, isGroupChat)

		replyText, newThreadID, err := b.queryEmployee(workCtx, userMessage, threadID, cfg.EmployeeName)
		if err != nil {
			log.Printf("[WeCom-LongConn] 调用数字员工失败: %v", err)
			b.sendStreamReply(reqID, streamID, "❌ "+err.Error(), true)
			return
		}

		if newThreadID != "" && newThreadID != threadID {
			// 缓存 key 需要包含 variable
			project, workspace, _ := b.threadVariable()
			variable := project + workspace
			cacheKey := longConnThreadKey(fromUser, cfg.EmployeeName, body.ChatID) + "\x00" + variable
			b.threadStore.Store(cacheKey, newThreadID)
		}

		log.Printf("[WeCom-LongConn] 回复消息 to=%s 长度=%d isGroup=%v", fromUser, len(replyText), isGroupChat)
		b.sendStreamReply(reqID, streamID, replyText, true)
	}

	if !b.enqueueWork(queueKey, work) {
		log.Printf("[WeCom-LongConn] 队列已满，拒绝消息 from=%s", fromUser)
		b.sendStreamReply(reqID, streamID, "⚠️ 消息处理中，请稍后再发。", true)
	}
}

// sendStreamReply 发送流式回复
func (b *LongConnBot) sendStreamReply(reqID, streamID, content string, finish bool) {
	replyBody := longConnStreamReplyBody{
		MsgType: "stream",
		Stream: longConnStreamContent{
			ID:      streamID,
			Content: content,
			Finish:  finish,
		},
	}

	bodyBytes, err := json.Marshal(replyBody)
	if err != nil {
		log.Printf("[WeCom-LongConn] 序列化回复失败: %v", err)
		return
	}

	err = b.sendFrame(&longConnFrame{
		Cmd:     cmdResponse,
		Headers: map[string]string{"req_id": reqID},
		Body:    bodyBytes,
	})
	if err != nil {
		log.Printf("[WeCom-LongConn] 发送回复失败: %v", err)
	}
}

// enqueueWork 将 work 投入 key 对应的串行队列
func (b *LongConnBot) enqueueWork(key string, work func()) bool {
	ch := make(chan func(), workerQueueSize)
	actual, loaded := b.workerQueues.LoadOrStore(key, ch)
	ch = actual.(chan func())
	if !loaded {
		go func() {
			for fn := range ch {
				fn()
			}
		}()
	}
	select {
	case ch <- work:
		return true
	default:
		return false
	}
}

// longConnThreadKey 生成长连接线程映射 key（包含 chatId 维度，避免跨群串话）
func longConnThreadKey(userID, employeeName, chatID string) string {
	if chatID != "" {
		return "lc:" + chatID + "\x00" + employeeName
	}
	return "lc:" + userID + "\x00" + employeeName
}

// threadVariable 根据 product 返回需要写入 Thread Variables 的值
// 优先使用渠道配置的 product，为空则使用全局配置。
func (b *LongConnBot) threadVariable() (project, workspace, region string) {
	// 优先使用渠道配置的 product，为空则使用全局配置
	productType := b.cfg.Product
	if productType == "" {
		productType = b.cmsConfig.Product
	}
	if config.IsSlsProduct(productType) {
		return b.cfg.Project, "", ""
	}
	return "", b.cfg.Workspace, b.cfg.Region
}

// getOrCreateThreadID 查找或新建 CMS 线程 ID
func (b *LongConnBot) getOrCreateThreadID(userID, employeeName, chatID string) (string, error) {
	project, workspace, region := b.threadVariable()
	variable := project + workspace

	// 缓存 key 包含 variable，确保 project/workspace 变更后使用新的 thread
	key := longConnThreadKey(userID, employeeName, chatID) + "\x00" + variable

	if v, ok := b.threadStore.Load(key); ok {
		return v.(string), nil
	}

	client, err := b.newSopClient()
	if err != nil {
		return "", err
	}

	raw := "wecom_lc\x00" + chatID + "\x00" + employeeName
	if chatID == "" {
		raw = "wecom_lc\x00" + userID + "\x00" + employeeName
	}
	h := md5.Sum([]byte(raw + "\x00" + variable))
	session := fmt.Sprintf("%x", h)

	listResp, listErr := client.ListThreads(employeeName, []sopchat.ThreadFilter{
		{Key: "session", Value: session},
	})
	if listErr != nil {
		log.Printf("[WeCom-LongConn] 列出线程失败（将尝试新建）: %v", listErr)
	} else if listResp.Body != nil {
		for _, t := range listResp.Body.Threads {
			if t == nil || t.ThreadId == nil || *t.ThreadId == "" {
				continue
			}
			if v, ok := t.Attributes["session"]; ok && v != nil && *v == session {
				threadID := *t.ThreadId
				log.Printf("[WeCom-LongConn] 找到已有线程 [employee=%s]: %s", employeeName, threadID)
				b.threadStore.Store(key, threadID)
				return threadID, nil
			}
		}
	}

	title := "WeCom-LongConn: " + userID
	if chatID != "" {
		title = "WeCom-LongConn-Group: " + chatID
	}

	log.Printf("[WeCom-LongConn] 创建新线程 [employee=%s] ...", employeeName)
	resp, err := client.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: employeeName,
		Title:        title,
		Attributes:   map[string]interface{}{"session": session},
		Project:      project,
		Workspace:    workspace,
		Region:       region,
	})
	if err != nil {
		return "", fmt.Errorf("调用 CreateThread 失败: %w", err)
	}
	if resp.Body == nil || resp.Body.ThreadId == nil || *resp.Body.ThreadId == "" {
		return "", fmt.Errorf("CreateThread 返回了空的 ThreadId")
	}

	threadID := *resp.Body.ThreadId
	log.Printf("[WeCom-LongConn] 新线程创建成功 [employee=%s]: %s", employeeName, threadID)
	b.threadStore.Store(key, threadID)
	return threadID, nil
}

// newSopClient 构造与 CMS 通信的 sopchat.Client
func (b *LongConnBot) newSopClient() (*sopchat.Client, error) {
	cmsConfig := &openapiutil.Config{
		AccessKeyId:     tea.String(b.cmsConfig.AccessKeyId),
		AccessKeySecret: tea.String(b.cmsConfig.AccessKeySecret),
		Endpoint:        tea.String(b.cmsConfig.Endpoint),
	}
	rawClient, err := cmsclient.NewClient(cmsConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 CMS 客户端失败: %w", err)
	}
	return &sopchat.Client{
		CmsClient:       rawClient,
		AccessKeyId:     b.cmsConfig.AccessKeyId,
		AccessKeySecret: b.cmsConfig.AccessKeySecret,
		Endpoint:        b.cmsConfig.Endpoint,
	}, nil
}

// queryEmployee 向 CMS 数字员工发送消息，返回回复文本和线程 ID
func (b *LongConnBot) queryEmployee(ctx context.Context, message, threadID, employeeName string) (string, string, error) {
	sopClient, err := b.newSopClient()
	if err != nil {
		return "", "", err
	}
	cms := sopClient.CmsClient

	cfg := b.Config()
	if cfg.ConciseReply {
		message += conciseInstruction
	}

	// 获取 project/workspace/region 用于传递给 CreateChat variables
	project, workspace, region := b.threadVariable()

	// 获取渠道配置的 product，为空则使用全局配置
	productType := cfg.Product
	if productType == "" {
		productType = b.cmsConfig.Product
	}

	nowTS := time.Now().Unix()
	variables := map[string]interface{}{
		"timeStamp": fmt.Sprintf("%d", nowTS),
		"timeZone":  "Asia/Shanghai",
		"language":  "zh",
	}
	if config.IsSlsProduct(productType) {
		variables["skill"] = "sop"
		if project != "" {
			variables["project"] = project
		}
	} else {
		if workspace != "" {
			variables["workspace"] = workspace
		}
		if region != "" {
			variables["region"] = region
		}
		// CMS product: add fromTime/toTime (15-minute window)
		now := time.Now()
		variables["fromTime"] = now.Add(-15 * time.Minute).Unix()
		variables["toTime"] = now.Unix()
	}
	request := &cmsclient.CreateChatRequest{
		DigitalEmployeeName: tea.String(employeeName),
		ThreadId:            tea.String(threadID),
		Action:              tea.String("create"),
		Messages: []*cmsclient.CreateChatRequestMessages{
			{
				Role: tea.String("user"),
				Contents: []*cmsclient.CreateChatRequestMessagesContents{
					{
						Type:  tea.String("text"),
						Value: tea.String(message),
					},
				},
			},
		},
		Variables: variables,
	}

	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	go cms.CreateChatWithSSE(request, make(map[string]*string), &dara.RuntimeOptions{}, responseChan, errorChan)

	var textParts []string
	returnedThreadID := threadID

	for {
		select {
		case <-ctx.Done():
			return strings.Join(textParts, ""), returnedThreadID, ctx.Err()

		case response, ok := <-responseChan:
			if !ok {
				return strings.Join(textParts, ""), returnedThreadID, nil
			}
			if response.Body == nil {
				continue
			}
			for _, msg := range response.Body.Messages {
				if msg == nil {
					continue
				}
				for _, content := range msg.Contents {
					if content == nil {
						continue
					}
					if t, ok := content["type"]; ok && t == "text" {
						if v, ok := content["value"]; ok {
							if s, ok := v.(string); ok {
								textParts = append(textParts, s)
							}
						}
					}
				}
			}

		case err, ok := <-errorChan:
			if ok && err != nil {
				return strings.Join(textParts, ""), returnedThreadID, err
			}
			return strings.Join(textParts, ""), returnedThreadID, nil
		}
	}
}
