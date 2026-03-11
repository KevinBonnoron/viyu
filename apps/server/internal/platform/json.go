package platform

import (
	"encoding/json"
	"errors"
)

// This file is the API boundary for identifiers: platform-native ids (YouTube, Twitch, PeerTube…) stay
// server-internal. Everything a client receives addresses videos and channels by their PocketBase record
// ids, and marshaling performs the swap, so every handler serializes with it for free. A value that was
// never stored has no record id to show and refuses to serialize: the native id never leaves the server.

// ErrNotStored is the failure of serializing a video or channel that has no record id.
var ErrNotStored = errors.New("the record is not stored: its native id must not leave the server")

// MarshalJSON addresses the video by its record id; its owner channel, when it has one, is addressed by the
// channel record id for the same reason.
func (v Video) MarshalJSON() ([]byte, error) {
	if v.RecordID == "" {
		return nil, ErrNotStored
	}
	type plain Video
	out := plain(v)
	out.ID = out.RecordID
	if out.ChannelRecordID != "" {
		out.ChannelID = out.ChannelRecordID
	} else if out.ChannelID != "" {
		return nil, ErrNotStored
	}
	return json.Marshal(out)
}

// MarshalJSON addresses the channel by its record id.
func (c Channel) MarshalJSON() ([]byte, error) {
	if c.RecordID == "" {
		return nil, ErrNotStored
	}
	type plain Channel
	out := plain(c)
	out.ID = out.RecordID
	return json.Marshal(out)
}

// MarshalJSON addresses the search channel card by its record id.
func (c ChannelCard) MarshalJSON() ([]byte, error) {
	if c.RecordID == "" {
		return nil, ErrNotStored
	}
	type plain ChannelCard
	out := plain(c)
	out.ID = out.RecordID
	return json.Marshal(out)
}

// MarshalJSON writes whichever of the video and the channel card the result holds.
func (r Result) MarshalJSON() ([]byte, error) {
	switch {
	case r.Video != nil:
		return json.Marshal(r.Video)
	case r.Channel != nil:
		return json.Marshal(r.Channel)
	}
	return []byte("null"), nil
}
