package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

const (
	aboutTimeout = 8 * time.Second
	// aboutLimit bounds the channel page read: the observed page is about 1.6 MiB.
	aboutLimit = 8 << 20
	// aboutLanguage pins the language of the channel page: the join date and the counts are display text,
	// parsed in English only.
	aboutLanguage = "en-US,en;q=0.9"
	// initialDataMarker precedes the JSON object of the data the channel page was rendered from.
	initialDataMarker = "var ytInitialData = "
	// consentCookie is the cookie yt-dlp sends to skip the consent interstitial YouTube serves to some
	// regions in place of the channel page.
	consentCookie = "SOCS=CAI"
)

var (
	joinedPattern = regexp.MustCompile(`^Joined ([A-Z][a-z]{2} [0-9]{1,2}, [0-9]{4})$`)
	viewsPattern  = regexp.MustCompile(`^([0-9]{1,3}(?:,[0-9]{3})*|[0-9]+) views?$`)
	videosPattern = regexp.MustCompile(`^([0-9]{1,3}(?:,[0-9]{3})*|[0-9]+) videos?$`)
)

// youtubeLaunch is the day YouTube opened to the public: no channel joined before it.
var youtubeLaunch = time.Date(2005, time.February, 1, 0, 0, 0, 0, time.UTC)

// channelAbout is what the About panel of a channel page reports. A field the page does not report, or
// reports in a shape that is not understood, stays empty: nothing is derived from anything else.
type channelAbout struct {
	CreatedAt  string
	Country    string
	VideoCount *float64
	ViewCount  *float64
}

// aboutPage is the part of the channel page data that holds the About panel. The channel's own page
// opens it on load: its data carries the panel as an engagement panel endpoint.
type aboutPage struct {
	Endpoints []struct {
		Show struct {
			Panel struct {
				List struct {
					Content struct {
						Sections struct {
							Contents []struct {
								Items struct {
									Contents []struct {
										About struct {
											Metadata struct {
												View aboutView `json:"aboutChannelViewModel"`
											} `json:"metadata"`
										} `json:"aboutChannelRenderer"`
									} `json:"contents"`
								} `json:"itemSectionRenderer"`
							} `json:"contents"`
						} `json:"sectionListRenderer"`
					} `json:"content"`
				} `json:"engagementPanelSectionListRenderer"`
			} `json:"engagementPanel"`
		} `json:"showEngagementPanelEndpoint"`
	} `json:"onResponseReceivedEndpoints"`
}

type aboutView struct {
	ChannelID  string `json:"channelId"`
	Country    string `json:"country"`
	JoinedDate struct {
		Content string `json:"content"`
	} `json:"joinedDateText"`
	ViewCount  string `json:"viewCountText"`
	VideoCount string `json:"videoCountText"`
}

// parseAbout reads the About panel out of the HTML of a channel's page. wantID, when not empty, is the
// canonical id the panel must describe. A page without the panel is an error; a panel field that is
// missing or invalid is left out.
func parseAbout(page []byte, wantID string, now time.Time) (channelAbout, error) {
	start := bytes.Index(page, []byte(initialDataMarker))
	if start < 0 {
		return channelAbout{}, errors.New("the channel page carries no data")
	}
	var data aboutPage
	if err := json.NewDecoder(bytes.NewReader(page[start+len(initialDataMarker):])).Decode(&data); err != nil {
		return channelAbout{}, err
	}
	for _, endpoint := range data.Endpoints {
		for _, section := range endpoint.Show.Panel.List.Content.Sections.Contents {
			for _, item := range section.Items.Contents {
				view := item.About.Metadata.View
				if view.ChannelID == "" && view.JoinedDate.Content == "" && view.ViewCount == "" && view.VideoCount == "" {
					continue
				}
				if wantID != "" && view.ChannelID != wantID {
					return channelAbout{}, errors.New("the channel page describes another channel")
				}
				return view.about(now), nil
			}
		}
	}
	return channelAbout{}, errors.New("the channel page carries no About panel")
}

