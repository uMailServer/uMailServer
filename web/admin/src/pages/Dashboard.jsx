import { useState, useEffect } from 'react'
import {
  Mail,
  Users,
  Globe,
  Server,
  Activity,
  AlertTriangle,
  CheckCircle,
  TrendingUp,
  TrendingDown
} from 'lucide-react'

function Dashboard() {
  const [stats, setStats] = useState({
    domains: 0,
    accounts: 0,
    messages: 0,
    queueSize: 0
  })
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // Fetch stats from API
    fetchStats()
  }, [])

  const fetchStats = async () => {
    try {
      const token = localStorage.getItem('adminToken')
      const response = await fetch('/api/v1/stats', {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })
      if (response.ok) {
        const data = await response.json()
        setStats(data)
      }
    } catch (error) {
      console.error('Failed to fetch stats:', error)
    } finally {
      setLoading(false)
    }
  }

  const cards = [
    {
      title: 'Domains',
      value: stats.domains,
      icon: Globe,
      color: 'bg-blue-500',
      trend: '+0',
      trendUp: true
    },
    {
      title: 'Accounts',
      value: stats.accounts,
      icon: Users,
      color: 'bg-green-500',
      trend: '+0',
      trendUp: true
    },
    {
      title: 'Messages',
      value: stats.messages,
      icon: Mail,
      color: 'bg-violet-500',
      trend: '+0',
      trendUp: true
    },
    {
      title: 'Queue Size',
      value: stats.queueSize,
      icon: Server,
      color: 'bg-orange-500',
      trend: '0',
      trendUp: true
    }
  ]

  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900 mb-6">Dashboard</h1>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
        {cards.map((card, index) => (
          <StatCard key={index} {...card} />
        ))}
      </div>

      {/* System Status */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6 mb-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">System Status</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <StatusItem
            icon={CheckCircle}
            label="SMTP Server"
            status="operational"
            color="text-green-500"
          />
          <StatusItem
            icon={CheckCircle}
            label="IMAP Server"
            status="operational"
            color="text-green-500"
          />
          <StatusItem
            icon={CheckCircle}
            label="HTTP API"
            status="operational"
            color="text-green-500"
          />
        </div>
      </div>

      {/* Recent Activity */}
      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Recent Activity</h2>
        <div className="space-y-3">
          <ActivityItem
            icon={Mail}
            text="New message received"
            time="2 minutes ago"
          />
          <ActivityItem
            icon={Users}
            text="New account created"
            time="1 hour ago"
          />
          <ActivityItem
            icon={Globe}
            text="Domain added: example.com"
            time="3 hours ago"
          />
        </div>
      </div>
    </div>
  )
}

function StatCard({ title, value, icon: Icon, color, trend, trendUp }) {
  return (
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6">
      <div className="flex items-center justify-between mb-4">
        <div className={`${color} p-3 rounded-lg`}>
          <Icon className="w-6 h-6 text-white" />
        </div>
        <div className={`flex items-center text-sm ${trendUp ? 'text-green-500' : 'text-red-500'}`}>
          {trendUp ? <TrendingUp className="w-4 h-4 mr-1" /> : <TrendingDown className="w-4 h-4 mr-1" />}
          {trend}
        </div>
      </div>
      <div className="text-2xl font-bold text-gray-900">{value.toLocaleString()}</div>
      <div className="text-sm text-gray-500">{title}</div>
    </div>
  )
}

function StatusItem({ icon: Icon, label, status, color }) {
  return (
    <div className="flex items-center p-4 bg-gray-50 rounded-lg">
      <Icon className={`w-5 h-5 ${color} mr-3`} />
      <div>
        <div className="font-medium text-gray-900">{label}</div>
        <div className="text-sm text-gray-500 capitalize">{status}</div>
      </div>
    </div>
  )
}

function ActivityItem({ icon: Icon, text, time }) {
  return (
    <div className="flex items-center p-3 hover:bg-gray-50 rounded-lg transition-colors">
      <div className="bg-violet-100 p-2 rounded-lg mr-3">
        <Icon className="w-4 h-4 text-violet-600" />
      </div>
      <div className="flex-1">
        <div className="text-gray-900">{text}</div>
        <div className="text-sm text-gray-500">{time}</div>
      </div>
    </div>
  )
}

export default Dashboard
