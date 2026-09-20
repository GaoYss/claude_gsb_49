package report

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 市民报修模块, 负责报修受理、重复合并与核实流转。
type Module struct {
	repository *Repository
	service    *Service
	handler    *Handler
}

// New 构造市民报修模块, lamps 用于解析灯杆编号, faults 用于核实后登记正式故障。
func New(db *gorm.DB, lamps LampPort, faults FaultCreator) *Module {
	repository := NewRepository(db)
	service := NewService(repository, lamps, faults)
	return &Module{
		repository: repository,
		service:    service,
		handler:    NewHandler(service),
	}
}

// Repository 暴露仓储, 供状态查询模块装配只读统计。
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
		group.POST("", m.handler.Create)
		group.GET("/meta", m.handler.Metadata)
		group.GET("/:id", m.handler.Get)
		group.POST("/:id/confirm", m.handler.Confirm)
		group.POST("/:id/reject", m.handler.Reject)
	}
}
