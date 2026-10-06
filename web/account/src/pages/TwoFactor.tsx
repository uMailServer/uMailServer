import { Shield } from 'lucide-react'

function TwoFactorPage() {
  // The portal has no self-service 2FA API: the server's TOTP sub-paths
  // (/api/v1/accounts/{email}/totp/...) sit behind adminMiddleware, so this
  // page can neither know nor change the account's real 2FA status. State
  // the limitation instead of fabricating status, QR codes, or recovery
  // codes.
  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Two-Factor Authentication</h2>

      <div className="flex items-center p-4 bg-yellow-50 border border-yellow-200 rounded-md">
        <Shield className="h-5 w-5 text-yellow-600 mr-3" />
        <div>
          <p className="font-medium text-yellow-800">
            Two-factor authentication is managed by your administrator
          </p>
          <p className="text-sm text-yellow-600">
            This portal cannot set up or disable two-factor authentication yet.
            Contact your administrator to enable it for your account.
          </p>
        </div>
      </div>
    </div>
  )
}

export default TwoFactorPage
