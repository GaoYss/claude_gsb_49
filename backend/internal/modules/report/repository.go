package report

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/apperr"
	"streetlight/pkg/pagination"
)

// Filter 是仓储层使用的报修查询条件, 日期已在服务层解析为时间。
type Filter struct {
	Keyword      string
	Status       string
	FaultType    string
	RoadName     string
	LampID       uint
	FaultID      uint
	ReportedFrom *time.Time
	ReportedTo   *time.Time
}

// Repository 负责市民报修的数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造市民报修仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) session(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// Create 新增报修记录。
func (r *Repository) Create(ctx context.Context, entity *Report) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("提交市民报修失败: %w", err)
	}
	return nil
}

// CreateWithUniqueNo 生成唯一报修单号并落库, 单号冲突时自动重试。
func (r *Repository) CreateWithUniqueNo(ctx context.Context, entity *Report, prefix string) error {
	for attempt := 0; attempt < 5; attempt++ {
		sequence, err := r.NextSequence(ctx, prefix)
		if err != nil {
			return err
		}
		entity.ReportNo = fmt.Sprintf("%s%04d", prefix, sequence+attempt)
		err = r.Create(ctx, entity)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
	}
	return apperr.Conflict("报修单号生成冲突, 请稍后重试")
}

// NextSequence 返回指定前缀下可用的下一个流水号。
func (r *Repository) NextSequence(ctx context.Context, prefix string) (int, error) {
	var latest string
	err := r.session(ctx).Model(&Report{}).
		Where("report_no LIKE ?", prefix+"%").
		Order("report_no DESC").
		Limit(1).
		Pluck("report_no", &latest).Error
	if err != nil {
		return 0, fmt.Errorf("生成报修单号失败: %w", err)
	}
	if latest == "" {
		return 1, nil
	}
	value, convErr := strconv.Atoi(strings.TrimPrefix(latest, prefix))
	if convErr != nil {
		return 1, nil
	}
	return value + 1, nil
}

// Update 保存报修单全部字段。
func (r *Repository) Update(ctx context.Context, entity *Report) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新报修单失败: %w", err)
	}
	return nil
}

// UpdateColumns 局部更新报修单字段。
func (r *Repository) UpdateColumns(ctx context.Context, id uint, columns map[string]any) error {
	if len(columns) == 0 {
		return nil
	}
	result := r.session(ctx).Model(&Report{}).Where("id = ?", id).Updates(columns)
	if result.Error != nil {
		return fmt.Errorf("更新报修单失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return apperr.NotFound("报修单不存在: id=%d", id)
	}
	return nil
}

// GetByID 按主键查询报修单。
func (r *Repository) GetByID(ctx context.Context, id uint) (*Report, error) {
	var entity Report
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("报修单不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询报修单失败: %w", err)
	}
	return &entity, nil
}

// GetByNo 按报修单号查询。
func (r *Repository) GetByNo(ctx context.Context, reportNo string) (*Report, error) {
	var entity Report
	err := r.session(ctx).Where("report_no = ?", strings.TrimSpace(reportNo)).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("报修单不存在: report_no=%s", reportNo)
	}
	if err != nil {
		return nil, fmt.Errorf("查询报修单失败: %w", err)
	}
	return &entity, nil
}

// List 分页查询报修记录。
func (r *Repository) List(ctx context.Context, filter Filter, page pagination.Query) ([]Report, int64, error) {
	base := func() *gorm.DB {
		return applyFilter(r.session(ctx).Model(&Report{}), filter)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计报修总数失败: %w", err)
	}

	entities := make([]Report, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询报修列表失败: %w", err)
	}
	return entities, total, nil
}

// ListDuplicates 查询并入指定主报修单的全部重复上报。
func (r *Repository) ListDuplicates(ctx context.Context, masterID uint) ([]Report, error) {
	entities := make([]Report, 0)
	err := r.session(ctx).
		Where("merged_into_id = ?", masterID).
		Order("reported_at ASC, id ASC").
		Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询重复报修失败: %w", err)
	}
	return entities, nil
}

