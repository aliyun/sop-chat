package feishu

import (
	"context"
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
	"github.com/alibabacloud-go/tea/tea"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"sop-chat/internal/config"
	"sop-chat/internal/i18n"
	"sop-chat/internal/session"
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
	threads *session.ThreadStore

	// key -> chan func()，每个 key 对应一个串行 worker
	workerQueues sync.Map

	// WebSocket 客户端生命周期
	cliMu  sync.Mutex
	cancel context.CancelFunc

	// 飞书消息发送客户端
	larkClient *lark.Client

	// 机器人自身的 open_id，用于群聊 @判断
	botOpenID string

	// 重连控制
	shouldRun         bool
	reconnectAttempts int
}

// NewBot 创建飞书机器人实例
func NewBot(ftConfig *config.FeishuConfig, cmsConfig *config.ClientConfig) *Bot {
	return &Bot{
		ftConfig:  ftConfig,
		cmsConfig: cmsConfig,
		threads:   session.NewThreadStore("[Feishu]"),
	}
}

// Config 返回当前机器人的配置快照
func (b *Bot) Config() *config.FeishuConfig {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.ftConfig
}

// cmsClientConfig 返回当前 CMS 客户端配置（并发安全）
func (b *Bot) cmsClientConfig() *config.ClientConfig {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.cmsConfig
}

func (b *Bot) uiLanguage() string {
	cmsCfg := b.cmsClientConfig()
	if cmsCfg == nil || strings.TrimSpace(cmsCfg.Language) == "" {
		return "zh"
	}
	return cmsCfg.Language
}

// UpdateConfig 热更新运行时配置
func (b *Bot) UpdateConfig(newCfg *config.FeishuConfig) {
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	b.ftConfig = newCfg
	log.Printf("[Feishu] 配置已热更新: appId=%s employee=%s", newCfg.AppID, newCfg.EmployeeName)
}

// UpdateCMSConfig 热更新 CMS 全局配置（language/product 等）
func (b *Bot) UpdateCMSConfig(newCfg *config.ClientConfig) {
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	b.cmsConfig = newCfg
	if newCfg != nil {
		log.Printf("[Feishu] CMS 配置已热更新: product=%s language=%s", newCfg.Product, newCfg.Language)
	}
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

	// 获取机器人自身的 open_id，用于群聊精确判断是否被 @
	if err := b.fetchBotOpenID(context.Background()); err != nil {
		log.Printf("[Feishu] 获取机器人 open_id 失败（群聊@判断可能不精确）: %v", err)
	}

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
	fmt.Println("botid:", b.botOpenID)
	// 群聊中只响应 @机器人 的消息，未被 @则忽略
	if chatType == "group" && !b.isBotMentioned(msg.Mentions) {
		log.Printf("[Feishu] 群聊消息未@机器人，忽略 chatId=%s sender=%s", chatID, senderOpenID)
		return nil
	}

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
		b.sendText(workCtx, chatID, i18n.ThinkingHint(b.uiLanguage()))

		threadId, err := b.getOrCreateThreadId(chatID, senderOpenID, cfg.EmployeeName)
		if err != nil {
			log.Printf("[Feishu] 创建线程失败: %v", err)
			b.sendText(workCtx, chatID, i18n.SessionCreateFailedHint(b.uiLanguage()))
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
			project, workspace, _ := b.threadVariable()
			variable := project + workspace
			b.threads.Store(threadKey(chatID, senderOpenID, cfg.EmployeeName)+"\x00"+variable, newThreadId)
		}

		log.Printf("[Feishu] 回复消息 chatId=%s 长度=%d", chatID, len(replyText))
		b.sendText(workCtx, chatID, replyText)
	}

	if !b.enqueueWork(queueKey, work) {
		log.Printf("[Feishu] 队列已满，拒绝消息 chatId=%s sender=%s", chatID, senderOpenID)
		b.sendText(ctx, chatID, i18n.BusyHint(b.uiLanguage()))
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

// fetchBotOpenID 通过飞书 API 获取机器人自身的 open_id
func (b *Bot) fetchBotOpenID(ctx context.Context) error {
	if b.larkClient == nil {
		return fmt.Errorf("larkClient 未初始化")
	}

	resp, err := b.larkClient.Do(ctx, &larkcore.ApiReq{
		HttpMethod:                "GET",
		ApiPath:                   "https://open.feishu.cn/open-apis/bot/v3/info",
		SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeTenant},
	})
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}

	var result struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(resp.RawBody, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("API 返回错误 code=%d", result.Code)
	}
	if result.Bot.OpenID == "" {
		return fmt.Errorf("API 返回空的 open_id")
	}

	b.botOpenID = result.Bot.OpenID
	log.Printf("[Feishu] 获取机器人 open_id 成功: %s", b.botOpenID)
	return nil
}

// isBotMentioned 检查消息的 mentions 中是否包含当前机器人
func (b *Bot) isBotMentioned(mentions []*larkim.MentionEvent) bool {
	// 如果未获取到 botOpenID，退化为只要有 mention 就认为被 @
	if b.botOpenID == "" {
		return len(mentions) > 0
	}
	for _, m := range mentions {
		if m != nil && m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId == b.botOpenID {
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
	return session.NewSopClient(b.cmsClientConfig())
}

// threadVariable 根据 product 返回需要写入 Thread Variables 的值
// 优先使用渠道配置的 product，为空则使用全局配置。
func (b *Bot) threadVariable() (project, workspace, region string) {
	cfg := b.Config()
	cmsCfg := b.cmsClientConfig()
	globalProduct := ""
	if cmsCfg != nil {
		globalProduct = cmsCfg.Product
	}
	return session.ThreadVariable(cfg.Product, globalProduct, cfg.Project, cfg.Workspace, cfg.Region)
}

// getOrCreateThreadId 查找或新建该会话对应的 CMS 线程 ID
func (b *Bot) getOrCreateThreadId(chatID, senderOpenID, employeeName string) (string, error) {
	project, workspace, region := b.threadVariable()
	variable := project + workspace

	client, err := b.newSopClient()
	if err != nil {
		return "", err
	}

	cacheKey := threadKey(chatID, senderOpenID, employeeName) + "\x00" + variable
	return b.threads.GetOrCreate(client, session.ThreadParams{
		CacheKey:     cacheKey,
		SessionRaw:   "feishu\x00" + chatID + "\x00" + senderOpenID + "\x00" + employeeName + "\x00" + variable,
		EmployeeName: employeeName,
		Title:        "Feishu: " + senderOpenID,
		Project:      project,
		Workspace:    workspace,
		Region:       region,
	})
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
	cmsCfg := b.cmsClientConfig()
	if cfg.ConciseReply {
		message += conciseInstruction
	}

	// 获取 project/workspace/region 用于传递给 CreateChat variables
	project, workspace, region := b.threadVariable()

	// 获取渠道配置的 product，为空则使用全局配置
	productType := cfg.Product
	if productType == "" && cmsCfg != nil {
		productType = cmsCfg.Product
	}

	nowTS := time.Now().Unix()
	variables := map[string]interface{}{
		"timeStamp": fmt.Sprintf("%d", nowTS),
		"timeZone":  "Asia/Shanghai",
		"language":  "zh",
	}
	if cmsCfg != nil {
		variables["language"] = cmsCfg.Language
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

	opts := &sopchat.QueryEmployeeOptions{
		CMSClient: cms,
		Request:   request,
	}
	result, err := sopchat.QueryEmployeeWithRetry(ctx, opts)
	if err != nil {
		return "", "", err
	}
	return result.Text, result.ThreadId, nil
}
