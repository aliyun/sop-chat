package dingtalk

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	dingclient "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"

	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"
)

// atMentionPattern 匹配 @xxx 格式（用于从 text 消息中去掉 @机器人 前缀）
var atMentionPattern = regexp.MustCompile(`@\S+\s*`)

// workerQueueSize 是每个串行队列允许积压的最大消息数
const workerQueueSize = 8

// Bot 封装钉钉机器人及其与 CMS 的对接逻辑
type Bot struct {
	// dtConfig 受 cfgMu 保护，所有读取须调用 config() 方法
	cfgMu     sync.RWMutex
	dtConfig  *config.DingTalkConfig
	cmsConfig *config.ClientConfig

	// 会话 -> 线程 ID 的映射，实现多轮对话上下文
	threadStore sync.Map

	// key（机器人+会话+人）-> chan func()，每个 key 对应一个串行 worker
	workerQueues sync.Map

	// Stream 客户端生命周期（Start/Stop 时持有锁）
	cliMu sync.Mutex
	cli   *dingclient.StreamClient
}

// enqueueWork 将 work 投入 key 对应的串行队列。
// 首次调用时自动创建 channel 并启动 worker goroutine。
// 若队列已满则返回 false，调用方应向用户反馈繁忙。
func (b *Bot) enqueueWork(key string, work func()) bool {
	ch := make(chan func(), workerQueueSize)
	actual, loaded := b.workerQueues.LoadOrStore(key, ch)
	ch = actual.(chan func())
	if !loaded {
		// 首次创建：启动该 key 专属的串行 worker
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

// NewBot 创建一个新的钉钉机器人实例
func NewBot(dtConfig *config.DingTalkConfig, cmsConfig *config.ClientConfig) *Bot {
	return &Bot{
		dtConfig:  dtConfig,
		cmsConfig: cmsConfig,
	}
}

// config 返回当前配置（并发安全）
func (b *Bot) config() *config.DingTalkConfig {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.dtConfig
}

// Config 返回当前机器人的配置快照（供外部调用）
func (b *Bot) Config() *config.DingTalkConfig {
	return b.config()
}

// UpdateConfig 热更新运行时配置（凭据不变的情况下生效）
func (b *Bot) UpdateConfig(newCfg *config.DingTalkConfig) {
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	b.dtConfig = newCfg
	log.Printf("[DingTalk] 配置已热更新: clientId=%s allowedGroupUsers=%v allowedDirectUsers=%v conciseReply=%v",
		newCfg.ClientId, newCfg.AllowedGroupUsers, newCfg.AllowedDirectUsers, newCfg.ConciseReply)
}

// Start 启动钉钉 Stream 连接（非阻塞：SDK 内部以 goroutine 运行消息循环）
func (b *Bot) Start() error {
	b.cliMu.Lock()
	defer b.cliMu.Unlock()

	if b.cli != nil {
		return nil // 已在运行，幂等
	}

	cli := dingclient.NewStreamClient(
		dingclient.WithAppCredential(dingclient.NewAppCredentialConfig(b.dtConfig.ClientId, b.dtConfig.ClientSecret)),
		dingclient.WithUserAgent(dingclient.NewDingtalkGoSDKUserAgent()),
		dingclient.WithAutoReconnect(true), // 断线自动重连，直到 Stop() 被调用
	)
	cli.RegisterChatBotCallbackRouter(b.onMessage)

	if err := cli.Start(context.Background()); err != nil {
		return err
	}
	b.cli = cli
	log.Printf("[DingTalk] 机器人已启动，绑定数字员工: %s", b.dtConfig.EmployeeName)
	return nil
}

// Stop 停止钉钉 Stream 连接，禁用自动重连后关闭 WebSocket
func (b *Bot) Stop() {
	b.cliMu.Lock()
	defer b.cliMu.Unlock()

	if b.cli == nil {
		return
	}
	// 必须先关闭自动重连，否则 Close() 会触发 processLoop 的 deferred reconnect()
	b.cli.AutoReconnect = false

	// SDK v0.9.1 存在 race condition：Close() 后 processLoop 的 goroutine 可能仍在向已关闭的 channel 发送数据
	// 使用 recover 防止 panic 导致程序崩溃
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[DingTalk] Stop() recovered from SDK panic: %v", r)
			}
		}()
		b.cli.Close()
	}()

	b.cli = nil
	log.Printf("[DingTalk] 机器人已停止")
}

