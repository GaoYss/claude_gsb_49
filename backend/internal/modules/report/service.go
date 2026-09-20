package report

import (
	"context"
	"strings"
	"time"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/pkg/pagination"
)

// reportSortSpec 定义报修列表接口允许的排序字段白名单。
var reportSortSpec = pagination.SortSpec{
	Allowed: map[string]string{
		"report_no":   "report_no",
		"reported_at": "reported_at",
		"status":      "status",
		"fault_type":  "fault_type",
		"created_at":  "created_at",
	},
	Default: "reported_at",
}

// LampPort 由路灯台账模块实现, 报修模块通过它解析灯杆编号。
type LampPort interface {
	Get(ctx context.Context, id uint) (*lamp.Lamp, error)
	GetByCode(ctx context.Context, code string) (*lamp.Lamp, error)
}

// FaultCreator 由故障模块实现, 核实通过时通过它登记正式故障。
type FaultCreator interface {
	Create(ctx context.Context, req fault.CreateRequest) (*fault.Fault, error)
}

// Service 承载市民报修的业务规则: 受理、自动合并、核实转故障、核实无效。
type Service struct {
	repo   *Repository
	lamps  LampPort
	faults FaultCreator
}

// NewService 构造市民报修服务。
func NewService(repo *Repository, lamps LampPort, faults FaultCreator) *Service {
	return &Service{repo: repo, lamps: lamps, faults: faults}
}

// Repository 暴露仓储, 供状态查询模块装配只读统计。
func (s *Service) Repository() *Repository { return s.repo }

// Get 查询报修详情, 附带被合并的重复上报与合并目标。
func (s *Service) Get(ctx context.Context, id uint) (*DetailResponse, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := &DetailResponse{Report: entity, MergedItems: make([]Report, 0)}
	items, err := s.repo.ListMergedInto(ctx, entity.ID)
	if err != nil {
		return nil, err
	}
	detail.MergedItems = items

	if entity.MergedIntoID != nil {
		master, err := s.repo.GetByID(ctx, *entity.MergedIntoID)
		if err == nil {
			detail.MergedInto = master
		}
	}
	return detail, nil
}

// List 分页查询报修列表, 同时返回归一化后的分页信息。
func (s *Service) List(ctx context.Context, query ListQuery) ([]Report, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, reportSortSpec)
	filter, err := buildFilter(query)
	if err != nil {
		return nil, 0, page, err
	}
	items, total, err := s.repo.List(ctx, filter, page)
	if err != nil {
		return nil, 0, page, err
	}
	return items, total, page, nil
}

// Create 受理市民报修: 凭灯杆编号或位置描述提交, 同一路灯短时间内的重复上报自动合并。
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Report, error) {
	lampCode := strings.TrimSpace(req.LampCode)
	locationDesc := strings.TrimSpace(req.LocationDesc)

	var device *lamp.Lamp
	if lampCode != "" {
		found, err := s.lamps.GetByCode(ctx, lampCode)
		if err != nil {
			if appErr, ok := apperr.As(err); ok && appErr.Code == apperr.CodeNotFound {
				return nil, apperr.BadRequest("灯杆编号 %s 不存在, 请核对灯杆标识或改用位置描述", lampCode)
			}
			return nil, err
		}
		device = found
	}
	if device == nil && locationDesc == "" {
		return nil, apperr.BadRequest("请填写灯杆编号或位置描述")
	}

	description := strings.TrimSpace(req.Description)
	if description == "" {
		return nil, apperr.BadRequest("报修描述不能为空")
	}

	faultType := strings.TrimSpace(req.FaultType)
	if faultType == "" {
		faultType = "其他"
	}
	if !isValidFaultType(faultType) {
		return nil, apperr.BadRequest("非法的故障现象: %s", faultType)
	}

	reportedAt, err := parseReportedAt(req.ReportedAt)
	if err != nil {
		return nil, err
	}

	entity := &Report{
		LampCode:      lampCode,
		LocationDesc:  locationDesc,
		FaultType:     faultType,
		Description:   description,
		Reporter:      strings.TrimSpace(req.Reporter),
		ReporterPhone: strings.TrimSpace(req.ReporterPhone),
		ReportedAt:    reportedAt,
		Status:        StatusPending,
	}
	if device != nil {
		entity.LampID = &device.ID
		entity.LampCode = device.Code
		entity.RoadName = device.RoadName
	}

	// 同一路灯在合并窗口内已有待核实报修时, 本次上报自动合并到最早的主单。
	if device != nil {
		master, err := s.repo.FindPendingByLamp(ctx, device.ID, reportedAt.Add(-MergeWindow))
		if err != nil {
			return nil, err
		}
		if master != nil {
			entity.Status = StatusMerged
			entity.MergedIntoID = &master.ID
			entity.MergedIntoNo = master.ReportNo
			if err := s.repo.CreateWithUniqueNo(ctx, entity, reportNoPrefix(reportedAt)); err != nil {
				return nil, err
			}
			if err := s.repo.IncrementMergeCount(ctx, master.ID); err != nil {
				return nil, err
			}
			return entity, nil
		}
	}

	if err := s.repo.CreateWithUniqueNo(ctx, entity, reportNoPrefix(reportedAt)); err != nil {
		return nil, err
	}
	return entity, nil
}

