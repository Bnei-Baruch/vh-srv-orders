# Priority ERP Cloud API – Developer Guide (OData/REST)

> This guide is tailored for **Priority Cloud** and optimized for use in IDEs like **Cursor**. It includes copy‑pasteable `curl` commands, a minimal TypeScript client, and common recipes. It summarizes official sources and points you to the docs for deeper details.

---

## Service Root (Base URL)

```
https://<your-host>/odata/Priority/<tabula.ini>/<environment>/
```

- `tabula.ini` and `<environment>` are the identifiers for your Priority tenant/environment.
- You can verify the model with the service metadata:
  ```http
  GET https://<your-host>/odata/Priority/<tabula.ini>/<environment>/$metadata
  ```

> The service is **OData** compliant (v4 style conventions). Entities map to **forms** in Priority.

---

## Authentication Options

Priority Cloud supports several auth methods. Choose one based on your use case:

### 1) Basic Auth (username/password)
```bash
curl -u 'USERNAME:PASSWORD' \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS?$top=1'
```

### 2) **Personal Access Token (PAT)**
- Create a PAT in Priority: *“REST Interface Access Tokens”* form.
- Use the token as **username** and the literal string `PAT` as the password:
```bash
curl -u 'YOUR_PAT:PAT' \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS?$top=1'
```

### 3) **OAuth 2.0 (Authorization Code / Client Credentials)**
Obtain an access token from your IdP/app registration, then call with `Authorization: Bearer`:
```bash
ACCESS_TOKEN="eyJ..."
curl -H "Authorization: Bearer $ACCESS_TOKEN" \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS?$top=1'
```

> Pick **PAT** for server‑to‑server integrations when available; use **OAuth2** when a user is present or if your security policy requires it.

---

## Quick Start – Read Your First Rows

```bash
# 1) Inspect the schema
curl -s -u 'YOUR_PAT:PAT' \
  'https://<host>/odata/Priority/<ini>/<env>/$metadata' > metadata.xml

# 2) List entities (forms): just open metadata.xml and search for <EntitySet Name="...">
# 3) Query customers (top 5, select specific fields)
curl -s -u 'YOUR_PAT:PAT' \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS?$select=CUSTNAME,CUSTDES&$top=5' | jq .
```

Common query options (OData):
- `$select=FIELD1,FIELD2` – limit fields
- `$filter=FIELD eq 'VALUE' and NUM gt 0`
- `$orderby=FIELD asc/desc`
- `$top=50&$skip=50` – paging
- `$expand=ChildEntitySet` – include related records

---

## CRUD (Create/Update/Delete)

> Data modification via API requires Priority **v17.2+** and proper permissions.

### Create
```bash
curl -X POST -u 'YOUR_PAT:PAT' \
  -H 'Content-Type: application/json' \
  -d '{"CUSTNAME":"ACME42","CUSTDES":"ACME Demo"}' \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS'
```

### Update (PATCH)
```bash
curl -X PATCH -u 'YOUR_PAT:PAT' \
  -H 'Content-Type: application/json' \
  -d '{"CUSTDES":"ACME Demo – Updated"}' \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS(PrimaryKeyValue)'
```

### Delete
```bash
curl -X DELETE -u 'YOUR_PAT:PAT' \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS(PrimaryKeyValue)'
```

> Primary keys are often numeric or composite. Inspect them in `$metadata` to craft the key syntax, e.g. `ENTITY(Key=...,OtherKey='...')`.

---

## Batching

Use `$batch` for multiple operations in a single HTTP round‑trip. Typical max change‑set size is **100 operations**.

```bash
curl -X POST -u 'YOUR_PAT:PAT' \
  -H "Content-Type: multipart/mixed; boundary=batch_123" \
  'https://<host>/odata/Priority/<ini>/<env>/$batch' \
  --data-binary @batch.txt
```

Example `batch.txt` skeleton:
```
--batch_123
Content-Type: application/http
Content-Transfer-Encoding: binary

GET CUSTOMERS?$top=2 HTTP/1.1

--batch_123
Content-Type: multipart/mixed; boundary=changeset_1

--changeset_1
Content-Type: application/http
Content-Transfer-Encoding: binary

POST CUSTOMERS HTTP/1.1
Content-Type: application/json

{"CUSTNAME":"ACME99","CUSTDES":"From $batch"}
--changeset_1--
--batch_123--
```

---

## Attachments / Files

On **Priority Cloud**, use the **FilesAPI** capability. Typical flow:
1. Create/obtain an attachment slot.
2. Upload file content to the returned path/endpoint.
3. Link the file to the record (e.g., via related entity/field).

> Ask your Priority admin whether **FilesAPI** is enabled and which entity handles attachments for your use case.

