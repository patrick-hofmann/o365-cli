package mail

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCategoryChangesPreserveUnrelatedNamesAndSendOnlyCategories(t *testing.T) {
	for _, test := range []struct {
		name, operation     string
		names, before, want []string
		dryRun, write       bool
	}{
		{"add preserves and deduplicates", "add", []string{"Newsletter", "Newsletter", " Finance "}, []string{"Finance", "Customer"}, []string{"Finance", "Customer", "Newsletter"}, false, true},
		{"remove preserves", "remove", []string{"Newsletter"}, []string{"Finance", "Newsletter"}, []string{"Finance"}, false, true},
		{"replace is explicit", "set", []string{"A, B", "Research"}, []string{"Finance"}, []string{"A, B", "Research"}, false, true},
		{"clear sends empty array", "clear", nil, []string{"Finance"}, []string{}, false, true},
		{"dry run never patches", "add", []string{"Newsletter"}, []string{"Finance"}, []string{"Finance", "Newsletter"}, true, false},
		{"repeated add is a no-op", "add", []string{"Finance"}, []string{"Finance"}, []string{"Finance"}, false, false},
		{"missing remove is a no-op", "remove", []string{"Absent"}, []string{"Finance"}, []string{"Finance"}, false, false},
		{"order does not cause a write", "set", []string{"B", "A"}, []string{"A", "B"}, []string{"B", "A"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient("synthetic-token")
			calls := 0
			client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/v1.0/me/messages/message-1" {
					t.Fatalf("wrong path: %s", r.URL)
				}
				if r.Method == http.MethodGet {
					if calls != 1 || r.URL.Query().Get("$select") != "id,categories" {
						t.Fatal("unexpected category read")
					}
					data, _ := json.Marshal(map[string]interface{}{"id": "message-1", "categories": test.before, "@odata.etag": `W/"version-1"`})
					return fixtureResponse(string(data)), nil
				}
				if !test.write || r.Method != http.MethodPatch {
					t.Fatal("unexpected mutation")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				var update map[string]json.RawMessage
				if err := json.Unmarshal(body, &update); err != nil {
					t.Fatal(err)
				}
				var names []string
				if len(update) != 1 || string(update["categories"]) == "null" {
					t.Fatalf("unsafe update: %s", body)
				}
				if err := json.Unmarshal(update["categories"], &names); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(names, test.want) {
					t.Fatalf("got %v; want %v", names, test.want)
				}
				data, _ := json.Marshal(map[string]interface{}{"id": "message-1", "categories": names})
				return fixtureResponse(string(data)), nil
			})
			result, err := client.ChangeMessageCategories("message-1", test.operation, test.names, test.dryRun)
			if err != nil {
				t.Fatal(err)
			}
			expectedCalls := 1
			if test.write {
				expectedCalls++
			}
			if calls != expectedCalls || result.Applied != test.write || result.DryRun != test.dryRun || !slices.Equal(result.Categories, test.want) {
				t.Fatalf("calls=%d result=%+v", calls, result)
			}
		})
	}
}

func TestCategoryUpdateFailsClosedAndNeverRetriesWrites(t *testing.T) {
	for _, test := range []struct {
		name, initial, updated string
		status                 int
		patches                int
	}{
		{"missing categories", `{"id":"message-1","@odata.etag":"version-1"}`, "", 200, 0},
		{"wrong identity", `{"id":"message-2","categories":[],"@odata.etag":"version-1"}`, "", 200, 0},
		{"conflicting update", `{"id":"message-1","categories":[],"@odata.etag":"version-1"}`, `{}`, 412, 1},
		{"throttled write", `{"id":"message-1","categories":[],"@odata.etag":"version-1"}`, `{}`, 429, 1},
		{"server failure", `{"id":"message-1","categories":[],"@odata.etag":"version-1"}`, `{}`, 503, 1},
		{"wrong update result", `{"id":"message-1","categories":[],"@odata.etag":"version-1"}`, `{"id":"message-1","categories":["Other"]}`, 200, 1},
		{"invalid response", `{"id":"message-1","categories":[],"@odata.etag":"version-1"}`, `{`, 200, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient("synthetic-token")
			patches := 0
			client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet {
					return fixtureResponse(test.initial), nil
				}
				patches++
				resp := fixtureResponse(test.updated)
				resp.StatusCode = test.status
				return resp, nil
			})
			if _, err := client.ChangeMessageCategories("message-1", "add", []string{"Newsletter"}, false); err == nil {
				t.Fatal("unsafe or failed update accepted")
			}
			if patches != test.patches {
				t.Fatalf("patches=%d", patches)
			}
		})
	}
}

func TestCategoryInputAndReadOnlyConnectionCannotWrite(t *testing.T) {
	client := NewClient("synthetic-token")
	calls := 0
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet {
			t.Fatal("read-only connection wrote categories")
		}
		return fixtureResponse(`{"id":"message-1","categories":[],"@odata.etag":"v1"}`), nil
	})
	for _, operation := range []string{"add", "remove", "set", "unknown"} {
		if _, err := client.ChangeMessageCategories("message-1", operation, nil, false); err == nil {
			t.Fatal("missing categories accepted")
		}
	}
	for _, name := range []string{"", "  ", "A\nB", "A\x00B"} {
		if _, err := client.ChangeMessageCategories("message-1", "add", []string{name}, false); err == nil {
			t.Fatal("invalid category accepted")
		}
	}
	if _, err := client.ChangeMessageCategories("../other", "add", []string{"A"}, false); err == nil {
		t.Fatal("invalid message ID accepted")
	}
	if calls != 0 {
		t.Fatal("invalid input reached Graph")
	}
	client.ReadOnly = true
	if _, err := client.ChangeMessageCategories("message-1", "add", []string{"A"}, false); err == nil {
		t.Fatal("read-only update accepted")
	}
}

func TestCategoryFieldsSurviveAllMailReadPaths(t *testing.T) {
	client := NewClient("synthetic-token")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if !slices.Contains(strings.Split(r.URL.Query().Get("$select"), ","), "categories") {
			t.Fatal("categories omitted from selection")
		}
		message := `{"id":"m1","from":{"emailAddress":{"address":"sender@example.invalid"}},"categories":["Finance"]}`
		if strings.HasSuffix(r.URL.Path, "/messages/m1") {
			return fixtureResponse(message), nil
		}
		return fixtureResponse(`{"value":[` + message + `]}`), nil
	})
	paths := []func() ([]Email, error){
		func() ([]Email, error) { return client.ListEmails("inbox", 1, false, false, false) },
		func() ([]Email, error) { return client.ListEmails("inbox", 1, false, true, false) },
		func() ([]Email, error) { return client.SearchEmails("inbox", "", "", time.Time{}, 1) },
		func() ([]Email, error) { return client.SearchEmailsKQL("", "test", 1) },
		func() ([]Email, error) {
			return client.ListEmailsFromSenders("inbox", []string{"sender@example.invalid"}, 1)
		},
	}
	for _, read := range paths {
		rows, err := read()
		if err != nil || len(rows) != 1 || !slices.Equal(rows[0].Categories, []string{"Finance"}) {
			t.Fatalf("missing categories: %+v %v", rows, err)
		}
	}
	row, err := client.GetEmail("inbox", "m1")
	if err != nil || !slices.Equal(row.Categories, []string{"Finance"}) {
		t.Fatalf("missing read categories: %+v %v", row, err)
	}
}
