package postmangen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultPostmanAPIBaseURL = "https://api.getpostman.com"

type SyncOptions struct {
	Generation Options
	Writer     io.Writer
	HTTPClient *http.Client
	APIBaseURL string
}

type SyncedTarget struct {
	Index        int
	Label        string
	CollectionID string
}

type SyncResult struct {
	Generation Result
	Targets    []SyncedTarget
}

type syncTarget struct {
	SyncedTarget
	apiKey string
}

// Sync generates the collection and uploads it to every target configured in
// .env or the process environment. Numbered targets have no fixed upper bound.
func Sync(opts SyncOptions) (SyncResult, error) {
	resolved, env, err := ResolveEnvironment(opts.Generation)
	if err != nil {
		return SyncResult{}, err
	}
	targets, err := postmanTargets(env)
	if err != nil {
		return SyncResult{}, err
	}
	generation, err := Generate(resolved)
	if err != nil {
		return SyncResult{}, err
	}
	result := SyncResult{Generation: generation}
	if len(targets) == 0 {
		return result, nil
	}
	collection, err := os.ReadFile(generation.OutputPath)
	if err != nil {
		return result, fmt.Errorf("read generated collection: %w", err)
	}
	if !json.Valid(collection) {
		return result, fmt.Errorf("generated collection %s is not valid JSON", generation.OutputPath)
	}
	payload := append([]byte(`{"collection":`), collection...)
	payload = append(payload, '}')
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(opts.APIBaseURL), "/")
	if baseURL == "" {
		baseURL = defaultPostmanAPIBaseURL
	}
	writer := opts.Writer
	if writer == nil {
		writer = io.Discard
	}
	for _, target := range targets {
		fmt.Fprintf(writer, "Syncing Postman target %s (collection %s)...\n", target.Label, target.CollectionID)
		if err := uploadCollection(client, baseURL, target, payload); err != nil {
			return result, err
		}
		result.Targets = append(result.Targets, target.SyncedTarget)
		fmt.Fprintf(writer, "Synced Postman target %s.\n", target.Label)
	}
	return result, nil
}

func postmanTargets(env map[string]string) ([]syncTarget, error) {
	globalAPIKey := strings.TrimSpace(env["POSTMAN_API_KEY"])
	targets := make([]syncTarget, 0)
	primaryID := strings.TrimSpace(env["POSTMAN_COLLECTION_ID"])
	if primaryID != "" {
		if globalAPIKey == "" {
			return nil, fmt.Errorf("Postman target primary: POSTMAN_COLLECTION_ID requires POSTMAN_API_KEY")
		}
		targets = append(targets, syncTarget{SyncedTarget: SyncedTarget{
			Index: 0, Label: firstNonBlank(env["POSTMAN_TARGET_NAME"], "primary"), CollectionID: primaryID,
		}, apiKey: globalAPIKey})
	}

	indexes := numberedTargetIndexes(env)
	for _, index := range indexes {
		suffix := "_" + strconv.Itoa(index)
		apiKey := strings.TrimSpace(env["POSTMAN_API_KEY"+suffix])
		collectionID := strings.TrimSpace(env["POSTMAN_COLLECTION_ID"+suffix])
		legacyCollectionID := strings.TrimSpace(env["POSTMAN_API_COLLECTION_ID"+suffix])
		if collectionID != "" && legacyCollectionID != "" && collectionID != legacyCollectionID {
			return nil, fmt.Errorf("Postman target %d: POSTMAN_COLLECTION_ID%s conflicts with POSTMAN_API_COLLECTION_ID%s", index, suffix, suffix)
		}
		if collectionID == "" {
			collectionID = legacyCollectionID
		}
		if apiKey == "" {
			apiKey = globalAPIKey
		}
		configuredAPIKey := strings.TrimSpace(env["POSTMAN_API_KEY"+suffix]) != ""
		configuredName := strings.TrimSpace(env["POSTMAN_TARGET_NAME"+suffix]) != ""
		if collectionID == "" {
			if configuredAPIKey || configuredName {
				return nil, fmt.Errorf("Postman target %d: set POSTMAN_COLLECTION_ID%s", index, suffix)
			}
			continue
		}
		if apiKey == "" {
			return nil, fmt.Errorf("Postman target %d: set POSTMAN_API_KEY%s or the shared POSTMAN_API_KEY", index, suffix)
		}
		targets = append(targets, syncTarget{SyncedTarget: SyncedTarget{
			Index: index, Label: firstNonBlank(env["POSTMAN_TARGET_NAME"+suffix], "target "+strconv.Itoa(index)), CollectionID: collectionID,
		}, apiKey: apiKey})
	}
	return targets, nil
}

func numberedTargetIndexes(env map[string]string) []int {
	prefixes := []string{
		"POSTMAN_API_KEY_",
		"POSTMAN_COLLECTION_ID_",
		"POSTMAN_API_COLLECTION_ID_",
		"POSTMAN_TARGET_NAME_",
	}
	seen := map[int]struct{}{}
	for key := range env {
		for _, prefix := range prefixes {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			index, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
			if err == nil && index > 0 {
				seen[index] = struct{}{}
			}
		}
	}
	indexes := make([]int, 0, len(seen))
	for index := range seen {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func uploadCollection(client *http.Client, baseURL string, target syncTarget, payload []byte) error {
	endpoint := baseURL + "/collections/" + url.PathEscape(target.CollectionID)
	request, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("prepare Postman target %s: %w", target.Label, err)
	}
	request.Header.Set("X-API-Key", target.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("sync Postman target %s: %w", target.Label, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = response.Status
	}
	return fmt.Errorf("sync Postman target %s: API returned %s: %s", target.Label, response.Status, detail)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
