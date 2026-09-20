package report_test

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/report"
)

type harness struct {
	lamps   *lamp.Service
	faults  *fault.Service
	reports *report.Service
	db      *gorm.DB
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.AutoMigrate(&lamp.Lamp{}, &fault.Fault{}, &report.Report{}))

	lampRepository := lamp.NewRepository(db)
	lampService := lamp.NewService(lampRepository)

	faultRepository := fault.NewRepository(db)
	faultService := fault.NewService(faultRepository, lampService)
	lampService.SetOpenFaultCounter(faultRepository)

	reportRepository := report.NewRepository(db)
	reportService := report.NewService(reportRepository, lampService, faultService)

	return &harness{lamps: lampService, faults: faultService, reports: reportService, db: db}
}

func (h *harness) createLamp(t *testing.T, code string) *lamp.Lamp {
	t.Helper()
	entity, err := h.lamps.Create(context.Background(), lamp.CreateRequest{
		Code:     code,
		RoadName: "中山路",
		LampType: lamp.LampTypeLED,
	})
	require.NoError(t, err)
	return entity
}

func (h *harness) createReport(t *testing.T, req report.CreateRequest) *report.Report {
	t.Helper()
	entity, err := h.reports.Create(context.Background(), req)
	require.NoError(t, err)
	return entity
}

func TestCreateReportResolvesLampByCode(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-001")

	created := h.createReport(t, report.CreateRequest{
		LampCode:    device.Code,
		FaultType:   "灯不亮",
		Description: "整灯不亮, 影响通行",
		Reporter:    "市民张先生",
	})

	require.NotEmpty(t, created.ReportNo)
	require.Equal(t, report.StatusPending, created.Status)
	require.NotNil(t, created.LampID)
	require.Equal(t, device.ID, *created.LampID)
	require.Equal(t, device.Code, created.LampCode)
	require.Equal(t, device.RoadName, created.RoadName)

	// 待核实报修不应影响路灯运行状态。
	reloaded, err := h.lamps.Get(ctx, device.ID)
	require.NoError(t, err)
	require.Equal(t, lamp.RunStatusNormal, reloaded.RunStatus)
}

func TestCreateReportWithLocationOnly(t *testing.T) {
	h := newHarness(t)

	created := h.createReport(t, report.CreateRequest{
		LocationDesc: "滨江路与解放路交叉口东南角灯杆",
		Description:  "灯杆检修门脱落",
	})

	require.Equal(t, report.StatusPending, created.Status)
	require.Nil(t, created.LampID)
	require.Empty(t, created.LampCode)
	require.Equal(t, "其他", created.FaultType)
}

func TestCreateReportRequiresCodeOrLocation(t *testing.T) {
	h := newHarness(t)

	_, err := h.reports.Create(context.Background(), report.CreateRequest{Description: "什么都没有"})
	require.Error(t, err)
	appErr, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArgument, appErr.Code)
}

func TestCreateReportRejectsUnknownLampCode(t *testing.T) {
	h := newHarness(t)

	_, err := h.reports.Create(context.Background(), report.CreateRequest{
		LampCode:    "LD-UNKNOWN",
		Description: "编号不存在",
	})
	require.Error(t, err)
	appErr, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeInvalidArgument, appErr.Code)
}

func TestDuplicateReportAutoMerges(t *testing.T) {
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-101")

	master := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "灯不亮"})
	require.Equal(t, report.StatusPending, master.Status)

	duplicate := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "路灯不亮"})
	require.Equal(t, report.StatusMerged, duplicate.Status)
	require.NotNil(t, duplicate.MergedIntoID)
	require.Equal(t, master.ID, *duplicate.MergedIntoID)
	require.Equal(t, master.ReportNo, duplicate.MergedIntoNo)

	// 再次重复上报仍合并到最早的主单, 主单重复次数累加。
	third := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "还是不亮"})
	require.Equal(t, report.StatusMerged, third.Status)
	require.Equal(t, master.ID, *third.MergedIntoID)

	detail, err := h.reports.Get(context.Background(), master.ID)
	require.NoError(t, err)
	require.Equal(t, 2, detail.Report.MergeCount)
	require.Len(t, detail.MergedItems, 2)
}

func TestReportOutsideMergeWindowDoesNotMerge(t *testing.T) {
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-102")

	master := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "灯不亮"})

	// 将主单上报时间拨到合并窗口之外, 新的上报应独立成单。
	outside := time.Now().Add(-report.MergeWindow - time.Hour)
	require.NoError(t, h.db.Model(&report.Report{}).Where("id = ?", master.ID).Update("reported_at", outside).Error)

	fresh := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "又不亮了"})
	require.Equal(t, report.StatusPending, fresh.Status)
	require.Nil(t, fresh.MergedIntoID)
}

