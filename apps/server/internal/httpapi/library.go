package httpapi

// registerLibrary registers what an account keeps on the platforms and in its profiles: the connected
// sources, the subscriptions and their groups, the notifications, the watch history, the feed and the
// statistics.
func (s *Server) registerLibrary(api Routes) {
	api.GET("/api/sources", private(s.listSources))
	api.PUT("/api/sources/{platform}/activation", private(s.setSourceActivation))
	api.POST("/api/sources/{platform}/connect", private(s.connectSource))
	api.POST("/api/sources/{platform}/check", private(s.checkSource))
	api.DELETE("/api/sources/{platform}", private(s.disconnectSource))
	api.GET("/api/sources/{platform}/feed", private(s.sourceFeed))
	api.POST("/api/sources/{platform}/watch", private(s.watchSource))

	api.POST("/api/subscriptions", s.createSubscription)
	api.POST("/api/subscriptions/group", s.groupSubscriptions)
	api.POST("/api/subscriptions/ungroup", s.ungroupSubscriptions)
	api.POST("/api/subscriptions/move", s.moveSubscriptions)
	api.POST("/api/subscriptions/refresh", s.refreshSubscriptions)

	api.GET("/api/notifications", s.notifications)
	api.POST("/api/notifications/seen", s.markNotificationsSeen)
	api.DELETE("/api/history", s.clearHistory)

	api.GET("/api/feed", s.profileFeed)
	api.GET("/api/recommendations", s.recommendations)
	api.GET("/api/stats", s.profileStats)
}
