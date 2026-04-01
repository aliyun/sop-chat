package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"sop-chat/internal/client"
	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/robfig/cron/v3"
)

// Scheduler 管理所有定时任务的生命周期
type Scheduler struct {
	mu        sync.Mutex
	cron      *cron.Cron
	entryIDs  map[string]cron.EntryID // task name -> cron entry id
	globalCfg *config.Config
	tasks     []config.ScheduledTaskConfig
	timezone  *time.Location
}

// New 创建并返回一个新的 Scheduler，使用指定时区
func New(timezone string) *Scheduler {
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		} else {
			log.Printf("[Scheduler] 警告: 无法加载时区 %q，使用 UTC: %v", timezone, err)
		}
	}

	c := cron.New(
		cron.WithLocation(loc),
		cron.WithLogger(cron.PrintfLogger(log.Default())),
	)
	return &Scheduler{
		cron:     c,
		entryIDs: make(map[string]cron.EntryID),
		timezone: loc,
	}
}

// Start 启动调度器并注册所有定时任务
func (s *Scheduler) Start(tasks []config.ScheduledTaskConfig, globalCfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.globalCfg = globalCfg
	s.tasks = tasks
	s.registerAll()
	s.cron.Start()
	log.Printf("[Scheduler] 调度器已启动，共加载 %d 个任务", len(s.entryIDs))
}

// Reload 热重载：停止所有旧任务，注册新任务列表
func (s *Scheduler) Reload(tasks []config.ScheduledTaskConfig, globalCfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 移除所有已注册的任务
	for name, id := range s.entryIDs {
		s.cron.Remove(id)
		log.Printf("[Scheduler] 移除任务: %s", name)
	}
	s.entryIDs = make(map[string]cron.EntryID)

	s.globalCfg = globalCfg
	s.tasks = tasks
	s.registerAll()
	log.Printf("[Scheduler] 调度器热重载完成，共 %d 个任务", len(s.entryIDs))
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.cron.Stop()
	log.Printf("[Scheduler] 调度器已停止")
}

// registerAll 注册所有启用的任务（需在持有锁的情况下调用）
func (s *Scheduler) registerAll() {
	for _, task := range s.tasks {
		if !task.Enabled {
			continue
		}
		if err := s.registerTask(task); err != nil {
			log.Printf("[Scheduler] 注册任务 %q 失败: %v", task.Name, err)
		}
	}
}

// registerTask 注册单个定时任务（需在持有锁的情况下调用）
func (s *Scheduler) registerTask(task config.ScheduledTaskConfig) error {
	if task.Name == "" {
		return fmt.Errorf("任务 name 不能为空")
	}
	if task.Cron == "" {
		return fmt.Errorf("任务 %q 的 cron 表达式为空", task.Name)
	}
	if task.EmployeeName == "" {
		return fmt.Errorf("任务 %q 的 employeeName 为空", task.Name)
	}
	if task.Prompt == "" {
		return fmt.Errorf("任务 %q 的 prompt 为空", task.Name)
	}
	if len(task.EffectiveWebhooks()) == 0 {
		return fmt.Errorf("任务 %q 的 webhook 未配置", task.Name)
	}

	taskCopy := task // 避免闭包捕获循环变量
	id, err := s.cron.AddFunc(task.Cron, func() {
		s.runTask(taskCopy)
	})
	if err != nil {
		return fmt.Errorf("解析 cron 表达式 %q 失败: %w", task.Cron, err)
	}

	s.entryIDs[task.Name] = id
	next := s.cron.Entry(id).Next
	webhooks := task.EffectiveWebhooks()
	whTypes := make([]string, len(webhooks))
	for i, w := range webhooks {
		whTypes[i] = w.Type
	}
	log.Printf("[Scheduler] 注册任务: name=%q cron=%q employee=%q webhooks=%v product=%q project=%q workspace=%q region=%q 问题=%s 下次执行: %s",
		task.Name, task.Cron, task.EmployeeName, whTypes, task.Product, task.Project, task.Workspace, task.Region, promptForLog(task.Prompt, 400), next.In(s.timezone).Format("2006-01-02 15:04:05 MST"))
	return nil
}

