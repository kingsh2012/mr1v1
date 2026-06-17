import { BrowserRouter, Routes, Route, Navigate, useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu, Button } from 'antd'
import {
  RobotOutlined, CloudServerOutlined, TrophyOutlined, LogoutOutlined,
  HomeOutlined, TeamOutlined, IdcardOutlined,
} from '@ant-design/icons'
import type { ReactNode } from 'react'
import AgentsPage from './pages/AgentsPage'
import RehldsConfigPage from './pages/RehldsConfigPage'
import MatchesPage from './pages/MatchesPage'
import WxRoomsPage from './pages/WxRoomsPage'
import WxUsersPage from './pages/WxUsersPage'
import LegacyPlayersPage from './pages/LegacyPlayersPage'
import LoginPage from './pages/LoginPage'
import api from './api'

const { Sider, Content, Header } = Layout

interface PageRoute {
  key: string
  path: string // 不带前导斜杠，用于内层 <Route path>
  label: string
  icon: ReactNode
  element: ReactNode
}

const pageRoutes: PageRoute[] = [
  { key: 'matches', path: 'matches', label: '比赛管理', icon: <TrophyOutlined />, element: <MatchesPage /> },
  { key: 'agents', path: 'agents', label: 'Agent 管理', icon: <RobotOutlined />, element: <AgentsPage /> },
  { key: 'rehlds', path: 'rehlds', label: 'Rehlds 镜像', icon: <CloudServerOutlined />, element: <RehldsConfigPage /> },
  { key: 'wx-rooms', path: 'wx-rooms', label: '房间列表', icon: <HomeOutlined />, element: <WxRoomsPage /> },
  { key: 'wx-users', path: 'wx-users', label: '微信用户', icon: <TeamOutlined />, element: <WxUsersPage /> },
  { key: 'legacy-players', path: 'legacy-players', label: '老玩家列表', icon: <IdcardOutlined />, element: <LegacyPlayersPage /> },
]

function MainLayout() {
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = pageRoutes.find(r => location.pathname === `/${r.path}`)?.key ?? 'matches'

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
          selectedKeys={[selectedKey]}
          onClick={({ key }) => {
            const target = pageRoutes.find(r => r.key === key)
            if (target) navigate(`/${target.path}`)
          }}
          items={pageRoutes.map(r => ({ key: r.key, icon: r.icon, label: r.label }))}
        />
      </Sider>
      <Layout>
        <Header style={{ background: '#fff', padding: '0 24px', display: 'flex', alignItems: 'center', justifyContent: 'flex-end' }}>
          <Button icon={<LogoutOutlined />} onClick={logout} type="text">退出登录</Button>
        </Header>
        <Content style={{ padding: 24 }}>
          <Routes>
            <Route path="/" element={<Navigate to="/matches" replace />} />
            {pageRoutes.map(r => <Route key={r.key} path={r.path} element={r.element} />)}
            <Route path="*" element={<Navigate to="/matches" replace />} />
          </Routes>
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
