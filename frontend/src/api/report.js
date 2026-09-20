import request from './request'

// 市民报修接口。
export const reportApi = {
  list: (params) => request.get('/reports', { params }),
  detail: (id) => request.get(`/reports/${id}`),
  submit: (data) => request.post('/reports', data),
  verify: (id, data) => request.post(`/reports/${id}/verify`, data),
  invalid: (id, data) => request.post(`/reports/${id}/invalid`, data),
  summary: () => request.get('/reports/summary'),
  meta: () => request.get('/reports/meta'),
}
