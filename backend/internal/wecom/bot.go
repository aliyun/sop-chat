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
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	"sop-chat/internal/config"
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
	cfgMu     sync.RWMutex
	wcConfig  *config.WeComConfig
	cmsConfig *config.ClientConfig

	cryptor    *WXBizMsgCrypt
	msgManager *MessageManager

	// 会话 -> 线程 ID 的映射
	threadStore sync.Map

	// key -> chan func()，每个 key 对应一个串行 worker
	workerQueues sync.Map
}

// NewBot 创建企业微信机器人实例
func NewBot(wcConfig *config.WeComConfig, cmsConfig *config.ClientConfig) (*Bot, error) {
	cryptor, err := NewWXBizMsgCrypt(wcConfig.Token, wcConfig.EncodingAESKey, wcConfig.CorpID)
	if err != nil {
		return nil, fmt.Errorf("初始化消息加解密失败: %w", err)
	}

	msgManager := NewMessageManager(wcConfig.CorpID, wcConfig.Secret, wcConfig.AgentID)

	return &Bot{
		wcConfig:   wcConfig,
		cmsConfig:  cmsConfig,
		cryptor:    cryptor,
		msgManager: msgManager,
	}, nil
}

// Config 返回当前机器人的配置快照
func (b *Bot) Config() *config.WeComConfig {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.wcConfig
}

// UpdateConfig 热更新运行时配置（不更新加解密实例，凭据变化需重建）
func (b *Bot) UpdateConfig(newCfg *config.WeComConfig) {
	b.cfgMu.Lock()
	defer b.cfgMu.Unlock()
	b.wcConfig = newCfg
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

	key := threadKey(msg.FromUserName, cfg.EmployeeName)

	work := func() {
		workCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// 立即发送"思考中"提示，让用户知道消息已收到
		_, _ = b.msgManager.SendTextToUser(workCtx, msg.FromUserName, "💭 思考中...")

		threadId, err := b.getOrCreateThreadId(msg.FromUserName, cfg.EmployeeName)
		if err != nil {
			log.Printf("[WeCom] 创建线程失败: %v", err)
			_, _ = b.msgManager.SendTextToUser(workCtx, msg.FromUserName, "❌ 创建会话失败，请稍后重试")
			return
		}

		log.Printf("[WeCom] 调用数字员工 employee=%s threadId=%s ...", cfg.EmployeeName, threadId)
		replyText, newThreadId, err := b.queryEmployee(workCtx, userMessage, threadId, cfg.EmployeeName)
		if err != nil {
			log.Printf("[WeCom] 调用数字员工失败: %v", err)
			_, _ = b.msgManager.SendTextToUser(workCtx, msg.FromUserName, "❌ "+err.Error())
			return
		}

		if newThreadId != "" && newThreadId != threadId {
			b.threadStore.Store(key, newThreadId)
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

	if !b.enqueueWork(key, work) {
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

// getOrCreateThreadId 查找或新建该用户对应的 CMS 线程 ID
func (b *Bot) getOrCreateThreadId(userID, employeeName string) (string, error) {
	key := threadKey(userID, employeeName)

	if v, ok := b.threadStore.Load(key); ok {
		return v.(string), nil
	}

	client, err := b.newSopClient()
	if err != nil {
		return "", err
	}

	workspace := fmt.Sprintf("wecom:%s:%s", userID, employeeName)

	listResp, listErr := client.ListThreads(employeeName, []sopchat.ThreadFilter{
		{Key: "workspace", Value: workspace},
	})
	if listErr != nil {
		log.Printf("[WeCom] 列出线程失败（将尝试新建）: %v", listErr)
	} else if listResp.Body != nil {
		for _, t := range listResp.Body.Threads {
			if t == nil || t.ThreadId == nil || *t.ThreadId == "" {
				continue
			}
			if t.Variables != nil && t.Variables.Workspace != nil &&
				*t.Variables.Workspace == workspace {
				threadId := *t.ThreadId
				log.Printf("[WeCom] 找到已有线程 [employee=%s]: %s", employeeName, threadId)
				b.threadStore.Store(key, threadId)
				return threadId, nil
			}
		}
	}

	log.Printf("[WeCom] 为用户 %s 创建新线程 [employee=%s] ...", userID, employeeName)
	resp, err := client.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: employeeName,
		Title:        "WeCom: " + userID,
		Attributes:   map[string]interface{}{"workspace": workspace},
	})
	if err != nil {
		return "", fmt.Errorf("调用 CreateThread 失败: %w", err)
	}
	if resp.Body == nil || resp.Body.ThreadId == nil || *resp.Body.ThreadId == "" {
		return "", fmt.Errorf("CreateThread 返回了空的 ThreadId")
	}

	threadId := *resp.Body.ThreadId
	log.Printf("[WeCom] 新线程创建成功 [employee=%s]: %s", employeeName, threadId)
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

	nowTS := time.Now().Unix()
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
		Variables: map[string]interface{}{
			"skill":     "sop",
			"timeStamp": fmt.Sprintf("%d", nowTS),
			"timeZone":  "Asia/Shanghai",
			"language":  "zh",
		},
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