// runTask 执行单个定时任务：向数字员工提问，然后发送结果到 Webhook
func (s *Scheduler) runTask(task config.ScheduledTaskConfig) {
	s.mu.Lock()
	globalCfg := s.globalCfg
	s.mu.Unlock()

	globalProduct := ""
	if globalCfg != nil {
		globalProduct = globalCfg.GetLegacyProduct()
	}
	taskProduct := config.ResolveScheduledTaskProduct(task.Product, task.Project, task.Workspace, globalProduct)
	taskProject := task.Project
	taskWorkspace := task.Workspace
	taskRegion := task.Region
	taskCloudAccountID := config.NormalizeCloudAccountID(task.CloudAccountID)

	prompt := task.Prompt
	if task.ConciseReply {
		prompt += "\n\n简化最终输出 适合聊天工具上阅读"
	}
	promptLog := promptForLog(prompt, 1200)

	log.Printf("[Scheduler] ========== 任务触发 ==========")
	log.Printf("[Scheduler] 任务名称: %q product=%q（配置中 product=%q）", task.Name, taskProduct, task.Product)
	log.Printf("[Scheduler] Cron 表达式: %q product=%q", task.Cron, taskProduct)
	log.Printf("[Scheduler] 数字员工: %q product=%q", task.EmployeeName, taskProduct)
	webhooks := task.EffectiveWebhooks()
	for i, wh := range webhooks {
		log.Printf("[Scheduler] Webhook[%d]: type=%s url=%s", i, wh.Type, maskURL(wh.URL))
	}
	log.Printf("[Scheduler] 产品配置: product=%q project=%q workspace=%q region=%q 执行用 product=%q",
		task.Product, task.Project, task.Workspace, task.Region, taskProduct)
	log.Printf("[Scheduler] 云账号配置: cloudAccountId=%q", taskCloudAccountID)
	log.Printf("[Scheduler] 问题: %s", promptLog)

	startTime := time.Now()

	if globalCfg == nil {
		log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 跳过：全局配置未加载", task.Name, taskProduct, promptLog)
		return
	}

	clientCfg, err := globalCfg.ResolveClientConfig(taskCloudAccountID)
	if err != nil || clientCfg == nil || clientCfg.AccessKeyId == "" {
		log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 跳过：cloudAccountId=%q 凭据未配置或无效 (%v)",
			task.Name, taskProduct, promptLog, taskCloudAccountID, err)
		return
	}

	reply, err := queryEmployee(clientCfg, task.Name, task.EmployeeName, prompt, s.timezone, taskProduct, taskProject, taskWorkspace, taskRegion)
	if err != nil {
		log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 查询数字员工失败: %v", task.Name, taskProduct, promptLog, err)
		return
	}

	log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 数字员工响应完成（%d 字）", task.Name, taskProduct, promptLog, len([]rune(reply)))

	if reply == "" {
		log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 数字员工返回空响应，跳过发送", task.Name, taskProduct, promptLog)
		return
	}

	for i, wh := range webhooks {
		log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 开始发送 Webhook[%d] (type=%s)...", task.Name, taskProduct, promptLog, i, wh.Type)
		raw, whErr := sendToWebhook(wh, reply)
		if whErr != nil {
			log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 发送 Webhook[%d] 失败: %v（平台响应: %s）", task.Name, taskProduct, promptLog, i, whErr, raw)
		}
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Printf("[Scheduler] 任务 %q product=%q 问题=%s 执行完成，耗时 %s", task.Name, taskProduct, promptLog, elapsed)
	log.Printf("[Scheduler] ========== 任务结束 product=%q 问题=%s ==========", taskProduct, promptLog)
}

