package compat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/getkin/kin-openapi/openapi3"
)

const canonicalServerURL = "https://compat.invalid/"

type RouteEntry struct {
	Service         string   `json:"service"`
	Method          string   `json:"method"`
	Path            string   `json:"path"`
	PathType        string   `json:"pathType"`
	Auth            bool     `json:"auth"`
	InternalCallers []string `json:"internalCallers,omitempty"`
}

func SnapshotRoutes(services ...*router.ServiceRouter) ([]byte, error) {
	entries := make([]RouteEntry, 0)
	seen := make(map[string]struct{})
	for _, service := range services {
		if service == nil {
			return nil, fmt.Errorf("route snapshot: nil service router")
		}
		for _, info := range service.GetRouters() {
			if info == nil || info.GetMethod() == "" || info.GetPath() == "" {
				return nil, fmt.Errorf("route snapshot: route method and path are required")
			}
			method := strings.ToUpper(info.GetMethod())
			path := info.GetPath()
			key := method + " " + path
			if _, ok := seen[key]; ok {
				return nil, fmt.Errorf("route snapshot: duplicate route %s", key)
			}
			seen[key] = struct{}{}
			entries = append(entries, RouteEntry{
				Service:         info.GetServiceName(),
				Method:          method,
				Path:            path,
				PathType:        string(info.GetPathType()),
				Auth:            info.GetAuth(),
				InternalCallers: info.GetInternalCallers(),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		if entries[i].Method != entries[j].Method {
			return entries[i].Method < entries[j].Method
		}
		return entries[i].Service < entries[j].Service
	})
	return marshalSnapshot(entries)
}

func SnapshotOpenAPI(req *http.Request, services ...*router.ServiceRouter) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("openapi snapshot: request is nil")
	}
	if err := validateOpenAPIInputs(services); err != nil {
		return nil, err
	}
	doc, ok := run.GetOpenApi(req, services...).(*openapi3.T)
	if !ok || doc == nil {
		return nil, fmt.Errorf("openapi snapshot: unexpected document type")
	}
	normalizeOpenAPI(doc)
	return marshalSnapshot(doc)
}

func validateOpenAPIInputs(services []*router.ServiceRouter) error {
	routes := make(map[string]struct{})
	operationIDs := make(map[string]struct{})
	for _, service := range services {
		if service == nil {
			return fmt.Errorf("openapi snapshot: nil service router")
		}
		infos := append(service.GetTypeRouters("public"), service.GetTypeRouters("private")...)
		for _, info := range infos {
			if info == nil || info.GetMethod() == "" || info.GetPath() == "" {
				return fmt.Errorf("openapi snapshot: route method and path are required")
			}
			method := strings.ToUpper(info.GetMethod())
			path := info.GetPath()
			key := method + " " + path
			if _, ok := routes[key]; ok {
				return fmt.Errorf("openapi snapshot: duplicate route %s", key)
			}
			routes[key] = struct{}{}
			if _, ok := operationIDs[path]; ok {
				return fmt.Errorf("openapi snapshot: duplicate operationId %s", path)
			}
			operationIDs[path] = struct{}{}
		}
	}
	return nil
}

func normalizeOpenAPI(doc *openapi3.T) {
	doc.Servers = openapi3.Servers{}
	if len(doc.Tags) > 0 {
		for _, tag := range doc.Tags {
			tag.Description = ""
		}
		sort.Slice(doc.Tags, func(i, j int) bool { return doc.Tags[i].Name < doc.Tags[j].Name })
	}
	if doc.Paths == nil {
		doc.Paths = openapi3.NewPaths()
	}
	for _, pathItem := range doc.Paths.Map() {
		for _, operation := range []*openapi3.Operation{
			pathItem.Connect, pathItem.Delete, pathItem.Get, pathItem.Head,
			pathItem.Options, pathItem.Patch, pathItem.Post, pathItem.Put, pathItem.Trace,
		} {
			if operation != nil {
				operation.Servers = &openapi3.Servers{{URL: canonicalServerURL}}
				normalizeOperation(operation)
			}
		}
	}
	if bearer := doc.Components.SecuritySchemes["Bearer"]; bearer != nil && bearer.Value != nil {
		bearer.Value.Description = "Bearer token authentication"
	}
}

func normalizeOperation(operation *openapi3.Operation) {
	if operation.Responses == nil {
		return
	}
	for _, response := range operation.Responses.Map() {
		if response == nil || response.Value == nil {
			continue
		}
		for _, media := range response.Value.Content {
			if media != nil && media.Schema != nil && media.Schema.Value != nil {
				media.Schema.Value.Example = nil
			}
		}
	}
}

func marshalSnapshot(value interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal compatibility snapshot: %w", err)
	}
	return append(data, '\n'), nil
}
