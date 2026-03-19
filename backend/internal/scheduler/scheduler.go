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
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/robfig/cron/v3"
)

// Scheduler 管理所有定时任务的生命周期
type Scheduler struct {
	mu        sync.Mutex
	cron      *cron.Cron
	entryIDs  map[string]cron.EntryID // task name -> cron entry id
	clientCfg *config.ClientConfig
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
func (s *Scheduler) Start(tasks []config.ScheduledTaskConfig, clientCfg *config.ClientConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clientCfg = clientCfg
	s.tasks = tasks
	s.registerAll()
	s.cron.Start()
	log.Printf("[Scheduler] 调度器已启动，共加载 %d 个任务", len(s.entryIDs))
}

// Reload 热重载：停止所有旧任务，注册新任务列表
func (s *Scheduler) Reload(tasks []config.ScheduledTaskConfig, clientCfg *config.ClientConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 移除所有已注册的任务
	for name, id := range s.entryIDs {
		s.cron.Remove(id)
		log.Printf("[Scheduler] 移除任务: %s", name)
	}
	s.entryIDs = make(map[string]cron.EntryID)

	s.clientCfg = clientCfg
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
	if task.Webhook.URL == "" {
		return fmt.Errorf("任务 %q 的 webhook.url 为空", task.Name)
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
	log.Printf("[Scheduler] 注册任务: name=%q cron=%q employee=%q webhook=%s product=%q project=%q workspace=%q region=%q 下次执行: %s",
		task.Name, task.Cron, task.EmployeeName, task.Webhook.Type, task.Product, task.Project, task.Workspace, task.Region, next.In(s.timezone).Format("2006-01-02 15:04:05 MST"))
	return nil
}

// runTask 执行单个定时任务：向数字员工提问，然后发送结果到 Webhook
func (s *Scheduler) runTask(task config.ScheduledTaskConfig) {
	s.mu.Lock()
	clientCfg := s.clientCfg
	s.mu.Unlock()

	log.Printf("[Scheduler] 开始执行任务: %q  employee=%q  webhook=%s  product=%q  project=%q  workspace=%q  region=%q",
		task.Name, task.EmployeeName, task.Webhook.Type, task.Product, task.Project, task.Workspace, task.Region)
	prompt := task.Prompt
	if task.ConciseReply {
		prompt += "\n\n简化最终输出 适合聊天工具上阅读"
	}
	log.Printf("[Scheduler] 任务 %q Prompt: %s", task.Name, prompt)
	startTime := time.Now()

	if clientCfg == nil || clientCfg.AccessKeyId == "" {
		log.Printf("[Scheduler] 任务 %q 跳过：CMS 凭据未配置", task.Name)
		return
	}

	// 确定任务使用的 product/project/workspace/region
	taskProduct := task.Product
	if taskProduct == "" {
		taskProduct = clientCfg.Product // 使用全局配置
	}
	taskProject := task.Project
	taskWorkspace := task.Workspace
	taskRegion := task.Region

	reply, err := queryEmployee(clientCfg, task.Name, task.EmployeeName, prompt, s.timezone, taskProduct, taskProject, taskWorkspace, taskRegion)
	if err != nil {
		log.Printf("[Scheduler] 任务 %q 查询数字员工失败: %v", task.Name, err)
		return
	}

	log.Printf("[Scheduler] 任务 %q 数字员工原始响应（%d 字）: %s",
		task.Name, len([]rune(reply)), reply)

	if reply == "" {
		log.Printf("[Scheduler] 任务 %q 数字员工返回空响应，跳过发送", task.Name)
		return
	}

	raw, err := sendToWebhook(task.Webhook, reply)
	if err != nil {
		log.Printf("[Scheduler] 任务 %q 发送 webhook 失败: %v（平台响应: %s）", task.Name, err, raw)
		return
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Printf("[Scheduler] 任务 %q 执行完成，耗时 %s，webhook 平台响应: %s", task.Name, elapsed, raw)
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
	sopClient, err := client.NewCMSClient(&client.Config{
		AccessKeyId:     clientCfg.AccessKeyId,
		AccessKeySecret: clientCfg.AccessKeySecret,
		Endpoint:        clientCfg.Endpoint,
	})
	if err != nil {
		return "", fmt.Errorf("创建 CMS 客户端失败: %w", err)
	}
	cms := sopClient.CmsClient

	// CMS API 要求必须传有效 ThreadId，先创建一次性线程
	threadTitle := fmt.Sprintf("[定时任务] %s @ %s", taskName, time.Now().In(loc).Format("2006-01-02 15:04:05"))
	threadResp, err := sopClient.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: employeeName,
		Title:        threadTitle,
	})
	if err != nil {
		return "", fmt.Errorf("创建线程失败: %w", err)
	}
	if threadResp.Body == nil || threadResp.Body.ThreadId == nil || *threadResp.Body.ThreadId == "" {
		return "", fmt.Errorf("CreateThread 返回了空的 ThreadId")
	}
	threadId := *threadResp.Body.ThreadId
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
	// 添加 project/workspace/region 到变量
	if product == "sls" {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	responseChan := make(chan *cmsclient.CreateChatResponse)
	errorChan := make(chan error)

	go cms.CreateChatWithSSE(request, make(map[string]*string), &dara.RuntimeOptions{}, responseChan, errorChan)

	var textParts []string
	responseCount, msgCount := 0, 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Scheduler] queryEmployee 超时，已收 %d 响应 %d 消息", responseCount, msgCount)
			return strings.Join(textParts, ""), ctx.Err()

		case response, ok := <-responseChan:
			if !ok {
				result := strings.Join(textParts, "")
				log.Printf("[Scheduler] queryEmployee 完成: employee=%q threadId=%s prompt=%q 耗时 %s 共 %d 帧 文本 %d 字",
					employeeName, threadId, message, time.Since(startSSE).Round(time.Millisecond), responseCount, len([]rune(result)))
				return result, nil
			}
			responseCount++

			if response.StatusCode != nil && *response.StatusCode != 200 {
				log.Printf("[Scheduler] queryEmployee 响应状态码异常: %d", *response.StatusCode)
			}
			if response.Body == nil {
				continue
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
				log.Printf("[Scheduler] queryEmployee 完成: employee=%q threadId=%s prompt=%q 耗时 %s 共 %d 帧 文本 %d 字",
					employeeName, threadId, message, time.Since(startSSE).Round(time.Millisecond), responseCount, len([]rune(result)))
				return result, nil
			}
			if err != nil {
				log.Printf("[Scheduler] queryEmployee SSE error: %v", err)
				return strings.Join(textParts, ""), err
			}
			result := strings.Join(textParts, "")
			log.Printf("[Scheduler] queryEmployee 完成: employee=%q threadId=%s prompt=%q 耗时 %s 共 %d 帧 文本 %d 字",
				employeeName, threadId, message, time.Since(startSSE).Round(time.Millisecond), responseCount, len([]rune(result)))
			return result, nil
		}
	}
}
