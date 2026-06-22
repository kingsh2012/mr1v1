import { useEffect, useState } from 'react'
import {
  Table, Tag, Button, Modal, Form, Input, Select, Space, message, Popconfirm, Switch,
} from 'antd'
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons'
import api from '../api'
import dayjs from 'dayjs'

interface MapItem {
  id: number
  category: string
  map_name: string
  enabled: boolean
  created_at: string
}

const CATEGORY_LABEL: Record<string, string> = {
  pistol: '手枪图池',
  rifle: '步枪图池',
  sniper: '狙击图池',
}

export default function MapsPage() {
  const [maps, setMaps] = useState<MapItem[]>([])
  const [loading, setLoading] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [form] = Form.useForm()

  const fetchMaps = async () => {
    setLoading(true)
    try {
      const res = await api.get<MapItem[]>('/maps')
      setMaps(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchMaps() }, [])

  const handleAdd = async () => {
    const values = await form.validateFields()
    await api.post('/maps', values)
    message.success('添加成功')
    setAddOpen(false)
    form.resetFields()
    fetchMaps()
  }

  const handleToggle = async (id: number, enabled: boolean) => {
    await api.patch(`/maps/${id}`, { enabled })
    message.success(enabled ? '已启用' : '已禁用')
    fetchMaps()
  }

  const handleDelete = async (id: number) => {
    await api.delete(`/maps/${id}`)
    message.success('已删除')
    fetchMaps()
  }

  const columns = [
    {
      title: '类型',
      dataIndex: 'category',
      key: 'category',
      filters: Object.entries(CATEGORY_LABEL).map(([value, text]) => ({ text, value })),
      onFilter: (value: boolean | React.Key, r: MapItem) => r.category === value,
      render: (v: string) => <Tag color="blue">{CATEGORY_LABEL[v] ?? v}</Tag>,
    },
    { title: '地图名', dataIndex: 'map_name', key: 'map_name' },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      render: (v: boolean, r: MapItem) => (
        <Switch checked={v} onChange={(checked) => handleToggle(r.id, checked)} />
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => dayjs(v).format('YYYY-MM-DD HH:mm:ss'),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, r: MapItem) => (
        <Popconfirm title="确认删除该地图？" onConfirm={() => handleDelete(r.id)} okText="删除" cancelText="取消">
          <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
        </Popconfirm>
      ),
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<PlusOutlined />} type="primary" onClick={() => setAddOpen(true)}>
          添加地图
        </Button>
        <Button onClick={fetchMaps}>刷新</Button>
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        dataSource={maps}
        columns={columns}
        pagination={false}
      />
      <Modal
        title="添加地图"
        open={addOpen}
        onOk={handleAdd}
        onCancel={() => { setAddOpen(false); form.resetFields() }}
        okText="添加"
        cancelText="取消"
      >
        <Form form={form} layout="vertical" initialValues={{ category: 'rifle' }}>
          <Form.Item name="category" label="武器类型" rules={[{ required: true }]}>
            <Select options={Object.entries(CATEGORY_LABEL).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
          <Form.Item name="map_name" label="地图名" rules={[{ required: true }]}>
            <Input placeholder="aim_map" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
