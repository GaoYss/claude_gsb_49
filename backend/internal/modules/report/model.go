package report

import "time"

// 报修核实状态。
const (
	StatusPending   = "pending"   // 待核实
	StatusConfirmed = "confirmed" // 已转故障
	StatusInvalid   = "invalid"   // 无效报修
	StatusMerged    = "merged"    // 已合并(重复上报)
)

// MergeWindow 是同一路灯重复报修自动合并的时间窗口。
const MergeWindow = 24 * time.Hour

// Statuses 返回全部报修状态取值。
func Statuses() []string {
	return []string{StatusPending, StatusConfirmed, StatusInvalid, StatusMerged}
}

// IsValidStatus 校验报修状态取值。
func IsValidStatus(status string) bool {
	for _, item := range Statuses() {
		if item == status {
			return true
		}
	}
	return false
}

// Report 市民报修记录, 经核实后转为正式故障或标记为无效。
type Report struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	ReportNo      string     `gorm:"size:64;uniqueIndex;not null" json:"report_no"`
	LampID        *uint      `gorm:"index" json:"lamp_id"`
	LampCode      string     `gorm:"size:64;index" json:"lamp_code"` // 市民提供的灯杆编号, 原样保留
	RoadName      string     `gorm:"size:128;index" json:"road_name"`
	LocationDesc  string     `gorm:"size:255" json:"location_desc"` // 无编号时的位置描述
	FaultType     string     `gorm:"size:32;index;not null;default:其他" json:"fault_type"`
	Description   string     `gorm:"size:512;not null" json:"description"` // 市民原始描述
	Reporter      string     `gorm:"size:64" json:"reporter"`
	ReporterPhone string     `gorm:"size:32" json:"reporter_phone"`
	ReportedAt    time.Time  `gorm:"index;not null" json:"reported_at"`
	Status        string     `gorm:"size:32;index;not null;default:pending" json:"status"`
	MergeCount    int        `gorm:"not null;default:0" json:"merge_count"` // 被合并的重复上报次数
	MergedIntoID  *uint      `gorm:"index" json:"merged_into_id"`           // 重复上报合并进入的主报修单
	FaultID       *uint      `json:"fault_id"`                              // 核实后生成的故障
	FaultNo       string     `gorm:"size:64" json:"fault_no"`
	VerifyRemark  string     `gorm:"size:255" json:"verify_remark"`
	InvalidReason string     `gorm:"size:255" json:"invalid_reason"` // 核实无效时必填
	VerifiedBy    string     `gorm:"size:64" json:"verified_by"`
	VerifiedAt    *time.Time `json:"verified_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// MergedIntoNo 仅在创建响应中回填, 用于提示合并目标单号, 不落库。
	MergedIntoNo string `gorm:"-" json:"merged_into_no,omitempty"`
}

// TableName 指定表名。
func (Report) TableName() string { return "report" }
