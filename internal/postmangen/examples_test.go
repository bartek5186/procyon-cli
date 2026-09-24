package postmangen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGenerateKeepsStaticRouteExamplesSeparateAndSupportsMedia(t *testing.T) {
	project := t.TempDir()
	writePostmanTestFile(t, filepath.Join(project, "routes.go"), `package main
func registerPublicRoutes(e *Echo, app *application) {
 e.GET("/v1/materials/:id", app.Detail)
 e.GET("/v1/materials/ranking", app.Ranking)
 e.GET("/v1/materials/:id/stream/*", app.Stream)
 e.HEAD("/v1/materials/:id/stream/*", app.StreamHead)
 e.GET("/v1/materials/:id/media", app.Media)
}
func registerAdminRoutes(e *Echo, app *application) {}
func registerUploadRoutes(e *Echo, app *application) {}
`)
	writePostmanTestFile(t, filepath.Join(project, "controllers", "media.go"), `package controllers
// Stream returns an HLS playlist.
func Stream() {}
`)
	writePostmanTestFile(t, filepath.Join(project, "plugins", "media", "plugin.go"), `package media
type Plugin struct{}
func (*Plugin) RegisterRoutes(routes Routes) { routes.Authenticated.HEAD("/media/:id", head) }
`)
	writePostmanTestFile(t, filepath.Join(project, "plugins", "media", "docs", "postman", "examples.json"), `{"examples":[
 {"key":"HEAD /v1/media/:id","response":{"status":200,"headers":{"ETag":"plugin-version"},"body":{"must_not_be_sent":true}}}
]}`)
	writePostmanTestFile(t, filepath.Join(project, "docs", "postman", "examples.json"), `{"examples":[
 {"key":"GET /v1/materials/:id","name":"Detail","request":{"path":{"id":"12"}},"response":{"status":200,"body":{"id":12}}},
 {"key":"GET /v1/materials/ranking","name":"Ranking","default":true,"request":{"query":{"sort":"likes"}},"response":{"status":200,"body":{"items":[]}}},
 {"key":"GET /v1/materials/:id/stream/*","name":"Playlist","request":{"path":{"id":"12","*":"pl/master.m3u8"}},"response":{"status":200,"headers":{"Content-Type":"application/vnd.apple.mpegurl","ETag":"v3","Cache-Control":"private, no-store"},"body":"#EXTM3U\n#EXT-X-VERSION:3\n"}},
 {"key":"HEAD /v1/materials/:id/stream/*","request":{"path":{"*":"pl/master.m3u8"}},"response":{"status":200,"headers":{"Content-Type":"application/vnd.apple.mpegurl","ETag":"v3"},"body":"must not be sent"}},
 {"key":"GET /v1/materials/:id/media","name":"Redirect","response":{"status":307,"headers":{"Location":"https://media.example.test/video.mp4"}}}
]}`)
	output := filepath.Join(project, "collection.json")
	result, err := Generate(Options{Root: project, Out: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.RouteCount != 6 {
		t.Fatalf("got %d routes, want 6 including application and plugin HEAD", result.RouteCount)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var collection postmanCollection
	if err := json.Unmarshal(data, &collection); err != nil {
		t.Fatal(err)
	}
	items := map[string]*postmanItem{}
	var walk func([]postmanItem)
	walk = func(nodes []postmanItem) {
		for i := range nodes {
			item := &nodes[i]
			if item.Request != nil {
				items[item.Request.Method+" /"+strings.Join(item.Request.URL.Path, "/")] = item
			}
			walk(item.Item)
		}
	}
	walk(collection.Item)
	detail := items["GET /v1/materials/:id"]
	if detail == nil || len(detail.Response) != 1 || !strings.HasSuffix(detail.Response[0].Name, " - Detail") || len(detail.Request.URL.Query) != 0 {
		t.Fatalf("ranking leaked into detail: %+v", detail)
	}
	ranking := items["GET /v1/materials/ranking"]
	if ranking == nil || len(ranking.Response) != 1 || len(ranking.Request.URL.Query) != 1 {
		t.Fatalf("missing ranking example: %+v", ranking)
	}
	stream := items["GET /v1/materials/:id/stream/:wildcardPath"]
	if stream == nil || len(stream.Response) != 1 || stream.Response[0].Body != "#EXTM3U\n#EXT-X-VERSION:3\n" {
		t.Fatalf("invalid playlist response: %+v", stream)
	}
	if stream.Request.Description != "Stream returns an HLS playlist." || stream.Request.URL.Raw != "{{baseURL}}/v1/materials/:id/stream/:wildcardPath" {
		t.Fatalf("invalid stream request: %+v", stream.Request)
	}
	for _, req := range []*postmanRequest{stream.Request, stream.Response[0].OriginalRequest} {
		want := []postmanURLVariable{{Key: "id", Value: "12"}, {Key: "wildcardPath", Value: "pl/master.m3u8"}}
		if !reflect.DeepEqual(req.URL.Variable, want) {
			t.Fatalf("wildcard values = %+v, want %+v", req.URL.Variable, want)
		}
	}
	for key, value := range map[string]string{"Content-Type": "application/vnd.apple.mpegurl", "Cache-Control": "private, no-store", "ETag": "v3"} {
		if got := exampleHeader(stream.Response[0].Header, key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	for _, key := range []string{"HEAD /v1/materials/:id/stream/:wildcardPath", "HEAD /v1/media/:id"} {
		item := items[key]
		if item == nil || len(item.Response) != 1 || item.Response[0].Body != "" || exampleHeader(item.Response[0].Header, "ETag") == "" {
			t.Fatalf("HEAD body or headers incorrect: %s: %+v", key, item)
		}
	}
	redirect := items["GET /v1/materials/:id/media"].Response[0]
	if redirect.Status != "Temporary Redirect" || redirect.Body != "" || exampleHeader(redirect.Header, "Location") != "https://media.example.test/video.mp4" {
		t.Fatalf("invalid redirect: %+v", redirect)
	}
	// Repeated generation must be byte-identical, including map-based headers.
	for i := 0; i < 5; i++ {
		if _, err := Generate(Options{Root: project, Out: output}); err != nil {
			t.Fatal(err)
		}
		again, err := os.ReadFile(output)
		if err != nil || !bytes.Equal(data, again) {
			t.Fatalf("generation is not deterministic: %v", err)
		}
	}
}

func TestManualRequestsAreIndependentAndOmittedRequestKeepsInference(t *testing.T) {
	var file manualExamplesFile
	if err := json.Unmarshal([]byte(`{"examples":[
 {"key":"POST /v1/items/:id","name":"Default","default":true,"request":{"path":{"id":"42"},"query":{"sort":"likes"},"headers":{"If-Match":"v2","Idempotency-Key":"key"},"body":{"title":"new"}}},
 {"key":"POST /v1/items/:id","name":"Missing header","request":{"path":{"id":"43"},"body":{"title":"retry"}},"response":{"status":428}},
 {"key":"POST /v1/items/:id","name":"Empty request","request":{},"response":{"status":400}},
 {"key":"POST /v1/items/:id","name":"Inferred","response":{"status":200}}
]}`), &file); err != nil {
		t.Fatal(err)
	}
	g := &generator{manualExamples: map[string][]manualExample{"POST /v1/items/:id": file.Examples}}
	base := &postmanRequest{
		Method: "POST", Auth: bearerAuth(), Description: "Request documentation",
		Header: []postmanHeader{{Key: "Content-Type", Value: "application/json"}},
		Body:   &postmanBody{Mode: "raw", Raw: `{"inferred":true}`},
		URL: postmanURL{Host: []string{"{{baseURL}}"}, Path: []string{"v1", "items", ":id"},
			Variable: []postmanURLVariable{{Key: "id", Value: "1"}}, Query: []postmanQueryParam{{Key: "limit", Value: "20"}}},
	}
	before := clonePostmanRequest(base)
	responses := g.responseExamples(route{Method: "POST", Path: "/v1/items/:id"}, base)
	if len(responses) != 4 || responses[0].Name != "201 Created - Default" {
		t.Fatalf("invalid default: %+v", responses)
	}
	for _, response := range responses {
		req := response.OriginalRequest
		if !reflect.DeepEqual(req.Auth, base.Auth) || req.Description != base.Description {
			t.Fatalf("lost request authentication or docs: %+v", req)
		}
		switch {
		case strings.HasSuffix(response.Name, " - Default"):
			if len(req.URL.Query) != 1 || req.URL.Query[0].Key != "sort" || req.URL.Variable[0].Value != "42" || exampleHeader(req.Header, "If-Match") != "v2" {
				t.Fatalf("invalid default request: %+v", req)
			}
		case strings.HasSuffix(response.Name, " - Missing header"):
			if len(req.URL.Query) != 0 || strings.Contains(req.URL.Raw, "?") || len(req.Header) != 1 || exampleHeader(req.Header, "Content-Type") != "application/json" || req.URL.Variable[0].Value != "43" || response.Status != "Precondition Required" {
				t.Fatalf("missing-header variant inherited defaults: %+v", response)
			}
		case strings.HasSuffix(response.Name, " - Empty request"):
			if len(req.URL.Query) != 0 || req.Body != nil || len(req.Header) != 0 || req.URL.Variable[0].Value != "1" {
				t.Fatalf("empty request inherited defaults: %+v", req)
			}
		case strings.HasSuffix(response.Name, " - Inferred"):
			if req.Body == nil || req.Body.Raw != base.Body.Raw || len(req.URL.Query) != 1 || req.URL.Query[0].Key != "limit" || req.URL.Variable[0].Value != "1" || len(req.Header) != 1 {
				t.Fatalf("omitted request lost inference or inherited a variant: %+v", req)
			}
		}
	}
	if !reflect.DeepEqual(base, before) {
		t.Fatal("rendering examples mutated the base request")
	}
}

func TestManualExamplesUseMostSpecificRegisteredRoute(t *testing.T) {
	routes := []route{
		{Method: "GET", Path: "/v1/:kind/:id"},
		{Method: "GET", Path: "/v1/items/:id"},
		{Method: "GET", Path: "/v1/items/ranking"},
		{Method: "POST", Path: "/v1/items/:id"},
	}
	g := &generator{routes: routes, manualExamples: map[string][]manualExample{
		"GET /v1/items/ranking":  {{Name: "Ranking"}},
		"GET /v1/items/42":       {{Key: "GET /v1/items/42", Name: "Concrete"}},
		"POST /v1/items/ranking": {{Key: "POST /v1/items/ranking", Name: "Other method"}},
	}}
	for i, want := range []string{"", "Concrete", "Ranking", "Other method"} {
		examples := g.manualExamplesForRoute(routes[i])
		if want == "" {
			if len(examples) != 0 {
				t.Fatalf("more specific examples leaked: %+v", examples)
			}
		} else if len(examples) != 1 || examples[0].Name != want {
			t.Fatalf("%+v: examples = %+v, want %s", routes[i], examples, want)
		}
	}
	item := g.routeItem(routes[1])
	if item.Request.URL.Variable[0].Value != "42" || item.Response[0].OriginalRequest.URL.Variable[0].Value != "42" {
		t.Fatalf("concrete key did not set path value: %+v", item)
	}
}

func TestResponseMediaTypesStatusAndBodylessResponses(t *testing.T) {
	for _, tt := range []struct {
		name, method, contentType, body, want string
		code                                  int
	}{
		{"JSON string", "GET", "", `"hello"`, `"hello"`, 200},
		{"explicit JSON", "GET", "application/json; charset=utf-8", `"hello"`, `"hello"`, 200},
		{"JSON suffix", "GET", "application/problem+json", `"problem"`, `"problem"`, 410},
		{"text", "GET", "text/plain; charset=utf-8", `"line\nnext"`, "line\nnext", 200},
		{"HEAD", "HEAD", "text/plain", `"ignored"`, "", 200},
		{"204", "DELETE", "application/json", `{"ignored":true}`, "", 204},
		{"205", "POST", "application/json", `{"ignored":true}`, "", 205},
		{"304", "GET", "application/json", `{"ignored":true}`, "", 304},
		{"informational", "GET", "application/json", `{"ignored":true}`, "", 103},
	} {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{"ETag": "tag"}
			if tt.contentType != "" {
				headers["content-type"] = tt.contentType
			}
			g := &generator{manualExamples: map[string][]manualExample{tt.method + " /test": {{Response: manualExampleResponse{Status: tt.code, Headers: headers, Body: json.RawMessage(tt.body)}}}}}
			response := g.routeItem(route{Method: tt.method, Path: "/test"}).Response[0]
			if response.Body != tt.want || exampleHeader(response.Header, "ETag") != "tag" {
				t.Fatalf("unexpected response: %+v", response)
			}
			wantType := tt.contentType
			if wantType == "" {
				wantType = "application/json"
			}
			if exampleHeader(response.Header, "Content-Type") != wantType {
				t.Fatalf("lost content type: %+v", response.Header)
			}
			if response.Status == "OK" && tt.code != 200 {
				t.Fatalf("incorrect status: %+v", response)
			}
		})
	}
	if got := (&generator{}).routeItem(route{Method: "HEAD", Path: "/test"}).Response[0]; got.Body != "" || len(got.Header) != 0 {
		t.Fatalf("inferred HEAD must be bodyless: %+v", got)
	}
	if statusTextForCode(599) != "Unknown Status" {
		t.Fatal("unknown status must not be described as OK")
	}
}

func TestWildcardDoesNotCollideWithNamedPathParameter(t *testing.T) {
	r := route{Method: "GET", Path: "/files/:wildcardPath/*"}
	g := &generator{manualExamples: map[string][]manualExample{"GET /files/:wildcardPath/*": {{Request: &manualExampleRequest{Path: map[string]string{"wildcardPath": "42", "*": "master.m3u8"}}}}}}
	item := g.routeItem(r)
	want := []postmanURLVariable{{Key: "wildcardPath", Value: "42"}, {Key: "wildcardPath2", Value: "master.m3u8"}}
	if !reflect.DeepEqual(item.Request.URL.Variable, want) {
		t.Fatalf("wildcard collided with path parameter: %+v", item.Request.URL.Variable)
	}
}

func exampleHeader(headers []postmanHeader, key string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Key, key) {
			return header.Value
		}
	}
	return ""
}
