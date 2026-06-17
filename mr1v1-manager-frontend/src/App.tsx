import { BrowserRouter, Routes, Route, useNavigate } from 'react-router-dom'
import { Layout, Menu, Button } from 'antd'
import { RobotOutlined, CloudServerOutlined, TrophyOutlined, LogoutOutlined } from '@ant-design/icons'
import { useState } from 'react'
import AgentsPage from './pages/AgentsPage'
import RehldsConfigPage from './pages/RehldsConfigPage'
import MatchesPage from './pages/MatchesPage'
import LoginPage from './pages/LoginPage'
import api from './api'

const { Sider, Content, Header } = Layout

type PageKey = 'matches' | 'agents' | 'rehlds'

function MainLayout() {
  const [page, setPage] = useState<PageKey>('matches')
  const navigate = useNavigate()

  async function logout() {
    await api.post('/auth/logout').catch(() => {})
    navigate('/login', { replace: true })
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider>
        <div style={{ color: '#fff', padding: '16px', fontWeight: 'bold', fontSize: 16 }}>
          MR1V1 控制台
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[page]}
          onClick={({ key }) => setPage(key as PageKey)}
          items={[
            { key: 'matches', icon: <TrophyOutlined />, label: '比赛管理' },
            { key: 'agents', icon: <RobotOutlined />, label: 'Agent 管理' },
            { key: 'rehlds', icon: <CloudServerOutlined />, label: 'Rehlds 镜像' },
          ]}
        />
      </Sider>
      <Layout>
        <Header style={{ background: '#fff', padding: '0 24px', display: 'flex', alignItems: 'center', justifyContent: 'flex-end' }}>
          <Button icon={<LogoutOutlined />} onClick={logout} type="text">退出登录</Button>
        </Header>
        <Content style={{ padding: 24 }}>
          {page === 'matches' && <MatchesPage />}
          {page === 'agents' && <AgentsPage />}
          {page === 'rehlds' && <RehldsConfigPage />}
        </Content>
      </Layout>
    </Layout>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/*" element={<MainLayout />} />
      </Routes>
    </BrowserRouter>
  )
}
