package report

var statusLabels = map[string]string{
	StatusPending:   "待核实",
	StatusConfirmed: "已转故障",
	StatusInvalid:   "无效",
	StatusMerged:    "已合并",
}

// StatusLabel 返回报修状态的中文名称, 未知取值原样返回。
func StatusLabel(status string) string {
	if label, ok := statusLabels[status]; ok {
		return label
	}
	return status
}
