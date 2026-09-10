// Package payment talks to Alipay (live page-pay) or a local mock.
package payment

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Config is loaded from environment. Secrets are never logged.
type Config struct {
	Enabled    bool
	Mock       bool
	Mode       string // page | international
	AppID      string
	PrivateKey string // PEM PKCS1/PKCS8
	PublicKey  string // Alipay public key PEM
	PublicBase string // e.g. https://example.com
	Gateway    string // default https://openapi.alipay.com/gateway.do
}

func envTruthy(k string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// LoadConfig reads Alipay-related env vars.
func LoadConfig() Config {
	appID := firstNonEmpty(os.Getenv("SUBPORT_ALIPAY_APP_ID"), os.Getenv("SUBPORT_ALIPAY_CLIENT_ID"))
	cfg := Config{
		Enabled:    envTruthy("SUBPORT_ALIPAY_ENABLED") || envTruthy("SUBPORT_ALIPAY_MOCK"),
		Mock:       envTruthy("SUBPORT_ALIPAY_MOCK"),
		Mode:       firstNonEmpty(os.Getenv("SUBPORT_ALIPAY_MODE"), "page"),
		AppID:      appID,
		PrivateKey: os.Getenv("SUBPORT_ALIPAY_MERCHANT_PRIVATE_KEY"),
		PublicKey:  os.Getenv("SUBPORT_ALIPAY_PUBLIC_KEY"),
		PublicBase: strings.TrimRight(os.Getenv("SUBPORT_PUBLIC_BASE_URL"), "/"),
		Gateway:    firstNonEmpty(os.Getenv("SUBPORT_ALIPAY_GATEWAY"), "https://openapi.alipay.com/gateway.do"),
	}
	return cfg
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ReadyForLive is true when live credentials are present.
func (c Config) ReadyForLive() bool {
	return c.AppID != "" && c.PrivateKey != "" && c.PublicKey != "" && c.PublicBase != ""
}

// CreatePayment builds a pay URL for the given order id and amount (CNY yuan string).
func (c Config) CreatePayment(orderID, subject, totalAmountCNY string) (payURL string, err error) {
	if c.Mock {
		base := c.PublicBase
		if base == "" {
			base = "http://127.0.0.1:8080"
		}
		return fmt.Sprintf("%s/api/payments/alipay/return?out_trade_no=%s&mock=1", base, url.QueryEscape(orderID)), nil
	}
	if !c.Enabled {
		return "", errors.New("alipay disabled")
	}
	if !c.ReadyForLive() {
		return "", errors.New("alipay credentials missing")
	}
	return c.buildPagePayURL(orderID, subject, totalAmountCNY)
}

func (c Config) notifyURL() string {
	return c.PublicBase + "/api/payments/alipay/notify"
}

func (c Config) returnURL() string {
	return c.PublicBase + "/api/payments/alipay/return"
}

func (c Config) buildPagePayURL(orderID, subject, totalAmountCNY string) (string, error) {
	biz := fmt.Sprintf(
		`{"out_trade_no":"%s","product_code":"FAST_INSTANT_TRADE_PAY","total_amount":"%s","subject":"%s"}`,
		escapeJSON(orderID), escapeJSON(totalAmountCNY), escapeJSON(subject),
	)
	params := map[string]string{
		"app_id":      c.AppID,
		"method":      "alipay.trade.page.pay",
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  c.notifyURL(),
		"return_url":  c.returnURL(),
		"biz_content": biz,
	}
	sign, err := signRSA2(params, c.PrivateKey)
	if err != nil {
		return "", err
	}
	params["sign"] = sign
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return c.Gateway + "?" + q.Encode(), nil
}

func escapeJSON(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return r.Replace(s)
}

func signRSA2(params map[string]string, privateKeyPEM string) (string, error) {
	content := sortedQuery(params)
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyNotify checks Alipay RSA2 signature over typical form notify fields.
func (c Config) VerifyNotify(vals map[string]string) error {
	if c.Mock {
		return nil
	}
	sign := vals["sign"]
	if sign == "" {
		return errors.New("missing sign")
	}
	signType := vals["sign_type"]
	if signType != "" && !strings.EqualFold(signType, "RSA2") {
		return errors.New("unsupported sign_type")
	}
	check := map[string]string{}
	for k, v := range vals {
		if k == "sign" || k == "sign_type" {
			continue
		}
		check[k] = v
	}
	content := sortedQuery(check)
	pub, err := parsePublicKey(c.PublicKey)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return err
	}
	h := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], raw)
}

func sortedQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

func parsePrivateKey(pemOrRaw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(normalizePEM(pemOrRaw, "RSA PRIVATE KEY")))
	if block == nil {
		block, _ = pem.Decode([]byte(normalizePEM(pemOrRaw, "PRIVATE KEY")))
	}
	if block == nil {
		// Try raw base64 PKCS8
		raw, err := base64.StdEncoding.DecodeString(stripPEMNoise(pemOrRaw))
		if err != nil {
			return nil, errors.New("invalid private key PEM")
		}
		key, err := x509.ParsePKCS8PrivateKey(raw)
		if err != nil {
			pk, err2 := x509.ParsePKCS1PrivateKey(raw)
			if err2 != nil {
				return nil, err
			}
			return pk, nil
		}
		rk, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not RSA private key")
		}
		return rk, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not RSA private key")
	}
	return rk, nil
}

func parsePublicKey(pemOrRaw string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(normalizePEM(pemOrRaw, "PUBLIC KEY")))
	var der []byte
	if block != nil {
		der = block.Bytes
	} else {
		raw, err := base64.StdEncoding.DecodeString(stripPEMNoise(pemOrRaw))
		if err != nil {
			return nil, errors.New("invalid public key")
		}
		der = raw
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rk, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not RSA public key")
	}
	return rk, nil
}

func stripPEMNoise(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimPrefix(s, "-----BEGINPUBLICKEY-----")
	s = strings.TrimPrefix(s, "-----BEGINRSAPRIVATEKEY-----")
	s = strings.TrimPrefix(s, "-----BEGINPRIVATEKEY-----")
	s = strings.TrimSuffix(s, "-----ENDPUBLICKEY-----")
	s = strings.TrimSuffix(s, "-----ENDRSAPRIVATEKEY-----")
	s = strings.TrimSuffix(s, "-----ENDPRIVATEKEY-----")
	return s
}

func normalizePEM(s, typ string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "BEGIN") {
		return s
	}
	body := stripPEMNoise(s)
	var b strings.Builder
	b.WriteString("-----BEGIN " + typ + "-----\n")
	for i := 0; i < len(body); i += 64 {
		end := i + 64
		if end > len(body) {
			end = len(body)
		}
		b.WriteString(body[i:end])
		b.WriteByte('\n')
	}
	b.WriteString("-----END " + typ + "-----\n")
	return b.String()
}
