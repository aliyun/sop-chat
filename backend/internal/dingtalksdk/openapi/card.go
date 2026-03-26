package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// CardData 表示卡片的动态数据。
type CardData struct {
	CardParamMap map[string]string `json:"cardParamMap"`
}

// CreateAndDeliverCardRequest 创建并投放卡片的请求参数。
type CreateAndDeliverCardRequest struct {
	CardTemplateId          string                       `json:"cardTemplateId"`
	OutTrackId              string                       `json:"outTrackId"`
	CallbackType            string                       `json:"callbackType,omitempty"`
	CardData                *CardData                    `json:"cardData,omitempty"`
	OpenSpaceId             string                       `json:"openSpaceId"`
	ImGroupOpenSpaceModel   *ImGroupOpenSpaceModel       `json:"imGroupOpenSpaceModel,omitempty"`
	ImGroupOpenDeliverModel *ImGroupOpenDeliverModel      `json:"imGroupOpenDeliverModel,omitempty"`
	ImRobotOpenSpaceModel   *ImRobotOpenSpaceModel       `json:"imRobotOpenSpaceModel,omitempty"`
	ImRobotOpenDeliverModel *ImRobotOpenDeliverModel      `json:"imRobotOpenDeliverModel,omitempty"`
}

// ImGroupOpenSpaceModel 群聊场景模型。
type ImGroupOpenSpaceModel struct {
	SupportForward bool                              `json:"supportForward"`
	Notification   *ImGroupOpenSpaceModelNotification `json:"notification,omitempty"`
}

// ImGroupOpenSpaceModelNotification 群聊通知配置。
type ImGroupOpenSpaceModelNotification struct {
	AlertContent    string `json:"alertContent"`
	NotificationOff bool   `json:"notificationOff"`
}

// ImGroupOpenDeliverModel 群聊投放模型。
type ImGroupOpenDeliverModel struct {
	RobotCode string `json:"robotCode"`
}

// ImRobotOpenSpaceModel 单聊场景模型。
type ImRobotOpenSpaceModel struct {
	SupportForward bool                              `json:"supportForward"`
	Notification   *ImRobotOpenSpaceModelNotification `json:"notification,omitempty"`
}

// ImRobotOpenSpaceModelNotification 单聊通知配置。
type ImRobotOpenSpaceModelNotification struct {
	AlertContent    string `json:"alertContent"`
	NotificationOff bool   `json:"notificationOff"`
}

// ImRobotOpenDeliverModel 单聊机器人投放模型。
type ImRobotOpenDeliverModel struct {
	SpaceType string `json:"spaceType"`
	RobotCode string `json:"robotCode"`
}

// DeliverResult 单个投放场域的投递结果。
type DeliverResult struct {
	SpaceId   string `json:"spaceId"`
	SpaceType string `json:"spaceType"`
	Success   bool   `json:"success"`
	ErrorMsg  string `json:"errorMsg"`
}

// CreateAndDeliverCardResponse 创建并投放卡片的响应。
type CreateAndDeliverCardResponse struct {
	OutTrackId     string          `json:"outTrackId"`
	DeliverResults []DeliverResult `json:"deliverResults"`
}

// CreateAndDeliverCard 创建并投放一张互动卡片。
// 除了检查 HTTP 层错误外，还会检查 deliverResults 中每个投递是否成功。
func (c *Client) CreateAndDeliverCard(ctx context.Context, req *CreateAndDeliverCardRequest) (*CreateAndDeliverCardResponse, error) {
	reqJSON, _ := json.Marshal(req)
	log.Printf("[OpenAPI] createAndDeliver 请求: %s", string(reqJSON))

	respBody, err := c.DoAPI(ctx, http.MethodPost, "/v1.0/card/instances/createAndDeliver", req)
	if err != nil {
		return nil, fmt.Errorf("创建卡片失败: %w", err)
	}

	log.Printf("[OpenAPI] createAndDeliver 响应: %s", string(respBody))

	var result struct {
		Success bool                          `json:"success"`
		Result  *CreateAndDeliverCardResponse `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析创建卡片响应失败: %w", err)
	}

	resp := result.Result
	if resp == nil {
		resp = &CreateAndDeliverCardResponse{OutTrackId: req.OutTrackId}
	}

	// 检查 deliverResults：只要有一个投递成功即视为整体成功
	anySuccess := false
	var firstErr string
	for _, dr := range resp.DeliverResults {
		if dr.Success {
			anySuccess = true
			break
		}
		if firstErr == "" && dr.ErrorMsg != "" {
			firstErr = fmt.Sprintf("spaceId=%s: %s", dr.SpaceId, dr.ErrorMsg)
		}
	}
	if len(resp.DeliverResults) > 0 && !anySuccess {
		return resp, fmt.Errorf("卡片投递失败: %s", firstErr)
	}

	return resp, nil
}

// UpdateCardRequest 更新卡片数据的请求参数。
type UpdateCardRequest struct {
	OutTrackId string    `json:"outTrackId"`
	CardData   *CardData `json:"cardData"`
}

// UpdateCard 更新已投放卡片的数据。
func (c *Client) UpdateCard(ctx context.Context, req *UpdateCardRequest) error {
	_, err := c.DoAPI(ctx, http.MethodPut, "/v1.0/card/instances", req)
	if err != nil {
		return fmt.Errorf("更新卡片失败: %w", err)
	}
	return nil
}

// StreamingUpdateRequest AI 卡片流式更新请求参数。
type StreamingUpdateRequest struct {
	OutTrackId string `json:"outTrackId"`           // 外部卡片实例 ID
	GUID       string `json:"guid"`                 // 幂等标识
	Key        string `json:"key"`                  // 流式更新的变量名
	Content    string `json:"content"`              // 本次更新的内容
	IsFull     bool   `json:"isFull"`               // 是否全量更新（markdown 必须为 true）
	IsFinalize bool   `json:"isFinalize,omitempty"` // 是否最后一帧（完成状态）
	IsError    bool   `json:"isError,omitempty"`    // 是否出错（失败状态）
}

// StreamingUpdate 调用 AI 卡片流式更新接口，实现打字机效果。
func (c *Client) StreamingUpdate(ctx context.Context, req *StreamingUpdateRequest) error {
	respBody, err := c.DoAPI(ctx, http.MethodPut, "/v1.0/card/streaming", req)
	if err != nil {
		return fmt.Errorf("AI 卡片流式更新失败: %w", err)
	}
	log.Printf("[OpenAPI] streaming 响应: %s", string(respBody))
	return nil
}
