// Package observability 提供进程内 Prometheus 指标注册与 HTTP 暴露能力。
package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const metricsNamespace = "uno_server"

// Metrics 持有单个服务实例的独立指标注册表，避免依赖全局注册表产生测试冲突。
type Metrics struct {
	registry            *prometheus.Registry
	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	webSocketMessages   *prometheus.CounterVec
	roomGarbageCollects *prometheus.CounterVec
	roomRestores        *prometheus.CounterVec
}

// New 创建带 Go 运行时、进程和 UNO 业务指标的独立注册表。
func New() *Metrics {
	metrics := &Metrics{
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "收到的 HTTP 请求总数。",
		}, []string{"method", "route", "status_class"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP 请求处理耗时秒数。",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),
		webSocketMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "websocket",
			Name:      "messages_total",
			Help:      "处理的 WebSocket 入站消息总数。",
		}, []string{"result"}),
		roomGarbageCollects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "room",
			Name:      "garbage_collections_total",
			Help:      "超过空房保留时限后回收的房间总数。",
		}, []string{"phase"}),
		roomRestores: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "room",
			Name:      "restores_total",
			Help:      "Redis 房间快照恢复结果总数。",
		}, []string{"result"}),
	}
	metrics.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.httpRequests,
		metrics.httpRequestDuration,
		metrics.webSocketMessages,
		metrics.roomGarbageCollects,
		metrics.roomRestores,
	)
	metrics.initializeBoundedLabels()
	return metrics
}

// initializeBoundedLabels 预创建固定标签序列，让零流量时的监控面板也能获得零值。
func (m *Metrics) initializeBoundedLabels() {
	for _, result := range []string{"ok", "bad_envelope", "error", "unknown_type"} {
		m.webSocketMessages.WithLabelValues(result)
	}
	for _, phase := range []string{"waiting", "playing", "settled", "unknown"} {
		m.roomGarbageCollects.WithLabelValues(phase)
	}
	for _, result := range []string{"restored", "discarded", "conflict", "load_error"} {
		m.roomRestores.WithLabelValues(result)
	}
}

// RegisterRuntimeGauges 注册从房间和连接管理器实时读取的低成本仪表盘指标。
func (m *Metrics) RegisterRuntimeGauges(roomCount, connectionCount func() int) {
	m.registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "room",
			Name:      "active",
			Help:      "当前仍由进程管理的房间数。",
		}, func() float64 { return float64(roomCount()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "websocket",
			Name:      "connections",
			Help:      "当前已鉴权 WebSocket 连接数。",
		}, func() float64 { return float64(connectionCount()) }),
	)
}

// Handler 返回仅绑定当前独立注册表的 Prometheus 抓取处理器。
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// ObserveHTTPRequest 记录一次 HTTP 请求，并将客户端可控维度压缩到固定标签集合。
func (m *Metrics) ObserveHTTPRequest(method, route string, statusCode int, duration time.Duration) {
	if m == nil {
		return
	}
	method = normalizeHTTPMethod(method)
	if route == "" {
		route = "unmatched"
	}
	statusClass := "unknown"
	if statusCode >= 100 && statusCode <= 599 {
		statusClass = strconv.Itoa(statusCode/100) + "xx"
	}
	m.httpRequests.WithLabelValues(method, route, statusClass).Inc()
	m.httpRequestDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}

// ObserveWebSocketMessage 记录一条入站消息的固定结果分类。
func (m *Metrics) ObserveWebSocketMessage(result string) {
	if m == nil {
		return
	}
	switch result {
	case "ok", "bad_envelope", "error", "unknown_type":
	default:
		result = "error"
	}
	m.webSocketMessages.WithLabelValues(result).Inc()
}

// ObserveRoomGarbageCollection 记录按房间阶段分类的空房回收。
func (m *Metrics) ObserveRoomGarbageCollection(phase string) {
	if m == nil {
		return
	}
	switch phase {
	case "waiting", "playing", "settled":
	default:
		phase = "unknown"
	}
	m.roomGarbageCollects.WithLabelValues(phase).Inc()
}

// ObserveRoomRestore 记录单条 Redis 房间快照的恢复结果。
func (m *Metrics) ObserveRoomRestore(result string) {
	if m == nil {
		return
	}
	switch result {
	case "restored", "discarded", "conflict", "load_error":
	default:
		result = "discarded"
	}
	m.roomRestores.WithLabelValues(result).Inc()
}

// normalizeHTTPMethod 避免任意方法名制造高基数时间序列。
func normalizeHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}
