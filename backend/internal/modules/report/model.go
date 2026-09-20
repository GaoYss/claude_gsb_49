package report

import "time"

// 报修单核实状态。
const (
	StatusPending   = "pending"   // 待核实
	StatusConfirmed = "confirmed" // 核实有效, 已转为正式故障
	StatusInvalid   = "invalid"   // 核实无效
	StatusMerged    = "merged"    // 重复上报, 已并入其它报修单
)

// 定位方式。
const (
	LocateByCode = "code" // 灯杆编号
	LocateByDesc = "desc" // 位置描述
)

// DefaultMergeWindow 是重复上报自动合并的默认时间窗口。
const DefaultMergeWindow = 2 * time.Hour

// Statuses 返回全部报修单状态取值。
func Statuses() []string {
	return []string{StatusPending, StatusConfirmed, StatusInvalid, StatusMerged}
}

// LocateMethods 返回全部定位方式取值。
func LocateMethods() []string {
	return []string{LocateByCode, LocateByDesc}
}

// IsValidStatus 校验报修单状态取值。
func IsValidStatus(status string) bool {
	for _, item := range Statuses() {
		if item == status {
			return true
		}
	}
	return false
}

// IsResolved 判断报修单是否已完成处置(有效 / 无效 / 合并均为终态)。
func IsResolved(status string) bool {
	return status == StatusConfirmed || status == StatusInvalid || status == StatusMerged
}

// Report 市民报修记录。
//
// 市民依据灯杆标识上的编号, 或直接描述位置提交报修, 先进入待核实行;
// 核实有效后转为正式故障(fault), 核实无效需注明原因,
// 短时间内同一路灯的重复上报自动合并到主报修单。
type Report struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	ReportNo       string     `gorm:"size:64;uniqueIndex;not null" json:"report_no"`
	LampID         *uint      `gorm:"index" json:"lamp_id"`
	LampCode       string     `gorm:"size:64;index" json:"lamp_code"`
	RoadName       string     `gorm:"size:128;index" json:"road_name"`
	LocationDesc   string     `gorm:"size:255" json:"location_desc"`
	FaultType      string     `gorm:"size:32;index;not null" json:"fault_type"`
	Content        string     `gorm:"size:512;not null" json:"content"`
	Reporter       string     `gorm:"size:64" json:"reporter"`
	ReporterPhone  string     `gorm:"size:32" json:"reporter_phone"`
	ReportedAt     time.Time  `gorm:"index;not null" json:"reported_at"`
	Status         string     `gorm:"size:32;index;not null;default:pending" json:"status"`
	FaultID        *uint      `gorm:"index" json:"fault_id"`
	FaultNo        string     `gorm:"size:64;index" json:"fault_no"`
	MergedIntoID   *uint      `gorm:"index" json:"merged_into_id"`
	MergedIntoNo   string     `gorm:"size:64" json:"merged_into_no"`
	InvalidReason  string     `gorm:"size:255" json:"invalid_reason"`
	VerifyRemark   string     `gorm:"size:255" json:"verify_remark"`
	VerifiedBy     string     `gorm:"size:64" json:"verified_by"`
	VerifiedAt     *time.Time `json:"verified_at"`
	DuplicateCount int        `gorm:"not null;default:0" json:"duplicate_count"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TableName 指定表名。
func (Report) TableName() string { return "citizen_report" }
