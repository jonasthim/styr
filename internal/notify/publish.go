package notify

import "context"

// Publish sends ev to every channel in channels whose Events list contains
// ev.Kind. It is fire-and-forget for the caller (it never returns an
// error and one channel's failure never affects another's delivery) but
// synchronous, so tests and callers that want to wait for delivery attempts
// can just call it. Every attempt is logged at info; a failed attempt is
// additionally logged at warn. The channel token is never logged.
func (s *Service) Publish(ctx context.Context, channels []Channel, ev Event) {
	for _, ch := range channels {
		if !wantsEvent(ch, ev.Kind) {
			continue
		}
		s.logger.Info("notify: sending",
			"channel_id", ch.ID, "channel_kind", ch.Kind, "event_kind", ev.Kind)
		if err := s.Send(ctx, ch, ev); err != nil {
			s.logger.Warn("notify: send failed",
				"channel_id", ch.ID, "channel_kind", ch.Kind, "event_kind", ev.Kind, "error", err)
		}
	}
}

func wantsEvent(ch Channel, kind string) bool {
	for _, want := range ch.Events {
		if want == kind {
			return true
		}
	}
	return false
}
