import { useEffect, useState } from 'react'
import {
  Table, Tag, Button, Modal, Form, Select, InputNumber, Radio, Space, message, Typography,
} from 'antd'
import { EditOutlined } from '@ant-design/icons'
import axios from 'axios'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const { Text } = Typography

interface Agent {
  uuid: string
  hostname: string
  public_ip: string
  local_ip: string
  cpu: string
  mem_mb: number
  disk_gb: number
  status: string
  rehlds_run_max: number
  rehlds_port_range: string
  create_time: string
  update_time: string
  heartbeat_time: string
}

const FMT = 'YYYY-MM-DD HH:mm:ss'

const STALE_SECONDS = 30

function isOnline(hbt: string): boolean {
  return dayjs().diff(dayjs(hbt), 'second') < STALE_SECONDS
}

// 将存储字符串解析回编辑状态
// "27015-27025" → { mode: 'range', start: 27015, end: 27025 }
// "27015,27020,27030" → { mode: 'list', ports: ['27015','27020','27030'] }
// "" → { mode: 'range', start: undefined, end: undefined }
function parsePortRange(raw: string): { mode: 'range' | 'list'; start?: number; end?: number; ports: string[] } {
  if (!raw) return { mode: 'range', ports: [] }
  if (raw.includes('-') && !raw.includes(',')) {
    const [a, b] = raw.split('-')
    return { mode: 'range', start: parseInt(a), end: parseInt(b), ports: [] }
  }
  return { mode: 'list', ports: raw.split(',').map(s => s.trim()).filter(Boolean) }
}

// 将编辑状态序列化为存储字符串
function serializePortRange(mode: 'range' | 'list', start?: number, end?: number, ports?: string[]): string {
  if (mode === 'range') {
    if (start == null || end == null) return ''
    return `${start}-${end}`
  }
  return (ports ?? []).join(',')
}

type PortMode = 'range' | 'list'

export default function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(false)
  const [editTarget, setEditTarget] = useState<Agent | null>(null)
  const [portMode, setPortMode] = useState<PortMode>('range')
  const [portStart, setPortStart] = useState<number | undefined>()
  const [portEnd, setPortEnd] = useState<number | undefined>()
  const [portList, setPortList] = useState<string[]>([])
  const [form] = Form.useForm()

  const fetchAgents = async () => {
    setLoading(true)
    try {
      const res = await axios.get<Agent[]>('/api/agents')
      setAgents(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchAgents()
    const t = setInterval(fetchAgents, 10000)
    return () => clearInterval(t)
  }, [])

  const openEdit = (agent: Agent) => {
    setEditTarget(agent)
    const parsed = parsePortRange(agent.rehlds_port_range)
    setPortMode(parsed.mode)
    setPortStart(parsed.start)
    setPortEnd(parsed.end)
    setPortList(parsed.ports)
    form.setFieldsValue({
      status: agent.status,
      rehlds_run_max: agent.rehlds_run_max,
    })
  }

  const handleSave = async () => {
    if (!editTarget) return
    const values = await form.validateFields()
    const portRange = serializePortRange(portMode, portStart, portEnd, portList)
    await axios.patch(`/api/agents/${editTarget.uuid}`, {
      ...values,
      rehlds_port_range: portRange,
    })
    message.success('保存成功')
    setEditTarget(null)
    fetchAgents()
  }

  const columns = [
    {
      title: '在线',
      key: 'online',
      width: 70,
      render: (_: unknown, r: Agent) => (
        <Tag color={isOnline(r.heartbeat_time) ? 'green' : 'default'}>
          {isOnline(r.heartbeat_time) ? '在线' : '离线'}
        </Tag>
      ),
    },
    { title: 'UUID', dataIndex: 'uuid', key: 'uuid', ellipsis: true },
    { title: 'Hostname', dataIndex: 'hostname', key: 'hostname' },
    { title: '公网 IP', dataIndex: 'public_ip', key: 'public_ip' },
    { title: '内网 IP', dataIndex: 'local_ip', key: 'local_ip' },
    { title: 'CPU核数', dataIndex: 'cpu', key: 'cpu' },
    { title: '内存(MB)', dataIndex: 'mem_mb', key: 'mem_mb' },
    { title: '磁盘(GB)', dataIndex: 'disk_gb', key: 'disk_gb' },
    {
      title: '调度状态',
      dataIndex: 'status',
      key: 'status',
      render: (v: string) => <Tag color={v === 'enabled' ? 'blue' : 'red'}>{v === 'enabled' ? '可调度' : '禁用'}</Tag>,
    },
    { title: '最大并发', dataIndex: 'rehlds_run_max', key: 'rehlds_run_max' },
    { title: '端口范围', dataIndex: 'rehlds_port_range', key: 'rehlds_port_range' },
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
      render: (v: string) => dayjs(v).format(FMT),
    },
    {
      title: '心跳时间',
      dataIndex: 'heartbeat_time',
      key: 'heartbeat_time',
      render: (v: string) => (
        <Text type="secondary" title={dayjs(v).format(FMT)}>{dayjs(v).fromNow()}</Text>
      ),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, r: Agent) => (
        <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>
          编辑
        </Button>
      ),
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button onClick={fetchAgents}>刷新</Button>
      </Space>
      <Table
        rowKey="uuid"
        loading={loading}
        dataSource={agents}
        columns={columns}
        scroll={{ x: 'max-content' }}
        pagination={false}
      />
      <Modal
        title={`编辑 Agent: ${editTarget?.hostname || ''}`}
        open={!!editTarget}
        onOk={handleSave}
        onCancel={() => setEditTarget(null)}
        okText="保存"
        cancelText="取消"
      >
        <Form form={form} layout="vertical">
          <Form.Item name="status" label="调度状态" rules={[{ required: true }]}>
            <Select
              options={[
                { label: '可调度', value: 'enabled' },
                { label: '禁用', value: 'disabled' },
              ]}
            />
          </Form.Item>
          <Form.Item name="rehlds_run_max" label="最大并发 rehlds 数">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="端口范围">
            <Radio.Group
              value={portMode}
              onChange={e => setPortMode(e.target.value)}
              style={{ marginBottom: 8 }}
            >
              <Radio.Button value="range">连续范围</Radio.Button>
              <Radio.Button value="list">自定义端口</Radio.Button>
            </Radio.Group>
            {portMode === 'range' ? (
              <Space>
                <InputNumber
                  min={1}
                  max={65535}
                  placeholder="起始端口"
                  value={portStart}
                  onChange={v => setPortStart(v ?? undefined)}
                />
                <span>—</span>
                <InputNumber
                  min={1}
                  max={65535}
                  placeholder="结束端口"
                  value={portEnd}
                  onChange={v => setPortEnd(v ?? undefined)}
                />
              </Space>
            ) : (
              <Select
                mode="tags"
                style={{ width: '100%' }}
                placeholder="输入端口号后回车，如 27015"
                value={portList}
                onChange={vals => setPortList(vals)}
                tokenSeparators={[',']}
                open={false}
                onInputKeyDown={e => {
                  const ch = e.key
                  if (!/[\d]/.test(ch) && !['Backspace', 'Delete', 'ArrowLeft', 'ArrowRight', 'Enter', 'Tab'].includes(ch)) {
                    e.preventDefault()
                  }
                }}
              />
            )}
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
