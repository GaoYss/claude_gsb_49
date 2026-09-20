package report

import (
	"context"
	"log/slog"
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
		"updated_at":  "updated_at",
	},
	Default: "reported_at",
}

// LampPort 由路灯台账模块实现, 报修模块通过它按编号匹配路灯。
type LampPort interface {
	Get(ctx context.Context, id uint) (*lamp.Lamp, error)
	GetByCode(ctx context.Context, code string) (*lamp.Lamp, error)
}

// FaultPort 由故障登记模块实现, 报修核实有效后通过它建立正式故障。
type FaultPort interface {
	Create(ctx context.Context, req fault.CreateRequest) (*fault.Fault, error)
	GetOpenByLamp(ctx context.Context, lampID uint) (*fault.Fault, error)
}

// Service 承载市民报修的业务规则: 受理合并、核实转故障、核实无效。
type Service struct {
	repo        *Repository
	lamps       LampPort
	faults      FaultPort
	mergeWindow time.Duration
}

// NewService 构造市民报修服务。
func NewService(repo *Repository, lamps LampPort, faults FaultPort) *Service {
	return &Service{repo: repo, lamps: lamps, faults: faults, mergeWindow: DefaultMergeWindow}
}

// Repository 暴露仓储, 供 bootstrap 装配其它模块所需的端口。
func (s *Service) Repository() *Repository { return s.repo }

// Submit 受理市民报修: 依据灯杆编号或位置描述登记, 并自动合并时间窗口内同一路灯的重复上报。
//
// 返回报修单与是否发生合并: merged=true 时新记录已标记为"重复合并", 主报修单计数累加。
func (s *Service) Submit(ctx context.Context, req CreateRequest) (*Report, bool, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, false, apperr.BadRequest("报修描述不能为空")
	}
	faultType := strings.TrimSpace(req.FaultType)
	if !isValidFaultType(faultType) {
		return nil, false, apperr.BadRequest("非法的故障类型: %s", faultType)
	}

	lampCode := strings.TrimSpace(req.LampCode)
	roadName := strings.TrimSpace(req.RoadName)
	locationDesc := strings.TrimSpace(req.LocationDesc)
	if lampCode == "" && (roadName == "" || locationDesc == "") {
		return nil, false, apperr.BadRequest("请填写灯杆编号, 或同时填写所在道路与位置描述, 便于定位路灯")
	}

	reportedAt, err := parseReportedAt(req.ReportedAt)
	if err != nil {
		return nil, false, err
	}

	entity := &Report{
		LampCode:      lampCode,
		RoadName:      roadName,
		LocationDesc:  locationDesc,
		FaultType:     faultType,
		Content:       content,
		Reporter:      strings.TrimSpace(req.Reporter),
		ReporterPhone: strings.TrimSpace(req.ReporterPhone),
		ReportedAt:    reportedAt,
		Status:        StatusPending,
	}

	// 市民填写了灯杆编号时, 尝试直接关联台账路灯; 编号不存在不阻断受理, 留待人工核实。
	if lampCode != "" {
		device, lookupErr := s.lamps.GetByCode(ctx, lampCode)
		if lookupErr == nil {
			entity.LampID = &device.ID
			entity.LampCode = device.Code
			entity.RoadName = device.RoadName
		} else if _, ok := apperr.As(lookupErr); !ok {
			return nil, false, lookupErr
		}
		// 编号未找到等业务错误: 保留市民原始编号与道路描述, 转人工核实。
	}

	// 自动合并: 时间窗口内同一路灯已有待核实报修时, 本次上报直接并入主单。
	master, err := s.repo.FindMergeCandidate(ctx, entity, s.mergeWindow)
	if err != nil {
		return nil, false, err
	}
	merged := master != nil
	if merged {
		entity.Status = StatusMerged
		entity.MergedIntoID = &master.ID
		entity.MergedIntoNo = master.ReportNo
		// 主单尚未关联台账而本次上报带来了编号/定位信息时, 顺手补全主单, 方便后续核实。
		if master.LampID == nil && entity.LampID != nil {
			columns := map[string]any{
				"lamp_id":   *entity.LampID,
				"lamp_code": entity.LampCode,
				"road_name": entity.RoadName,
			}
			if err := s.repo.UpdateColumns(ctx, master.ID, columns); err != nil {
				return nil, false, err
			}
		}
	}

	if err := s.repo.CreateWithUniqueNo(ctx, entity, reportNoPrefix(reportedAt)); err != nil {
		return nil, false, err
	}

	if merged {
		if err := s.repo.UpdateColumns(ctx, master.ID, map[string]any{
			"duplicate_count": master.DuplicateCount + 1,
		}); err != nil {
			slog.Warn("累加主报修单重复次数失败", "master_id", master.ID, "error", err)
		}
	}
	return entity, merged, nil
}

