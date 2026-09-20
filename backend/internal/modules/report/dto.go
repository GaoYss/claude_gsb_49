package report

import "streetlight/pkg/pagination"

// CreateRequest 市民报修提交请求, 灯杆编号与位置描述至少填写一项。
type CreateRequest struct {
	LampCode      string `json:"lamp_code" binding:"omitempty,max=64"`
	LocationDesc  string `json:"location_desc" binding:"omitempty,max=255"`
	FaultType     string `json:"fault_type" binding:"omitempty,max=32"`
	Description   string `json:"description" binding:"required,max=512"`
	Reporter      string `json:"reporter" binding:"omitempty,max=64"`
	ReporterPhone string `json:"reporter_phone" binding:"omitempty,max=32"`
	ReportedAt    string `json:"reported_at" binding:"omitempty,max=32"`
}

// ConfirmRequest 核实通过请求, 将报修转为正式故障。
type ConfirmRequest struct {
	LampID     uint   `json:"lamp_id"` // 报修未关联路灯时必填
	FaultType  string `json:"fault_type" binding:"omitempty,max=32"`
	FaultLevel string `json:"fault_level" binding:"omitempty,oneof=low normal high urgent"`
	Remark     string `json:"remark" binding:"omitempty,max=255"`
	Operator   string `json:"operator" binding:"omitempty,max=64"`
}

// RejectRequest 核实无效请求, 必须说明原因。
type RejectRequest struct {
	Reason   string `json:"reason" binding:"required,max=255"`
	Operator string `json:"operator" binding:"omitempty,max=64"`
}

// ListQuery 报修列表查询条件。
type ListQuery struct {
	pagination.Params
	Keyword   string `form:"keyword"` // 报修单号 / 灯杆编号 / 位置 / 报修人 / 描述
	Status    string `form:"status"`
	RoadName  string `form:"road_name"`
	StartDate string `form:"start_date"` // 上报日期起, 格式 YYYY-MM-DD
	EndDate   string `form:"end_date"`   // 上报日期止, 格式 YYYY-MM-DD
}

// Meta 报修模块字典与队列统计, 供前端渲染筛选与队列头部。
type Meta struct {
	Statuses         []string         `json:"statuses"`
	MergeWindowHours float64          `json:"merge_window_hours"`
	StatusCounts     map[string]int64 `json:"status_counts"`
	TodayCount       int64            `json:"today_count"`
}

// DetailResponse 报修详情: 主单 + 被合并的重复上报 + 合并目标(若本单被合并)。
type DetailResponse struct {
	Report      *Report  `json:"report"`
	MergedItems []Report `json:"merged_items"`
	MergedInto  *Report  `json:"merged_into,omitempty"`
}