// errorMessage 从 err 中提取可读的错误信息，不含堆栈。
// 对阿里云 SDK 的 SDKError 只取 Code + Message；其他错误直接返回 err.Error()。
func errorMessage(err error) string {
	var sdkErr *tea.SDKError
	if errors.As(err, &sdkErr) {
		code := tea.StringValue(sdkErr.Code)
		msg := tea.StringValue(sdkErr.Message)
		if code != "" && msg != "" {
			return fmt.Sprintf("[%s] %s", code, msg)
		}
		if msg != "" {
			return msg
		}
		if code != "" {
			return code
		}
	}
	return err.Error()
}

// replyAtMarkdown 向钉钉发送 Markdown 消息，并 @ 提问者。
// title 作为钉钉 markdown 消息的标题字段（不显示在正文中，但出现在通知预览）。
// atDingtalkIds 负责触发客户端通知和渲染高亮 @，content 本身不再重复拼 @前缀，
// 避免钉钉客户端自动显示的 @ 头与手动拼接的前缀重叠造成双 @。
func replyAtMarkdown(ctx context.Context, webhook, senderId, title, content string) error {
	body := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]interface{}{
			"title": title,
			"text":  content,
		},
		"at": map[string]interface{}{
			"atDingtalkIds": []string{senderId},
			"isAtAll":       false,
		},
	}
	return chatbot.NewChatbotReplier().ReplyMessage(ctx, webhook, body)
}

// replyError 向钉钉回复一条错误提示，前缀固定为 "❌ " 以便用户识别。
func replyError(ctx context.Context, webhook string, err error) {
	replier := chatbot.NewChatbotReplier()
	_ = replier.SimpleReplyText(ctx, webhook, []byte("❌ "+errorMessage(err)))
}

