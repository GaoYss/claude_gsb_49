package report_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/report"
)

type harness struct {
	lamps   *lamp.Service
	faults  *fault.Service
	reports *report.Service
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

	return &harness{lamps: lampService, faults: faultService, reports: reportService}
}

func (h *harness) createLamp(t *testing.T, code, road string) *lamp.Lamp {
	t.Helper()
	power := 120
	entity, err := h.lamps.Create(context.Background(), lamp.CreateRequest{
		Code: code, RoadName: road, LampType: lamp.LampTypeLED, Power: &power,
	})
	require.NoError(t, err)
	return entity
}

func TestSubmitByLampCodeResolvesLamp(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-001", "中山路")

	record, merged, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode:  device.Code,
		FaultType: "灯不亮",
		Content:   "灯杆标识 LD-T-001, 晚上整灯不亮",
		Reporter:  "市民张阿姨",
	})
	require.NoError(t, err)
	require.False(t, merged)
	require.Equal(t, report.StatusPending, record.Status)
	require.NotNil(t, record.LampID)
	require.Equal(t, device.ID, *record.LampID)
	require.Equal(t, "中山路", record.RoadName)
	require.NotEmpty(t, record.ReportNo)
}

func TestSubmitWithoutCodeNeedsLocation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	_, _, err := h.reports.Submit(ctx, report.CreateRequest{
		FaultType: "灯不亮",
		Content:   "路上有盏灯不亮",
	})
	require.Error(t, err, "既无编号也无位置描述时应拒绝受理")

	record, _, err := h.reports.Submit(ctx, report.CreateRequest{
		RoadName:     "解放路",
		LocationDesc: "惠民超市门口公交站牌旁第三根灯杆",
		FaultType:    "灯不亮",
		Content:      "惠民超市门口那根灯杆不亮",
		Reporter:     "路人甲",
	})
	require.NoError(t, err)
	require.Nil(t, record.LampID)
	require.Equal(t, "解放路", record.RoadName)
}

func TestUnknownLampCodeStillAccepted(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	record, _, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode:  "LD-404",
		FaultType: "灯杆倾斜",
		Content:   "编号看不清, 凭印象写的 LD-404",
	})
	require.NoError(t, err, "编号在台账中不存在时不应阻断受理")
	require.Nil(t, record.LampID)
	require.Equal(t, "LD-404", record.LampCode)
}

func TestDuplicateReportAutoMerges(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-002", "建设大道")

	master, merged, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode: device.Code, FaultType: "灯不亮", Content: "第一次上报: 灯不亮", Reporter: "甲",
	})
	require.NoError(t, err)
	require.False(t, merged)

	dup, merged, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode: device.Code, FaultType: "灯不亮", Content: "第二次上报: 同一根杆子也不亮", Reporter: "乙",
	})
	require.NoError(t, err)
	require.True(t, merged, "时间窗口内同一路灯的重复上报应自动合并")
	require.Equal(t, report.StatusMerged, dup.Status)
	require.NotNil(t, dup.MergedIntoID)
	require.Equal(t, master.ID, *dup.MergedIntoID)

	detail, err := h.reports.GetDetail(ctx, master.ID)
	require.NoError(t, err)
	require.Equal(t, 1, detail.DuplicateCount)
	require.Len(t, detail.Duplicates, 1)
	require.Equal(t, "第二次上报: 同一根杆子也不亮", detail.Duplicates[0].Content)
}

func TestVerifyCreatesCitizenFaultWithOriginalContent(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-003", "园区北路")

	record, _, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode: device.Code, FaultType: "灯不亮",
		Content: "市民原话: 连续三晚不亮, 请尽快处理", Reporter: "市民李", ReporterPhone: "13700000000",
	})
	require.NoError(t, err)

	// 核实前的重复上报自动并入主单。
	dup, merged, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode: device.Code, FaultType: "灯不亮", Content: "我也发现这根杆不亮", Reporter: "市民陈",
	})
	require.NoError(t, err)
	require.True(t, merged)
	require.Equal(t, report.StatusMerged, dup.Status)

	confirmed, err := h.reports.Verify(ctx, record.ID, report.VerifyRequest{
		LampID:       device.ID,
		FaultLevel:   "high",
		VerifyRemark: "现场复核驱动电源损坏",
		VerifiedBy:   "核实员王五",
	})
	require.NoError(t, err)
	require.Equal(t, report.StatusConfirmed, confirmed.Status)
	require.NotNil(t, confirmed.FaultID)
	require.NotEmpty(t, confirmed.FaultNo)

	target, err := h.faults.GetByID(ctx, *confirmed.FaultID)
	require.NoError(t, err)
	require.Equal(t, fault.SourceCitizen, target.Source, "正式故障来源应为市民上报")
	require.Equal(t, "市民原话: 连续三晚不亮, 请尽快处理", target.Description, "正式故障应保留市民原始描述")
	require.Equal(t, fault.LevelHigh, target.FaultLevel)
	require.Equal(t, "市民李", target.Reporter)
	require.Equal(t, "13700000000", target.ReporterPhone)
	require.Equal(t, fault.StatusPending, target.Status)

	// 被合并的重复上报也应关联到同一正式故障, 每条市民来音都能追溯处置结果。
	dupDetail, err := h.reports.Get(ctx, dup.ID)
	require.NoError(t, err)
	require.NotNil(t, dupDetail.FaultID)
	require.Equal(t, target.ID, *dupDetail.FaultID)
}

func TestInvalidRequiresReason(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-004", "滨江路")

	record, _, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode: device.Code, FaultType: "灯具常亮", Content: "白天天亮着",
	})
	require.NoError(t, err)

	_, err = h.reports.Invalid(ctx, record.ID, report.InvalidRequest{Reason: "   "})
	require.Error(t, err, "核实无效必须说明原因")

	invalid, err := h.reports.Invalid(ctx, record.ID, report.InvalidRequest{
		Reason: "现场核实为周边景观灯, 非市政路灯, 转相关单位", VerifiedBy: "核实员赵六",
	})
	require.NoError(t, err)
	require.Equal(t, report.StatusInvalid, invalid.Status)
	require.Contains(t, invalid.InvalidReason, "景观灯")

	_, err = h.reports.Verify(ctx, record.ID, report.VerifyRequest{LampID: device.ID})
	require.Error(t, err, "已判无效的报修单不允许再次核实")
}

func TestVerifyLinksExistingOpenFault(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-T-005", "学院路")

	// 内部巡检已先登记一条未闭环故障。
	existing, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: device.ID, FaultType: "线路故障", Source: fault.SourceInspection,
		Description: "巡检先行登记", Reporter: "巡检员",
	})
	require.NoError(t, err)

	record, _, err := h.reports.Submit(ctx, report.CreateRequest{
		LampCode: device.Code, FaultType: "线路故障", Content: "市民也反映这根杆不亮",
	})
	require.NoError(t, err)

	confirmed, err := h.reports.Verify(ctx, record.ID, report.VerifyRequest{LampID: device.ID})
	require.NoError(t, err)
	require.Equal(t, existing.ID, *confirmed.FaultID, "同灯已有未闭环故障时应关联既有工单")

	list, err := h.faults.ListByLamp(ctx, device.ID)
	require.NoError(t, err)
	require.Len(t, list, 1, "不应重复创建正式故障")
}
