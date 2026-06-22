import { useEffect, useState } from 'react'
import { Table, Button, Space, Typography } from 'antd'
import api from '../api'
import dayjs from 'dayjs'

const { Paragraph } = Typography
const FMT = 'YYYY-MM-DD HH:mm:ss'

interface Feedback {
  id: number
  nickname: string
  content: string
  created_at: string
}

export default function FeedbackPage() {
  const [items, setItems] = useState<Feedback[]>([])
  const [loading, setLoading] = useState(false)

  const fetchItems = async () => {
    setLoading(true)
    try {
      const res = await api.get<Feedback[]>('/wx-feedback')
      setItems(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchItems() }, [])

  const columns = [
    { title: '昵称', dataIndex: 'nickname', key: 'nickname', width: 160 },
    {
      title: '建议内容', dataIndex: 'content', key: 'content',
      render: (v: string) => <Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{v}</Paragraph>,
    },
    {
      title: '提交时间', dataIndex: 'created_at', key: 'created_at', width: 180,
      render: (v: string) => dayjs(v).format(FMT),
    },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button onClick={fetchItems}>刷新</Button>
      </Space>
      <Table rowKey="id" loading={loading} dataSource={items} columns={columns} />
    </>
  )
}
