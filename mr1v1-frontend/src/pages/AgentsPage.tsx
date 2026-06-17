import { useEffect, useRef, useState } from 'react'
import {
  Table, Tag, Button, Modal, Form, Select, InputNumber, Radio, Space, message,
  Typography, Alert, Spin, Descriptions,
} from 'antd'
import { EditOutlined } from '@ant-design/icons'
import api from '../api'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

const { Text } = Typography
const FMT = 'YYYY-MM-DD HH:mm:ss'
const STALE_SECONDS = 30

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
  running_containers: string
  create_time: string
  update_time: string
  heartbeat_time: string
}

interface ContainerDetail {
  id: string
  image: string
  command: string
  created: number
  status: string
  names: string[] | null
  env: string[] | null
  labels: Record<string, string> | null
}

function ContainerExpand({ agentUUID }: { agentUUID: string }) {
  const [loading, setLoading] = useState(false)
  const [containers, setContainers] = useState<ContainerDetail[] | null>(null)
  const [expandedEnv, setExpandedEnv] = useState<Record<string, boolean>>({})
  const fetched = useRef(false)

  useEffect(() => {
    if (fetched.current) return
    fetched.current = true
    setLoading(true)
    api.get<ContainerDetail[]>(`/agents/${agentUUID}/containers`)
      .then(r => setContainers(r.data ?? []))
      .catch(() => setContainers([]))
      .finally(() => setLoading(false))
  }, [agentUUID])

  if (loading) return <Spin style={{ margin: 16 }} />
  if (!containers) return null

  const envColumns = [
    { title: '变量名', dataIndex: 'key', key: 'key', width: 220 },
    { title: '值', dataIndex: 'val', key: 'val', ellipsis: true },
  ]

  const cols = [
    { title: 'CONTAINER ID', dataIndex: 'id', key: 'id', width: 120 },
    {
      title: 'IMAGE',
      dataIndex: 'image',
      key: 'image',
      ellipsis: true,
      render: (v: string) => <Text code style={{ fontSize: 12 }}>{v}</Text>,
    },
    {
      title: 'COMMAND',
      dataIndex: 'command',
      key: 'command',
      width: 180,
      ellipsis: true,
      render: (v: string) => <Text code style={{ fontSize: 12 }}>{v}</Text>,
    },
    {
      title: 'CREATED',
      dataIndex: 'created',
      key: 'created',
      width: 160,
      render: (v: number) => dayjs.unix(v).format('YYYY-MM-DD HH:mm:ss'),
    },
    {
      title: 'STATUS',
      dataIndex: 'status',
      key: 'status',
      width: 160,
      render: (v: string) => {
        const up = v.toLowerCase().startsWith('up')
        return <Tag color={up ? 'green' : 'default'}>{v}</Tag>
      },
    },
    {
      title: 'NAMES',
      dataIndex: 'names',
      key: 'names',
      render: (v: string[] | null) => (v ?? []).map(n => (
        <Tag key={n} style={{ marginBottom: 2 }}>{n.replace(/^\//, '')}</Tag>
      )),
    },
    {
      title: 'ENV',
      key: 'env',
      width: 80,
      render: (_: unknown, r: ContainerDetail) => {
        const count = (r.env ?? []).length
        if (count === 0) return <Text type="secondary">—</Text>
        const open = expandedEnv[r.id]
        return (
          <Button size="small" type="link" style={{ padding: 0 }}
            onClick={() => setExpandedEnv(prev => ({ ...prev, [r.id]: !open }))}>
            {open ? '收起' : `查看(${count})`}
          </Button>
        )
      },
    },
  ]

  return (
    <div style={{ padding: '8px 0' }}>
      <Table<ContainerDetail>
        rowKey="id"
        dataSource={containers}
        columns={cols}
        size="small"
        pagination={false}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '该主机暂无容器' }}
        expandable={{
          expandedRowKeys: Object.entries(expandedEnv)
            .filter(([, v]) => v).map(([k]) => k),
          expandedRowRender: (r: ContainerDetail) => {
            const envRows = (r.env ?? []).map(line => {
              const idx = line.indexOf('=')
              return idx < 0
                ? { key: line, val: '' }
                : { key: line.slice(0, idx), val: line.slice(idx + 1) }
            })
            return (
              <Table
                rowKey="key"
                dataSource={envRows}
                columns={envColumns}
                size="small"
                pagination={false}
                style={{ margin: '4px 0' }}
              />
            )
          },
          showExpandColumn: false,
        }}
      />
      {containers.length > 0 && (
        <Descriptions size="small" style={{ marginTop: 8 }}>
          <Descriptions.Item label="容器总数">{containers.length}</Descriptions.Item>
          <Descriptions.Item label="运行中">
            {containers.filter(c => c.status.toLowerCase().startsWith('up')).length}
          </Descriptions.Item>
        </Descriptions>
      )}
    </div>
  )
}

