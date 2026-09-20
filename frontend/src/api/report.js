import request from './request'

// 市民报修接口。
export const reportApi = {
  list: (params) => request.get('/reports', { params }),
  detail: (id) => request.get(`/reports/${id}`),
  create: (data) => request.post('/reports', data),
  confirm: (id, data) => request.post(`/reports/${id}/confirm`, data),
  reject: (id, data) => request.post(`/reports/${id}/reject`, data),
  meta: () => request.get('/reports/meta'),
}
