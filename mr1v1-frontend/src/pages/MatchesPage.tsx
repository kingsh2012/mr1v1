import { useEffect, useState, useRef } from 'react'
import {
  Table, Tag, Button, Modal, Form, Input, Space, message, Popconfirm,
  Typography, Tooltip, Spin, Tabs, Descriptions, Alert,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, DeleteOutlined, PoweroffOutlined, ReloadOutlined } from '@ant-design/icons'
import axios from 'axios'
import dayjs from 'dayjs'

const { Text } = Typography
const FMT = 'YYYY-MM-DD HH:mm:ss'

// ── interfaces ────────────────────────────────────────────────────────────────

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

interface ServerInfo {
  protocol: number
  name: string
  map: string
  folder: string
  game: string
  app_id: number
  players: number
  max_players: number
  bots: number
  server_type: string
  server_os: string
  visibility: boolean
  vac: boolean
  version: string
}

interface Player {
  index: number
  name: string
  score: number
  duration: number
}

interface PlayerInfo {
  count: number
  players: Player[] | null
}

interface RulesInfo {
  count: number
  rules: Record<string, string>
}

interface ServerQuery {
  info?: ServerInfo
  players?: PlayerInfo
  rules?: RulesInfo
  info_error?: string
  players_error?: string
  rules_error?: string
}

// ── constants ─────────────────────────────────────────────────────────────────

const ACTOR_COLOR: Record<string, string> = {
  platform: 'blue',
  agent: 'green',
  game: 'orange',
}

const ACTION_LABEL: Record<string, string> = {
  create_dispatched: '下发创建指令',
  container_started: '容器启动',
  container_error: '容器异常',
  container_stopped: '容器停止',
  end_dispatched: '下发结束指令',
  destroy_dispatched: '下发销毁指令',
  timeout_destroy: '超时自动销毁',
  match_started: '比赛开始',
  match_ended: '比赛结束',
}

const STATE_COLOR: Record<string, string> = {
  creating: 'processing',
  waiting: 'warning',
  playing: 'blue',
  finished: 'success',
  terminated: 'error',
  timeout: 'orange',
  error: 'error',
}

const STATE_LABEL: Record<string, string> = {
  creating: '创建中',
  waiting: '等待玩家',
  playing: '比赛进行中',
  finished: '正常结束',
  terminated: '平台终止',
  timeout: '超时终止',
  error: '异常',
}

const ACTIVE_STATES = new Set(['creating', 'waiting', 'playing'])

// ── sub-components ────────────────────────────────────────────────────────────

const LOG_COLS: ColumnsType<OpLog> = [
  { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180,
    render: (v: string) => dayjs(v).format(FMT) },
  { title: '来源', dataIndex: 'actor', key: 'actor', width: 90,
    render: (v: string) => <Tag color={ACTOR_COLOR[v] ?? 'default'}>{v}</Tag> },
  { title: '操作', dataIndex: 'action', key: 'action', width: 160,
    render: (v: string) => ACTION_LABEL[v] ?? v },
  { title: '详情', dataIndex: 'detail', key: 'detail',
    render: (v: string) => <Text code style={{ fontSize: 12 }}>{v}</Text> },
]

