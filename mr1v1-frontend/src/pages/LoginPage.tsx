import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Form, Input, Typography, message } from 'antd'
import api from '../api'

const { Title, Text } = Typography

export default function LoginPage() {
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  async function onFinish(values: { username: string; password: string }) {
    setLoading(true)
    try {
      await api.post('/auth/login', values)
      navigate('/', { replace: true })
    } catch {
      message.error('用户名或密码错误')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      background: '#0f172a',
    }}>
      <div style={{
        width: 360,
        background: '#1e293b',
        borderRadius: 16,
        padding: '40px 36px',
        boxShadow: '0 24px 64px rgba(0,0,0,.5)',
      }}>
        <Title level={3} style={{ color: '#f1f5f9', marginBottom: 4 }}>MR1V1 管理后台</Title>
        <Text style={{ color: '#64748b', display: 'block', marginBottom: 28 }}>请登录以继续</Text>
        <Form layout="vertical" onFinish={onFinish} requiredMark={false}>
          <Form.Item name="username" label={<span style={{ color: '#94a3b8' }}>用户名</span>} rules={[{ required: true }]}>
            <Input placeholder="admin" autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label={<span style={{ color: '#94a3b8' }}>密码</span>} rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0, marginTop: 8 }}>
            <Button type="primary" htmlType="submit" block loading={loading}>
              登 录
            </Button>
          </Form.Item>
        </Form>
      </div>
    </div>
  )
}