// onMessage 处理钉钉消息回调
// 签名符合 chatbot.IChatBotMessageHandler
func (b *Bot) onMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	userText := extractText(data)
	if userText == "" {
		log.Printf("[DingTalk] 忽略空消息 conversationId=%s msgtype=%s", data.ConversationId, data.Msgtype)
		return nil, nil
	}

	log.Printf("[DingTalk] 收到消息 conversationId=%s sender=%s msgtype=%s: %s",
		data.ConversationId, data.SenderNick, data.Msgtype, userText)

	// 白名单校验（均在取 cfg 快照之前，直接调用 b.config() 保证读取最新配置）
	if !b.isConversationAllowed(data.ConversationType, data.ConversationTitle) {
		log.Printf("[DingTalk] 群 %q 不在群白名单中，已拒绝", data.ConversationTitle)
		replier := chatbot.NewChatbotReplier()
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("抱歉，该群暂未开放机器人问答功能。"))
		return nil, nil
	}
	if !b.isSenderAllowed(data.ConversationType, data.SenderNick) {
		log.Printf("[DingTalk] 用户 %s 不在白名单中（conversationType=%s），已拒绝", data.SenderNick, data.ConversationType)
		replier := chatbot.NewChatbotReplier()
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("抱歉，您暂时没有使用该机器人的权限。"))
		return nil, nil
	}

	// 提前捕获所有需要的值，避免 goroutine 中访问 data 指针
	// config() 在此处取一次快照，保证本次请求全程使用同一份配置
	cfg := b.config()
	webhook := data.SessionWebhook
	expiredAt := data.SessionWebhookExpiredTime
	conversationId := data.ConversationId
	conversationType := data.ConversationType // "1"=单聊 "2"=群聊
	conversationTitle := data.ConversationTitle
	senderNick := data.SenderNick
	senderId := data.SenderId
	// 路由解析：按群名匹配，找不到则用默认配置
	route := b.resolveRoute(conversationType, conversationTitle)
	if route.employeeName != cfg.EmployeeName {
		log.Printf("[DingTalk] 群 %q 命中路由规则，路由到数字员工: %s product=%s project=%s workspace=%s", conversationTitle, route.employeeName, route.product, route.project, route.workspace)
	}

	// worker queue key 不含 variable，保证同一会话的消息串行处理
	queueKey := threadKey(conversationId, senderNick, route.employeeName)

	replier := chatbot.NewChatbotReplier()

	// 构造本次请求的处理函数，投入该 key 的串行队列
	work := func() {
		deadline := time.Unix(expiredAt/1000, 0).Add(-5 * time.Second)
		asyncCtx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()

		threadId, err := b.getOrCreateThreadIdWithRoute(conversationId, senderNick, route)
		if err != nil {
			log.Printf("[DingTalk] 创建线程失败: %v", err)
			replyError(asyncCtx, webhook, err)
			return
		}

		log.Printf("[DingTalk] 正在调用数字员工 employeeName=%s threadId=%q ...", route.employeeName, threadId)
		replyText, newThreadId, err := b.queryEmployeeWithRoute(asyncCtx, userText, threadId, route)
		if err != nil {
			log.Printf("[DingTalk] 调用数字员工失败: %v", err)
			replyError(asyncCtx, webhook, err)
			return
		}
		log.Printf("[DingTalk] 数字员工返回内容（长度=%d）: %s", len(replyText), replyText)

		if newThreadId != "" && newThreadId != threadId {
			log.Printf("[DingTalk] 线程 ID 变更: %q -> %q，更新映射", threadId, newThreadId)
			// 缓存 key 需要包含 variable
			variable := route.project + route.workspace
			cacheKey := threadKey(conversationId, senderNick, route.employeeName) + "\x00" + variable
			b.threadStore.Store(cacheKey, newThreadId)
		}

		log.Printf("[DingTalk] 正在回复钉钉消息，sessionWebhook=%s", webhook)

		var replyErr error
		if conversationType == "2" {
			// 群聊：@ 提问者，触发客户端通知和高亮
			replyErr = replyAtMarkdown(asyncCtx, webhook, senderId, "回复", replyText)
		} else {
			// 单聊：直接回复，无需 @
			replyErr = chatbot.NewChatbotReplier().ReplyMessage(asyncCtx, webhook, map[string]interface{}{
				"msgtype": "markdown",
				"markdown": map[string]interface{}{
					"title": "回复",
					"text":  replyText,
				},
			})
		}
		if replyErr != nil {
			log.Printf("[DingTalk] 回复消息失败: %v", replyErr)
		} else {
			log.Printf("[DingTalk] 回复成功")
		}
	}

	// 尝试入队：同一 key 的消息串行执行，不同 key 并发处理
	if !b.enqueueWork(queueKey, work) {
		log.Printf("[DingTalk] 队列已满，拒绝消息 conversationId=%s sender=%s", conversationId, senderNick)
		_ = replier.SimpleReplyText(ctx, webhook, []byte("⚠️ 消息处理中，请稍后再发。"))
		return nil, nil
	}

	// 入队成功，立即告知用户已收到
	_ = replier.SimpleReplyText(ctx, webhook, []byte("⏳ 收到，正在处理中..."))
	return nil, nil
}