// Verify 核实有效: 将报修单(及其重复上报)转为正式故障, 来源为市民上报并保留原始描述。
func (s *Service) Verify(ctx context.Context, id uint, req VerifyRequest) (*Report, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusPending {
		return nil, apperr.Conflict("报修单 %s 当前状态为 %s, 不允许核实", entity.ReportNo, StatusLabel(entity.Status))
	}

	device, err := s.lamps.Get(ctx, req.LampID)
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

	// 该路灯已有未闭环故障(内部巡检先登记等场景)时不再重复建工单, 直接关联既有故障。
	target, err := s.faults.GetOpenByLamp(ctx, device.ID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		target, err = s.faults.Create(ctx, fault.CreateRequest{
			LampID:        device.ID,
			FaultType:     faultType,
			FaultLevel:    level,
			Source:        fault.SourceCitizen,
			Description:   entity.Content, // 正式故障保留市民的原始描述
			Reporter:      entity.Reporter,
			ReporterPhone: entity.ReporterPhone,
			ReportedAt:    entity.ReportedAt.Format("2006-01-02 15:04:05"),
		})
		if err != nil {
			return nil, err
		}
	}

	now := time.Now()
	entity.Status = StatusConfirmed
	entity.LampID = &device.ID
	entity.LampCode = device.Code
	entity.RoadName = device.RoadName
	entity.FaultID = &target.ID
	entity.FaultNo = target.FaultNo
	entity.FaultType = faultType
	entity.VerifiedBy = strings.TrimSpace(req.VerifiedBy)
	entity.VerifyRemark = strings.TrimSpace(req.VerifyRemark)
	entity.VerifiedAt = &now
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}

	// 被合并的重复上报同样关联到正式故障, 保证每条市民来音都能追溯到处置结果。
	duplicates, err := s.repo.ListDuplicates(ctx, entity.ID)
	if err != nil {
		return nil, err
	}
	for _, dup := range duplicates {
		columns := map[string]any{
			"lamp_id":   device.ID,
			"lamp_code": device.Code,
			"road_name": device.RoadName,
			"fault_id":  target.ID,
			"fault_no":  target.FaultNo,
		}
		if err := s.repo.UpdateColumns(ctx, dup.ID, columns); err != nil {
			slog.Warn("回填重复报修故障链接失败", "report_id", dup.ID, "error", err)
		}
	}

	return entity, nil
}

// Invalid 核实无效: 必须注明原因。
func (s *Service) Invalid(ctx context.Context, id uint, req InvalidRequest) (*Report, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status != StatusPending {
		return nil, apperr.Conflict("报修单 %s 当前状态为 %s, 不允许核实", entity.ReportNo, StatusLabel(entity.Status))
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, apperr.BadRequest("核实无效时必须说明原因")
	}

	now := time.Now()
	entity.Status = StatusInvalid
	entity.InvalidReason = reason
	entity.VerifiedBy = strings.TrimSpace(req.VerifiedBy)
	entity.VerifiedAt = &now
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	return entity, nil
}

// Get 查询报修单详情。
func (s *Service) Get(ctx context.Context, id uint) (*Report, error) {
	return s.repo.GetByID(ctx, id)
}

// GetDetail 查询报修单详情及其被合并的重复上报记录。
func (s *Service) GetDetail(ctx context.Context, id uint) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	duplicates, err := s.repo.ListDuplicates(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Detail{Report: *entity, Duplicates: duplicates}, nil
}

// List 分页查询报修队列。
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

// Summary 汇总报修队列的核实情况。
func (s *Service) Summary(ctx context.Context) (*QueueSummary, error) {
	byStatus, err := s.repo.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayNew, err := s.repo.CountReportedBetween(ctx, todayStart, todayStart.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	summary := &QueueSummary{
		Total:          sumCounts(byStatus),
		PendingTotal:   byStatus[StatusPending],
		ConfirmedTotal: byStatus[StatusConfirmed],
		InvalidTotal:   byStatus[StatusInvalid],
		MergedTotal:    byStatus[StatusMerged],
		TodayNew:       todayNew,
	}
	return summary, nil
}

// Metadata 返回报修模块字典。
func (s *Service) Metadata() *Meta {
	return &Meta{
		Statuses:      Statuses(),
		LocateMethods: LocateMethods(),
		FaultTypes:    fault.FaultTypes(),
		MergeWindowH:  s.mergeWindow.Hours(),
	}
}

// buildFilter 将列表查询参数转换为仓储条件, 并解析日期区间。
func buildFilter(query ListQuery) (Filter, error) {
	filter := Filter{
		Keyword:   strings.TrimSpace(query.Keyword),
		Status:    strings.TrimSpace(query.Status),
		FaultType: strings.TrimSpace(query.FaultType),
		RoadName:  strings.TrimSpace(query.RoadName),
		LampID:    query.LampID,
		FaultID:   query.FaultID,
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

// reportNoPrefix 生成报修单号前缀, 例如 BX20260920。
func reportNoPrefix(reportedAt time.Time) string {
	return "BX" + reportedAt.Format("20060102")
}

// parseReportedAt 解析上报时间, 支持常见日期时间格式, 为空时取当前时间。
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

// isValidFaultType 校验故障类型是否在故障模块允许的范围内。
func isValidFaultType(faultType string) bool {
	for _, item := range fault.FaultTypes() {
		if item == faultType {
			return true
		}
	}
	return false
}

func sumCounts(counts map[string]int64) int64 {
	var total int64
	for _, value := range counts {
		total += value
	}
	return total
}
