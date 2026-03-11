package chat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// ServeLive joins the shared room of channel on the platform named key for user and streams the chat as
// Server-Sent Events (`: connected`, `event: ready`, `event: message`, `event: unavailable`, `: ping`)
// until the request ends, the room's owner stops or the channel's chat becomes unreachable. A failure
// before the first byte is returned as an *apierr.Error and nothing is written; once the stream started,
// ServeLive returns nil. Who may watch, and whether the platform is enabled for them, is the caller's call.
func (s *Service) ServeLive(w http.ResponseWriter, r *http.Request, user, key string, source platform.Chat, channel string) error {
	w.Header().Set("Cache-Control", "no-store")
	if !source.ValidChannel(channel) {
		return apierr.Invalid("Provide a valid channel.")
	}
	release, ok := s.users.acquire(user, s.opts.MaxPerUser)
	if !ok {
		return apierr.New(http.StatusTooManyRequests, apierr.CodeChatConnections, "Too many chat connections are open on this account. Close one and try again.")
	}
	defer release()
	sub, leave, err := s.join(key, source, channel)
	if err != nil {
		return err
	}
	defer leave()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	send := func(format string, args ...any) bool {
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	if !send(": connected\n\n") {
		return nil
	}
	ping := time.NewTicker(s.opts.PingInterval)
	defer ping.Stop()
	ctx := r.Context()
	readyChannel := sub.ready
	var messages <-chan platform.ChatMessage
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sub.ended:
			return nil
		case <-readyChannel:
			readyChannel, messages = nil, sub.messages
			if !send("event: ready\ndata: {}\n\n") {
				return nil
			}
		case message := <-messages:
			data, err := json.Marshal(message)
			if err != nil || !send("event: message\ndata: %s\n\n", data) {
				return nil
			}
		case <-sub.unavailable:
			send("event: unavailable\ndata: {}\n\n")
			return nil
		case <-ping.C:
			if !send(": ping\n\n") {
				return nil
			}
		}
	}
}
