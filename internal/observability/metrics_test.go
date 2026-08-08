package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestMetricsHandler 验证业务计数、低基数归一化和运行时仪表盘均可被抓取。
func TestMetricsHandler(t *testing.T) {
	metrics := New()
	metrics.RegisterRuntimeGauges(func() int { return 3 }, func() int { return 2 })
	metrics.ObserveHTTPRequest(http.MethodGet, "/healthz", http.StatusOK, 25*time.Millisecond)
	metrics.ObserveHTTPRequest("CUSTOM", "unmatched", http.StatusInternalServerError, time.Millisecond)
	metrics.ObserveWebSocketMessage("unexpected")
	metrics.ObserveRoomGarbageCollection("playing")
	metrics.ObserveRoomRestore("restored")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("指标状态码=%d", recorder.Code)
	}
	body := recorder.Body.String()
	checks := []string{
		`uno_server_room_active 3`,
		`uno_server_websocket_connections 2`,
		`uno_server_http_requests_total{method="GET",route="/healthz",status_class="2xx"} 1`,
		`uno_server_http_requests_total{method="OTHER",route="unmatched",status_class="5xx"} 1`,
		`uno_server_websocket_messages_total{result="error"} 1`,
		`uno_server_room_garbage_collections_total{phase="playing"} 1`,
		`uno_server_room_restores_total{result="restored"} 1`,
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Fatalf("指标输出缺少 %q", check)
		}
	}
}
