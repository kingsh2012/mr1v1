import { useEffect, useState } from 'react'
import { Table, Button, Space, Input } from 'antd'
import api from '../api'

interface LegacyPlayer {
  steam_id: string
  name: string
  score: number
  total_match: number
  total_match_win: number
  kd: number
  win_rate: number
  rating: number
}

export default function LegacyPlayersPage() {
  const [players, setPlayers] = useState<LegacyPlayer[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')

  const fetchPlayers = async (kw?: string) => {
    setLoading(true)
    try {
      const res = await api.get<LegacyPlayer[]>('/legacy-players', { params: kw ? { keyword: kw } : {} })
      setPlayers(res.data ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchPlayers() }, [])

  const columns = [
    { title: '昵称', dataIndex: 'name', key: 'name' },
    { title: 'SteamID', dataIndex: 'steam_id', key: 'steam_id', ellipsis: true },
    { title: '积分', dataIndex: 'score', key: 'score', sorter: (a: LegacyPlayer, b: LegacyPlayer) => a.score - b.score },
    { title: '总场次', dataIndex: 'total_match', key: 'total_match' },
    { title: '胜场', dataIndex: 'total_match_win', key: 'total_match_win' },
    { title: '胜率', dataIndex: 'win_rate', key: 'win_rate', render: (v: number) => `${v}%` },
    { title: 'K/D', dataIndex: 'kd', key: 'kd' },
    { title: 'Rating', dataIndex: 'rating', key: 'rating' },
  ]

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Input.Search
          placeholder="按昵称搜索"
          allowClear
          style={{ width: 240 }}
          value={keyword}
          onChange={e => setKeyword(e.target.value)}
          onSearch={v => fetchPlayers(v)}
        />
        <Button onClick={() => fetchPlayers(keyword)}>刷新</Button>
      </Space>
      <Table rowKey="steam_id" loading={loading} dataSource={players} columns={columns} />
    </>
  )
}
