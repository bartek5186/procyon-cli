package postmangen

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type recordingTransport struct {
	requests []*http.Request
	bodies   [][]byte
	status   int
	body     string
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	transport.requests = append(transport.requests, request)
	transport.bodies = append(transport.bodies, body)
	status := transport.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(transport.body)),
		Request:    request,
	}, nil
}

func TestPostmanTargetsSupportUnlimitedSparseIndexesAndSharedKey(t *testing.T) {
	targets, err := postmanTargets(map[string]string{
		"POSTMAN_API_KEY":         "shared-secret",
		"POSTMAN_COLLECTION_ID":   "primary-id",
		"POSTMAN_COLLECTION_ID_1": "one-id",
		"POSTMAN_TARGET_NAME_1":   "Staging",
		"POSTMAN_API_KEY_8":       "separate-secret",
		"POSTMAN_COLLECTION_ID_8": "eight-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 || targets[0].Index != 0 || targets[1].Index != 1 || targets[2].Index != 8 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	if targets[1].apiKey != "shared-secret" || targets[1].Label != "Staging" || targets[2].apiKey != "separate-secret" {
		t.Fatalf("unexpected target configuration: %+v", targets)
	}
}

func TestPostmanTargetsDiscoversEightNumberedTargets(t *testing.T) {
	env := map[string]string{"POSTMAN_API_KEY": "shared-secret"}
	for index := 1; index <= 8; index++ {
		env[fmt.Sprintf("POSTMAN_COLLECTION_ID_%d", index)] = fmt.Sprintf("collection-%d", index)
	}
	targets, err := postmanTargets(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 8 || targets[7].Index != 8 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
}

func TestPostmanTargetsRejectIncompleteNumberedTarget(t *testing.T) {
	_, err := postmanTargets(map[string]string{"POSTMAN_API_KEY_6": "secret"})
	if err == nil || !strings.Contains(err.Error(), "POSTMAN_COLLECTION_ID_6") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncUploadsEveryConfiguredTargetWithoutLoggingKeys(t *testing.T) {
	project := t.TempDir()
	writePostmanTestFile(t, filepath.Join(project, "routes.go"), `package main
func registerPublicRoutes(e *Echo, app *application) { e.GET("/health", app.Health) }
func registerAdminRoutes(e *Echo, app *application) {}
func registerUploadRoutes(e *Echo, app *application) {}
`)
	t.Setenv("POSTMAN_API_KEY", "shared-secret")
	t.Setenv("POSTMAN_COLLECTION_ID", "primary-id")
	t.Setenv("POSTMAN_COLLECTION_ID_2", "second-id")
	t.Setenv("POSTMAN_TARGET_NAME_2", "Production")
	transport := &recordingTransport{}
	var output bytes.Buffer
	result, err := Sync(SyncOptions{
		Generation: Options{Root: project, Out: "collection.json"},
		Writer:     &output,
		HTTPClient: &http.Client{Transport: transport},
		APIBaseURL: "https://postman.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Targets) != 2 || len(transport.requests) != 2 {
		t.Fatalf("unexpected sync result: %+v requests=%d", result, len(transport.requests))
	}
	for index, request := range transport.requests {
		if request.Method != http.MethodPut || request.Header.Get("X-API-Key") != "shared-secret" {
			t.Fatalf("unexpected request %d: %+v", index, request)
		}
		if !strings.Contains(string(transport.bodies[index]), `"collection":`) {
			t.Fatalf("request %d has invalid payload: %s", index, transport.bodies[index])
		}
	}
	if strings.Contains(output.String(), "shared-secret") {
		t.Fatalf("API key leaked to output: %s", output.String())
	}
	if !strings.Contains(output.String(), "primary") || !strings.Contains(output.String(), "Production") {
		t.Fatalf("missing target progress: %s", output.String())
	}
}

func TestSyncGeneratesWithoutTargets(t *testing.T) {
	project := t.TempDir()
	writePostmanTestFile(t, filepath.Join(project, "routes.go"), `package main
func registerPublicRoutes(e *Echo, app *application) {}
func registerAdminRoutes(e *Echo, app *application) {}
func registerUploadRoutes(e *Echo, app *application) {}
`)
	for _, key := range []string{"POSTMAN_API_KEY", "POSTMAN_COLLECTION_ID", "POSTMAN_API_KEY_1", "POSTMAN_COLLECTION_ID_1", "POSTMAN_TARGET_NAME_1"} {
		t.Setenv(key, "")
	}
	result, err := Sync(SyncOptions{Generation: Options{Root: project}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Targets) != 0 || result.Generation.OutputPath == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
