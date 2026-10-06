import { User } from 'lucide-react'

function ProfilePage() {
  // The portal has no self-service profile API: the server exposes no
  // displayName/timezone fields in its account model and the account routes
  // are admin-gated, so this page can neither show nor edit real profile
  // data — it does not even know the signed-in address (no session
  // context). State the limitation instead of a fabricated identity and a
  // save flow that persists nothing.
  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Profile Settings</h2>

      <div className="flex items-center p-4 bg-blue-50 border border-blue-200 rounded-md">
        <User className="h-5 w-5 text-blue-600 mr-3" />
        <div className="text-sm text-blue-700">
          <p className="font-medium mb-1">Managed by your administrator</p>
          <p>
            This portal cannot display or edit profile settings yet. Contact
            your administrator for changes to your account.
          </p>
        </div>
      </div>
    </div>
  )
}

export default ProfilePage