function isOnline(hbt: string): boolean {
  return dayjs().diff(dayjs(hbt), 'second') < STALE_SECONDS
}

function parsePortRange(raw: string): { mode: 'range' | 'list'; start?: number; end?: number; ports: string[] } {
  if (!raw) return { mode: 'range', ports: [] }
  if (raw.includes('-') && !raw.includes(',')) {
    const [a, b] = raw.split('-')
    return { mode: 'range', start: parseInt(a), end: parseInt(b), ports: [] }
  }
  return { mode: 'list', ports: raw.split(',').map(s => s.trim()).filter(Boolean) }
}

function serializePortRange(mode: 'range' | 'list', start?: number, end?: number, ports?: string[]): string {
  if (mode === 'range') {
    if (start == null || end == null) return ''
    return `${start}-${end}`
  }
  return (ports ?? []).join(',')
}

function portCount(mode: 'range' | 'list', start?: number, end?: number, ports?: string[]): number {
  if (mode === 'range') {
    if (start == null || end == null || end < start) return 0
    return end - start + 1
  }
  return (ports ?? []).length
}

// 只允许数字键，屏蔽其他字符
function numbersOnly(e: React.KeyboardEvent) {
  if (
    !/^\d$/.test(e.key) &&
    !['Backspace', 'Delete', 'ArrowLeft', 'ArrowRight', 'Tab', 'Enter'].includes(e.key)
  ) {
    e.preventDefault()
  }
}

type PortMode = 'range' | 'list'

