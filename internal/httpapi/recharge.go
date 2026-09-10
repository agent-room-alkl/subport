package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
	"github.com/agent-room-alkl/subport/internal/payment"
	"github.com/agent-room-alkl/subport/internal/store"
)

func (s *Server) paymentRoutes(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case rest == "alipay/notify" && r.Method == http.MethodPost:
		s.handleAlipayNotify(w, r)
	case rest == "alipay/return" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		s.handleAlipayReturn(w, r)
	default:
		fail(w, http.StatusNotFound, "no such payment endpoint")
	}
}

func (s *Server) handleAlipayReturn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>支付结果</title></head><body style="font-family:sans-serif;padding:2rem"><h1>支付结果处理中</h1><p>请返回控制台查看额度。</p><p><a href="/console">返回控制台</a></p></body></html>`)
}

func (s *Server) handleAlipayNotify(w http.ResponseWriter, r *http.Request) {
	cfg := payment.LoadConfig()
	_ = r.ParseForm()
	vals := map[string]string{}
	for k, vs := range r.Form {
		if len(vs) > 0 {
			vals[k] = vs[0]
		}
	}
	// Also accept JSON body for local tests.
	if len(vals) == 0 && strings.Contains(r.Header.Get("Content-Type"), "json") {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		for k, v := range body {
			vals[k] = v
		}
	}

	if cfg.Mock {
		// Dev shortcut: X-Subport-Mock-Notify: 1 with out_trade_no skips signature.
		if r.Header.Get("X-Subport-Mock-Notify") != "1" && vals["mock"] != "1" {
			http.Error(w, "mock notify requires X-Subport-Mock-Notify: 1", http.StatusForbidden)
			return
		}
	} else {
		if !cfg.ReadyForLive() {
			http.Error(w, "alipay not configured", http.StatusServiceUnavailable)
			return
		}
		if err := cfg.VerifyNotify(vals); err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
	}

	orderID := vals["out_trade_no"]
	tradeStatus := vals["trade_status"]
	tradeNo := vals["trade_no"]
	if orderID == "" {
		http.Error(w, "missing out_trade_no", http.StatusBadRequest)
		return
	}
	if tradeStatus != "" && tradeStatus != "TRADE_SUCCESS" && tradeStatus != "TRADE_FINISHED" {
		_, _ = io.WriteString(w, "success") // acknowledge non-success without crediting
		return
	}
	_, _, _, err := s.Store.MarkOrderPaidAndCredit(orderID, tradeNo, model.TopupSourceAlipay, "", "alipay notify")
	if err != nil {
		http.Error(w, "order error", http.StatusBadRequest)
		return
	}
	_, _ = io.WriteString(w, "success")
}

// adminRechargeRoutes handles /api/users*, /api/topups, /api/payment-orders.
// Returns true if the request was handled.
func (s *Server) adminRechargeRoutes(w http.ResponseWriter, r *http.Request, rest string) bool {
	switch {
	case rest == "users" && r.Method == http.MethodGet:
		out := []map[string]any{}
		for _, u := range s.Store.ListUsers() {
			out = append(out, model.PublicUser(u))
		}
		jsonOut(w, out)
		return true

	case strings.HasPrefix(rest, "users/") && strings.HasSuffix(rest, "/topups") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "users/"), "/topups")
		id = strings.TrimSuffix(id, "/")
		var in struct {
			Credit int64  `json:"credit"`
			Note   string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		if in.Credit <= 0 {
			fail(w, http.StatusBadRequest, "credit must be positive")
			return true
		}
		me := s.current(r)
		top, order, err := s.Store.AdminManualTopup(id, in.Credit, me.ID, in.Note, true)
		if err != nil {
			if err == store.ErrNotFound {
				fail(w, http.StatusNotFound, "user not found")
				return true
			}
			fail(w, http.StatusBadRequest, err.Error())
			return true
		}
		u, _ := s.Store.UserByID(id)
		jsonOut(w, map[string]any{
			"topup":  top,
			"order":  order,
			"user":   model.PublicUser(u),
		})
		return true

	case strings.HasPrefix(rest, "users/") && strings.HasSuffix(rest, "/topups") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(rest, "users/"), "/topups")
		id = strings.TrimSuffix(id, "/")
		jsonOut(w, s.Store.ListTopupsOf(id, 100))
		return true

	case rest == "topups" && r.Method == http.MethodGet:
		limit := queryLimit(r, 100)
		jsonOut(w, s.Store.ListTopups(limit))
		return true

	case rest == "payment-orders" && r.Method == http.MethodGet:
		limit := queryLimit(r, 100)
		jsonOut(w, s.Store.ListPaymentOrders(limit))
		return true
	}
	return false
}

func queryLimit(r *http.Request, def int) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			return n
		}
	}
	return def
}

// consoleRechargeRoutes handles console recharge endpoints. Returns true if handled.
func (s *Server) consoleRechargeRoutes(w http.ResponseWriter, r *http.Request, rest string, me currentUser) bool {
	switch {
	case rest == "packages" && r.Method == http.MethodGet:
		cfg := payment.LoadConfig()
		jsonOut(w, map[string]any{
			"packages":          store.LoadRechargePackages(),
			"alipay_enabled":    cfg.Enabled || cfg.Mock,
			"alipay_mock":       cfg.Mock,
			"mock_pay_available": cfg.Mock,
		})
		return true

	case rest == "recharge" && r.Method == http.MethodPost:
		var in struct {
			PackageID      string `json:"package_id"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, http.StatusBadRequest, "bad request")
			return true
		}
		pkg, ok := store.PackageByID(in.PackageID)
		if !ok {
			fail(w, http.StatusBadRequest, "unknown package")
			return true
		}
		cfg := payment.LoadConfig()
		if !cfg.Mock && !cfg.Enabled {
			fail(w, http.StatusServiceUnavailable, "alipay not configured")
			return true
		}
		if !cfg.Mock && !cfg.ReadyForLive() {
			fail(w, http.StatusServiceUnavailable, "alipay credentials missing")
			return true
		}

		expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
		order, err := s.Store.CreatePaymentOrder(model.PaymentOrder{
			UserID:          me.ID,
			Provider:        model.ProviderAlipay,
			PackageID:       pkg.ID,
			AmountFiatCents: pkg.AmountFiatCents,
			Currency:        pkg.Currency,
			QuotaCredit:     pkg.QuotaCredit,
			Status:          model.OrderPending,
			IdempotencyKey:  in.IdempotencyKey,
			ExpiresAt:       expires,
		})
		if err != nil {
			fail(w, http.StatusInternalServerError, "could not create order")
			return true
		}

		yuan := fmt.Sprintf("%.2f", float64(pkg.AmountFiatCents)/100.0)
		payURL, perr := cfg.CreatePayment(order.ID, "Subport "+pkg.Label, yuan)
		if perr != nil {
			fail(w, http.StatusServiceUnavailable, perr.Error())
			return true
		}
		_ = s.Store.SetPaymentOrderPayURL(order.ID, payURL)
		order.PayURL = payURL

		jsonOut(w, map[string]any{
			"order":              order,
			"pay_url":            payURL,
			"mock_pay_available": cfg.Mock,
		})
		return true

	case strings.HasPrefix(rest, "recharge/") && strings.HasSuffix(rest, "/mock-pay") && r.Method == http.MethodPost:
		cfg := payment.LoadConfig()
		if !cfg.Mock {
			fail(w, http.StatusForbidden, "mock pay disabled")
			return true
		}
		oid := strings.TrimSuffix(strings.TrimPrefix(rest, "recharge/"), "/mock-pay")
		oid = strings.TrimSuffix(oid, "/")
		order, err := s.Store.GetPaymentOrder(oid)
		if err != nil || order.UserID != me.ID {
			fail(w, http.StatusNotFound, "order not found")
			return true
		}
		paid, top, credited, err := s.Store.MarkOrderPaidAndCredit(oid, "mock-"+oid, model.TopupSourceMock, me.ID, "mock pay")
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return true
		}
		u, _ := s.Store.UserByID(me.ID)
		jsonOut(w, map[string]any{
			"order":    paid,
			"topup":    top,
			"credited": credited,
			"user":     model.PublicUser(u),
		})
		return true

	case strings.HasPrefix(rest, "recharge/") && r.Method == http.MethodGet:
		oid := strings.TrimPrefix(rest, "recharge/")
		if strings.Contains(oid, "/") {
			return false
		}
		order, err := s.Store.GetPaymentOrder(oid)
		if err != nil || order.UserID != me.ID {
			fail(w, http.StatusNotFound, "order not found")
			return true
		}
		jsonOut(w, order)
		return true

	case rest == "topups" && r.Method == http.MethodGet:
		jsonOut(w, s.Store.ListTopupsOf(me.ID, 100))
		return true
	}
	return false
}
