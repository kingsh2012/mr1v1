import { useEffect, useState } from 'react'
import {
  Table, Tag, Button, Modal, Form, Input, Space, message, Popconfirm,
} from 'antd'

import { PlusOutlined, CheckCircleOutlined } from '@ant-design/icons'
import api from '../api'
import dayjs from 'dayjs'

interface RehldsConfig {
  id: number
  image: string
  version: string
  is_active: boolean
  created_at: string
}

export default function RehldsConfigPage() {
  const [configs, setConfigs] = useState<RehldsConfig[]>([])
  const [loading, setLoading] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [form] = Form.useForm()

  const fetchConfigs = async () => {
    setLoading(true)
    try {
      const res = await api.get<RehldsConfig[]>('/rehlds-configs')
      setConfigs(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchConfigs() }, [])

  const handleAdd = async () => {
    const values = await form.validateFields()
    await api.post('/rehlds-configs', values)
    message.success('添加成功')
    setAddOpen(false)
    form.resetFields()
    fetchConfigs()
  }

  const handleActivate = async (id: number) => {
    await api.patch(`/rehlds-configs/${id}/activate`)
    message.success('已激活')
    fetchConfigs()
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '镜像', dataIndex: 'image', key: 'image' },
    { title: '版本', dataIndex: 'version', key: 'version' },
    {
      title: '当前激活',
      dataIndex: 'is_active',
      key: 'is_active',
      render: (v: boolean) => v ? <Tag color="green">激活</Tag> : <Tag>未激活</Tag>,
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
      render: (_: unknown, r: RehldsConfig) => r.is_active
        ? <Button size="small" icon={<CheckCircleOutlined />} disabled>已激活</Button>
        : (
          <Popconfirm
            title="确认激活该镜像？"
            onConfirm={() => handleActivate(r.id)}
            okText="确认"
            cancelText="取消"
          >
            <Button size="small" icon={<CheckCircleOutlined />}>激活</Button>
          </Popconfirm>
        ),
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button icon={<PlusOutlined />} type="primary" onClick={() => setAddOpen(true)}>
          添加镜像
        </Button>
        <Button onClick={fetchConfigs}>刷新</Button>
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        dataSource={configs}
        columns={columns}
        pagination={false}
      />
      <Modal
        title="添加 Rehlds 镜像配置"
        open={addOpen}
        onOk={handleAdd}
        onCancel={() => { setAddOpen(false); form.resetFields() }}
        okText="添加"
        cancelText="取消"
      >
        <Form form={form} layout="vertical">
          <Form.Item name="image" label="镜像地址" rules={[{ required: true }]}>
            <Input placeholder="registry.cn-beijing.aliyuncs.com/kingsh2012/mr1v1-rehlds:v0.2.6" />
          </Form.Item>
          <Form.Item name="version" label="版本标签">
            <Input placeholder="v0.2.6" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
