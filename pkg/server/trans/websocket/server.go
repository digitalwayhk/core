package websocket

// func websocket(own *rest.Server) {
// 	hub := NewHub()
// 	hub.serviceContext = own.context
// 	go hub.Run()
// 	own.context.Hub = hub
// 	own.Server.AddRoute(rest.Route{
// 		Method:  http.MethodGet,
// 		Path:    "/ws",
// 		Handler: websocketHandler(own.context),
// 	})
// }
// func websocketauth(own *rest.Server) {
// 	opts := make([]rest.RouteOption, 0)
// 	opts = append(opts, rest.WithJwt(own.context.Config.Auth.AccessSecret))
// 	own.Server.AddRoute(rest.Route{
// 		Method:  http.MethodGet,
// 		Path:    "/wsauth",
// 		Handler: websocketHandler(own.context),
// 	}, opts...)
// }
// func websocketHandler(sc *router.ServiceContext) http.HandlerFunc {
// 	return func(w http.ResponseWriter, r *http.Request) {
// 		ServeWs(sc.Hub.(*Hub), w, r)
// 	}
// }
