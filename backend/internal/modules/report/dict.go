package report

var statusLabels = map[string]string{
	StatusPending:   "待核实",
	StatusConfirmed: "核实有效",
	StatusInvalid:   "核实无效",
	StatusMerged:    "重复合并",
}

var locateMethodLabels = map[string]string{
	LocateByCode: "灯杆编号定位",
	LocateByDesc: "位置描述定位",
}

// StatusLabel 返回报修状态的中文名称, 未知取值原样返回。
func StatusLabel(status string) string {
	if label, ok := statusLabels[status]; ok {
		return label
	}
	return status
}

// LocateMethodLabel 返回定位方式的中文名称。
func LocateMethodLabel(method string) string {
	if label, ok := locateMethodLabels[method]; ok {
		return label
	}
	return method
}