// extractText 从钉钉消息中提取纯文本，支持 text 和 richText 两种消息类型
func extractText(data *chatbot.BotCallbackDataModel) string {
	switch data.Msgtype {
	case "text":
		// text 消息：直接取 text.content，去掉开头的 @机器人 前缀
		raw := strings.TrimSpace(data.Text.Content)
		// 去掉开头所有 @xxx 片段（群聊中机器人被 @ 时会带上）
		cleaned := strings.TrimSpace(atMentionPattern.ReplaceAllString(raw, ""))
		if cleaned != "" {
			return cleaned
		}
		// 如果去掉 @xxx 后为空（说明消息就只有 @），返回原始内容
		return raw

	case "richText":
		// richText 消息：从 content.richText 数组中拼接 text 片段，跳过 at 片段
		content, ok := data.Content.(map[string]interface{})
		if !ok {
			return ""
		}
		richTextRaw, ok := content["richText"]
		if !ok {
			return ""
		}
		parts, ok := richTextRaw.([]interface{})
		if !ok {
			return ""
		}
		var sb strings.Builder
		for _, p := range parts {
			part, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			partType, _ := part["type"].(string)
			switch partType {
			case "text", "":
				if txt, ok := part["text"].(string); ok {
					sb.WriteString(txt)
				}
			case "at":
				// 跳过 @机器人 片段，不将其计入问题内容
			}
		}
		return strings.TrimSpace(sb.String())

	default:
		// 其他类型（图片、语音等）暂不处理
		log.Printf("[DingTalk] 不支持的消息类型: %s，已忽略", data.Msgtype)
		return ""
	}
}

// threadKey 对 conversationId + senderNick + employeeName 取 MD5，
// 同时作为内存 threadStore 的 key 和 CMS thread attribute session 的值。
// 包含 employeeName 确保切换路由后不会复用属于其他员工的旧线程。
func threadKey(conversationId, senderNick, employeeName string) string {
	h := md5.Sum([]byte(conversationId + "\x00" + senderNick + "\x00" + employeeName))
	return fmt.Sprintf("%x", h)
}

// resolvedRoute 包含路由解析结果
type resolvedRoute struct {
	employeeName string
	product      string
	project      string
	workspace    string
	region       string
}

// resolveRoute 根据群名称匹配路由规则，返回应处理本次消息的路由信息。
// 单聊（conversationType != "2"）或匹配不到规则时，返回默认配置。
func (b *Bot) resolveRoute(conversationType, conversationTitle string) resolvedRoute {
	cfg := b.config()
	result := resolvedRoute{
		employeeName: cfg.EmployeeName,
		product:      cfg.Product,
		project:      cfg.Project,
		workspace:    cfg.Workspace,
		region:       cfg.Region,
	}
	if conversationType == "2" {
		for _, route := range cfg.ConversationRoutes {
			if route.ConversationTitle == conversationTitle && route.EmployeeName != "" {
				result.employeeName = route.EmployeeName
				// 路由级别的配置优先
				if route.Product != "" {
					result.product = route.Product
				}
				if route.Project != "" {
					result.project = route.Project
				}
				if route.Workspace != "" {
					result.workspace = route.Workspace
				}
				if route.Region != "" {
					result.region = route.Region
				}
				break
			}
		}
	}
	// 如果 product 为空，使用全局配置
	if result.product == "" {
		result.product = b.cmsConfig.Product
	}
	return result
}

// isSenderAllowed 按会话类型检查发送者是否在对应白名单内。
//   - 群聊（conversationType=="2"）：检查 allowedGroupUsers；为空时放行所有群成员
//   - 单聊（conversationType=="1" 或其他）：检查 allowedDirectUsers；为空时放行所有单聊用户
func (b *Bot) isSenderAllowed(conversationType, senderNick string) bool {
	cfg := b.config()
	if conversationType == "2" {
		// 群聊场景
		if len(cfg.AllowedGroupUsers) == 0 {
			return true
		}
		for _, u := range cfg.AllowedGroupUsers {
			if u == senderNick {
				return true
			}
		}
		return false
	}
	// 单聊场景
	if len(cfg.AllowedDirectUsers) == 0 {
		return true
	}
	for _, u := range cfg.AllowedDirectUsers {
		if u == senderNick {
			return true
		}
	}
	return false
}