---

## Errors & Permissions

- HTTP status codes reflect success/failure (`2xx`, `4xx`, `5xx`).
- The API respects **field‑level** and **record‑level** permissions. You won’t see or be able to write fields you’re not permitted to.
- Large results will include paging via `$top/$skip` (and may include `@odata.nextLink`). Always implement pagination.

---

## Common Recipes

### Filter by date range
```bash
curl -s -u 'YOUR_PAT:PAT' \
  --get 'https://<host>/odata/Priority/<ini>/<env>/ORDERS' \
  --data-urlencode "\$filter=CURDATE ge 2025-01-01 and CURDATE lt 2025-12-31" \
  --data-urlencode "\$select=ORDNAME,CUSTNAME,CURDATE" \
  --data-urlencode "\$orderby=CURDATE desc" | jq .
```

### Expand child rows (e.g., order lines)
```bash
curl -s -u 'YOUR_PAT:PAT' \
  'https://<host>/odata/Priority/<ini>/<env>/ORDERS?$expand=ORDERITEMS($select=PARTNAME,QUANT)'
```

### Partial field selection
```bash
curl -s -u 'YOUR_PAT:PAT' \
  'https://<host>/odata/Priority/<ini>/<env>/CUSTOMERS?$select=CUSTNAME,EMAIL'
```

---

## Minimal TypeScript Client (Node 18+)

```ts
// priority-odata.ts
export class PriorityClient {
  constructor(
    private baseUrl: string,              // e.g., https://host/odata/Priority/tabula.ini/env
    private auth: { type: 'pat'|'basic'|'bearer', user?: string, pass?: string, token?: string }
  ) {}

  private headers(extra: Record<string,string> = {}) {
    const h: Record<string,string> = { 'Accept': 'application/json', ...extra };
    if (this.auth.type === 'pat') {
      const base64 = Buffer.from(`${this.auth.user}:${'PAT'}`).toString('base64');
      h['Authorization'] = `Basic ${base64}`;
    } else if (this.auth.type === 'basic') {
      const base64 = Buffer.from(`${this.auth.user}:${this.auth.pass}`).toString('base64');
      h['Authorization'] = `Basic ${base64}`;
    } else if (this.auth.type === 'bearer') {
      h['Authorization'] = `Bearer ${this.auth.token}`;
    }
    return h;
  }

  async get(path: string) {
    const res = await fetch(`${this.baseUrl}/${path}`, { headers: this.headers() });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${await res.text()}`);
    return res.json();
  }

  async post(path: string, body: any) {
    const res = await fetch(`${this.baseUrl}/${path}`, {
      method: 'POST',
      headers: this.headers({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(body)
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${await res.text()}`);
    return res.json();
  }

  async patch(path: string, body: any) {
    const res = await fetch(`${this.baseUrl}/${path}`, {
      method: 'PATCH',
      headers: this.headers({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(body)
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${await res.text()}`);
    return res.json();
  }

  async delete(path: string) {
    const res = await fetch(`${this.baseUrl}/${path}`, {
      method: 'DELETE',
      headers: this.headers()
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${await res.text()}`);
    return true;
  }
}
```

Usage:
```ts
// example.ts
import { PriorityClient } from './priority-odata';

const client = new PriorityClient(
  'https://<host>/odata/Priority/<ini>/<env>',
  { type: 'pat', user: 'YOUR_PAT' }
);

async function run() {
  const customers = await client.get('CUSTOMERS?$top=5&$select=CUSTNAME,CUSTDES');
  console.log(customers);

  const created = await client.post('CUSTOMERS', { CUSTNAME: 'ACME_TS', CUSTDES: 'From TS client' });
  console.log(created);

  await client.patch(`CUSTOMERS(${created?.Key || ''})`, { CUSTDES: 'Updated' });
}

run().catch(console.error);
```

---

## Notes for Production

- Respect throttling/limits; prefer `$select` and server‑side filtering.
- Implement retries for transient 5xx errors.
- Always log `request-id`/correlation ids (if provided) and payload sizes.
- Use `$batch` when you need transactional groups of operations (≤100 ops per change set).

---

## References (official)

- REST API portal (Priority): https://prioritysoftware.github.io/restapi/
- Auth (OAuth2, PAT, Basic): https://prioritysoftware.github.io/restapi/authenticate/
- OData guide (PDF): https://cdn.priority-software.com/docs/Priority_OData_API.pdf
- Query options: https://prioritysoftware.github.io/restapi/query/
- Modify data: https://prioritysoftware.github.io/restapi/modify/
- Metadata endpoint: https://prioritysoftware.github.io/restapi/request/
- Web SDK (for in‑UI apps): https://prioritysoftware.github.io/api/

