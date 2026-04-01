package wecom

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"

	"sop-chat/internal/config"
	"sop-chat/internal/session"
	"sop-chat/pkg/sopchat"
)

// workerQueueSize 每个串行队列允许积压的最大消息数
const workerQueueSize = 8

// ReceivedMessage 企业微信接收的消息结构
type ReceivedMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgID        string   `xml:"MsgId"`
	AgentID      int      `xml:"AgentID"`
}

// Bot 封装企业微信机器人及其与 CMS 的对接逻辑
type Bot struct {
	cfgMu        sync.RWMutex
	wcConfig     *config.WeComConfig
	cmsConfig    *config.ClientConfig
	globalConfig *config.Config

	cryptor    *WXBizMsgCrypt
	msgManager *MessageManager

	// 会话 -> 线程 ID 的映射
	threads *session.ThreadStore

	// key -> chan func()，每个 key 对应一个串行 worker
	workerQueues sync.Map
}

// NewBot 创建企业微信机器人实例
func NewBot(wcConfig *config.WeComConfig, cmsConfig *config.ClientConfig, globalConfig *config.Config) (*Bot, error) {
	cryptor, err := NewWXBizMsgCrypt(wcConfig.Token, wcConfig.EncodingAESKey, wcConfig.CorpID)
	if err != nil {
		return nil, fmt.Errorf("初始化消息加解密失败: %w", err)
	}

	msgManager := NewMessageManager(wcConfig.CorpID, wcConfig.Secret, wcConfig.AgentID)

	return &Bot{
		wcConfig:     wcConfig,
		cmsConfig:    cmsConfig,
		globalConfig: globalConfig,
		cryptor:      cryptor,
		msgManager:   msgManager,
		threads:      session.NewThreadStore("[WeCom]"),
	}, nil
}

