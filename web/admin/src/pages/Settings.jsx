import { useState } from 'react'
import { Save, Shield, Server, Mail } from 'lucide-react'

function Settings() {
  const [activeTab, setActiveTab] = useState('general')
  const [settings, setSettings] = useState({
    hostname: '',
    maxMessageSize: 50,
    smtpPort: 587,
    imapPort: 993,
    enableTls: true,
    enableSpamFilter: true
  })

  const handleSave = (e) => {
    e.preventDefault()
    // TODO: Save settings to API
    alert('Settings saved (not implemented)')
  }

  const tabs = [
    { id: 'general', label: 'General', icon: Server },
    { id: 'smtp', label: 'SMTP', icon: Mail },
    { id: 'security', label: 'Security', icon: Shield }
  ]

  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900 mb-6">Settings</h1>

      <div className="flex flex-col lg:flex-row gap-6">
        {/* Sidebar */}
        <div className="w-full lg:w-64">
          <nav className="space-y-1">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`w-full flex items-center px-4 py-2 text-left rounded-lg transition-colors ${
                  activeTab === tab.id
                    ? 'bg-violet-100 text-violet-700'
                    : 'text-gray-700 hover:bg-gray-100'
                }`}
              >
                <tab.icon className="w-5 h-5 mr-3" />
                {tab.label}
              </button>
            ))}
          </nav>
        </div>

        {/* Content */}
        <div className="flex-1 bg-white rounded-lg shadow-sm border border-gray-200 p-6">
          <form onSubmit={handleSave}>
            {activeTab === 'general' && (
              <div className="space-y-6">
                <h2 className="text-lg font-semibold text-gray-900">General Settings</h2>

                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    Server Hostname
                  </label>
                  <input
                    type="text"
                    value={settings.hostname}
                    onChange={(e) => setSettings({ ...settings, hostname: e.target.value })}
                    placeholder="mail.example.com"
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-violet-500"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    Max Message Size (MB)
                  </label>
                  <input
                    type="number"
                    value={settings.maxMessageSize}
                    onChange={(e) => setSettings({ ...settings, maxMessageSize: parseInt(e.target.value) })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-violet-500"
                    min="1"
                    max="100"
                  />
                </div>
              </div>
            )}

            {activeTab === 'smtp' && (
              <div className="space-y-6">
                <h2 className="text-lg font-semibold text-gray-900">SMTP Settings</h2>

                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    SMTP Port
                  </label>
                  <input
                    type="number"
                    value={settings.smtpPort}
                    onChange={(e) => setSettings({ ...settings, smtpPort: parseInt(e.target.value) })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-violet-500"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    IMAP Port
                  </label>
                  <input
                    type="number"
                    value={settings.imapPort}
                    onChange={(e) => setSettings({ ...settings, imapPort: parseInt(e.target.value) })}
                    className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-violet-500"
                  />
                </div>

                <div>
                  <label className="flex items-center">
                    <input
                      type="checkbox"
                      checked={settings.enableTls}
                      onChange={(e) => setSettings({ ...settings, enableTls: e.target.checked })}
                      className="w-4 h-4 text-violet-600 border-gray-300 rounded focus:ring-violet-500"
                    />
                    <span className="ml-2 text-sm text-gray-700">Enable TLS</span>
                  </label>
                </div>
              </div>
            )}

            {activeTab === 'security' && (
              <div className="space-y-6">
                <h2 className="text-lg font-semibold text-gray-900">Security Settings</h2>

                <div>
                  <label className="flex items-center">
                    <input
                      type="checkbox"
                      checked={settings.enableSpamFilter}
                      onChange={(e) => setSettings({ ...settings, enableSpamFilter: e.target.checked })}
                      className="w-4 h-4 text-violet-600 border-gray-300 rounded focus:ring-violet-500"
                    />
                    <span className="ml-2 text-sm text-gray-700">Enable Spam Filter</span>
                  </label>
                </div>
              </div>
            )}

            <div className="mt-8 pt-6 border-t border-gray-200">
              <button
                type="submit"
                className="flex items-center px-4 py-2 bg-violet-600 text-white rounded-lg hover:bg-violet-700 transition-colors"
              >
                <Save className="w-5 h-5 mr-2" />
                Save Settings
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  )
}

export default Settings
