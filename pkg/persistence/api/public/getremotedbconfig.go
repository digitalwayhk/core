package public

// type GetRemoteDBConfig struct {
// 	Name string `json:"name"`
// }

// func (own *GetRemoteDBConfig) Parse(req types.IRequest) error {
// 	own.Name = req.GetValue("name")
// 	return nil
// }
// func (own *GetRemoteDBConfig) Validation(req types.IRequest) error {
// 	return nil
// }

// func (own *GetRemoteDBConfig) Do(req types.IRequest) (interface{}, error) {
// 	list := models.NewRemoteDbConfigList(false)
// 	if own.Name != "" {
// 		return list.SearchName(own.Name)
// 	}
// 	res, _, err := list.SearchAll(1, 10)
// 	return res, err
// }

// func (own *GetRemoteDBConfig) RouterInfo() *types.RouterInfo {
// 	return api.ServerRouterInfo(own)
// }
