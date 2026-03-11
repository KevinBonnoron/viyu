package httpapi

// registerAccount registers the routes of the signed-in account: its own settings, the transfer of its data
// and the administration of the instance's users. Only the registration status is public.
func (s *Server) registerAccount(api Routes) {
	api.GET("/api/account", s.account)
	api.PATCH("/api/account", s.updateAccount)
	api.GET("/api/account/export", s.exportAccount)
	api.POST("/api/account/import", s.importAccount)

	api.PublicGET("/api/registration", s.registration)
	api.GET("/api/admin/registration", s.adminRegistration)
	api.PATCH("/api/admin/registration", s.adminSetRegistration)

	api.GET("/api/admin/users", s.adminListUsers)
	api.POST("/api/admin/users", s.adminCreateUser)
	api.PATCH("/api/admin/users/{id}", s.adminUpdateUser)
	api.DELETE("/api/admin/users/{id}", s.adminDeleteUser)
}