// isConversationAllowed 检查本次消息所在会话是否被允许。
// 群白名单为空时放行所有；有值时单聊（conversationType=="1"）始终放行，
// 群聊（conversationType=="2"）须 conversationTitle 在白名单中。
func (b *Bot) isConversationAllowed(conversationType, conversationTitle string) bool {
	cfg := b.config()
	if len(cfg.AllowedConversations) == 0 {
		return true
	}
	if conversationType != "2" {
		return true // 单聊不受群白名单限制
	}
	for _, c := range cfg.AllowedConversations {
		if c == conversationTitle {
			return true
		}
	}
	return false
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

// threadVariable 根据 product 返回需要写入 Thread Variables 的值：
// SLS 产品返回 project，CMS 产品返回 workspace 和 region。
// 优先使用渠道配置的 product，为空则使用全局配置。
func (b *Bot) threadVariable() (project, workspace, region string) {
	// 优先使用渠道配置的 product，为空则使用全局配置
	productType := b.dtConfig.Product
	if productType == "" {
		productType = b.cmsConfig.Product
	}
	if config.IsSlsProduct(productType) {
		return b.dtConfig.Project, "", ""
	}
	return "", b.dtConfig.Workspace, b.dtConfig.Region
}

// getOrCreateThreadId 查找或新建该会话对应的 CMS 线程 ID。
// 查找顺序：内存缓存 → ListThreads(session attribute 过滤) → CreateThread 新建。
// employeeName 决定线程归属的数字员工（路由后的目标员工）。
func (b *Bot) getOrCreateThreadId(conversationId, senderNick, employeeName string) (string, error) {
	project, workspace, region := b.threadVariable()
	variable := project + workspace // 两者互斥，至多一个非空

	// 缓存 key 包含 variable，确保 project/workspace 变更后使用新的 thread
	key := threadKey(conversationId, senderNick, employeeName) + "\x00" + variable

	// 1. 先查内存缓存
	if v, ok := b.threadStore.Load(key); ok {
		return v.(string), nil
	}

	client, err := b.newSopClient()
	if err != nil {
		return "", err
	}

	// session 带前缀，确保不同平台（钉钉/企业微信）的线程不会冲突
	// 格式: md5("dingtalk\x00" + key + "\x00" + variable)
	sh := md5.Sum([]byte("dingtalk\x00" + key + "\x00" + variable))
	session := fmt.Sprintf("%x", sh)

	// 2. 缓存 miss：用 session attribute 过滤，找已有的线程（进程重启后恢复上下文）
	listResp, listErr := client.ListThreads(employeeName, []sopchat.ThreadFilter{
		{Key: "session", Value: session},
	})
	if listErr != nil {
		log.Printf("[DingTalk] 列出线程失败（将尝试新建）: %v", listErr)
	} else if listResp.Body != nil {
		for _, t := range listResp.Body.Threads {
			if t == nil || t.ThreadId == nil || *t.ThreadId == "" {
				continue
			}
			if v, ok := t.Attributes["session"]; ok && v != nil && *v == session {
				threadId := *t.ThreadId
				log.Printf("[DingTalk] 会话 %s(%s) 找到已有线程 [employee=%s]: %s", conversationId, senderNick, employeeName, threadId)
				b.threadStore.Store(key, threadId)
				return threadId, nil
			}
		}
	}

	// 3. 远端也没有，创建新线程，并写入 session attribute 供后续查找
	log.Printf("[DingTalk] 为会话 %s(%s) 创建新线程 [employee=%s] ...", conversationId, senderNick, employeeName)
	resp, err := client.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: employeeName,
		Title:        "DingTalk: " + senderNick,
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
	log.Printf("[DingTalk] 会话 %s(%s) 新线程创建成功 [employee=%s]: %s", conversationId, senderNick, employeeName, threadId)
	b.threadStore.Store(key, threadId)
	return threadId, nil
}

// getOrCreateThreadIdWithRoute 根据路由信息获取或创建线程
func (b *Bot) getOrCreateThreadIdWithRoute(conversationId, senderNick string, route resolvedRoute) (string, error) {
	variable := route.project + route.workspace // 两者互斥，至多一个非空

	// 缓存 key 包含 variable，确保 project/workspace 变更后使用新的 thread
	key := threadKey(conversationId, senderNick, route.employeeName) + "\x00" + variable

	// 1. 先查内存缓存
	if v, ok := b.threadStore.Load(key); ok {
		return v.(string), nil
	}

	client, err := b.newSopClient()
	if err != nil {
		return "", err
	}

	// session 带前缀，确保不同平台（钉钉/企业微信）的线程不会冲突
	// 格式: md5("dingtalk\x00" + key + "\x00" + variable)
	sh := md5.Sum([]byte("dingtalk\x00" + key + "\x00" + variable))
	session := fmt.Sprintf("%x", sh)

	// 2. 缓存 miss：用 session attribute 过滤，找已有的线程（进程重启后恢复上下文）
	listResp, listErr := client.ListThreads(route.employeeName, []sopchat.ThreadFilter{
		{Key: "session", Value: session},
	})
	if listErr != nil {
		log.Printf("[DingTalk] 列出线程失败（将尝试新建）: %v", listErr)
	} else if listResp.Body != nil {
		for _, t := range listResp.Body.Threads {
			if t == nil || t.ThreadId == nil || *t.ThreadId == "" {
				continue
			}
			if v, ok := t.Attributes["session"]; ok && v != nil && *v == session {
				threadId := *t.ThreadId
				log.Printf("[DingTalk] 会话 %s(%s) 找到已有线程 [employee=%s]: %s", conversationId, senderNick, route.employeeName, threadId)
				b.threadStore.Store(key, threadId)
				return threadId, nil
			}
		}
	}

	// 3. 远端也没有，创建新线程，并写入 session attribute 供后续查找
	log.Printf("[DingTalk] 为会话 %s(%s) 创建新线程 [employee=%s] ...", conversationId, senderNick, route.employeeName)
	resp, err := client.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: route.employeeName,
		Title:        "DingTalk: " + senderNick,
		Attributes:   map[string]interface{}{"session": session},
		Project:      route.project,
		Workspace:    route.workspace,
		Region:       route.region,
	})
	if err != nil {
		return "", fmt.Errorf("调用 CreateThread 失败: %w", err)
	}
	if resp.Body == nil || resp.Body.ThreadId == nil || *resp.Body.ThreadId == "" {
		return "", fmt.Errorf("CreateThread 返回了空的 ThreadId")
	}

	threadId := *resp.Body.ThreadId
	log.Printf("[DingTalk] 会话 %s(%s) 新线程创建成功 [employee=%s]: %s", conversationId, senderNick, route.employeeName, threadId)
	b.threadStore.Store(key, threadId)
	return threadId, nil
}

