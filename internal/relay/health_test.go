package relay

import (
	"encoding/json"
	"github.com/xallor-ai/xallor-remote/internal/admission"
	"github.com/xallor-ai/xallor-remote/internal/protocol"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 目的：GET /health 必须是普通 HTTP，不能走 WebSocket Accept。
// 前置：空 Hub。预期：200 且 ok=true。
func TestShould_returnOK_whenHealthRequested(t *testing.T) {
	srv := httptest.NewServer(Handler(NewHub(newMemStore(), nil, nil, admission.New(admission.Limits{PerDevice: protocol.MaxDeviceRequests})), nil))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Service != "relay" {
		t.Fatalf("%+v", body)
	}
}
