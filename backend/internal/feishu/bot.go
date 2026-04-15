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
	"sop-chat/internal/session"
	"sop-chat/pkg/sopchat"
)

// workerQueueSize 每个串行队列允许积压的最大消息数
const workerQueueSize = 8

// Bot 封装飞书机器人及其与 CMS 的对接逻辑
type Bot struct {
	cfgMu        sync.RWMutex
	ftConfig     *config.FeishuConfig
	cmsConfig    *config.ClientConfig
	globalConfig *config.Config

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
func NewBot(ftConfig *config.FeishuConfig, cmsConfig *config.ClientConfig, globalConfig *config.Config) *Bot {
	return &Bot{
		ftConfig:     ftConfig,
		cmsConfig:    cmsConfig,
		globalConfig: globalConfig,
		threads:      session.NewThreadStore("[Feishu]"),
	}
}

// Config 返回当前机器人的配置快照
func (b *Bot) Config() *config.FeishuConfig {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.ftConfig
}

// GlobalConfig 返回当前机器人引用的全局配置。
func (b *Bot) GlobalConfig() *config.Config {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.globalConfig
}

// CMSConfig 返回当前绑定的云账号客户端配置（用于热重载比较）。
func (b *Bot) CMSConfig() *config.ClientConfig {
	return b.cmsConfig
}

// UpdateConfig 热更新运行时配置
func (b *Bot) UpdateConfig(newCfg *config.FeishuConfig, globalConfig *config.Config) {
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	b.ftConfig = newCfg
	b.globalConfig = globalConfig
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
	if !b.isChatAllowed(chatID) {
		log.Printf("[Feishu] 群聊 %s 不在白名单中，已拒绝", chatID)
		return nil
	}
	if !b.isUserAllowed(senderOpenID) {
		log.Printf("[Feishu] 用户 %s 不在白名单中，已拒绝", senderOpenID)
		return nil
	}

	target := b.resolveTarget(userMessage)
	key := threadKey(chatID, senderOpenID, target.employeeName)

	// worker queue key 不含 variable，保证同一会话的消息串行处理
	queueKey := key

	// 异步处理，避免阻塞事件回调
	work := func() {
		workCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// 立即发送处理中提示，让用户知道消息已收到
		b.sendText(workCtx, chatID, "收到，正在处理中...")

		threadId, err := b.getOrCreateThreadId(chatID, senderOpenID, target)
		if err != nil {
			log.Printf("[Feishu] 创建线程失败: %v", err)
			b.sendText(workCtx, chatID, "创建会话失败，请稍后重试")
			return
		}

		log.Printf("[Feishu] 调用数字员工 cloudAccountId=%s employee=%s threadId=%s ...", target.cloudAccountID, target.employeeName, threadId)
		replyText, newThreadId, err := b.queryEmployee(workCtx, userMessage, threadId, target)
		if err != nil {
			log.Printf("[Feishu] 调用数字员工失败: %v", err)
			b.sendText(workCtx, chatID, "处理失败："+err.Error())
			return
		}

		if newThreadId != "" && newThreadId != threadId {
			scope := threadScope(target.cloudAccountID, target.project, target.workspace, target.region)
			cacheKey := threadKey(chatID, senderOpenID, target.employeeName) + "\x00" + scope
			b.threads.Store(cacheKey, newThreadId)
		}

		log.Printf("[Feishu] 回复消息 chatId=%s 长度=%d", chatID, len(replyText))
		b.sendText(workCtx, chatID, replyText)
	}

	if !b.enqueueWork(queueKey, work) {
		log.Printf("[Feishu] 队列已满，拒绝消息 chatId=%s sender=%s", chatID, senderOpenID)
		b.sendText(ctx, chatID, "当前有消息正在处理中，请稍后再发。")
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

func threadScope(cloudAccountID, project, workspace, region string) string {
	return config.NormalizeCloudAccountID(cloudAccountID) + "\x00" + project + "\x00" + workspace + "\x00" + region
}

type resolvedTarget struct {
	employeeName   string
	cloudAccountID string
	product        string
	project        string
	workspace      string
	region         string
	clientConfig   *config.ClientConfig
}

func (b *Bot) resolveTarget(message string) resolvedTarget {
	cfg := b.Config()
	target := resolvedTarget{
		employeeName:   cfg.EmployeeName,
		cloudAccountID: config.NormalizeCloudAccountID(cfg.CloudAccountID),
		product:        cfg.Product,
		project:        cfg.Project,
		workspace:      cfg.Workspace,
		region:         cfg.Region,
		clientConfig:   b.cmsConfig,
	}
	if target.product == "" && b.cmsConfig != nil {
		target.product = b.cmsConfig.Product
	}

	globalCfg := b.GlobalConfig()
	if globalCfg != nil {
		accountID, matched, ambiguous := globalCfg.ResolveMessageCloudAccountID(message, target.cloudAccountID)
		if len(ambiguous) > 1 {
			log.Printf("[Feishu] 消息 %q 命中多个 cloudAccountId=%v，继续使用默认账号 %q", promptForRouteLog(message), ambiguous, target.cloudAccountID)
		}
		if matched {
			if clientCfg, err := globalCfg.ResolveClientConfig(accountID); err == nil {
				target.cloudAccountID = clientCfg.CloudAccountID
				target.clientConfig = clientCfg
			} else {
				log.Printf("[Feishu] cloudAccountId=%q 解析失败，继续使用默认账号 %q: %v", accountID, target.cloudAccountID, err)
			}
		}
	}

	if route := config.FindCloudAccountRoute(cfg.CloudAccountRoutes, target.cloudAccountID); route != nil {
		if route.EmployeeName != "" {
			target.employeeName = route.EmployeeName
		}
		if route.Product != "" {
			target.product = route.Product
		}
		if route.Project != "" {
			target.project = route.Project
		}
		if route.Workspace != "" {
			target.workspace = route.Workspace
		}
		if route.Region != "" {
			target.region = route.Region
		}
	}

	if target.clientConfig == nil {
		target.clientConfig = &config.ClientConfig{
			CloudAccountID: target.cloudAccountID,
		}
		if b.cmsConfig != nil {
			target.clientConfig.AccessKeyId = b.cmsConfig.AccessKeyId
			target.clientConfig.AccessKeySecret = b.cmsConfig.AccessKeySecret
			target.clientConfig.Endpoint = b.cmsConfig.Endpoint
			target.clientConfig.Product = b.cmsConfig.Product
		}
	}
	if target.product == "" && target.clientConfig != nil {
		target.product = target.clientConfig.Product
	}

	return target
}

func promptForRouteLog(message string) string {
	text := strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	runes := []rune(text)
	if len(runes) <= 80 {
		return text
	}
	return string(runes[:80]) + "..."
}

// newSopClient 构造与 CMS 通信的 sopchat.Client
func (b *Bot) newSopClient() (*sopchat.Client, error) {
	return b.newSopClientWithConfig(b.cmsConfig)
}

// newSopClientWithConfig 使用指定账号凭据构造与 CMS 通信的 sopchat.Client。
func (b *Bot) newSopClientWithConfig(clientCfg *config.ClientConfig) (*sopchat.Client, error) {
	if clientCfg == nil {
		return nil, fmt.Errorf("CMS 客户端配置为空")
	}
	return session.NewSopClient(clientCfg)
}

// threadVariable 根据 product 返回需要写入 Thread Variables 的值
// 优先使用渠道配置的 product，为空则使用全局配置。
func (b *Bot) threadVariable() (project, workspace, region string) {
	return session.ThreadVariable(b.ftConfig.Product, b.cmsConfig.Product, b.ftConfig.Project, b.ftConfig.Workspace, b.ftConfig.Region)
}

func threadVariableForTarget(target resolvedTarget) (project, workspace, region string) {
	if config.IsSlsProduct(target.product) {
		return target.project, "", ""
	}
	return "", target.workspace, target.region
}

// getOrCreateThreadId 查找或新建该会话对应的 CMS 线程 ID
func (b *Bot) getOrCreateThreadId(chatID, senderOpenID string, target resolvedTarget) (string, error) {
	project, workspace, region := threadVariableForTarget(target)
	scope := threadScope(target.cloudAccountID, project, workspace, region)

	// 缓存 key 包含订阅和变量，确保 cloudAccountId / project / workspace / region 变更后使用新的 thread
	key := threadKey(chatID, senderOpenID, target.employeeName) + "\x00" + scope

	client, err := b.newSopClientWithConfig(target.clientConfig)
	if err != nil {
		return "", err
	}

	return b.threads.GetOrCreate(client, session.ThreadParams{
		CacheKey:     key,
		SessionRaw:   "feishu\x00" + chatID + "\x00" + senderOpenID + "\x00" + target.employeeName + "\x00" + scope,
		EmployeeName: target.employeeName,
		Title:        "Feishu: " + senderOpenID,
		Project:      project,
		Workspace:    workspace,
		Region:       region,
	})
}

// queryEmployee 向 CMS 数字员工发送消息，返回回复文本和线程 ID
func (b *Bot) queryEmployee(ctx context.Context, message, threadId string, target resolvedTarget) (string, string, error) {
	sopClient, err := b.newSopClientWithConfig(target.clientConfig)
	if err != nil {
		return "", "", err
	}
	cms := sopClient.CmsClient

	cfg := b.Config()

	project, workspace, region := threadVariableForTarget(target)
	productType := target.product
	if productType == "" && target.clientConfig != nil {
		productType = target.clientConfig.Product
	}
	if productType == "" && b.cmsConfig != nil {
		productType = b.cmsConfig.Product
	}
	message = config.ApplyReplyStyleInstruction(message, cfg.ConciseReply, productType)

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
		DigitalEmployeeName: tea.String(target.employeeName),
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

	runtime := sopchat.NewSSERuntimeOptions()
	go cms.CreateChatWithSSECtx(ctx, request, make(map[string]*string), runtime, responseChan, errorChan)

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
			// 检测 done 消息
			if sopchat.IsDoneMessage(response.Body) {
				return strings.Join(textParts, ""), returnedThreadId, nil
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
