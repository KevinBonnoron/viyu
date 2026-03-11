package platform

// Chapter is one marked section of a video.
type Chapter struct {
	Title     string  `json:"title"`
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
}

// Video is a video as a platform describes it, and as the API answers it. The platform-native ID stays
// server-internal: once the video is stored, RecordID replaces it at the API boundary (see MarshalJSON).
type Video struct {
	// RecordID and ChannelRecordID are the PocketBase ids of the stored video and of its owner channel.
	RecordID              string    `json:"recordId,omitempty"`
	ChannelRecordID       string    `json:"channelRecordId,omitempty"`
	ID                    string    `json:"id"`
	Platform              string    `json:"platform"`
	Title                 string    `json:"title"`
	OriginalTitle         string    `json:"originalTitle"`
	OriginalTitleVerified bool      `json:"originalTitleVerified"`
	OriginalLanguage      string    `json:"originalLanguage"`
	ChannelID             string    `json:"channelId"`
	ChannelName           string    `json:"channelName"`
	ChannelThumbnail      string    `json:"channelThumbnail"`
	Creators              []string  `json:"creators,omitempty"`
	Thumbnail             string    `json:"thumbnail"`
	Thumbnails            []string  `json:"thumbnails"`
	Description           string    `json:"description"`
	Duration              float64   `json:"duration"`
	Tags                  []string  `json:"tags"`
	Chapters              []Chapter `json:"chapters"`
	Published             string    `json:"published"` // "YYYY-MM-DD" for a day, RFC3339 UTC for an exact time, "" if unknown
	ViewCount             float64   `json:"viewCount"`
	LikeCount             *float64  `json:"likeCount"`
	DislikeCount          *float64  `json:"dislikeCount"`
	Source                string    `json:"source,omitempty"`
	// SubscriberOnly marks a VOD that only an account subscribed to the channel can watch.
	SubscriberOnly bool `json:"subscriberOnly,omitempty"`
	// Live marks a stream in progress: it has a live edge to go back to and no fixed duration.
	Live bool `json:"live,omitempty"`
	// Comments says how the platform offers the conversation about this video (CommentsList or
	// CommentsChat); it is set when a single video is answered, and absent when there is none.
	Comments string `json:"comments,omitempty"`
	// Category is what the video or stream is about as its platform puts it: a game, a category.
	Category string `json:"category,omitempty"`

	// FullMetadata marks a video read from its own page, whose details may replace what is stored.
	FullMetadata bool `json:"-"`
	// SubscriberChecked tells a VOD whose restriction was looked up from one that was not: only the
	// former may overwrite what is stored.
	SubscriberChecked bool `json:"-"`
	// Manifest is the HLS manifest the page gave, when it gave one with the metadata.
	Manifest Manifest `json:"-"`
}

// ThumbnailURLs lists the artwork URLs of the video, the main one first.
func (v Video) ThumbnailURLs() []string {
	if len(v.Thumbnails) != 0 {
		return v.Thumbnails
	}
	if v.Thumbnail != "" {
		return []string{v.Thumbnail}
	}
	return nil
}

// Channel is a channel profile.
type Channel struct {
	ID              string  `json:"id"`
	Platform        string  `json:"platform"`
	Name            string  `json:"name"`
	Thumbnail       string  `json:"thumbnail"`
	Description     string  `json:"description"`
	SubscriberCount float64 `json:"subscriberCount"`
	// RecordID is the PocketBase id of the stored channel.
	RecordID string `json:"recordId,omitempty"`
}

// ChannelCard is a channel as a search result shows it.
type ChannelCard struct {
	ID        string `json:"id"`
	Platform  string `json:"platform"`
	Name      string `json:"name"`
	Thumbnail string `json:"thumbnail"`
	Source    string `json:"source,omitempty"`
	// RecordID is the PocketBase id of the stored channel; it replaces ID at the API boundary.
	RecordID string `json:"recordId,omitempty"`
}

// Result is one search result: a video or a channel card.
type Result struct {
	Video   *Video
	Channel *ChannelCard
}

// SearchPage is one page of search results.
type SearchPage struct {
	Results []Result `json:"results"`
	Page    int      `json:"page"`
	HasMore bool     `json:"hasMore"`
	// Failures names the platforms a combined search could not query, so the page can say why their results
	// are missing instead of silently showing only the others.
	Failures []SearchFailure `json:"failures,omitempty"`
}

// SearchFailure is a platform a combined search skipped, with the client-safe error it answered.
type SearchFailure struct {
	Platform string `json:"platform"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// LiveStream says whether a channel is broadcasting now and, if so, which video plays it.
type LiveStream struct {
	Live bool `json:"live"`
	// VideoID is the stream's native video ID (the channel login on Twitch). It stays server-side: clients
	// open the stream through RecordID instead.
	VideoID string `json:"-"`
	// RecordID is the PocketBase id of the stored stream video.
	RecordID string  `json:"recordId,omitempty"`
	Title    string  `json:"title,omitempty"`
	Viewers  float64 `json:"viewers,omitempty"`
	// Category is the game or topic of the stream, when the platform says it.
	Category string `json:"category,omitempty"`
	// Thumbnail is the preview picture when the platform's lookup already knows it; the others derive it from VideoID.
	Thumbnail string `json:"-"`
}

// Manifest is the HLS entry point of a video and the headers its host expects.
type Manifest struct {
	URL     string
	Headers map[string]string
}

// Comment is one top-level comment with the bounded replies the platform fetched.
type Comment struct {
	ID          string `json:"id"`
	Author      string `json:"author"`
	Text        string `json:"text"`
	PublishedAt string `json:"publishedAt"`
	LikeCount   int64  `json:"likeCount"`
	// Replies are the answer threads fetched for this comment; absent when the platform offered none
	// (a thread with no replies) or when it does not fetch them.
	Replies []Comment `json:"replies,omitempty"`
}

// The ways a platform offers the conversation about a video; Video.Comments carries one of them.
const (
	CommentsList = "list"
	CommentsChat = "chat"
)

// Session says who a platform request runs as. The zero value is a public request; a connected one carries
// the account and the private credentials made for that single request.
type Session struct {
	UserID string
	// CookieFile is the private cookie jar handed to the downloader.
	CookieFile string
	// Cookies is the decrypted, normalized cookie text, for platforms that call their own API with it.
	Cookies string
}

// Connected tells whether the request runs as a connected account.
func (s Session) Connected() bool { return s.UserID != "" }

// Feed is one page of a connected account's personal feed.
type Feed struct {
	Videos  []Video `json:"videos"`
	Page    int     `json:"page"`
	HasMore bool    `json:"hasMore"`
}

// Descriptor is what the Sources screen shows of a platform: name, description, capabilities and the
// ways an account can connect (none for a platform that is public only).
type Descriptor struct {
	Platform          string   `json:"platform"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Capabilities      []string `json:"capabilities"`
	ConnectionMethods []string `json:"connectionMethods"`
}

// LiveEnd says what to forget of a channel that has just been found not to be live.
type LiveEnd struct {
	// ChannelIDs are the forms under which the channel may be stored.
	ChannelIDs []string
	// StubVideoID is the native id of the pseudo-video that stands for the stream in progress, "" when the
	// platform stores the stream under its final video id. The stub is deleted unless something references
	// it; every other stream video of the channel is turned into an ordinary video (live flag cleared,
	// metadata expired so the next opening fetches the final duration and date).
	StubVideoID string
}
