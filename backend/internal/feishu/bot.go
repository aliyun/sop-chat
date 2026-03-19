package feishu

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

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"
)

// workerQueueSize 每个串行队列允许积压的最大消息数
const workerQueueSize = 8

// Bot 封装飞书机器人及其与 CMS 的对接逻辑
type Bot struct {
	cfgMu     sync.RWMutex
	ftConfig  *config.FeishuConfig
	cmsConfig *config.ClientConfig

	// 会话 -> 线程 ID 的映射
	threadStore sync.Map

	// key -> chan func()，每个 key 对应一个串行 worker
	workerQueues sync.Map

	// WebSocket 客户端生命周期
	cliMu  sync.Mutex
	cancel context.CancelFunc

	// 飞书消息发送客户端
	larkClient *lark.Client

	// 重连控制
	shouldRun         bool
	reconnectAttempts int
}

// NewBot 创建飞书机器人实例
func NewBot(ftConfig *config.FeishuConfig, cmsConfig *config.ClientConfig) *Bot {
	return &Bot{
		ftConfig:  ftConfig,
		cmsConfig: cmsConfig,
	}
}

// Config 返回当前机器人的配置快照
func (b *Bot) Config() *config.FeishuConfig {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.ftConfig
}

// UpdateConfig 热更新运行时配置
func (b *Bot) UpdateConfig(newCfg *config.FeishuConfig) {
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	b.ftConfig = newCfg
	log.Printf("[Feishu] 配置已热更新: appId=%s employee=%s", newCfg.AppID, newCfg.EmployeeName)
}

// Start 启动飞书 WebSocket 连接（非阻塞）
func (b *Bot) Start() error {
	b.cliMu.Lock()
	defer b.cliMu.Unlock()

	if b.cancel != nil {
		return nil // 已在运行，幂等
	}

	cfg := b.Config()

	// 创建飞书客户端（用于发送消息）
	b.larkClient = lark.NewClient(cfg.AppID, cfg.AppSecret,
		lark.WithLogLevel(larkcore.LogLevelWarn),
		lark.WithEnableTokenCache(true),
	)

	// 创建事件处理器
	handler := dispatcher.NewEventDispatcher(
		cfg.VerificationToken,
		cfg.EventEncryptKey,
	).OnP2MessageReceiveV1(b.onMessage)

	// 创建 WebSocket 客户端
	wsClient := larkws.NewClient(
		cfg.AppID,
		cfg.AppSecret,
		larkws.WithEventHandler(handler),
		larkws.WithLogLevel(larkcore.LogLevelWarn),
	)

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.shouldRun = true
	b.reconnectAttempts = 0

	// 启动连接循环（含自动重连）
	go b.connectLoop(ctx, wsClient)

	log.Printf("[Feishu] 机器人已启动，appId=%s，绑定数字员工: %s", cfg.AppID, cfg.EmployeeName)
	return nil
}

