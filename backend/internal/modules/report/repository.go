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
	RoadName     string
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
		return fmt.Errorf("提交报修失败: %w", err)
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

// UpdateColumns 局部更新报修字段。
func (r *Repository) UpdateColumns(ctx context.Context, id uint, columns map[string]any) error {
	if len(columns) == 0 {
		return nil
	}
	result := r.session(ctx).Model(&Report{}).Where("id = ?", id).Updates(columns)
	if result.Error != nil {
		return fmt.Errorf("更新报修失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return apperr.NotFound("报修记录不存在: id=%d", id)
	}
	return nil
}

// IncrementMergeCount 将主报修单的重复上报次数加一。
func (r *Repository) IncrementMergeCount(ctx context.Context, id uint) error {
	result := r.session(ctx).Model(&Report{}).
		Where("id = ?", id).
		UpdateColumn("merge_count", gorm.Expr("merge_count + 1"))
	if result.Error != nil {
		return fmt.Errorf("更新重复上报次数失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return apperr.NotFound("报修记录不存在: id=%d", id)
	}
	return nil
}

// GetByID 按主键查询报修。
func (r *Repository) GetByID(ctx context.Context, id uint) (*Report, error) {
	var entity Report
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("报修记录不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询报修失败: %w", err)
	}
	return &entity, nil
}

// FindPendingByLamp 查询某盏路灯在 since 之后上报的最早一条待核实报修, 用于重复上报合并。
func (r *Repository) FindPendingByLamp(ctx context.Context, lampID uint, since time.Time) (*Report, error) {
	var entity Report
	err := r.session(ctx).
		Where("lamp_id = ? AND status = ? AND reported_at >= ?", lampID, StatusPending, since).
		Order("reported_at ASC, id ASC").
		First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询待核实报修失败: %w", err)
	}
	return &entity, nil
}

// ListMergedInto 查询合并进某条报修的重复上报记录。
func (r *Repository) ListMergedInto(ctx context.Context, masterID uint) ([]Report, error) {
	entities := make([]Report, 0)
	err := r.session(ctx).
		Where("merged_into_id = ?", masterID).
		Order("reported_at ASC, id ASC").
		Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询合并报修失败: %w", err)
	}
	return entities, nil
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

// Count 统计报修总数。
func (r *Repository) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := r.session(ctx).Model(&Report{}).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("统计报修总数失败: %w", err)
	}
	return total, nil
}

// CountByColumn 按列分组统计, column 仅允许来自内部常量。
func (r *Repository) CountByColumn(ctx context.Context, column string) (map[string]int64, error) {
	type row struct {
		Label string
		Total int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Report{}).
		Select(column + " AS label, COUNT(*) AS total").
		Group(column).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("分组统计 %s 失败: %w", column, err)
	}

	result := make(map[string]int64, len(rows))
	for _, item := range rows {
		result[item.Label] = item.Total
	}
	return result, nil
}

// CountReportedBetween 统计指定时间区间内上报的报修数量。
func (r *Repository) CountReportedBetween(ctx context.Context, from, to time.Time) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&Report{}).
		Where("reported_at >= ? AND reported_at < ?", from, to).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计区间报修失败: %w", err)
	}
	return total, nil
}

// applyFilter 统一拼装报修列表查询条件。
func applyFilter(statement *gorm.DB, filter Filter) *gorm.DB {
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		statement = statement.Where(
			"report_no LIKE ? OR lamp_code LIKE ? OR location_desc LIKE ? OR reporter LIKE ? OR description LIKE ?",
			like, like, like, like, like,
		)
	}
	if filter.Status != "" {
		statement = statement.Where("status = ?", filter.Status)
	}
	if value := strings.TrimSpace(filter.RoadName); value != "" {
		statement = statement.Where("road_name = ?", value)
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
