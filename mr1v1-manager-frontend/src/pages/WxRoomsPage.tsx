import { useEffect, useState } from 'react'
import { Table, Tag, Button, Space } from 'antd'
import api from '../api'
import dayjs from 'dayjs'

interface WxRoom {
  id: string
  title: string
  creator_openid: string
  creator_name: string
  joiner_openid: string
  joiner_name: string
  locked: boolean
  status: string
  created_at: string
  deleted_at?: string
}

const FMT = 'YYYY-MM-DD HH:mm:ss'

export default function WxRoomsPage() {
  const [rooms, setRooms] = useState<WxRoom[]>([])
  const [loading, setLoading] = useState(false)

  const fetchRooms = async () => {
    setLoading(true)
    try {
      const res = await api.get<WxRoom[]>('/wx-rooms')
      setRooms(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchRooms() }, [])

  const statusColor: Record<string, string> = {
    waiting: 'blue',
    ready: 'gold',
    matched: 'green',
  }

  const columns = [
    { title: '房间标题', dataIndex: 'title', key: 'title' },
    { title: '房主', dataIndex: 'creator_name', key: 'creator_name' },
    {
      title: '对手',
      dataIndex: 'joiner_name',
      key: 'joiner_name',
      render: (v: string) => v || <span style={{ color: '#999' }}>无</span>,
    },
    {
      title: '密码',
      dataIndex: 'locked',
      key: 'locked',
      render: (v: boolean) => v ? <Tag color="orange">有密码</Tag> : <Tag>无密码</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (v: string, r: WxRoom) => r.deleted_at
        ? <Tag color="default">已关闭</Tag>
        : <Tag color={statusColor[v] || 'default'}>{v}</Tag>,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (v: string) => dayjs(v).format(FMT),
    },
    {
      title: '关闭时间',
      dataIndex: 'deleted_at',
      key: 'deleted_at',
      render: (v?: string) => v ? dayjs(v).format(FMT) : '-',
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button onClick={fetchRooms}>刷新</Button>
      </Space>
      <Table rowKey="id" loading={loading} dataSource={rooms} columns={columns} />
    </>
  )
}
