import { AlertCircle } from 'lucide-react'

function ForwardingPage() {
  // The portal has no self-service forwarding API: the account model's
  // ForwardTo/ForwardKeepCopy fields exist only on the admin-gated
  // PUT /api/v1/accounts/{email}, and this page has no session context
  // holding the account address. State the limitation instead of describing
  // or simulating forwarding that cannot be configured.
  return (
    <div>
      <h2 className="text-lg font-medium text-gray-900 mb-6">Mail Forwarding</h2>

      <div className="flex items-start p-4 bg-blue-50 border border-blue-200 rounded-md">
        <AlertCircle className="h-5 w-5 text-blue-600 mr-3 flex-shrink-0 mt-0.5" />
        <div className="text-sm text-blue-700">
          <p className="font-medium mb-1">Managed by your administrator</p>
          <p>
            This portal cannot configure mail forwarding yet. Contact your
            administrator to set up forwarding for your account.
          </p>
        </div>
      </div>
    </div>
  )
}

export default ForwardingPage
