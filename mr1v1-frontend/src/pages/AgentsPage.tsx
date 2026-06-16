import { useEffect, useState } from 'react'
import {
  Table, Tag, Button, Modal, Form, Input, InputNumber, Space, message, Typography,
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

const STALE_SECONDS = 30

function isOnline(heartbeat_time: string): boolean {
  return dayjs().diff(dayjs(heartbeat_time), 'second') < STALE_SECONDS
}

export default function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(false)
  const [editTarget, setEditTarget] = useState<Agent | null>(null)
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
    form.setFieldsValue({
      status: agent.status,
      rehlds_run_max: agent.rehlds_run_max,
      rehlds_port_range: agent.rehlds_port_range,
    })
  }

  const handleSave = async () => {
    if (!editTarget) return
    const values = await form.validateFields()
    await axios.patch(`/api/agents/${editTarget.uuid}`, values)
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
    { title: 'CPU', dataIndex: 'cpu', key: 'cpu', ellipsis: true },
    { title: '内存(MB)', dataIndex: 'mem_mb', key: 'mem_mb' },
    { title: '磁盘(GB)', dataIndex: 'disk_gb', key: 'disk_gb' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (v: string) => <Tag color={v === 'enabled' ? 'blue' : 'red'}>{v}</Tag>,
    },
    { title: '最大并发', dataIndex: 'rehlds_run_max', key: 'rehlds_run_max' },
    { title: '端口范围', dataIndex: 'rehlds_port_range', key: 'rehlds_port_range' },
    {
      title: '心跳',
      dataIndex: 'heartbeat_time',
      key: 'heartbeat_time',
      render: (v: string) => (
        <Text type="secondary" title={v}>{dayjs(v).fromNow()}</Text>
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
            <Input placeholder="enabled / disabled" />
          </Form.Item>
          <Form.Item name="rehlds_run_max" label="最大并发 rehlds 数">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="rehlds_port_range" label="端口范围 (如 27015-27025)">
            <Input placeholder="27015-27025" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