// connectLoop 连接循环（含重连逻辑）
func (b *Bot) connectLoop(ctx context.Context, wsClient *larkws.Client) {
	for b.shouldRun {
		err := wsClient.Start(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("[Feishu] WebSocket 连接断开: %v", err)
		}

		if !b.shouldRun || ctx.Err() != nil {
			return
		}

		// 指数退避重连
		delay := b.reconnectDelay()
		log.Printf("[Feishu] %s 后重连 (attempt=%d)", delay, b.reconnectAttempts)
		b.reconnectAttempts++

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// reconnectDelay 计算重连延迟（指数退避）
func (b *Bot) reconnectDelay() time.Duration {
	base := 5 * time.Second
	maxDelay := 60 * time.Second
	delay := time.Duration(float64(base) * math.Pow(2, float64(b.reconnectAttempts)))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// Stop 停止飞书 WebSocket 连接
func (b *Bot) Stop() {
	b.cliMu.Lock()
	defer b.cliMu.Unlock()

	if b.cancel == nil {
		return
	}
	b.shouldRun = false // 停止重连循环
	b.cancel()
	b.cancel = nil
	log.Printf("[Feishu] 机器人已停止")
}

// enqueueWork 将 work 投入 key 对应的串行队列
func (b *Bot) enqueueWork(key string, work func()) bool {
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

// onMessage 处理飞书消息接收事件
func (b *Bot) onMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	msg := event.Event.Message
	sender := event.Event.Sender

	msgType := ""
	if msg.MessageType != nil {
		msgType = *msg.MessageType
	}
	msgID := ""
	if msg.MessageId != nil {
		msgID = *msg.MessageId
	}

	userMessage := extractTextFromMessage(msg)
	if strings.TrimSpace(userMessage) == "" {
		log.Printf("[Feishu] 忽略空消息或不支持的类型 msgId=%s type=%s", msgID, msgType)
		return nil
	}

	senderOpenID := ""
	if sender != nil && sender.SenderId != nil && sender.SenderId.OpenId != nil {
		senderOpenID = *sender.SenderId.OpenId
	}

	chatID := ""
	chatType := ""
	if msg.ChatId != nil {
		chatID = *msg.ChatId
	}
	if msg.ChatType != nil {
		chatType = *msg.ChatType
	}

	log.Printf("[Feishu] 收到消息 chatId=%s chatType=%s sender=%s: %s", chatID, chatType, senderOpenID, userMessage)

	// 白名单校验
	cfg := b.Config()
	if !b.isChatAllowed(chatID) {
		log.Printf("[Feishu] 群聊 %s 不在白名单中，已拒绝", chatID)
		return nil
	}
	if !b.isUserAllowed(senderOpenID) {
		log.Printf("[Feishu] 用户 %s 不在白名单中，已拒绝", senderOpenID)
		return nil
	}

	key := threadKey(chatID, senderOpenID, cfg.EmployeeName)

	// worker queue key 不含 variable，保证同一会话的消息串行处理
	queueKey := key

	// 异步处理，避免阻塞事件回调
	work := func() {
		workCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// 立即发送"思考中"提示，让用户知道消息已收到
		b.sendText(workCtx, chatID, "💭 思考中...")

		threadId, err := b.getOrCreateThreadId(chatID, senderOpenID, cfg.EmployeeName)
		if err != nil {
			log.Printf("[Feishu] 创建线程失败: %v", err)
			b.sendText(workCtx, chatID, "❌ 创建会话失败，请稍后重试")
			return
		}

		log.Printf("[Feishu] 调用数字员工 employee=%s threadId=%s ...", cfg.EmployeeName, threadId)
		replyText, newThreadId, err := b.queryEmployee(workCtx, userMessage, threadId, cfg.EmployeeName)
		if err != nil {
			log.Printf("[Feishu] 调用数字员工失败: %v", err)
			b.sendText(workCtx, chatID, "❌ "+err.Error())
			return
		}

		if newThreadId != "" && newThreadId != threadId {
			// 缓存 key 需要包含 variable
			project, workspace, _ := b.threadVariable()
			variable := project + workspace
			cacheKey := threadKey(chatID, senderOpenID, cfg.EmployeeName) + "\x00" + variable
			b.threadStore.Store(cacheKey, newThreadId)
		}

		log.Printf("[Feishu] 回复消息 chatId=%s 长度=%d", chatID, len(replyText))
		b.sendText(workCtx, chatID, replyText)
	}

	if !b.enqueueWork(queueKey, work) {
		log.Printf("[Feishu] 队列已满，拒绝消息 chatId=%s sender=%s", chatID, senderOpenID)
		b.sendText(ctx, chatID, "⚠️ 消息处理中，请稍后再发。")
	}

	return nil
}

// sendText 通过飞书 API 发送文本消息
func (b *Bot) sendText(ctx context.Context, chatID, text string) {
	if b.larkClient == nil {
		return
	}
	content := fmt.Sprintf(`{"text":%q}`, text)
	resp, err := b.larkClient.Im.Message.Create(ctx,
		larkim.NewCreateMessageReqBuilder().
			ReceiveIdType("chat_id").
			Body(larkim.NewCreateMessageReqBodyBuilder().
				MsgType("text").
				ReceiveId(chatID).
				Content(content).
				Build()).
			Build())
	if err != nil {
		log.Printf("[Feishu] 发送消息失败: %v", err)
		return
	}
	if !resp.Success() {
		log.Printf("[Feishu] 发送消息失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
}

// extractTextFromMessage 从飞书消息中提取纯文本
func extractTextFromMessage(msg *larkim.EventMessage) string {
	if msg.Content == nil {
		return ""
	}
	msgType := ""
	if msg.MessageType != nil {
		msgType = *msg.MessageType
	}
	if msgType != "" && msgType != "text" {
		return ""
	}

	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*msg.Content), &content); err != nil {
		return ""
	}

	// 去除 @机器人 的 mention 标记（格式: @_user_N）
	text := content.Text
	for strings.Contains(text, "@_user_") {
		start := strings.Index(text, "@_user_")
		end := start + 8
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		text = text[:start] + text[end:]
	}

	return strings.TrimSpace(text)
}

// isChatAllowed 检查群聊是否在白名单内
func (b *Bot) isChatAllowed(chatID string) bool {
	cfg := b.Config()
	if len(cfg.AllowedChats) == 0 {
		return true
	}
	for _, c := range cfg.AllowedChats {
		if c == chatID {
			return true
		}
	}
	return false
}

// isUserAllowed 检查用户是否在白名单内
func (b *Bot) isUserAllowed(openID string) bool {
	cfg := b.Config()
	if len(cfg.AllowedUsers) == 0 {
		return true
	}
	for _, u := range cfg.AllowedUsers {
		if u == openID {
			return true
		}
	}
	return false
}

// threadKey 生成线程映射 key
func threadKey(chatID, senderOpenID, employeeName string) string {
	return chatID + "\x00" + senderOpenID + "\x00" + employeeName
}

// newSopClient 构造与 CMS 通信的 sopchat.Client
func (b *Bot) newSopClient() (*sopchat.Client, error) {
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

// threadVariable 根据 product 返回需要写入 Thread Variables 的值
// 优先使用渠道配置的 product，为空则使用全局配置。
func (b *Bot) threadVariable() (project, workspace, region string) {
	// 优先使用渠道配置的 product，为空则使用全局配置
	productType := b.ftConfig.Product
	if productType == "" {
		productType = b.cmsConfig.Product
	}
	if config.IsSlsProduct(productType) {
		return b.ftConfig.Project, "", ""
	}
	return "", b.ftConfig.Workspace, b.ftConfig.Region
}

// getOrCreateThreadId 查找或新建该会话对应的 CMS 线程 ID
func (b *Bot) getOrCreateThreadId(chatID, senderOpenID, employeeName string) (string, error) {
	project, workspace, region := b.threadVariable()
	variable := project + workspace

	// 缓存 key 包含 variable，确保 project/workspace 变更后使用新的 thread
	key := threadKey(chatID, senderOpenID, employeeName) + "\x00" + variable

	if v, ok := b.threadStore.Load(key); ok {
		return v.(string), nil
	}

	client, err := b.newSopClient()
	if err != nil {
		return "", err
	}

	h := md5.Sum([]byte("feishu\x00" + chatID + "\x00" + senderOpenID + "\x00" + employeeName + "\x00" + variable))
	session := fmt.Sprintf("%x", h)

	listResp, listErr := client.ListThreads(employeeName, []sopchat.ThreadFilter{
		{Key: "session", Value: session},
	})
	if listErr != nil {
		log.Printf("[Feishu] 列出线程失败（将尝试新建）: %v", listErr)
	} else if listResp.Body != nil {
		for _, t := range listResp.Body.Threads {
			if t == nil || t.ThreadId == nil || *t.ThreadId == "" {
				continue
			}
			if v, ok := t.Attributes["session"]; ok && v != nil && *v == session {
				threadId := *t.ThreadId
				log.Printf("[Feishu] 找到已有线程 [employee=%s]: %s", employeeName, threadId)
				b.threadStore.Store(key, threadId)
				return threadId, nil
			}
		}
	}

	log.Printf("[Feishu] 为 chatId=%s sender=%s 创建新线程 [employee=%s] ...", chatID, senderOpenID, employeeName)
	resp, err := client.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: employeeName,
		Title:        "Feishu: " + senderOpenID,
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

	threadId := *resp.Body.ThreadId
	log.Printf("[Feishu] 新线程创建成功 [employee=%s]: %s", employeeName, threadId)
	b.threadStore.Store(key, threadId)
	return threadId, nil
}

// conciseInstruction 简洁模式附加指令
const conciseInstruction = "\n\n（请用简洁的纯文本回答，避免复杂排版，适合在 IM 中直接阅读，控制在几句话以内。 尽量拟人的语气，少用markdown）"

// queryEmployee 向 CMS 数字员工发送消息，返回回复文本和线程 ID
func (b *Bot) queryEmployee(ctx context.Context, message, threadId, employeeName string) (string, string, error) {
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
		ThreadId:            tea.String(threadId),
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
	returnedThreadId := threadId

	for {
		select {
		case <-ctx.Done():
			return strings.Join(textParts, ""), returnedThreadId, ctx.Err()

		case response, ok := <-responseChan:
			if !ok {
				return strings.Join(textParts, ""), returnedThreadId, nil
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
				return strings.Join(textParts, ""), returnedThreadId, err
			}
			return strings.Join(textParts, ""), returnedThreadId, nil
		}
	}
}
