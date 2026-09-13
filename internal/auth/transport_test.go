package auth

import (
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPodsTLSUsesOnlyTheAssignedPublicRoots(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("synthetic")) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "roots.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PODS_CA_FILE", path)
	client, err := PodsHTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "synthetic" {
		t.Fatal("TLS fixture failed")
	}
	if err := os.WriteFile(path, []byte("not certificates"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PodsHTTPClient(); err == nil {
		t.Fatal("invalid roots accepted")
	}
}
func TestExportSyntheticPodsCache(t *testing.T) {
	output := os.Getenv("PODS_TEST_CACHE_OUTPUT")
	if output == "" {
		t.Skip("explicit synthetic fixture export only")
	}
	t.Setenv("PODS_CA_FILE", "")
	fixture := &offlineOAuthTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = fixture
	t.Cleanup(func() { http.DefaultTransport = previous })
	directory := t.TempDir()
	client, err := NewReadOnlyOAuthClient("", directory)
	if err != nil {
		t.Fatal(err)
	}
	_, results, err := client.StartDeviceCodeFlow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result := <-results; result.Error != nil {
		t.Fatal(result.Error)
	}
	content, err := os.ReadFile(filepath.Join(directory, "token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, content, 0600); err != nil {
		t.Fatal(err)
	}
}
