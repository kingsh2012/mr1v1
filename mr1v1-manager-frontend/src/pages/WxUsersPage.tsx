import { useEffect, useState } from 'react'
import { Table, Avatar, Button, Space, Typography, Popconfirm, message } from 'antd'
import { UserOutlined, DeleteOutlined } from '@ant-design/icons'
import api from '../api'
import dayjs from 'dayjs'

interface WxUser {
  openid: string
  steam_id: string
  nickname: string
  avatar_url: string
  created_at: string
  updated_at: string
}

const { Text } = Typography
const FMT = 'YYYY-MM-DD HH:mm:ss'

export default function WxUsersPage() {
  const [users, setUsers] = useState<WxUser[]>([])
  const [loading, setLoading] = useState(false)

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
      width: 90,
      render: (_: unknown, r: WxUser) => (
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
      ),
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button onClick={fetchUsers}>刷新</Button>
      </Space>
      <Table rowKey="openid" loading={loading} dataSource={users} columns={columns} />
    </>
  )
}