// Confirm 核实通过: 报修转为正式故障, 保留市民来源与原始描述。
func (s *Service) Confirm(ctx context.Context, id uint, req ConfirmRequest) (*Report, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusPending {
		return nil, apperr.Conflict("报修 %s 当前状态为 %s, 不能重复核实", entity.ReportNo, StatusLabel(entity.Status))
	}

	lampID := entity.LampID
	if lampID == nil {
		if req.LampID == 0 {
			return nil, apperr.BadRequest("该报修只有位置描述, 请先指定关联路灯再转故障")
		}
		lampID = &req.LampID
	}
	device, err := s.lamps.Get(ctx, *lampID)
	if err != nil {
		return nil, err
	}

	faultType := strings.TrimSpace(req.FaultType)
	if faultType == "" {
		faultType = entity.FaultType
	}
	if !isValidFaultType(faultType) {
		return nil, apperr.BadRequest("非法的故障类型: %s", faultType)
	}

	level := strings.TrimSpace(req.FaultLevel)
	if level == "" {
		level = fault.LevelNormal
	}

	created, err := s.faults.Create(ctx, fault.CreateRequest{
		LampID:        device.ID,
		FaultType:     faultType,
		FaultLevel:    level,
		Source:        fault.SourceCitizen,
		Description:   entity.Description,
		Reporter:      entity.Reporter,
		ReporterPhone: entity.ReporterPhone,
		ReportedAt:    entity.ReportedAt.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		return nil, err
	}

	now := time.Now()
	columns := map[string]any{
		"status":        StatusConfirmed,
		"fault_id":      created.ID,
		"fault_no":      created.FaultNo,
		"verify_remark": strings.TrimSpace(req.Remark),
		"verified_by":   strings.TrimSpace(req.Operator),
		"verified_at":   now,
	}
	if entity.LampID == nil {
		columns["lamp_id"] = device.ID
		columns["lamp_code"] = device.Code
		columns["road_name"] = device.RoadName
	}
	if err := s.repo.UpdateColumns(ctx, entity.ID, columns); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, entity.ID)
}

// Reject 核实无效: 必须说明原因, 报修单留存备查。
func (s *Service) Reject(ctx context.Context, id uint, req RejectRequest) (*Report, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusPending {
		return nil, apperr.Conflict("报修 %s 当前状态为 %s, 不能重复核实", entity.ReportNo, StatusLabel(entity.Status))
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, apperr.BadRequest("核实为无效时必须填写原因")
	}

	now := time.Now()
	columns := map[string]any{
		"status":         StatusInvalid,
		"invalid_reason": reason,
		"verified_by":    strings.TrimSpace(req.Operator),
		"verified_at":    now,
	}
	if err := s.repo.UpdateColumns(ctx, entity.ID, columns); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, entity.ID)
}

// Metadata 返回报修模块字典与队列统计。
func (s *Service) Metadata(ctx context.Context) (*Meta, error) {
	counts, err := s.repo.CountByColumn(ctx, "status")
	if err != nil {
		return nil, err
	}

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today, err := s.repo.CountReportedBetween(ctx, todayStart, todayStart.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}

	return &Meta{
		Statuses:         Statuses(),
		MergeWindowHours: MergeWindow.Hours(),
		StatusCounts:     counts,
		TodayCount:       today,
	}, nil
}

// buildFilter 将列表查询参数转换为仓储条件, 并解析日期区间。
func buildFilter(query ListQuery) (Filter, error) {
	filter := Filter{
		Keyword:  strings.TrimSpace(query.Keyword),
		Status:   strings.TrimSpace(query.Status),
		RoadName: strings.TrimSpace(query.RoadName),
	}

	if filter.Status != "" && !IsValidStatus(filter.Status) {
		return filter, apperr.BadRequest("非法的报修状态: %s", filter.Status)
	}

	if strings.TrimSpace(query.StartDate) != "" {
		from, err := parseDay(query.StartDate)
		if err != nil {
			return filter, err
		}
		filter.ReportedFrom = &from
	}
	if strings.TrimSpace(query.EndDate) != "" {
		to, err := parseDay(query.EndDate)
		if err != nil {
			return filter, err
		}
		to = to.AddDate(0, 0, 1)
		filter.ReportedTo = &to
	}
	if filter.ReportedFrom != nil && filter.ReportedTo != nil && filter.ReportedTo.Before(*filter.ReportedFrom) {
		return filter, apperr.BadRequest("结束日期不能早于开始日期")
	}
	return filter, nil
}

// reportNoPrefix 生成报修单号前缀, 例如 BX20260913。
func reportNoPrefix(reportedAt time.Time) string {
	return "BX" + reportedAt.Format("20060102")
}

// isValidFaultType 校验故障现象是否在故障模块允许的类型范围内。
func isValidFaultType(faultType string) bool {
	for _, item := range fault.FaultTypes() {
		if item == faultType {
			return true
		}
	}
	return false
}

// parseReportedAt 解析上报时间, 支持 RFC3339 与常见的日期时间格式, 为空时取当前时间。
func parseReportedAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now(), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, apperr.BadRequest("上报时间格式不正确, 建议使用 YYYY-MM-DD HH:mm:ss: %s", value)
}

// parseDay 解析 YYYY-MM-DD 日期, 返回当天零点。
func parseDay(value string) (time.Time, error) {
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), time.Local)
	if err != nil {
		return time.Time{}, apperr.BadRequest("日期格式应为 YYYY-MM-DD, 当前值: %s", value)
	}
	return date, nil
}
