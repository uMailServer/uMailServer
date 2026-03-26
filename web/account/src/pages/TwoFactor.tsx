import { useState } from 'react'
import { Shield, Smartphone, Copy, Check } from 'lucide-react'

function TwoFactorPage() {
  const [enabled, setEnabled] = useState(false)
  const [showSetup, setShowSetup] = useState(false)
  const [verificationCode, setVerificationCode] = useState('')
  const [copied, setCopied] = useState(false)

  const backupCodes = [
    '1234 5678 9012',
    '3456 7890 1234',
    '5678 9012 3456',
    '7890 1234 5678',
    '9012 3456 7890',
    '2345 6789 0123',
    '4567 8901 2345',
    '6789 0123 4567',
  ]

  const copyCodes = () => {
    navigator.clipboard.writeText(backupCodes.join('\n'))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (enabled) {
    return (
      <div>
        <h2 className="text-lg font-medium text-gray-900 mb-6">Two-Factor Authentication</h2>

        <div className="flex items-center p-4 bg-green-50 border border-green-200 rounded-md mb-6">
          <Shield className="h-5 w-5 text-green-600 mr-3" />
          <div>
            <p className="font-medium text-green-800">Two-factor authentication is enabled</p>
            <p className="text-sm text-green-600">Your account is more secure</p>
          </div>
        </div>

        <div className="space-y-6">
          <div>
            <h3 className="text-sm font-medium text-gray-900 mb-2">Backup Codes</h3>
            <p className="text-sm text-gray-600 mb-4">
              Save these backup codes in a safe place. You can use them to sign in if you lose access to your authenticator app.
            </p>

            <div className="bg-gray-100 rounded-md p-4">
              <div className="grid grid-cols-2 gap-2">
                {backupCodes.map((code, index) => (
                  <code key={index} className="text-sm font-mono text-gray-700">
                    {code}
                  </code>
                ))}
              </div>
            </div>

            <button
              onClick={copyCodes}
              className="mt-4 flex items-center text-sm text-primary-600 hover:text-primary-700"
            >
              {copied ? (
                <>
                  <Check className="h-4 w-4 mr-2" />
                  Copied!
                </>
              ) : (
                <>
                  <Copy className="h-4 w-4 mr-2" />
                  Copy codes
                </>
              )}
            </button>
          </div>

          <div className="border-t pt-6">
            <button
              onClick={() => setEnabled(false)}
              className="text-red-600 hover:text-red-700 text-sm font-medium"
            >
              Disable two-factor authentication
            </button>
          </div>
        </div>
      </div>
    )
  }

  if (showSetup) {
    return (
      <div>
        <h2 className="text-lg font-medium text-gray-900 mb-6">Set Up Two-Factor Authentication</h2>

        <div className="space-y-6">
          <div className="flex items-start">
            <div className="flex-shrink-0">
              <div className="flex items-center justify-center h-12 w-12 rounded-full bg-primary-100">
                <Smartphone className="h-6 w-6 text-primary-600" />
              </div>
            </div>
            <div className="ml-4">
              <h3 className="text-base font-medium text-gray-900">1. Scan the QR code</h3>
              <p className="text-sm text-gray-600 mt-1">
                Open your authenticator app and scan this code
              </p>
              <div className="mt-4 bg-white border-2 border-dashed border-gray-300 rounded-lg p-8 flex items-center justify-center">
                <div className="text-center">
                  <div className="bg-gray-900 text-white p-4 rounded">
                    [QR Code Placeholder]
                  </div>
                  <p className="mt-2 text-xs text-gray-500 font-mono">
                    ABCD EFGH IJKL MNOP
                  </p>
                </div>
              </div>
            </div>
          </div>

          <div className="flex items-start">
            <div className="flex-shrink-0">
              <div className="flex items-center justify-center h-12 w-12 rounded-full bg-primary-100 text-primary-600 font-semibold">
                2
              </div>
            </div>
            <div className="ml-4">
              <h3 className="text-base font-medium text-gray-900">2. Enter verification code</h3>
              <p className="text-sm text-gray-600 mt-1">
                Enter the 6-digit code from your authenticator app
              </p>
              <div className="mt-4">
                <input
                  type="text"
                  value={verificationCode}
                  onChange={(e) => setVerificationCode(e.target.value)}
                  placeholder="000000"
                  maxLength={6}
                  className="block w-32 px-3 py-2 border border-gray-300 rounded-md shadow-sm focus:ring-primary-500 focus:border-primary-500 text-center text-lg tracking-widest"
                />
              </div>
            </div>
          </div>

          <div className="flex space-x-3">
            <button
              onClick={() => setShowSetup(false)}
              className="px-4 py-2 border border-gray-300 rounded-md text-sm font-medium text-gray-700 bg-white hover:bg-gray-50"
            >
              Cancel
            </button>
            <button
              onClick={() => setEnabled(true)}
              className="px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-primary-600 hover:bg-primary-700"
            >
              Enable 2FA
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Two-Factor Authentication</h2>

      <div className="flex items-center p-4 bg-yellow-50 border border-yellow-200 rounded-md mb-6">
        <Shield className="h-5 w-5 text-yellow-600 mr-3" />
        <div>
          <p className="font-medium text-yellow-800">Two-factor authentication is not enabled</p>
          <p className="text-sm text-yellow-600">Add an extra layer of security to your account</p>
        </div>
      </div>

      <button
        onClick={() => setShowSetup(true)}
        className="px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-primary-600 hover:bg-primary-700"
      >
        Set up two-factor authentication
      </button>
    </div>
  )
}

export default TwoFactorPage
