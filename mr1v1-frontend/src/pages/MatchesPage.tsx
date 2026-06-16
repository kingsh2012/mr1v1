import { useEffect, useState, useRef } from 'react'
import {
  Table, Tag, Button, Modal, Form, Input, Space, message, Popconfirm, Typography, Tooltip, Spin,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
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

interface OpLog {
  id: number
  match_id: string
  actor: string
  action: string
  detail: string
  created_at: string
}

const ACTOR_COLOR: Record<string, string> = {
  platform: 'blue',
  agent: 'green',
  game: 'orange',
}

const ACTION_LABEL: Record<string, string> = {
  create_dispatched:  '下发创建指令',
  container_started:  '容器启动',
  container_error:    '容器异常',
  container_stopped:  '容器停止',
  end_dispatched:     '下发结束指令',
  destroy_dispatched: '下发销毁指令',
  match_started:      '比赛开始',
  match_ended:        '比赛结束',
}

const LOG_COLUMNS: ColumnsType<OpLog> = [
  {
    title: '时间',
    dataIndex: 'created_at',
    key: 'created_at',
    width: 180,
    render: (v: string) => dayjs(v).format(FMT),
  },
  {
    title: '来源',
    dataIndex: 'actor',
    key: 'actor',
    width: 90,
    render: (v: string) => <Tag color={ACTOR_COLOR[v] ?? 'default'}>{v}</Tag>,
  },
  {
    title: '操作',
    dataIndex: 'action',
    key: 'action',
    width: 160,
    render: (v: string) => ACTION_LABEL[v] ?? v,
  },
  {
    title: '详情',
    dataIndex: 'detail',
    key: 'detail',
    render: (v: string) => <Text code style={{ fontSize: 12 }}>{v}</Text>,
  },
]

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
  const [logsCache, setLogsCache] = useState<Record<string, OpLog[]>>({})
  const [logsLoading, setLogsLoading] = useState<Set<string>>(new Set())
  const logsFetchedRef = useRef<Set<string>>(new Set())

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

  const fetchLogs = async (matchID: string) => {
    if (logsFetchedRef.current.has(matchID)) return
    logsFetchedRef.current.add(matchID)
    setLogsLoading(prev => new Set(prev).add(matchID))
    try {
      const res = await axios.get<OpLog[]>(`/api/matches/${matchID}/logs`)
      setLogsCache(prev => ({ ...prev, [matchID]: res.data ?? [] }))
    } finally {
      setLogsLoading(prev => { const s = new Set(prev); s.delete(matchID); return s })
    }
  }

  const handleExpand = (expanded: boolean, record: Match) => {
    if (expanded) fetchLogs(record.match_id)
  }

  const expandedRowRender = (record: Match) => {
    const logs = logsCache[record.match_id]
    if (logsLoading.has(record.match_id) || !logs) {
      return <Spin style={{ padding: '16px 0' }} />
    }
    return (
      <Table<OpLog>
        rowKey="id"
        dataSource={logs}
        columns={LOG_COLUMNS}
        pagination={false}
        size="small"
        style={{ margin: '0 48px' }}
        locale={{ emptyText: '暂无操作记录' }}
      />
    )
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
        expandable={{
          expandedRowRender,
          onExpand: handleExpand,
        }}
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