// about converts the display text of the panel. The country is a display name in the language of the page,
// so it is kept only once another field proved that language is the English one that was asked for.
func (v aboutView) about(now time.Time) channelAbout {
	var about channelAbout
	about.CreatedAt = parseJoined(v.JoinedDate.Content, now)
	about.ViewCount = parseCount(viewsPattern, v.ViewCount)
	about.VideoCount = parseCount(videosPattern, v.VideoCount)
	if about.CreatedAt != "" || about.ViewCount != nil || about.VideoCount != nil {
		about.Country = cleanCountry(v.Country)
	}
	return about
}

// parseJoined reads "Joined Aug 22, 2007" as the day 2007-08-22. A date before YouTube existed or after now
// is not a join date.
func parseJoined(text string, now time.Time) string {
	match := joinedPattern.FindStringSubmatch(normalizeSpaces(text))
	if match == nil {
		return ""
	}
	day, err := time.Parse("Jan 2, 2006", match[1])
	if err != nil || day.Before(youtubeLaunch) || day.After(now.UTC().AddDate(0, 0, 1)) {
		return ""
	}
	return day.Format(time.DateOnly)
}

// parseCount reads "6,129 videos" or "395,868,404 views" as a number; anything else is unknown.
func parseCount(pattern *regexp.Regexp, text string) *float64 {
	match := pattern.FindStringSubmatch(normalizeSpaces(text))
	if match == nil {
		return nil
	}
	count, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
	if err != nil {
		return nil
	}
	return &count
}

func cleanCountry(name string) string {
	name = strings.TrimSpace(normalizeSpaces(name))
	if name == "" || len(name) > 100 {
		return ""
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return name
}

func normalizeSpaces(text string) string { return strings.ReplaceAll(text, "\u00a0", " ") }

// aboutPath is the channel page of a canonical id or an @handle; anything else has none.
func aboutPath(id string) (string, bool) {
	switch {
	case channelIDPattern.MatchString(id):
		return "/channel/" + id + "/about", true
	case handlePattern.MatchString(id):
		return "/@" + url.PathEscape(strings.TrimPrefix(id, "@")) + "/about", true
	}
	return "", false
}

// errNoAboutPage means the channel is named by something that has no public page address.
var errNoAboutPage = errors.New("the channel has no page address")

// canonicalID is id when it is a canonical channel id, and nothing otherwise.
func canonicalID(id string) string {
	if channelIDPattern.MatchString(id) {
		return id
	}
	return ""
}

// aboutMetadata reads the About panel of a channel's public page. The request carries no account: the
// panel is public, and a connected session's cookies never leave the yt-dlp runs.
func (p *Platform) aboutMetadata(ctx context.Context, id string) (channelAbout, error) {
	path, ok := aboutPath(id)
	if !ok {
		return channelAbout{}, errNoAboutPage
	}
	ctx, cancel := context.WithTimeout(ctx, aboutTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.aboutURL+path, nil)
	if err != nil {
		return channelAbout{}, err
	}
	request.Header.Set("Accept-Language", aboutLanguage)
	request.Header.Set("Cookie", consentCookie)
	response, err := p.http.Do(request)
	if err != nil {
		return channelAbout{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return channelAbout{}, &statusError{status: response.StatusCode}
	}
	page, err := io.ReadAll(io.LimitReader(response.Body, aboutLimit))
	if err != nil {
		return channelAbout{}, err
	}
	wantID := ""
	if channelIDPattern.MatchString(id) {
		wantID = id
	}
	return parseAbout(page, wantID, time.Now())
}

// enrichChannel completes a channel profile with the About panel of the page at target, a canonical id or
// an @handle. A page that does not answer leaves the profile as it is; the listing it belongs to never
// fails because of it.
func (p *Platform) enrichChannel(ctx context.Context, target string, info *platform.Channel) {
	about, err := p.aboutMetadata(ctx, target)
	if err != nil {
		if ctx.Err() == nil && !errors.Is(err, errNoAboutPage) {
			p.log.Warn("Channel about metadata unavailable", "channel", target, "error", err)
		}
		return
	}
	info.CreatedAt, info.Country, info.VideoCount, info.ViewCount = about.CreatedAt, about.Country, about.VideoCount, about.ViewCount
}