// conciseInstruction 是开启简洁模式时附加到用户消息末尾的指令
const conciseInstruction = "\n\n（请用简洁的回答，控制在几句话以内，尽量拟人的语气。）"

// queryEmployee 向 CMS 数字员工发送消息，返回收集到的回复文本和线程 ID。
// employeeName 为路由解析后的目标员工（可能与 cfg.EmployeeName 不同）。
func (b *Bot) queryEmployee(ctx context.Context, message, threadId, employeeName string) (string, string, error) {
	sopClient, err := b.newSopClient()
	if err != nil {
		return "", "", err
	}
	cms := sopClient.CmsClient

	cfg := b.config()
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
				// 从 Contents 中提取 text 类型的内容
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

// queryEmployeeWithRoute 向 CMS 数字员工发送消息，使用路由级别的 product/project/workspace。
func (b *Bot) queryEmployeeWithRoute(ctx context.Context, message, threadId string, route resolvedRoute) (string, string, error) {
	sopClient, err := b.newSopClient()
	if err != nil {
		return "", "", err
	}
	cms := sopClient.CmsClient

	cfg := b.config()
	if cfg.ConciseReply {
		message += conciseInstruction
	}

	// 使用路由级别的 product/project/workspace/region
	productType := route.product
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
		if route.project != "" {
			variables["project"] = route.project
		}
	} else {
		if route.workspace != "" {
			variables["workspace"] = route.workspace
		}
		if route.region != "" {
			variables["region"] = route.region
		}
		// CMS product: add fromTime/toTime (15-minute window)
		now := time.Now()
		variables["fromTime"] = now.Add(-15 * time.Minute).Unix()
		variables["toTime"] = now.Unix()
	}
	request := &cmsclient.CreateChatRequest{
		DigitalEmployeeName: tea.String(route.employeeName),
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
				// 从 Contents 中提取 text 类型的内容
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
