import { useEffect, useState } from 'react'
import {
  Table, Tag, Button, Modal, Form, Input, Space, message, Popconfirm, Typography, Tooltip,
} from 'antd'
import { PlusOutlined, DeleteOutlined, PoweroffOutlined } from '@ant-design/icons'
import axios from 'axios'
import dayjs from 'dayjs'

const { Text } = Typography
const FMT = 'YYYY-MM-DD HH:mm:ss'

interface Match {
  match_id: string
  p0_steamid: string
  p1_steamid: string
  server_name: string
  agent_uuid: string
  port: number
  image: string
  state: string
  create_time: string
  update_time: string
}

const STATE_COLOR: Record<string, string> = {
  creating:   'processing',
  waiting:    'warning',
  playing:    'success',
  finished:   'default',
  terminated: 'orange',
  error:      'error',
}

const STATE_LABEL: Record<string, string> = {
  creating:   '创建中',
  waiting:    '等待玩家',
  playing:    '比赛进行中',
  finished:   '正常结束',
  terminated: '平台终止',
  error:      '异常',
}

const ACTIVE_STATES = new Set(['creating', 'waiting', 'playing'])

export default function MatchesPage() {
  const [matches, setMatches] = useState<Match[]>([])
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [form] = Form.useForm()

  const fetchMatches = async () => {
    setLoading(true)
    try {
      const res = await axios.get<Match[]>('/api/matches')
      setMatches(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchMatches()
    const t = setInterval(fetchMatches, 8000)
    return () => clearInterval(t)
  }, [])

  const handleCreate = async () => {
    const values = await form.validateFields()
    setCreating(true)
    try {
      const res = await axios.post('/api/matches', values)
      message.success(`比赛已创建，match_id: ${res.data.match_id}`)
      setCreateOpen(false)
      form.resetFields()
      fetchMatches()
    } catch (e: any) {
      message.error(e?.response?.data || '创建失败')
    } finally {
      setCreating(false)
    }
  }

  const handleEnd = async (matchID: string) => {
    await axios.post(`/api/matches/${matchID}/end`)
    message.success('已发送结束指令（RCON倒计时后销毁）')
    fetchMatches()
  }

  const handleDestroy = async (matchID: string) => {
    await axios.post(`/api/matches/${matchID}/destroy`)
    message.success('已发送强制销毁指令')
    fetchMatches()
  }

  const columns = [
    {
      title: '状态',
      dataIndex: 'state',
      key: 'state',
      width: 100,
      render: (v: string) => (
        <Tag color={STATE_COLOR[v] ?? 'default'}>{STATE_LABEL[v] ?? v}</Tag>
      ),
    },
    {
      title: 'Match ID',
      dataIndex: 'match_id',
      key: 'match_id',
      ellipsis: true,
    },
    { title: '玩家0 SteamID', dataIndex: 'p0_steamid', key: 'p0_steamid' },
    { title: '玩家1 SteamID', dataIndex: 'p1_steamid', key: 'p1_steamid' },
    { title: '服务器名', dataIndex: 'server_name', key: 'server_name' },
    { title: 'Agent', dataIndex: 'agent_uuid', key: 'agent_uuid', ellipsis: true },
    { title: '端口', dataIndex: 'port', key: 'port', width: 80 },
    {
      title: '创建时间',
      dataIndex: 'create_time',
      key: 'create_time',
      render: (v: string) => dayjs(v).format(FMT),
    },
    {
      title: '更新时间',
      dataIndex: 'update_time',
      key: 'update_time',
      render: (v: string) => (
        <Text type="secondary">{dayjs(v).format(FMT)}</Text>
      ),
    },
    {
      title: '操作',
      key: 'action',
      width: 160,
      render: (_: unknown, r: Match) =>
        ACTIVE_STATES.has(r.state) ? (
          <Space size={4}>
            <Tooltip title="RCON倒计时通知玩家后销毁容器">
              <Popconfirm
                title="结束比赛？将发送RCON倒计时指令后销毁容器。"
                onConfirm={() => handleEnd(r.match_id)}
                okText="确认"
                cancelText="取消"
              >
                <Button size="small" icon={<PoweroffOutlined />}>结束</Button>
              </Popconfirm>
            </Tooltip>
            <Tooltip title="立即强制停止容器，不通知玩家">
              <Popconfirm
                title="强制销毁容器？玩家不会收到通知。"
                onConfirm={() => handleDestroy(r.match_id)}
                okText="确认"
                cancelText="取消"
              >
                <Button size="small" danger icon={<DeleteOutlined />}>销毁</Button>
              </Popconfirm>
            </Tooltip>
          </Space>
        ) : null,
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          创建比赛
        </Button>
        <Button onClick={fetchMatches}>刷新</Button>
      </Space>

      <Table
        rowKey="match_id"
        loading={loading}
        dataSource={matches}
        columns={columns}
        scroll={{ x: 'max-content' }}
        pagination={{ pageSize: 20 }}
      />

      <Modal
        title="创建比赛"
        open={createOpen}
        onOk={handleCreate}
        confirmLoading={creating}
        onCancel={() => { setCreateOpen(false); form.resetFields() }}
        okText="创建"
        cancelText="取消"
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="p0_steamid"
            label="玩家0 SteamID"
            rules={[{ required: true, message: '请输入玩家0 SteamID' }]}
          >
            <Input placeholder="STEAM_0:0:12345678" />
          </Form.Item>
          <Form.Item
            name="p1_steamid"
            label="玩家1 SteamID"
            rules={[{ required: true, message: '请输入玩家1 SteamID' }]}
          >
            <Input placeholder="STEAM_0:0:87654321" />
          </Form.Item>
          <Form.Item name="server_name" label="服务器名（可选）">
            <Input placeholder="留空自动生成" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