export default function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(false)
  const [editTarget, setEditTarget] = useState<Agent | null>(null)
  const [expandedKeys, setExpandedKeys] = useState<string[]>([])
  const [portMode, setPortMode] = useState<PortMode>('range')
  const [portStart, setPortStart] = useState<number | undefined>()
  const [portEnd, setPortEnd] = useState<number | undefined>()
  const [portList, setPortList] = useState<string[]>([])
  const [form] = Form.useForm()

  const fetchAgents = async () => {
    setLoading(true)
    try {
      const res = await api.get<Agent[]>('/agents')
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
    form.setFieldsValue({ status: agent.status, rehlds_run_max: agent.rehlds_run_max })
  }

  const handleSave = async () => {
    if (!editTarget) return
    const values = await form.validateFields()
    const runMax: number = values.rehlds_run_max ?? 0
    const cnt = portCount(portMode, portStart, portEnd, portList)

    // 端口数量校验
    if (cnt < runMax) {
      message.error(`端口数量（${cnt}）不能少于 REHLDS 最大并发数（${runMax}）`)
      return
    }

    // 端口范围格式校验
    if (portMode === 'range') {
      if (portStart == null || portEnd == null) {
        message.error('请填写起始端口和结束端口')
        return
      }
      if (portEnd <= portStart) {
        message.error('结束端口必须大于起始端口')
        return
      }
    } else {
      if (portList.length === 0) {
        message.error('请至少输入一个端口号')
        return
      }
      const invalid = portList.find(p => !/^\d+$/.test(p) || +p < 1 || +p > 65535)
      if (invalid) {
        message.error(`端口号无效：${invalid}`)
        return
      }
    }

    const portRange = serializePortRange(portMode, portStart, portEnd, portList)
    await api.patch(`/agents/${editTarget.uuid}`, { ...values, rehlds_port_range: portRange })
    message.success('保存成功')
    setEditTarget(null)
    fetchAgents()
  }

  const runMax: number = Form.useWatch('rehlds_run_max', form) ?? 0
  const cnt = portCount(portMode, portStart, portEnd, portList)
  const portInsufficient = cnt > 0 && runMax > 0 && cnt < runMax

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
      render: (v: string) => (
        <Tag color={v === 'enabled' ? 'blue' : 'red'}>{v === 'enabled' ? '可调度' : '禁用'}</Tag>
      ),
    },
    { title: 'REHLDS最大并发', dataIndex: 'rehlds_run_max', key: 'rehlds_run_max' },
    { title: 'REHLDS端口范围', dataIndex: 'rehlds_port_range', key: 'rehlds_port_range' },
    {
      title: '运行中容器',
      dataIndex: 'running_containers',
      key: 'running_containers',
      render: (v: string) => v
        ? v.split(',').map(id => (
            <Tag key={id} color="blue" style={{ marginBottom: 2 }}>{id.slice(0, 8)}</Tag>
          ))
        : <Text type="secondary">无</Text>,
    },
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
        <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>编辑</Button>
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
        expandable={{
          expandedRowKeys: expandedKeys,
          onExpand: (expanded, record) => {
            setExpandedKeys(expanded
              ? [...expandedKeys, record.uuid]
              : expandedKeys.filter(k => k !== record.uuid))
          },
          expandedRowRender: (record: Agent) => <ContainerExpand agentUUID={record.uuid} />,
        }}
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
          <Form.Item name="rehlds_run_max" label="REHLDS最大并发数">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item label="REHLDS端口范围" style={{ marginBottom: portInsufficient ? 4 : undefined }}>
            <Radio.Group
              value={portMode}
              onChange={e => setPortMode(e.target.value)}
              style={{ marginBottom: 8 }}
            >
              <Radio.Button value="range">连续范围</Radio.Button>
              <Radio.Button value="list">自定义端口</Radio.Button>
            </Radio.Group>

            {/* 两种模式输入区固定在同一位置，高度一致 */}
            <div style={{ minHeight: 32 }}>
              {portMode === 'range' ? (
                <Space>
                  <InputNumber
                    min={1}
                    max={65535}
                    placeholder="起始端口"
                    value={portStart}
                    onChange={v => setPortStart(v ?? undefined)}
                    onKeyDown={numbersOnly}
                    style={{ width: 120 }}
                  />
                  <span style={{ userSelect: 'none' }}>—</span>
                  <InputNumber
                    min={1}
                    max={65535}
                    placeholder="结束端口"
                    value={portEnd}
                    onChange={v => setPortEnd(v ?? undefined)}
                    onKeyDown={numbersOnly}
                    style={{ width: 120 }}
                  />
                  {cnt > 0 && <Text type="secondary">共 {cnt} 个端口</Text>}
                </Space>
              ) : (
                <>
                  <Select
                    mode="tags"
                    style={{ width: '100%' }}
                    placeholder="输入端口号后按 Enter 添加"
                    value={portList}
                    onChange={vals => {
                      // 过滤掉非纯数字的 tag
                      setPortList(vals.filter((v: string) => /^\d+$/.test(v)))
                    }}
                    tokenSeparators={[',']}
                    open={false}
                    onInputKeyDown={numbersOnly}
                  />
                  {portList.length > 0 && (
                    <Text type="secondary" style={{ marginTop: 4, display: 'block' }}>
                      共 {portList.length} 个端口
                    </Text>
                  )}
                </>
              )}
            </div>
          </Form.Item>

          {portInsufficient && (
            <Alert
              type="warning"
              showIcon
              message={`端口数量（${cnt}）少于 REHLDS 最大并发数（${runMax}），保存时将被拒绝`}
              style={{ marginBottom: 16 }}
            />
          )}
        </Form>
      </Modal>
    </>
  )
}