function fmtDuration(sec: number) {
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}分${s.toString().padStart(2, '0')}秒`
}

const PLAYER_COLS: ColumnsType<Player> = [
  { title: '#', dataIndex: 'index', key: 'index', width: 50 },
  { title: '玩家名', dataIndex: 'name', key: 'name' },
  { title: '得分', dataIndex: 'score', key: 'score', width: 80 },
  { title: '在线时长', dataIndex: 'duration', key: 'duration', width: 120,
    render: (v: number) => fmtDuration(v) },
]

function ServerInfoTab({ q }: { q: ServerQuery }) {
  if (q.info_error) return <Alert type="error" message={`查询失败：${q.info_error}`} />
  if (!q.info) return <Spin />
  const i = q.info
  return (
    <Descriptions size="small" bordered column={2}>
      <Descriptions.Item label="服务器名">{i.name}</Descriptions.Item>
      <Descriptions.Item label="地图">{i.map}</Descriptions.Item>
      <Descriptions.Item label="玩家">{i.players} / {i.max_players}（Bot {i.bots}）</Descriptions.Item>
      <Descriptions.Item label="游戏">{i.game}</Descriptions.Item>
      <Descriptions.Item label="类型">{i.server_type}</Descriptions.Item>
      <Descriptions.Item label="系统">{i.server_os}</Descriptions.Item>
      <Descriptions.Item label="VAC">{i.vac ? '开启' : '关闭'}</Descriptions.Item>
      <Descriptions.Item label="密码">{i.visibility ? '有' : '无'}</Descriptions.Item>
      <Descriptions.Item label="版本" span={2}>{i.version}</Descriptions.Item>
    </Descriptions>
  )
}

function PlayersTab({ q }: { q: ServerQuery }) {
  if (q.players_error) return <Alert type="error" message={`查询失败：${q.players_error}`} />
  if (!q.players) return <Spin />
  return (
    <Table<Player>
      rowKey="index"
      size="small"
      dataSource={q.players.players ?? []}
      columns={PLAYER_COLS}
      pagination={false}
      locale={{ emptyText: '暂无玩家' }}
    />
  )
}

function RulesTab({ q, search }: { q: ServerQuery; search: string }) {
  const [page, setPage] = useState(1)
  if (q.rules_error) return <Alert type="error" message={`查询失败：${q.rules_error}`} />
  if (!q.rules) return <Spin />
  const entries = Object.entries(q.rules.rules)
    .filter(([k, v]) => !search || k.includes(search) || v.includes(search))
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => ({ key: k, value: v }))
  return (
    <Table<{ key: string; value: string }>
      rowKey="key"
      size="small"
      dataSource={entries}
      columns={[
        { title: '参数名', dataIndex: 'key', key: 'key', width: '40%' },
        { title: '值', dataIndex: 'value', key: 'value' },
      ]}
      pagination={{ pageSize: 20, size: 'small', current: page, onChange: setPage }}
      locale={{ emptyText: '无匹配参数' }}
    />
  )
}

// ── main component ────────────────────────────────────────────────────────────

export default function MatchesPage() {
  const [matches, setMatches] = useState<Match[]>([])
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [form] = Form.useForm()

  const [logsCache, setLogsCache] = useState<Record<string, OpLog[]>>({})
  const [logsLoading, setLogsLoading] = useState<Set<string>>(new Set())
  const logsFetchedRef = useRef<Set<string>>(new Set())

  const [serverCache, setServerCache] = useState<Record<string, ServerQuery>>({})
  const [serverLoading, setServerLoading] = useState<Set<string>>(new Set())
  const serverFetchedRef = useRef<Set<string>>(new Set())

  const [ruleSearch, setRuleSearch] = useState<Record<string, string>>({})

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
    } catch {
      setLogsCache(prev => ({ ...prev, [matchID]: [] }))
    } finally {
      setLogsLoading(prev => { const s = new Set(prev); s.delete(matchID); return s })
    }
  }

  const fetchServer = async (matchID: string) => {
    if (serverFetchedRef.current.has(matchID)) return
    serverFetchedRef.current.add(matchID)
    setServerLoading(prev => new Set(prev).add(matchID))
    try {
      const res = await axios.get<ServerQuery>(`/api/matches/${matchID}/server`)
      setServerCache(prev => ({ ...prev, [matchID]: res.data }))
    } catch (e: any) {
      const msg = e?.response?.data || e?.message || '请求失败'
      setServerCache(prev => ({
        ...prev,
        [matchID]: { info_error: msg, players_error: msg, rules_error: msg },
      }))
    } finally {
      setServerLoading(prev => { const s = new Set(prev); s.delete(matchID); return s })
    }
  }

  const refreshServer = (matchID: string) => {
    serverFetchedRef.current.delete(matchID)
    setServerCache(prev => { const n = { ...prev }; delete n[matchID]; return n })
    fetchServer(matchID)
  }

  const handleExpand = (expanded: boolean, record: Match) => {
    if (!expanded) return
    fetchLogs(record.match_id)
    fetchServer(record.match_id)
  }

  const expandedRowRender = (record: Match) => {
    const mid = record.match_id
    const logs = logsCache[mid]
    const sq = serverCache[mid]
    const svrLoading = serverLoading.has(mid)

    const logTab = logsLoading.has(mid) || !logs
      ? <Spin style={{ padding: 16 }} />
      : <Table<OpLog> rowKey="id" size="small" dataSource={logs} columns={LOG_COLS}
          pagination={false} locale={{ emptyText: '暂无操作记录' }} />

    const svrTab = svrLoading || !sq
      ? <Spin style={{ padding: 16 }} />
      : <ServerInfoTab q={sq} />

    const plrTab = svrLoading || !sq
      ? <Spin style={{ padding: 16 }} />
      : <PlayersTab q={sq} />

    const ruleTab = svrLoading || !sq
      ? <Spin style={{ padding: 16 }} />
      : (
        <Space direction="vertical" style={{ width: '100%' }}>
          <Input.Search
            placeholder="搜索参数名或值"
            allowClear
            style={{ maxWidth: 320 }}
            onSearch={v => setRuleSearch(prev => ({ ...prev, [mid]: v }))}
            onChange={e => !e.target.value && setRuleSearch(prev => ({ ...prev, [mid]: '' }))}
          />
          <RulesTab q={sq} search={ruleSearch[mid] ?? ''} />
        </Space>
      )

    const svrDisabled = !ACTIVE_STATES.has(record.state)

    return (
      <div style={{ padding: '8px 48px' }}>
        <Tabs
          size="small"
          tabBarExtraContent={
            !svrDisabled && (
              <Tooltip title="刷新服务器数据">
                <Button size="small" icon={<ReloadOutlined />} onClick={() => refreshServer(mid)} />
              </Tooltip>
            )
          }
          items={[
            { key: 'logs', label: '操作日志', children: logTab },
            { key: 'server', label: '服务器信息', disabled: svrDisabled, children: svrTab },
            { key: 'players', label: '在线玩家', disabled: svrDisabled, children: plrTab },
            { key: 'rules', label: '服务器参数', disabled: svrDisabled, children: ruleTab },
          ]}
        />
      </div>
    )
  }

  const columns = [
    {
      title: '状态', dataIndex: 'state', key: 'state', width: 100,
      render: (v: string) => <Tag color={STATE_COLOR[v] ?? 'default'}>{STATE_LABEL[v] ?? v}</Tag>,
    },
    { title: 'Match ID', dataIndex: 'match_id', key: 'match_id', ellipsis: true },
    { title: '玩家0 SteamID', dataIndex: 'p0_steamid', key: 'p0_steamid' },
    { title: '玩家1 SteamID', dataIndex: 'p1_steamid', key: 'p1_steamid' },
    { title: '服务器名', dataIndex: 'server_name', key: 'server_name' },
    { title: 'Agent', dataIndex: 'agent_uuid', key: 'agent_uuid', ellipsis: true },
    { title: '端口', dataIndex: 'port', key: 'port', width: 80 },
    { title: '创建时间', dataIndex: 'create_time', key: 'create_time',
      render: (v: string) => dayjs(v).format(FMT) },
    { title: '更新时间', dataIndex: 'update_time', key: 'update_time',
      render: (v: string) => <Text type="secondary">{dayjs(v).format(FMT)}</Text> },
    {
      title: '操作', key: 'action', width: 160,
      render: (_: unknown, r: Match) =>
        ACTIVE_STATES.has(r.state) ? (
          <Space size={4}>
            <Tooltip title="RCON倒计时通知玩家后销毁容器">
              <Popconfirm title="结束比赛？将发送RCON倒计时指令后销毁容器。"
                onConfirm={() => handleEnd(r.match_id)} okText="确认" cancelText="取消">
                <Button size="small" icon={<PoweroffOutlined />}>结束</Button>
              </Popconfirm>
            </Tooltip>
            <Tooltip title="立即强制停止容器，不通知玩家">
              <Popconfirm title="强制销毁容器？玩家不会收到通知。"
                onConfirm={() => handleDestroy(r.match_id)} okText="确认" cancelText="取消">
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
        expandable={{ expandedRowRender, onExpand: handleExpand }}
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
          <Form.Item name="p0_steamid" label="玩家0 SteamID"
            rules={[{ required: true, message: '请输入玩家0 SteamID' }]}>
            <Input placeholder="STEAM_0:0:12345678" />
          </Form.Item>
          <Form.Item name="p1_steamid" label="玩家1 SteamID"
            rules={[{ required: true, message: '请输入玩家1 SteamID' }]}>
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
