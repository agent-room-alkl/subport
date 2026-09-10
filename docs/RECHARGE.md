# Subport Recharge

Admin manual top-up and user self-recharge via Alipay (page-pay) with a local mock path.

## Semantics

Paid credit **increases `quota_total`** (`AddQuotaCredit`). Historical `quota_used` stays meaningful.
Compensation still uses `sqliteCreditQuota` (reduces `quota_used`) — that path is for refunds, not purchases.

`remaining ≈ quota_total - quota_used` (reservations still held separately).

## Packages

Defaults (override with `SUBPORT_RECHARGE_PACKAGES` JSON array):

| id   | fiat   | credit   |
|------|--------|----------|
| p10  | 10 CNY | 200000   |
| p50  | 50 CNY | 1000000  |
| p100 | 100 CNY| 2200000  |

Example env:

```bash
SUBPORT_RECHARGE_PACKAGES='[{"id":"p10","label":"10 CNY","amount_fiat_cents":1000,"currency":"CNY","quota_credit":200000}]'
```

## Environment

| Variable | Purpose |
|----------|---------|
| `SUBPORT_ALIPAY_MOCK=1` | Enable mock pay URL + `POST /api/console/recharge/{id}/mock-pay` |
| `SUBPORT_ALIPAY_ENABLED=1` | Enable live Alipay |
| `SUBPORT_ALIPAY_MODE` | `page` (default) — `alipay.trade.page.pay` |
| `SUBPORT_ALIPAY_APP_ID` / `SUBPORT_ALIPAY_CLIENT_ID` | App id |
| `SUBPORT_ALIPAY_MERCHANT_PRIVATE_KEY` | Merchant RSA private key (PEM or raw base64) |
| `SUBPORT_ALIPAY_PUBLIC_KEY` | Alipay public key for notify verify |
| `SUBPORT_PUBLIC_BASE_URL` | Public origin, e.g. `https://pay.example.com` (no trailing slash) |
| `SUBPORT_ALIPAY_GATEWAY` | Optional; default `https://openapi.alipay.com/gateway.do` |
| `SUBPORT_RECHARGE_PACKAGES` | Optional JSON package list |

Never commit secrets. Live create returns **503** if credentials are missing and mock is off.

## APIs

### Admin (`requireAdmin`)

- `GET /api/users` — public users + remaining
- `POST /api/users/{id}/topups` `{ "credit": 100000, "note": "gift" }`
- `GET /api/users/{id}/topups`
- `GET /api/topups?limit=`
- `GET /api/payment-orders?limit=`

### Console (session)

- `GET /api/console/packages`
- `POST /api/console/recharge` `{ "package_id": "p50", "idempotency_key": "optional" }`
- `GET /api/console/recharge/{order_id}`
- `POST /api/console/recharge/{order_id}/mock-pay` (mock only)
- `GET /api/console/topups`

### Payments (public)

- `POST /api/payments/alipay/notify` — RSA2 verify when live; mock accepts `X-Subport-Mock-Notify: 1`
- `GET /api/payments/alipay/return` — HTML “支付结果处理中…”

Notify fields: `out_trade_no` = order id, `trade_status` = `TRADE_SUCCESS` / `TRADE_FINISHED`, optional `trade_no`.

## Admin curl

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"subport-admin"}' | jq -r .token)

curl -s http://127.0.0.1:8080/api/users -H "Authorization: Bearer $TOKEN" | jq .

curl -s -X POST http://127.0.0.1:8080/api/users/USER_ID/topups \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"credit":500000,"note":"manual"}' | jq .
```

## User mock smoke

```bash
export SUBPORT_ALIPAY_MOCK=1
export SUBPORT_PUBLIC_BASE_URL=http://127.0.0.1:8080
# start subport.exe …

# register / login as user, then:
curl -s -X POST http://127.0.0.1:8080/api/console/recharge \
  -H "Authorization: Bearer $USER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"package_id":"p50"}' | jq .

curl -s -X POST http://127.0.0.1:8080/api/console/recharge/ORDER_ID/mock-pay \
  -H "Authorization: Bearer $USER_TOKEN" | jq .

curl -s http://127.0.0.1:8080/api/console/quota \
  -H "Authorization: Bearer $USER_TOKEN" | jq .
# quota_total should rise by 1000000 once; second mock-pay must not double credit
```

## Alipay live notes

1. Create an Alipay open platform app with **电脑网站支付** (`alipay.trade.page.pay`).
2. Upload merchant public key; paste Alipay public key into `SUBPORT_ALIPAY_PUBLIC_KEY`.
3. Set `SUBPORT_PUBLIC_BASE_URL` to a reachable HTTPS origin so `notify_url` / `return_url` work.
4. Enable `SUBPORT_ALIPAY_ENABLED=1`, disable mock.
5. CreatePayment builds a signed gateway URL; after pay, Alipay POSTs notify → `MarkOrderPaidAndCredit` (idempotent).

## UI

- Admin Vue: nav **充值** → `/recharge` (user select, credit, recent topups/orders).
- User console: `/console` after login (quota, packages, mock-pay, API keys).