// Config 返回当前机器人的配置快照
func (b *Bot) Config() *config.WeComConfig {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.wcConfig
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

// UpdateConfig 热更新运行时配置（不更新加解密实例，凭据变化需重建）
func (b *Bot) UpdateConfig(newCfg *config.WeComConfig, globalConfig *config.Config) {
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	b.wcConfig = newCfg
	b.globalConfig = globalConfig
	log.Printf("[WeCom] 配置已热更新: corpId=%s employee=%s", newCfg.CorpID, newCfg.EmployeeName)
}

// HandleCallback 处理企业微信回调请求（可直接挂载到 Gin 或 net/http）
func (b *Bot) HandleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	msgSignature := query.Get("msg_signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")

	if r.Method == http.MethodGet {
		// URL 验证
		echoStr := query.Get("echostr")
		plaintext, err := b.cryptor.VerifyURL(msgSignature, timestamp, nonce, echoStr)
		if err != nil {
			log.Printf("[WeCom] URL 验证失败: %v", err)
			http.Error(w, "验证失败", http.StatusForbidden)
			return
		}
		log.Printf("[WeCom] URL 验证成功")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(plaintext))
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[WeCom] 读取请求体失败: %v", err)
		http.Error(w, "读取请求失败", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	plaintext, err := b.cryptor.DecryptMsg(msgSignature, timestamp, nonce, body)
	if err != nil {
		log.Printf("[WeCom] 解密消息失败: %v", err)
		http.Error(w, "解密失败", http.StatusForbidden)
		return
	}

	var msg ReceivedMessage
	if err := xml.Unmarshal(plaintext, &msg); err != nil {
		log.Printf("[WeCom] 解析消息失败: %v", err)
		http.Error(w, "解析消息失败", http.StatusBadRequest)
		return
	}

	log.Printf("[WeCom] 收到消息 from=%s type=%s msgId=%s", msg.FromUserName, msg.MsgType, msg.MsgID)

	// 立即返回 success，防止企业微信重试
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("success"))

	if msg.MsgType != "text" {
		log.Printf("[WeCom] 不支持的消息类型: %s", msg.MsgType)
		return
	}

	userMessage := strings.TrimSpace(msg.Content)
	if userMessage == "" {
		return
	}

	cfg := b.Config()

	// 用户白名单校验
	if !b.isUserAllowed(msg.FromUserName) {
		log.Printf("[WeCom] 用户 %s 不在白名单中，已拒绝", msg.FromUserName)
		return
	}

	target := b.resolveTarget(userMessage)
	key := threadKey(msg.FromUserName, target.employeeName)

	// worker queue key 不含 variable，保证同一用户的消息串行处理
	queueKey := key

	work := func() {
		workCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// 立即发送"思考中"提示，让用户知道消息已收到
		_, _ = b.msgManager.SendTextToUser(workCtx, msg.FromUserName, "💭 思考中...")

		threadId, err := b.getOrCreateThreadId(msg.FromUserName, target)
		if err != nil {
			log.Printf("[WeCom] 创建线程失败: %v", err)
			_, _ = b.msgManager.SendTextToUser(workCtx, msg.FromUserName, "❌ 创建会话失败，请稍后重试")
			return
		}

		log.Printf("[WeCom] 调用数字员工 cloudAccountId=%s employee=%s threadId=%s ...", target.cloudAccountID, target.employeeName, threadId)
		replyText, newThreadId, err := b.queryEmployee(workCtx, userMessage, threadId, target)
		if err != nil {
			log.Printf("[WeCom] 调用数字员工失败: %v", err)
			_, _ = b.msgManager.SendTextToUser(workCtx, msg.FromUserName, "❌ "+err.Error())
			return
		}

		if newThreadId != "" && newThreadId != threadId {
			scope := threadScope(target.cloudAccountID, target.project, target.workspace, target.region)
			cacheKey := threadKey(msg.FromUserName, target.employeeName) + "\x00" + scope
			b.threads.Store(cacheKey, newThreadId)
		}

		log.Printf("[WeCom] 回复消息 to=%s 长度=%d", msg.FromUserName, len(replyText))
		// 企业微信 markdown 有长度限制，超长时降级为文本
		wecomMd := ConvertMarkdownToWecom(replyText)
		if _, err := b.msgManager.SendMarkdownToUser(workCtx, msg.FromUserName, wecomMd); err != nil {
			log.Printf("[WeCom] 发送 Markdown 失败，降级为文本: %v", err)
			_, _ = b.msgManager.SendTextToUser(workCtx, msg.FromUserName, replyText)
		}

		// 如果配置了群机器人 Webhook，同步推送到群聊
		if webhookURL := cfg.WebhookURL; webhookURL != "" {
			if err := SendWebhookMarkdown(workCtx, webhookURL, wecomMd); err != nil {
				log.Printf("[WeCom] Webhook 发送 Markdown 失败，降级为文本: %v", err)
				if textErr := SendWebhookText(workCtx, webhookURL, replyText); textErr != nil {
					log.Printf("[WeCom] Webhook 文本降级也失败: %v", textErr)
				}
			} else {
				log.Printf("[WeCom] Webhook 推送成功 to=%s", webhookURL)
			}
		}
	}

	if !b.enqueueWork(queueKey, work) {
		log.Printf("[WeCom] 队列已满，拒绝消息 from=%s", msg.FromUserName)
		_, _ = b.msgManager.SendTextToUser(context.Background(), msg.FromUserName, "⚠️ 消息处理中，请稍后再发。")
	}
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

// isUserAllowed 检查用户是否在白名单内
func (b *Bot) isUserAllowed(userID string) bool {
	cfg := b.Config()
	if len(cfg.AllowedUsers) == 0 {
		return true
	}
	for _, u := range cfg.AllowedUsers {
		if u == userID {
			return true
		}
	}
	return false
}

// threadKey 生成线程映射 key
func threadKey(userID, employeeName string) string {
	return userID + "\x00" + employeeName
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
			log.Printf("[WeCom] 消息 %q 命中多个 cloudAccountId=%v，继续使用默认账号 %q", promptForRouteLog(message), ambiguous, target.cloudAccountID)
		}
		if matched {
			if clientCfg, err := globalCfg.ResolveClientConfig(accountID); err == nil {
				target.cloudAccountID = clientCfg.CloudAccountID
				target.clientConfig = clientCfg
			} else {
				log.Printf("[WeCom] cloudAccountId=%q 解析失败，继续使用默认账号 %q: %v", accountID, target.cloudAccountID, err)
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
	return session.ThreadVariable(b.wcConfig.Product, b.cmsConfig.Product, b.wcConfig.Project, b.wcConfig.Workspace, b.wcConfig.Region)
}

func threadVariableForTarget(target resolvedTarget) (project, workspace, region string) {
	if config.IsSlsProduct(target.product) {
		return target.project, "", ""
	}
	return "", target.workspace, target.region
}

// getOrCreateThreadId 查找或新建该用户对应的 CMS 线程 ID
func (b *Bot) getOrCreateThreadId(userID string, target resolvedTarget) (string, error) {
	project, workspace, region := threadVariableForTarget(target)
	scope := threadScope(target.cloudAccountID, project, workspace, region)

	// 缓存 key 包含订阅和变量，确保 cloudAccountId / project / workspace / region 变更后使用新的 thread
	key := threadKey(userID, target.employeeName) + "\x00" + scope

	client, err := b.newSopClientWithConfig(target.clientConfig)
	if err != nil {
		return "", err
	}

	return b.threads.GetOrCreate(client, session.ThreadParams{
		CacheKey:     key,
		SessionRaw:   "wecom\x00" + userID + "\x00" + target.employeeName + "\x00" + scope,
		EmployeeName: target.employeeName,
		Title:        "WeCom: " + userID,
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
