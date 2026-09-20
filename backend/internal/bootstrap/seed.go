package bootstrap

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/repair"
	"streetlight/internal/modules/report"
)

const hour = time.Hour

// seedRepairCase 描述一条演示维修记录, 时间字段为距当前时刻的时长。
type seedRepairCase struct {
	repairman   string
	team        string
	startedAgo  time.Duration
	finishedAgo time.Duration // 为 0 表示仍在维修中
	result      string
	content     string
	materials   string
	cost        float64
}

// seedFaultCase 描述一条演示故障记录。
type seedFaultCase struct {
	lampIndex   int
	faultType   string
	level       string
	source      string
	description string
	reporter    string
	reportedAgo time.Duration
	status      string
	closed      bool
	repairs     []seedRepairCase
}

// seed 在数据库为空时写入演示数据, 便于启动后立即体验完整业务流程。
func seed(db *gorm.DB) error {
	var count int64
	if err := db.Model(&lamp.Lamp{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now()
	lamps := buildSeedLamps(now)
	if err := db.Create(&lamps).Error; err != nil {
		return fmt.Errorf("写入路灯台账演示数据失败: %w", err)
	}

	cases := seedFaultCases()
	faults := make([]fault.Fault, 0, len(cases))
	sequences := map[string]int{}
	for _, item := range cases {
		device := lamps[item.lampIndex]
		reportedAt := now.Add(-item.reportedAgo)
		prefix := "GD" + reportedAt.Format("20060102")
		sequences[prefix]++

		faults = append(faults, fault.Fault{
			FaultNo:       fmt.Sprintf("%s%04d", prefix, sequences[prefix]),
			LampID:        device.ID,
			LampCode:      device.Code,
			RoadName:      device.RoadName,
			FaultType:     item.faultType,
			FaultLevel:    item.level,
			Source:        item.source,
			Description:   item.description,
			Reporter:      item.reporter,
			ReporterPhone: "13800001234",
			ReportedAt:    reportedAt,
			Status:        item.status,
		})
	}
	if err := db.Create(&faults).Error; err != nil {
		return fmt.Errorf("写入故障演示数据失败: %w", err)
	}

	repairs := make([]repair.Repair, 0)
	repairRanges := make([][2]int, len(cases))
	sequences = map[string]int{}
	for index, item := range cases {
		device := lamps[item.lampIndex]
		start := len(repairs)
		for _, expect := range item.repairs {
			startedAt := now.Add(-expect.startedAgo)
			prefix := "WX" + startedAt.Format("20060102")
			sequences[prefix]++

			record := repair.Repair{
				RepairNo:     fmt.Sprintf("%s%04d", prefix, sequences[prefix]),
				FaultID:      faults[index].ID,
				FaultNo:      faults[index].FaultNo,
				LampID:       device.ID,
				LampCode:     device.Code,
				Repairman:    expect.repairman,
				RepairTeam:   expect.team,
				ContactPhone: "13900005678",
				StartedAt:    startedAt,
				Status:       repair.StatusOngoing,
				Content:      expect.content,
				Materials:    expect.materials,
				Cost:         expect.cost,
			}
			if expect.finishedAgo > 0 {
				finishedAt := now.Add(-expect.finishedAgo)
				record.FinishedAt = &finishedAt
				record.Status = repair.StatusFinished
				record.Result = expect.result
			}
			repairs = append(repairs, record)
		}
		repairRanges[index] = [2]int{start, len(repairs)}
	}
	if len(repairs) > 0 {
		if err := db.Create(&repairs).Error; err != nil {
			return fmt.Errorf("写入维修记录演示数据失败: %w", err)
		}
	}

	for index, item := range cases {
		start, end := repairRanges[index][0], repairRanges[index][1]
		columns := map[string]any{"repair_count": end - start}
		if end > start {
			columns["latest_repair_id"] = repairs[end-1].ID
		}
		if item.closed {
			closedAt := now.Add(-item.reportedAgo / 2)
			columns["closed_at"] = closedAt
			columns["close_remark"] = "现场已恢复照明并复核确认, 故障闭环"
		}
		if err := db.Model(&fault.Fault{}).Where("id = ?", faults[index].ID).Updates(columns).Error; err != nil {
			return fmt.Errorf("回填故障演示数据失败: %w", err)
		}
	}

	if err := syncSeedLampStatus(db, faults, lamps); err != nil {
		return err
	}

	if err := seedReports(db, now, lamps, faults); err != nil {
		return err
	}

	slog.Info("演示数据初始化完成",
		"路灯", len(lamps),
		"故障", len(faults),
		"维修记录", len(repairs),
	)
	return nil
}

// seedReportCase 描述一条演示市民报修, resolvedFault 为转故障对应的演示故障下标(-1 表示无)。
type seedReportCase struct {
	lampIndex     int    // -1 表示未关联台账
	unknownCode   string // 非空时模拟市民填写了台账中不存在的编号
	roadName      string
	locationDesc  string
	faultType     string
	content       string
	reporter      string
	phone         string
	reportedAgo   time.Duration
	status        string
	resolvedFault int // 核实有效: 对应 faults 下标
	invalidReason string
	verifiedAgo   time.Duration
	verifiedBy    string
	mergedInto    int // merged 状态: 主报修单在本批数据中的下标
}

// seedReports 写入市民报修演示数据, 覆盖待核实 / 有效 / 无效 / 重复合并四种状态。
func seedReports(db *gorm.DB, now time.Time, lamps []lamp.Lamp, faults []fault.Fault) error {
	cases := seedReportCases()
	reports := make([]report.Report, 0, len(cases))
	sequences := map[string]int{}

	for _, item := range cases {
		reportedAt := now.Add(-item.reportedAgo)
		prefix := "BX" + reportedAt.Format("20060102")
		sequences[prefix]++

		entity := report.Report{
			ReportNo:      fmt.Sprintf("%s%04d", prefix, sequences[prefix]),
			FaultType:     item.faultType,
			Content:       item.content,
			Reporter:      item.reporter,
			ReporterPhone: item.phone,
			RoadName:      item.roadName,
			LocationDesc:  item.locationDesc,
			ReportedAt:    reportedAt,
			Status:        item.status,
		}

		switch {
		case item.unknownCode != "":
			entity.LampCode = item.unknownCode
		case item.lampIndex >= 0:
			device := lamps[item.lampIndex]
			entity.LampID = &device.ID
			entity.LampCode = device.Code
			if entity.RoadName == "" {
				entity.RoadName = device.RoadName
			}
		}

		if item.resolvedFault >= 0 {
			target := faults[item.resolvedFault]
			entity.FaultID = &target.ID
			entity.FaultNo = target.FaultNo
			entity.LampID = &target.LampID
			entity.LampCode = target.LampCode
			entity.RoadName = target.RoadName
		}
		if item.invalidReason != "" {
			entity.InvalidReason = item.invalidReason
		}
		if item.verifiedAgo > 0 {
			verifiedAt := now.Add(-item.verifiedAgo)
			entity.VerifiedAt = &verifiedAt
			entity.VerifiedBy = item.verifiedBy
		}
		reports = append(reports, entity)
	}

	// 第二轮回填合并关系(依赖主报修单已生成的 ID 与单号)。
	for index, item := range cases {
		if item.mergedInto < 0 {
			continue
		}
		master := reports[item.mergedInto]
		reports[index].MergedIntoID = &master.ID
		reports[index].MergedIntoNo = master.ReportNo
		if master.FaultID != nil {
			reports[index].FaultID = master.FaultID
			reports[index].FaultNo = master.FaultNo
		}
	}

	if err := db.Create(&reports).Error; err != nil {
		return fmt.Errorf("写入市民报修演示数据失败: %w", err)
	}

	// 累加主报修单的重复上报计数。
	masterCounts := map[int]int{}
	for _, item := range cases {
		if item.mergedInto >= 0 {
			masterCounts[item.mergedInto]++
		}
	}
	for masterIndex, count := range masterCounts {
		if err := db.Model(&report.Report{}).
			Where("id = ?", reports[masterIndex].ID).
			Update("duplicate_count", count).Error; err != nil {
			return fmt.Errorf("回填报修重复次数失败: %w", err)
		}
	}

	slog.Info("市民报修演示数据初始化完成", "报修单", len(reports))
	return nil
}

// seedReportCases 返回市民报修演示场景。
// 注意: resolvedFault 的下标与 seedFaultCases 返回顺序一一对应。
func seedReportCases() []seedReportCase {
	return []seedReportCase{
		// 0: 待核实, 市民按灯杆编号报修(台账中存在, 自动定位)。
		{
			lampIndex: 14, faultType: "灯不亮",
			content:  "灯杆上牌子写着 LD-00015, 连着两天晚上都不亮, 旁边的灯是好的",
			reporter: "周敏", phone: "13611112222",
			reportedAgo: 2 * hour, status: report.StatusPending,
			resolvedFault: -1,
		},
		// 1: 待核实, 市民只描述了位置, 没有提供编号。
		{
			lampIndex: -1, roadName: "解放路", locationDesc: "解放路与迎宾大道交叉口东南角斑马线旁",
			faultType: "灯光闪烁",
			content:   "交叉口那根杆子的灯一直在闪, 晃眼睛, 过斑马线的时候看不清路",
			reporter:  "吴晓", phone: "13533334444",
			reportedAgo: 5 * hour, status: report.StatusPending,
			resolvedFault: -1,
		},
		// 2: 待核实, 市民填写的编号在台账中查不到, 保留原文转人工。
		{
			unknownCode: "LD-9-88", roadName: "滨江路",
			locationDesc: "滨江路健身步道入口附近", faultType: "灯杆倾斜",
			content:  "牌子上编号有点花, 看着像 LD-9-88, 杆子往步道这边歪了",
			reporter: "郑海", phone: "13755556666",
			reportedAgo: 90 * time.Minute, status: report.StatusPending,
			resolvedFault: -1,
		},
		// 3: 核实有效, 对应 seedFaultCases 下标 1(建设大道, 市民来源)。
		{
			lampIndex: 1, faultType: "灯光闪烁",
			content:  "建设大道这段路灯一闪一闪的, 晚上开车经过特别影响视线",
			reporter: "李梅", phone: "13800001234",
			reportedAgo: 31 * hour, status: report.StatusConfirmed,
			resolvedFault: 1, verifiedAgo: 29 * hour, verifiedBy: "值班核实员陈晨",
		},
		// 4: 与 3 同一路灯的重复上报, 已并入 3, 并随主单关联同一故障。
		{
			lampIndex: 1, faultType: "灯光闪烁",
			content:  "补充一下, 是建设大道靠近公交港湾那根, 还是一直闪",
			reporter: "匿名市民", phone: "",
			reportedAgo: 30 * hour, status: report.StatusMerged,
			resolvedFault: 1, mergedInto: 3,
		},
		// 5: 核实有效, 对应 seedFaultCases 下标 12(中山路, 市民来源, 待处理)。
		{
			lampIndex: 12, faultType: "灯不亮",
			content:  "我们小区门口这根灯杆连续两晚不亮, 老人晚上出门不安全",
			reporter: "李梅", phone: "13911110000",
			reportedAgo: 9 * hour, status: report.StatusConfirmed,
			resolvedFault: 12, verifiedAgo: 7 * hour, verifiedBy: "值班核实员陈晨",
		},
		// 6: 核实无效: 非市政路灯。
		{
			lampIndex: -1, roadName: "学院路", locationDesc: "学院路文创园停车场内部",
			faultType: "灯不亮",
			content:   "文创园停车场里有根路灯不亮了",
			reporter:  "冯磊", phone: "13677778888",
			reportedAgo: 20 * hour, status: report.StatusInvalid,
			resolvedFault: -1, invalidReason: "现场核实为园区自建景观照明, 非市政路灯, 已转文创园物业处理",
			verifiedAgo: 18 * hour, verifiedBy: "值班核实员陈晨",
		},
		// 7: 核实无效: 重复报修且现场正常(误报)。
		{
			lampIndex: 16, faultType: "灯具常亮",
			content:  "大白天的这盏灯还亮着, 是不是坏了",
			reporter: "何丽", phone: "13599990000",
			reportedAgo: 26 * hour, status: report.StatusInvalid,
			resolvedFault: -1, invalidReason: "现场核实为监控中心远程试灯, 灯具运行正常, 已向报修人解释",
			verifiedAgo: 25 * hour, verifiedBy: "监控中心",
		},
		// 8: 待核实主报修单, 带 1 条已合并的重复上报。
		{
			lampIndex: 17, faultType: "灯不亮",
			content:  "学院路靠近食堂这一侧的灯杆不亮",
			reporter: "许洋", phone: "13822223333",
			reportedAgo: 4 * hour, status: report.StatusPending,
			resolvedFault: -1,
		},
		// 9: 并入 8 的重复上报。
		{
			lampIndex: 17, faultType: "灯不亮",
			content:  "食堂门口那根黑了的灯杆, 我也报一下",
			reporter: "学生家长", phone: "",
			reportedAgo: 3 * hour, status: report.StatusMerged,
			resolvedFault: -1, mergedInto: 8,
		},
	}
}

// buildSeedLamps 生成 6 条道路共 30 盏路灯的台账数据。
func buildSeedLamps(now time.Time) []lamp.Lamp {
	roads := []struct {
		name     string
		district string
	}{
		{"中山路", "城东区"},
		{"建设大道", "城东区"},
		{"园区北路", "高新区"},
		{"滨江路", "城西区"},
		{"解放路", "城西区"},
		{"学院路", "高新区"},
	}
	types := []string{lamp.LampTypeLED, lamp.LampTypeSodium, lamp.LampTypeMetal, lamp.LampTypeSolar}
	powers := []int{60, 100, 150, 200, 250}

	result := make([]lamp.Lamp, 0, len(roads)*5)
	index := 0
	for roadIndex, road := range roads {
		for position := 0; position < 5; position++ {
			index++
			installDate := time.Date(
				now.Year()-1-roadIndex%2, time.Month(1+position), 15,
				0, 0, 0, 0, now.Location(),
			)
			result = append(result, lamp.Lamp{
				Code:        fmt.Sprintf("LD-%05d", index),
				Name:        fmt.Sprintf("%s%d号灯杆", road.name, position+1),
				RoadName:    road.name,
				District:    road.district,
				Address:     fmt.Sprintf("%s%d号", road.name, (position+1)*100),
				Longitude:   116.40 + float64(roadIndex)*0.01 + float64(position)*0.001,
				Latitude:    39.90 + float64(roadIndex)*0.01 + float64(position)*0.001,
				LampType:    types[(roadIndex+position)%len(types)],
				Power:       powers[(roadIndex+position)%len(powers)],
				PoleHeight:  8 + float64(position%3)*1.5,
				RunStatus:   lamp.RunStatusNormal,
				InstallDate: &installDate,
				Remark:      "演示数据",
			})
		}
	}
	return result
}

// seedFaultCases 返回演示故障场景, 覆盖待处理 / 维修中 / 已修复 / 已关闭四种状态。
func seedFaultCases() []seedFaultCase {
	return []seedFaultCase{
		{
			lampIndex: 0, faultType: "灯不亮", level: fault.LevelHigh, source: fault.SourceInspection,
			description: "夜间巡检发现整灯不亮, 相邻灯杆照明正常", reporter: "王建国",
			reportedAgo: 6 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 1, faultType: "灯光闪烁", level: fault.LevelNormal, source: fault.SourceCitizen,
			description: "市民来电反馈该路段灯光持续闪烁, 影响行车视线", reporter: "李梅",
			reportedAgo: 30 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 2, faultType: "灯具常亮", level: fault.LevelLow, source: fault.SourceMonitoring,
			description: "控制平台监测到该灯具白天仍处于点亮状态", reporter: "监控中心",
			reportedAgo: 40 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 3, faultType: "线路故障", level: fault.LevelUrgent, source: fault.SourceInspection,
			description: "电缆接头烧蚀, 该支路 3 盏路灯同时失电", reporter: "赵强",
			reportedAgo: 3 * hour, status: fault.StatusProcessing,
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 2 * hour,
					content: "已到场排查, 确认电缆接头烧蚀, 正在更换接头", materials: "防水接头 2 套",
					cost: 180,
				},
			},
		},
		{
			lampIndex: 4, faultType: "控制箱故障", level: fault.LevelHigh, source: fault.SourceMonitoring,
			description: "控制箱通讯中断, 远程无法下发开关灯指令", reporter: "监控中心",
			reportedAgo: 5 * hour, status: fault.StatusProcessing,
			repairs: []seedRepairCase{
				{
					repairman: "陈鹏", team: "市政照明二班", startedAgo: 1 * hour,
					content: "检查控制箱通讯模块, 疑似模块损坏", materials: "通讯模块 1 个", cost: 260,
				},
			},
		},
		{
			lampIndex: 5, faultType: "灯不亮", level: fault.LevelNormal, source: fault.SourceCitizen,
			description: "该灯杆夜间不亮, 疑似驱动电源损坏", reporter: "张伟",
			reportedAgo: 26 * hour, status: fault.StatusRepaired,
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 25 * hour, finishedAgo: 20 * hour,
					result: repair.ResultFixed, content: "更换 LED 驱动电源并复测绝缘",
					materials: "驱动电源 1 个", cost: 220,
				},
			},
		},
		{
			lampIndex: 6, faultType: "灯具破损", level: fault.LevelNormal, source: fault.SourceInspection,
			description: "灯具外罩被外物击碎, 需整体更换灯头", reporter: "王建国",
			reportedAgo: 50 * hour, status: fault.StatusRepaired,
			repairs: []seedRepairCase{
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 48 * hour, finishedAgo: 44 * hour,
					result: repair.ResultFixed, content: "更换灯头总成并密封处理",
					materials: "LED 灯头 1 套", cost: 460,
				},
			},
		},
		{
			lampIndex: 7, faultType: "灯杆倾斜", level: fault.LevelHigh, source: fault.SourceCitizen,
			description: "车辆剐蹭导致灯杆倾斜约 8 度, 存在安全隐患", reporter: "孙倩",
			reportedAgo: 72 * hour, status: fault.StatusClosed, closed: true,
			repairs: []seedRepairCase{
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 70 * hour, finishedAgo: 60 * hour,
					result: repair.ResultFixed, content: "重新浇筑基础法兰并校正灯杆垂直度",
					materials: "基础法兰 1 套", cost: 980,
				},
			},
		},
		{
			lampIndex: 8, faultType: "灯不亮", level: fault.LevelNormal, source: fault.SourceMonitoring,
			description: "平台告警该灯杆回路电流为零", reporter: "监控中心",
			reportedAgo: 96 * hour, status: fault.StatusClosed, closed: true,
			repairs: []seedRepairCase{
				{
					repairman: "陈鹏", team: "市政照明一班", startedAgo: 94 * hour, finishedAgo: 90 * hour,
					result: repair.ResultFixed, content: "更换熔断器并紧固接线端子",
					materials: "熔断器 1 只", cost: 60,
				},
			},
		},
		{
			lampIndex: 9, faultType: "灯光闪烁", level: fault.LevelNormal, source: fault.SourceInspection,
			description: "灯具有明显频闪, 疑似驱动电源老化", reporter: "王建国",
			reportedAgo: 120 * hour, status: fault.StatusClosed, closed: true,
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 118 * hour, finishedAgo: 112 * hour,
					result: repair.ResultFixed, content: "更换驱动电源, 频闪消除",
					materials: "驱动电源 1 个", cost: 220,
				},
			},
		},
		{
			lampIndex: 10, faultType: "线路故障", level: fault.LevelUrgent, source: fault.SourceInspection,
			description: "地埋电缆绝缘老化, 绝缘电阻不达标", reporter: "赵强",
			reportedAgo: 150 * hour, status: fault.StatusClosed, closed: true,
			repairs: []seedRepairCase{
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 148 * hour, finishedAgo: 140 * hour,
					result: repair.ResultPendingParts, content: "检测确认需整段更换电缆, 等待物料到场",
					materials: "电缆 40 米", cost: 120,
				},
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 130 * hour, finishedAgo: 120 * hour,
					result: repair.ResultFixed, content: "更换老化电缆并做绝缘测试, 测试合格",
					materials: "电缆 40 米, 热缩管 4 套", cost: 1560,
				},
			},
		},
		{
			lampIndex: 11, faultType: "灯具常亮", level: fault.LevelLow, source: fault.SourceOther,
			description: "白天常亮, 疑似接触器粘连", reporter: "社区网格员",
			reportedAgo: 10 * hour, status: fault.StatusRepaired,
			repairs: []seedRepairCase{
				{
					repairman: "陈鹏", team: "市政照明二班", startedAgo: 9 * hour, finishedAgo: 7 * hour,
					result: repair.ResultFixed, content: "更换接触器, 恢复远程开关灯控制",
					materials: "交流接触器 1 只", cost: 150,
				},
			},
		},
		{
			lampIndex: 12, faultType: "灯不亮", level: fault.LevelNormal, source: fault.SourceCitizen,
			description: "居民反映该灯杆连续两晚不亮", reporter: "李梅",
			reportedAgo: 8 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 13, faultType: "控制箱故障", level: fault.LevelHigh, source: fault.SourceMonitoring,
			description: "控制箱电流异常波动, 疑似内部接触不良", reporter: "监控中心",
			reportedAgo: 15 * hour, status: fault.StatusProcessing,
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 12 * hour,
					content: "拆检控制箱, 正在逐路测量回路电流", materials: "万用表、绝缘胶带", cost: 40,
				},
			},
		},
	}
}

// syncSeedLampStatus 依据演示故障数据回填路灯运行状态, 保证台账与故障一致。
func syncSeedLampStatus(db *gorm.DB, faults []fault.Fault, lamps []lamp.Lamp) error {
	statuses := map[uint]string{}
	for _, item := range faults {
		switch item.Status {
		case fault.StatusProcessing:
			statuses[item.LampID] = lamp.RunStatusMaintenance
		case fault.StatusPending:
			if statuses[item.LampID] != lamp.RunStatusMaintenance {
				statuses[item.LampID] = lamp.RunStatusFault
			}
		}
	}

	for index, device := range lamps {
		if index >= 18 && index%5 == 4 {
			statuses[device.ID] = lamp.RunStatusOffline
		}
	}

	for lampID, status := range statuses {
		err := db.Model(&lamp.Lamp{}).Where("id = ?", lampID).Update("run_status", status).Error
		if err != nil {
			return fmt.Errorf("同步演示路灯运行状态失败: %w", err)
		}
	}
	return nil
}
