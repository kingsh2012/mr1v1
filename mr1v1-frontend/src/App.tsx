import { useState } from 'react'
import { Layout, Menu } from 'antd'
import { RobotOutlined, CloudServerOutlined, TrophyOutlined } from '@ant-design/icons'
import AgentsPage from './pages/AgentsPage'
import RehldsConfigPage from './pages/RehldsConfigPage'
import MatchesPage from './pages/MatchesPage'

const { Sider, Content } = Layout

type PageKey = 'matches' | 'agents' | 'rehlds'

export default function App() {
  const [page, setPage] = useState<PageKey>('matches')

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
        <Content style={{ padding: 24 }}>
          {page === 'matches' && <MatchesPage />}
          {page === 'agents' && <AgentsPage />}
          {page === 'rehlds' && <RehldsConfigPage />}
        </Content>
      </Layout>
    </Layout>
  )
}
