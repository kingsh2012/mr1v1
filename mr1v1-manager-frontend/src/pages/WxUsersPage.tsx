import { useEffect, useState } from 'react'
import { Table, Avatar, Button, Space, Typography, Popconfirm, message, Tag, Switch, Modal, Form, Input } from 'antd'
import { UserOutlined, DeleteOutlined, EditOutlined } from '@ant-design/icons'
import api from '../api'
import dayjs from 'dayjs'

interface WxUser {
  openid: string
  steam_id: string
  nickname: string
  avatar_url: string
  status: string
  created_at: string
  updated_at: string
}

const { Text } = Typography
const FMT = 'YYYY-MM-DD HH:mm:ss'

export default function WxUsersPage() {
  const [users, setUsers] = useState<WxUser[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<WxUser | null>(null)
  const [form] = Form.useForm()

  const fetchUsers = async () => {
    setLoading(true)
    try {
      const res = await api.get<WxUser[]>('/wx-users')
      setUsers(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchUsers() }, [])

  const handleDelete = async (openid: string) => {
    await api.delete(`/wx-users/${openid}`)
    message.success('已删除')
    fetchUsers()
  }

  const handleToggleStatus = async (u: WxUser, enabled: boolean) => {
    await api.patch(`/wx-users/${u.openid}`, { status: enabled ? 'enabled' : 'disabled' })
    message.success(enabled ? '已启用' : '已禁用，该用户的登录态会立刻失效')
    fetchUsers()
  }

  const openEdit = (u: WxUser) => {
    setEditing(u)
    form.setFieldsValue({ nickname: u.nickname, steam_id: u.steam_id, avatar_url: u.avatar_url })
  }

  const submitEdit = async () => {
    if (!editing) return
    const values = await form.validateFields()
    await api.patch(`/wx-users/${editing.openid}`, values)
    message.success('已保存')
    setEditing(null)
    fetchUsers()
  }

  const columns = [
    {
      title: '头像',
      dataIndex: 'avatar_url',
      key: 'avatar_url',
      width: 70,
      render: (v: string) => <Avatar src={v || undefined} icon={!v && <UserOutlined />} />,
    },
    {
      title: '昵称',
      dataIndex: 'nickname',
      key: 'nickname',
      render: (v: string) => v || <Text type="secondary">未设置</Text>,
    },
    { title: 'OpenID', dataIndex: 'openid', key: 'openid', ellipsis: true },
    {
      title: 'SteamID',
      dataIndex: 'steam_id',
      key: 'steam_id',
      render: (v: string) => v || <Text type="secondary">未绑定</Text>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (v: string, r: WxUser) => (
        <Space>
          <Tag color={v === 'enabled' ? 'green' : 'red'}>{v === 'enabled' ? '启用' : '禁用'}</Tag>
          <Switch
            size="small"
            checked={v === 'enabled'}
            onChange={(checked) => handleToggleStatus(r, checked)}
          />
        </Space>
      ),
    },
    {
      title: '注册时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => dayjs(v).format(FMT),
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      render: (v: string) => dayjs(v).format(FMT),
    },
    {
      title: '操作',
      key: 'action',
      width: 160,
      render: (_: unknown, r: WxUser) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>编辑</Button>
          <Popconfirm
            title="删除该微信用户？"
            description="会一并删掉该用户创建的房间，无法恢复。"
            onConfirm={() => handleDelete(r.openid)}
            okText="删除"
            okType="danger"
            cancelText="取消"
          >
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button onClick={fetchUsers}>刷新</Button>
      </Space>
      <Table rowKey="openid" loading={loading} dataSource={users} columns={columns} />

      <Modal
        title="编辑微信用户"
        open={!!editing}
        onCancel={() => setEditing(null)}
        onOk={submitEdit}
        okText="保存"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          <Form.Item label="昵称" name="nickname">
            <Input placeholder="昵称" />
          </Form.Item>
          <Form.Item label="SteamID" name="steam_id">
            <Input placeholder="STEAM_0:Y:Z" />
          </Form.Item>
          <Form.Item label="头像URL" name="avatar_url">
            <Input placeholder="https://..." />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
