package report

import (
	"streetlight/pkg/pagination"
)

// CreateRequest 市民报修提交请求。
//
// 市民可以填写灯杆标识上的编号(lamp_code), 也可以只描述位置(location_desc + road_name),
// 二者至少提供其一。
type CreateRequest struct {
	LampCode      string `json:"lamp_code" binding:"max=64"`
	RoadName      string `json:"road_name" binding:"max=128"`
	LocationDesc  string `json:"location_desc" binding:"max=255"`
	FaultType     string `json:"fault_type" binding:"required,max=32"`
	Content       string `json:"content" binding:"required,max=512"`
	Reporter      string `json:"reporter" binding:"max=64"`
	ReporterPhone string `json:"reporter_phone" binding:"max=32"`
	ReportedAt    string `json:"reported_at" binding:"omitempty,max=32"`
}

// VerifyRequest 核实有效请求: 将报修单转为正式故障。
//
// 核实人可对故障类型、等级做专业修正; 报修单的原始描述始终原样保留,
// 正式故障的描述默认取原始描述, 也允许在核实备注中补充。
type VerifyRequest struct {
	LampID       uint   `json:"lamp_id" binding:"required"`
	FaultType    string `json:"fault_type" binding:"omitempty,max=32"`
	FaultLevel   string `json:"fault_level" binding:"omitempty,oneof=low normal high urgent"`
	VerifyRemark string `json:"verify_remark" binding:"max=255"`
	VerifiedBy   string `json:"verified_by" binding:"max=64"`
}

// InvalidRequest 核实无效请求, 必须说明无效原因。
type InvalidRequest struct {
	Reason     string `json:"reason" binding:"required,max=255"`
	VerifiedBy string `json:"verified_by" binding:"max=64"`
}

// ListQuery 报修队列查询条件。
type ListQuery struct {
	pagination.Params
	Keyword   string `form:"keyword"` // 报修单号 / 灯杆编号 / 道路 / 描述 / 上报人
	Status    string `form:"status"`
	FaultType string `form:"fault_type"`
	RoadName  string `form:"road_name"`
	LampID    uint   `form:"lamp_id"`
	FaultID   uint   `form:"fault_id"`
	StartDate string `form:"start_date"` // 上报日期起, 格式 YYYY-MM-DD
	EndDate   string `form:"end_date"`   // 上报日期止, 格式 YYYY-MM-DD
}

// QueueSummary 报修队列汇总, 用于页面顶部计数。
type QueueSummary struct {
	Total          int64 `json:"total"`
	PendingTotal   int64 `json:"pending_total"`
	ConfirmedTotal int64 `json:"confirmed_total"`
	InvalidTotal   int64 `json:"invalid_total"`
	MergedTotal    int64 `json:"merged_total"`
	TodayNew       int64 `json:"today_new"`
}

// Detail 报修单详情, 附带被合并的重复报修记录。
type Detail struct {
	Report
	Duplicates []Report `json:"duplicates"`
}

// Meta 报修模块字典, 供前端渲染下拉框。
type Meta struct {
	Statuses      []string `json:"statuses"`
	LocateMethods []string `json:"locate_methods"`
	FaultTypes    []string `json:"fault_types"`
	MergeWindowH  float64  `json:"merge_window_hours"`
}
