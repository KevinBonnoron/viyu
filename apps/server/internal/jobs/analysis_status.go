package jobs

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// ChannelStatus is where the series analysis of one channel stands. ChannelID is the channel's record id.
type ChannelStatus struct {
	ChannelID   string `json:"channelId"`
	ChannelName string `json:"channelName"`
	Platform    string `json:"platform"`
	Status      Status `json:"status"`
	Loaded      int    `json:"loaded"`
	// Episodes is the part of Status that can still add episodes (the catalog and the analysis worker);
	// Dates is the part that can still fill publication dates (the metadata jobs).
	Episodes Status `json:"-"`
	Dates    Status `json:"-"`
}

// Overview is where the series analysis of a set of channels stands: the channels, the jobs they wait for,
// and the one status that tells whether anything is still going on. Serialized, it is the "analysis" of the
// series answer; Sync is its "sync".
type Overview struct {
	Channels []ChannelStatus `json:"channels"`
	Metadata Counts          `json:"metadata"`
	OCR      Counts          `json:"ocr"`
	// Status is the status of the channels merged: running outranks pending, which outranks complete. It is
	// never an error: what failed is tried again (pending) or failed for good (nothing to wait for).
	Status Status `json:"-"`
	// Loaded is the number of catalog videos loaded across the channels.
	Loaded int `json:"-"`
}

// Sync is the overall state of an Overview.
func (o Overview) Sync() Sync { return Sync{Status: o.Status, Loaded: o.Loaded} }

// Overview reports the analysis of channels, given by their records. A channel whose platform lists no
// catalog is not part of the answer: it has nothing to analyse. A channel analysed twice under two ids of the
// same channel is reported once.
//
// It answers from what is stored (one batched query for the catalogs of all channels) and from the counters
// of the queues, so polling it is cheap however many channels and videos there are. It never infers
// anything and never reads series. A catalog that is due (complete and old, or never analysed since the
// process started) is reported pending and wakes its channel.
func (a *Analysis) Overview(channels []*core.Record) (Overview, error) {
	out := Overview{Channels: []ChannelStatus{}, Status: Complete}
	listed := a.distinct(channels)
	states, err := a.catalogs.states(listed)
	if err != nil {
		return out, err
	}
	inUse := map[string]bool{}
	for _, channel := range listed {
		platformID := channel.GetString("platform")
		used, known := inUse[platformID]
		if !known {
			used = a.sources.PlatformInUse(platformID)
			inUse[platformID] = used
		}
		state := states[channel.Id]
		counts := a.jobCounts(channel.Id)
		status := a.channelStatus(channel.Id, state, counts, used)
		episodes, dates := Complete, Complete
		if used {
			episodes = Merge(Complete, Merge(state.Status, a.workerStatus(channel.Id, state)))
			dates = countsStatus(counts.metadata)
		}
		out.Channels = append(out.Channels, ChannelStatus{ChannelID: channel.Id, ChannelName: channel.GetString("name"), Platform: platformID, Status: status, Loaded: state.Loaded, Episodes: episodes, Dates: dates})
		out.Status = Merge(out.Status, status)
		out.Loaded += state.Loaded
		out.Metadata.Merge(counts.metadata)
		out.OCR.Merge(counts.ocr)
	}
	return out, nil
}

// ChannelStatus reports the analysis of one channel (see Overview). A channel with nothing to analyse is
// complete.
func (a *Analysis) ChannelStatus(channel *core.Record) (ChannelStatus, error) {
	overview, err := a.Overview([]*core.Record{channel})
	if err != nil {
		return ChannelStatus{}, err
	}
	if len(overview.Channels) == 0 {
		return ChannelStatus{ChannelID: channel.Id, ChannelName: channel.GetString("name"), Platform: channel.GetString("platform"), Status: Complete, Episodes: Complete, Dates: Complete}, nil
	}
	return overview.Channels[0], nil
}

// distinct keeps the channels whose platform lists catalogs, each channel once.
func (a *Analysis) distinct(channels []*core.Record) []*core.Record {
	seen := make(map[CatalogKey]struct{}, len(channels))
	kept := make([]*core.Record, 0, len(channels))
	for _, channel := range channels {
		key, ok := a.catalogs.canonicalKey(channel)
		if _, dup := seen[key]; !ok || dup {
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, channel)
	}
	return kept
}

// channelStatus merges what stands in the way of a channel's series being up to date: its catalog, the
// worker, and its metadata and thumbnail jobs. A platform nobody uses is complete: nothing will run for it.
func (a *Analysis) channelStatus(channelID string, catalog catalogState, counts channelCounts, inUse bool) Status {
	if !inUse {
		return Complete
	}
	status := Merge(catalog.Status, a.workerStatus(channelID, catalog))
	status = Merge(status, countsStatus(counts.metadata))
	return Merge(status, countsStatus(counts.ocr))
}

// workerStatus is the state of the analysis worker for a channel. A channel it never analysed since the
// start, or whose catalog is due for a new synchronization, is woken: this is what turns "stored series that
// nobody computed in this process" into work, without inferring anything on the read.
func (a *Analysis) workerStatus(channelID string, catalog catalogState) Status {
	mark := a.marks.get(channelID)
	if mark == "" || (mark == Complete && catalog.Stale) {
		a.Wake(channelID)
		return Pending
	}
	if mark == Complete {
		return ""
	}
	return mark
}

// countsStatus is the status of a set of jobs: running if one runs, pending if one waits (one that failed
// and waits for its next attempt included), else complete. A job that failed for good is not reported: it is
// not retried and nothing is waited for, so clients are not told of what they cannot act on.
func countsStatus(c Counts) Status {
	switch {
	case c.Running > 0:
		return Running
	case c.Pending > 0:
		return Pending
	}
	return Complete
}