// ListByFault 查询关联到指定正式故障的全部报修单(主报修单 + 被合并的重复上报)。
func (r *Repository) ListByFault(ctx context.Context, faultID uint) ([]Report, error) {
	entities := make([]Report, 0)
	err := r.session(ctx).
		Where("fault_id = ?", faultID).
		Order("reported_at ASC, id ASC").
		Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询故障来源报修失败: %w", err)
	}
	return entities, nil
}

// CountByStatus 按核实状态统计报修单数量。
func (r *Repository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	type row struct {
		Label string
		Total int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Report{}).
		Select("status AS label, COUNT(*) AS total").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("统计报修状态失败: %w", err)
	}
	result := make(map[string]int64, len(rows))
	for _, item := range rows {
		result[item.Label] = item.Total
	}
	return result, nil
}

// CountReportedBetween 统计指定时间区间内上报的报修单数量。
func (r *Repository) CountReportedBetween(ctx context.Context, from, to time.Time) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&Report{}).
		Where("reported_at >= ? AND reported_at < ?", from, to).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计报修数量失败: %w", err)
	}
	return total, nil
}

// FindMergeCandidate 在时间窗口内查找可合并的主报修单(待核实状态)。
//
// 匹配优先级:
//  1. 已定位到同一盏路灯(lamp_id 相同);
//  2. 未定位路灯但填写了相同的灯杆编号(lamp_code 相同);
//  3. 既无编号也未定位时, 道路与位置描述完全一致。
func (r *Repository) FindMergeCandidate(ctx context.Context, candidate *Report, window time.Duration) (*Report, error) {
	from := candidate.ReportedAt.Add(-window)
	to := candidate.ReportedAt.Add(window)

	statement := r.session(ctx).Model(&Report{}).
		Where("status = ?", StatusPending).
		Where("reported_at >= ? AND reported_at <= ?", from, to)

	switch {
	case candidate.LampID != nil:
		// 已定位台账: 命中同一盏灯; 若同时带来编号, 也命中编号相同但尚未定位的主报修单。
		if code := strings.TrimSpace(candidate.LampCode); code != "" {
			statement = statement.Where(
				"(lamp_id = ? OR (lamp_id IS NULL AND lamp_code = ?))",
				*candidate.LampID, code,
			)
		} else {
			statement = statement.Where("lamp_id = ?", *candidate.LampID)
		}
	case strings.TrimSpace(candidate.LampCode) != "":
		// 只有编号: 编号相同即视为同一路灯(主单是否已定位均可)。
		statement = statement.Where("lamp_code = ?", strings.TrimSpace(candidate.LampCode))
	default:
		// 纯位置描述: 道路与位置描述一致且都没有编号。
		statement = statement.
			Where("lamp_code = '' AND road_name = ? AND location_desc = ?",
				strings.TrimSpace(candidate.RoadName), strings.TrimSpace(candidate.LocationDesc))
	}

	var entity Report
	err := statement.Order("reported_at DESC, id DESC").First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查找重复报修失败: %w", err)
	}
	return &entity, nil
}

// applyFilter 统一拼装修报列表查询条件。
func applyFilter(statement *gorm.DB, filter Filter) *gorm.DB {
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		statement = statement.Where(
			"report_no LIKE ? OR lamp_code LIKE ? OR road_name LIKE ? OR location_desc LIKE ? OR content LIKE ? OR reporter LIKE ?",
			like, like, like, like, like, like,
		)
	}
	if filter.Status != "" {
		statement = statement.Where("status = ?", filter.Status)
	}
	if filter.FaultType != "" {
		statement = statement.Where("fault_type = ?", filter.FaultType)
	}
	if value := strings.TrimSpace(filter.RoadName); value != "" {
		statement = statement.Where("road_name = ?", value)
	}
	if filter.LampID > 0 {
		statement = statement.Where("lamp_id = ?", filter.LampID)
	}
	if filter.FaultID > 0 {
		statement = statement.Where("fault_id = ?", filter.FaultID)
	}
	if filter.ReportedFrom != nil {
		statement = statement.Where("reported_at >= ?", *filter.ReportedFrom)
	}
	if filter.ReportedTo != nil {
		statement = statement.Where("reported_at < ?", *filter.ReportedTo)
	}
	return statement
}

// isUniqueViolation 兼容 sqlite 与 postgres 的唯一约束冲突判断。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "unique violation")
}
