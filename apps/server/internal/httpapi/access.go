package httpapi

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
	"github.com/KevinBonnoron/viyu/apps/server/internal/catalog"
	"github.com/KevinBonnoron/viyu/apps/server/internal/media"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

// connection says which connected source a request asked for (?source=) and the revision of the session
// it is bound to. The zero value is a public request.
type connection struct {
	Source   string
	Revision string
}

// Connected tells whether the request runs as a connected account.
func (c connection) Connected() bool { return c.Source != "" }

// connectionOf validates a request that asks for a connected source: the account must be signed in, use
// the source, and have it connected. A public request (no ?source=) needs nothing. The answer is private
// to the account, so caches must not keep it.
func (s *Server) connectionOf(e *core.RequestEvent) (connection, error) {
	source := e.Request.URL.Query().Get("source")
	if source == "" {
		return connection{}, nil
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	if e.Auth == nil || e.Auth.Collection().Name != "users" {
		return connection{}, apierr.SourceAuthRequired()
	}
	if err := s.Sources.RequireEnabled(e.Auth.Id, source); err != nil {
		return connection{}, err
	}
	revision, err := s.Sources.SessionVersion(e.Auth.Id, source)
	if err != nil {
		return connection{}, err
	}
	return connection{Source: source, Revision: revision}, nil
}

// session is connectionOf plus the private credentials of the connection for one downloader process. The
// returned cleanup must always be called.
func (s *Server) session(e *core.RequestEvent) (connection, platform.Session, func(), error) {
	conn, err := s.connectionOf(e)
	if err != nil || !conn.Connected() {
		return conn, platform.Session{}, func() {}, err
	}
	session, cleanup, err := s.Sources.Open(e.Auth.Id, conn.Source)
	if err != nil {
		return conn, platform.Session{}, func() {}, err
	}
	return conn, session, cleanup, nil
}

// requestedPlatform names the platform a request is about: the one the selected source implies, else the
// `platform` parameter, else the registry's default.
func (s *Server) requestedPlatform(e *core.RequestEvent, source string) (platform.Platform, error) {
	id := e.Request.URL.Query().Get("platform")
	if source != "" {
		if id != "" && id != source {
			return nil, apierr.InvalidInput("The selected source and platform must match.")
		}
		id = source
	}
	p, ok := s.Platforms.Or(id)
	if !ok {
		return nil, apierr.InvalidInput("Unknown video platform.")
	}
	return p, nil
}

// streamKey authorizes a stream or resource request with the account's normal bearer token (the routes are
// bound to RequireAuth): the account must use the video's platform, and a connected source must still be the
// very connection (revision) the manifest was made for. It returns the manifest key and the source asked for.
func (s *Server) streamKey(e *core.RequestEvent) (media.Key, string, error) {
	source := e.Request.URL.Query().Get("source")
	recordID := e.Request.PathValue("videoId")
	record, err := s.Store.Video(recordID)
	if err != nil || !catalog.VisibleTo(record, viewerID(e)) {
		return media.Key{}, source, apierr.StreamUnavailable()
	}
	platformID, nativeID := record.GetString("platform"), record.GetString("platform_id")
	if source != "" && platformID != source {
		return media.Key{}, source, apierr.InvalidInput("The selected source and platform must match.")
	}
	if e.Auth == nil || e.Auth.Collection().Name != "users" {
		return media.Key{}, source, apierr.AuthRequired()
	}
	key := media.Key{VideoID: nativeID, RecordID: recordID, Platform: platformID}
	if err := s.Sources.RequireEnabled(e.Auth.Id, platformID); err != nil {
		return media.Key{}, source, err
	}
	if source == "" {
		return key, source, nil
	}
	revision, err := s.Sources.SessionVersion(e.Auth.Id, source)
	if err != nil {
		return media.Key{}, source, err
	}
	key.UserID, key.Revision = e.Auth.Id, revision
	return key, source, nil
}
