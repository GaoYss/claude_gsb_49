package report

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 市民报修模块: 受理市民依据灯杆编号或位置描述提交的报修,
// 经人工核实后转为正式故障或判为无效, 短时间内的重复上报自动合并。
type Module struct {
	repository *Repository
	service    *Service
	handler    *Handler
}

// New 构造市民报修模块, lamps 提供路灯定位能力, faults 提供转正故障能力。
func New(db *gorm.DB, lamps LampPort, faults FaultPort) *Module {
	repository := NewRepository(db)
	service := NewService(repository, lamps, faults)
	return &Module{
		repository: repository,
		service:    service,
		handler:    NewHandler(service),
	}
}

// Service 暴露业务服务, 供其它模块装配使用。
func (m *Module) Service() *Service { return m.service }

// Repository 暴露仓储, 供状态查询模块装配只读视图。
func (m *Module) Repository() *Repository { return m.repository }

// Name 实现 module.Module 接口。
func (m *Module) Name() string { return "市民报修" }

// Models 实现 module.Module 接口。
func (m *Module) Models() []any { return []any{&Report{}} }

// RegisterRoutes 实现 module.Module 接口。
func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	group := api.Group("/reports")
	{
		group.GET("", m.handler.List)
		group.POST("", m.handler.Submit)
		group.GET("/summary", m.handler.Summary)
		group.GET("/meta", m.handler.Metadata)
		group.GET("/:id", m.handler.Get)
		group.POST("/:id/verify", m.handler.Verify)
		group.POST("/:id/invalid", m.handler.Invalid)
	}
}
