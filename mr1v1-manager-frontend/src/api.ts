import axios from 'axios'

const api = axios.create({ baseURL: '/api/manager' })

api.interceptors.response.use(
  res => {
    // 后端统一响应格式 { code: 0, data: ... }，自动解包成 res.data
    if (res.data && typeof res.data === 'object' && 'code' in res.data && 'data' in res.data) {
      res.data = res.data.data
    }
    return res
  },
  err => {
    if (err.response?.status === 401) {
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

export default api
