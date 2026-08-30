package metrics

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"hookline/internal/delivery"
	"hookline/internal/domain"
)

type store interface {
	QueueStats(context.Context, time.Time) (int64, int64, int64, float64, error)
}
type Registry struct {
	s          store
	c          domain.Clock
	r          *prometheus.Registry
	p, i, o    prometheus.Gauge
	dead       atomic.Uint64
	duration   prometheus.Histogram
	deliveries *prometheus.CounterVec
}

func New(s store, c domain.Clock) *Registry {
	m := &Registry{s: s, c: c, r: prometheus.NewRegistry(), p: prometheus.NewGauge(prometheus.GaugeOpts{Name: "hookline_messages_pending", Help: "Pending messages."}), i: prometheus.NewGauge(prometheus.GaugeOpts{Name: "hookline_messages_in_flight", Help: "In-flight messages."}), o: prometheus.NewGauge(prometheus.GaugeOpts{Name: "hookline_oldest_pending_seconds", Help: "Oldest pending age."}), duration: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "hookline_delivery_duration_seconds", Help: "Delivery duration."}), deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "hookline_delivery_total", Help: "Delivery attempts."}, []string{"code"})}
	d := prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "hookline_messages_dead_total", Help: "Dead messages."}, func() float64 { return float64(m.dead.Load()) })
	m.r.MustRegister(m.p, m.i, m.o, d, m.duration, m.deliveries)
	return m
}

func (m *Registry) ObserveDelivery(v delivery.Result) {
	code := "network_error"
	if v.StatusCode != nil {
		code = strconv.Itoa(*v.StatusCode)
	}
	m.deliveries.WithLabelValues(code).Inc()
	m.duration.Observe(v.Duration.Seconds())
}

func (m *Registry) Handler() http.Handler {
	base := promhttp.HandlerFor(m.r, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		p, i, d, o, e := m.s.QueueStats(c, m.c.Now())
		if e == nil {
			if d < 0 {
				d = 0
			}
			m.p.Set(float64(p))
			m.i.Set(float64(i))
			m.o.Set(o)
			m.dead.Store(uint64(d))
		}
		base.ServeHTTP(w, r)
	})
}
