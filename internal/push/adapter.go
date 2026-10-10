package push

// APIAdapter adapts *Service to the internal/api PushService interface, whose
// SendNotification takes a user ID rather than a *Subscription (the method
// sets differ, so *Service cannot be passed to the API directly).
type APIAdapter struct{ *Service }

// NewAPIAdapter wraps svc for use as an api.PushService.
func NewAPIAdapter(svc *Service) *APIAdapter { return &APIAdapter{Service: svc} }

// SendNotification delivers notif to every subscription of userID.
func (a *APIAdapter) SendNotification(userID string, notif *Notification) error {
	return a.Service.SendToUser(userID, notif)
}