func TestLocationOnlyReportsDoNotMerge(t *testing.T) {
	h := newHarness(t)

	first := h.createReport(t, report.CreateRequest{LocationDesc: "路口东南角", Description: "灯不亮"})
	second := h.createReport(t, report.CreateRequest{LocationDesc: "路口东南角", Description: "灯还是不亮"})

	require.Equal(t, report.StatusPending, first.Status)
	require.Equal(t, report.StatusPending, second.Status)
}

func TestConfirmConvertsReportToFault(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-201")

	created := h.createReport(t, report.CreateRequest{
		LampCode:      device.Code,
		FaultType:     "灯不亮",
		Description:   "市民原始描述: 整灯不亮",
		Reporter:      "李阿姨",
		ReporterPhone: "13600000000",
	})

	confirmed, err := h.reports.Confirm(ctx, created.ID, report.ConfirmRequest{
		FaultLevel: fault.LevelHigh,
		Remark:     "电话回访属实",
		Operator:   "值班员",
	})
	require.NoError(t, err)
	require.Equal(t, report.StatusConfirmed, confirmed.Status)
	require.NotNil(t, confirmed.FaultID)
	require.NotEmpty(t, confirmed.FaultNo)
	require.Equal(t, "值班员", confirmed.VerifiedBy)
	require.NotNil(t, confirmed.VerifiedAt)

	// 生成的故障保留市民来源与原始描述。
	converted, err := h.faults.GetByID(ctx, *confirmed.FaultID)
	require.NoError(t, err)
	require.Equal(t, fault.SourceCitizen, converted.Source)
	require.Equal(t, "市民原始描述: 整灯不亮", converted.Description)
	require.Equal(t, "李阿姨", converted.Reporter)
	require.Equal(t, fault.LevelHigh, converted.FaultLevel)
	require.WithinDuration(t, created.ReportedAt, converted.ReportedAt, time.Second)

	// 路灯运行状态联动为故障。
	reloaded, err := h.lamps.Get(ctx, device.ID)
	require.NoError(t, err)
	require.Equal(t, lamp.RunStatusFault, reloaded.RunStatus)
}

func TestConfirmLocationOnlyReportRequiresLamp(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-202")

	created := h.createReport(t, report.CreateRequest{
		LocationDesc: "路口东南角灯杆",
		Description:  "检修门脱落",
	})

	_, err := h.reports.Confirm(ctx, created.ID, report.ConfirmRequest{})
	require.Error(t, err)

	confirmed, err := h.reports.Confirm(ctx, created.ID, report.ConfirmRequest{LampID: device.ID})
	require.NoError(t, err)
	require.Equal(t, report.StatusConfirmed, confirmed.Status)
	require.NotNil(t, confirmed.LampID)
	require.Equal(t, device.ID, *confirmed.LampID)
	require.Equal(t, device.Code, confirmed.LampCode)
}

func TestConfirmRejectsRepeatedVerification(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-203")

	created := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "灯不亮"})
	_, err := h.reports.Confirm(ctx, created.ID, report.ConfirmRequest{})
	require.NoError(t, err)

	_, err = h.reports.Confirm(ctx, created.ID, report.ConfirmRequest{})
	require.Error(t, err)
	appErr, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeConflict, appErr.Code)
}

func TestConfirmFailsWhenLampHasOpenFault(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-204")

	_, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: device.ID, FaultType: "灯不亮", Description: "巡检已登记",
	})
	require.NoError(t, err)

	created := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "市民也报修同一盏灯"})
	_, err = h.reports.Confirm(ctx, created.ID, report.ConfirmRequest{})
	require.Error(t, err)
	appErr, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeConflict, appErr.Code)
}

func TestRejectRequiresReason(t *testing.T) {
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-205")

	created := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "灯杆摇晃"})

	_, err := h.reports.Reject(context.Background(), created.ID, report.RejectRequest{Reason: "  "})
	require.Error(t, err)
}

func TestRejectMarksReportInvalid(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-R-206")

	created := h.createReport(t, report.CreateRequest{LampCode: device.Code, Description: "灯杆摇晃"})
	rejected, err := h.reports.Reject(ctx, created.ID, report.RejectRequest{
		Reason:   "现场核查灯杆牢固, 系误报",
		Operator: "值班员",
	})
	require.NoError(t, err)
	require.Equal(t, report.StatusInvalid, rejected.Status)
	require.Equal(t, "现场核查灯杆牢固, 系误报", rejected.InvalidReason)
	require.NotNil(t, rejected.VerifiedAt)

	// 已核实的报修不能再次核实。
	_, err = h.reports.Reject(ctx, created.ID, report.RejectRequest{Reason: "重复操作"})
	require.Error(t, err)
	appErr, ok := apperr.As(err)
	require.True(t, ok)
	require.Equal(t, apperr.CodeConflict, appErr.Code)
}