// QueryEmployee 向数字员工发送消息，等待完整响应并返回文本（公开，供外部触发测试使用）
func QueryEmployee(clientCfg *config.ClientConfig, employeeName, message string) (string, error) {
	return queryEmployee(clientCfg, "手动触发", employeeName, message, time.Local, clientCfg.Product, "", "", "")
}

// QueryEmployeeWithVariables 向数字员工发送消息，支持指定 product/project/workspace/region
func QueryEmployeeWithVariables(clientCfg *config.ClientConfig, employeeName, message, product, project, workspace, region string) (string, error) {
	return queryEmployee(clientCfg, "手动触发", employeeName, message, time.Local, product, project, workspace, region)
}

// queryEmployee 向数字员工发送消息，等待完整响应并返回文本
func queryEmployee(clientCfg *config.ClientConfig, taskName, employeeName, message string, loc *time.Location, product, project, workspace, region string) (string, error) {
	msgLog := promptForLog(message, 1200)
	msgShort := promptForLog(message, 300)
	log.Printf("[Scheduler] queryEmployee 开始: task=%q employee=%q product=%q 问题=%s", taskName, employeeName, product, msgLog)

	sopClient, err := client.NewCMSClient(&client.Config{
		CloudAccountID:  clientCfg.CloudAccountID,
		AccessKeyId:     clientCfg.AccessKeyId,
		AccessKeySecret: clientCfg.AccessKeySecret,
		Endpoint:        clientCfg.Endpoint,
	})
	if err != nil {
		log.Printf("[Scheduler] queryEmployee product=%q 问题=%s 创建 CMS 客户端失败: %v", product, msgShort, err)
		return "", fmt.Errorf("创建 CMS 客户端失败: %w", err)
	}
	log.Printf("[Scheduler] queryEmployee product=%q 问题=%s CMS 客户端创建成功", product, msgShort)
	cms := sopClient.CmsClient

	// CMS API 要求必须传有效 ThreadId，先创建一次性线程
	threadTitle := fmt.Sprintf("[定时任务] %s @ %s", taskName, time.Now().In(loc).Format("2006-01-02 15:04:05"))
	log.Printf("[Scheduler] queryEmployee product=%q 问题=%s 创建线程: %s", product, msgShort, threadTitle)
	threadResp, err := sopClient.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: employeeName,
		Title:        threadTitle,
	})
	if err != nil {
		log.Printf("[Scheduler] queryEmployee product=%q 问题=%s 创建线程失败: %v", product, msgShort, err)
		return "", fmt.Errorf("创建线程失败: %w", err)
	}
	if threadResp.Body == nil || threadResp.Body.ThreadId == nil || *threadResp.Body.ThreadId == "" {
		log.Printf("[Scheduler] queryEmployee product=%q 问题=%s CreateThread 返回了空的 ThreadId", product, msgShort)
		return "", fmt.Errorf("CreateThread 返回了空的 ThreadId")
	}
	threadId := *threadResp.Body.ThreadId
	log.Printf("[Scheduler] queryEmployee product=%q 问题=%s 线程创建成功: threadId=%s", product, msgShort, threadId)
	nowTS := time.Now().Unix()
	variables := map[string]interface{}{
		"timeStamp": fmt.Sprintf("%d", nowTS),
		"timeZone":  "Asia/Shanghai",
		"language":  "zh",
	}
	// 根据 product 配置决定是否附加 skill=sop
	if config.IsSlsProduct(product) {
		variables["skill"] = "sop"
	}
	// 添加 project/workspace/region 到变量（与 IsSlsProduct 一致，避免空 product 时混用 SLS skill 与 CMS 变量）
	if config.IsSlsProduct(product) {
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

	startSSE := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	log.Printf("[Scheduler] queryEmployee 开始 SSE 流式请求: employee=%q threadId=%s product=%q 问题=%s timeout=30m", employeeName, threadId, product, msgLog)

	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	runtime := sopchat.NewSSERuntimeOptions()
	go cms.CreateChatWithSSECtx(ctx, request, make(map[string]*string), runtime, responseChan, errorChan)

	var textParts []string
	responseCount, msgCount := 0, 0
	lastProgressLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Scheduler] queryEmployee product=%q 问题=%s 超时，已收 %d 响应 %d 消息", product, msgShort, responseCount, msgCount)
			return strings.Join(textParts, ""), ctx.Err()

		case response, ok := <-responseChan:
			if !ok {
				result := strings.Join(textParts, "")
				log.Printf("[Scheduler] queryEmployee 完成: employee=%q threadId=%s product=%q 问题=%s 耗时 %s 共 %d 帧 文本 %d 字",
					employeeName, threadId, product, msgShort, time.Since(startSSE).Round(time.Millisecond), responseCount, len([]rune(result)))
				return result, nil
			}
			responseCount++

			// 每 30 秒打印一次进度
			if time.Since(lastProgressLog) > 30*time.Second {
				log.Printf("[Scheduler] queryEmployee 进行中: employee=%q product=%q 问题=%s 已收 %d 帧 %d 消息 耗时 %s",
					employeeName, product, msgShort, responseCount, msgCount, time.Since(startSSE).Round(time.Second))
				lastProgressLog = time.Now()
			}

			if response.StatusCode != nil && *response.StatusCode != 200 {
				log.Printf("[Scheduler] queryEmployee product=%q 问题=%s 响应状态码异常: %d", product, msgShort, *response.StatusCode)
			}
			if response.Body == nil {
				continue
			}

			// 检测 done 消息
			if sopchat.IsDoneMessage(response.Body) {
				result := strings.Join(textParts, "")
				log.Printf("[Scheduler] queryEmployee 完成: employee=%q threadId=%s product=%q 问题=%s 耗时 %s 共 %d 帧 文本 %d 字",
					employeeName, threadId, product, msgShort, time.Since(startSSE).Round(time.Millisecond), responseCount, len([]rune(result)))
				return result, nil
			}

			for _, msg := range response.Body.Messages {
				if msg == nil {
					continue
				}
				msgCount++
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
			if !ok {
				result := strings.Join(textParts, "")
				log.Printf("[Scheduler] queryEmployee 完成: employee=%q threadId=%s product=%q 问题=%s 耗时 %s 共 %d 帧 文本 %d 字",
					employeeName, threadId, product, msgShort, time.Since(startSSE).Round(time.Millisecond), responseCount, len([]rune(result)))
				return result, nil
			}
			if err != nil {
				log.Printf("[Scheduler] queryEmployee product=%q 问题=%s SSE 错误: %v", product, msgShort, err)
				return strings.Join(textParts, ""), err
			}
			result := strings.Join(textParts, "")
			log.Printf("[Scheduler] queryEmployee 完成: employee=%q threadId=%s product=%q 问题=%s 耗时 %s 共 %d 帧 文本 %d 字",
				employeeName, threadId, product, msgShort, time.Since(startSSE).Round(time.Millisecond), responseCount, len([]rune(result)))
			return result, nil
		}
	}
}

// maskURL 遮蔽 URL 中的敏感信息，只显示域名和路径的前后部分
func maskURL(url string) string {
	if len(url) < 30 {
		return url
	}
	// 保留前 20 个字符和后 10 个字符
	return url[:20] + "..." + url[len(url)-10:]
}

// promptForLog 将发给数字员工的问题压成单行并截断，便于日志检索（避免换行撑爆日志）。
func promptForLog(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(strings.TrimSpace(s))
	if len(r) <= maxRunes {
		return string(r)
	}
	total := len(r)
	return string(r[:maxRunes]) + fmt.Sprintf("…(共%d字)", total)
}

// PromptForLog 与定时任务日志使用相同截断规则，供 API 层打印「问题」字段。
func PromptForLog(s string, maxRunes int) string {
	return promptForLog(s, maxRunes)
}
