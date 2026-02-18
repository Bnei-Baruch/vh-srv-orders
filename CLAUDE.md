# Orders Service

Go 1.21 service for managing orders, payments, and billing. Module: `gitlab.bbdev.team/vh/pay/orders`.

## Project Structure

```
cmd/            Cobra CLI commands (server, billing, worker, migrate, importer)
api/            Gin HTTP handlers, middleware, route definitions
  middleware/   Auth, logging, recovery, sentry, events
common/         Shared config (global singleton), constants, sentinel errors
domain/         Business logic orchestration
  billing/      Billing workflow (flag, skip, charge, muhlafim)
repo/           Data access layer (pgx, raw SQL)
events/         Event system (NATS JetStream emitter + handlers)
pkg/            Reusable packages
  keycloak/     Keycloak client, token source
  pelecard/     Pelecard payment gateway client
  profiles/     Profile service API client + NATS listener
  testutil/     Test database setup (pgtestdb)
  utils/        Logging helpers, HTTP utils, S3, sync primitives
importers/      Data import utilities
internal/mocks/ Auto-generated mockery mocks
db/migrations/  SQL migration files (golang-migrate)
```

Do NOT restructure toward `internal/service/`, `internal/repository/`, etc. The top-level layout (`api/`, `domain/`, `repo/`, `common/`) is intentional.

## Best Practices

### Tests
- New features and bug fixes should include tests
- Follow the existing test style in the file you're working in (scenario-based, testify, mockery)
- Use `require` for preconditions that should abort the test, `assert` for checks that should report and continue

### Naming
- English only for all identifiers, comments, and log messages. Hebrew domain terms (muhlafim, masof) are acceptable when they match external API terminology
- Meaningful, descriptive names. No single-letter variables except receivers and loop indices
- `camelCase` for params and locals — never `snake_case` (e.g. `dbURL` not `db_url`)
- Single-letter receivers matching the type: `o` for OrdersDB/OrdersAPI, `s` for BillingService, `eh` for EventsHandler, `c` for Client

### Functions
- Keep functions under ~50 lines. Extract helpers when a function does too much
- Single responsibility — a function should do one thing clearly
- Prefer early returns to reduce nesting

### Interfaces
- When adding a public method to `*OrdersDB`: add it to the `OrdersRepository` interface in `repo/orders_repository.go`, then run `mockery` to regenerate mocks
- Same applies to `PelecardAPI`, `ProfileService`, `TokenSource` — update interface + regenerate
- Define interfaces in the same package as the implementation

### Imports
Three groups separated by blank lines: stdlib, external, internal.

```go
import (
    "context"
    "fmt"

    "github.com/gin-gonic/gin"

    "gitlab.bbdev.team/vh/pay/orders/common"
)
```

### General
- Don't comment out code — delete it (git has history)
- Don't leave `fmt.Println` — use `utils.LogFor(ctx)` for all logging
- Don't silently ignore errors. The one accepted pattern is `_ = c.Error(...)` in handlers (Gin idiom — attaches error for middleware)
- Always pass `context.Context` through — don't create background contexts unless required (e.g. `context.WithoutCancel` for payments)

## Code Style

- Constructors: `NewXxx()` returning concrete types, not interfaces
- Setter injection for optional/swappable dependencies: `func (o *OrdersDB) SetProfileService(ps profiles.ProfileService)`
- No DI framework. No functional options. Dependencies wired in `api/app.go` via `Initialize()` method chain
- Constants in `common/consts.go` — `CamelCase` for exported names
- Context keys are `SCREAMING_SNAKE` strings defined in `common/consts.go`
- Minimal comments. Don't add godoc to everything. Only comment non-obvious business logic
- Nullable fields use `github.com/volatiletech/null/v9`

## Error Handling

Wrap with `fmt.Errorf` using the calling method name as prefix:

```go
return nil, fmt.Errorf("o.GetOrCreateAccount: %w", err)
```

Sentinel errors in `common/errors.go`. Check with `errors.Is()`. Fatal startup errors use `utils.LogFatal()`.

## Logging

stdlib `log/slog` via context. No zap, no logrus.

```go
utils.LogFor(ctx).Info("message", slog.String("key", "value"))
utils.LogFor(ctx).Error("operation failed", slog.Any("err", err))
utils.SentryFor(ctx).CaptureException(err)
```

Logger is enriched per-request in middleware with `request_id`. Workers add `worker_id`, `order_id`.

## Interfaces

`OrdersRepository` (~60 methods) is the central interface. It's large and due for a breakdown, but don't refactor it as part of unrelated work. Mockery generates mocks from it.

Small interfaces for external dependencies: `PelecardAPI` (1), `ProfileService` (3), `EventEmitter` (2), `EventHandler` (2), `ChargeExecutor` (1), `TokenSource` (2). Defined in the same package as their primary implementation.

## Handler Patterns

Handlers are methods on `*OrdersAPI`. They call `o.repo.*` directly — no service layer between handlers and repo.

### Request binding

```go
// JSON body — bind into repo types or local structs
var req repo.Order
if err := c.ShouldBindJSON(&req); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
}

// Path params — strconv.Atoi
id, err := strconv.Atoi(c.Param("id"))
if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id! Accepted value is INTEGER", "success": false})
    return
}

// Query params — c.Query with manual defaults, no ShouldBindQuery
skip := c.Query("skip")
if skip == "" {
    skip = "0"
}
```

### Error responses

Internal errors NEVER return a JSON body. The error is attached to gin context for middleware (logging/sentry):

```go
if err != nil {
    if errors.Is(err, common.ErrInvalidValues) {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    } else if errors.Is(err, common.ErrNoRowsAffected) {
        c.Status(http.StatusNotFound)
    } else {
        c.Status(http.StatusInternalServerError)
        _ = c.Error(fmt.Errorf("repo.MethodName: %w", err))
    }
    return
}
```

### Success responses

```go
c.JSON(http.StatusOK, gin.H{"message": "Fetched!", "data": result, "success": true})       // GET
c.JSON(http.StatusCreated, gin.H{"message": "Created!", "data": id, "success": true})       // POST
c.JSON(http.StatusOK, gin.H{"message": "Updated!", "success": true})                        // PATCH
c.JSON(http.StatusOK, gin.H{"message": "Deleted!", "success": true})                        // DELETE
```

### Auth / permissions

Claims extracted from context as `*middleware.IDTokenClaims`. Use the helper methods on `*OrdersAPI`:

```go
// Admin-only — sets 403 and returns false if unauthorized
if !o.HasAnyRole(c, common.RoleRoot, common.RoleAdmin) {
    return
}

// User-or-admin — returns (isAuthenticated, isAdmin, keycloakId)
isAuthUser, isAdmin, keycloakId := o.isAuthUserOrHasAnyRole(c, common.RoleAdmin, common.RoleRoot)
if !isAuthUser {
    return
}
```

## Repo Patterns

All methods on `*OrdersDB` (embeds `*pgxpool.Pool`). Context always first param. Raw SQL, no ORM.

### Single row

```go
func (o *OrdersDB) GetOrderByID(ctx context.Context, orderID uint) (*Order, error) {
    var order Order
    if err := o.QueryRow(ctx, `SELECT id, "Type", ... FROM orders WHERE id=$1`, orderID).Scan(
        &order.ID, &order.Type, ...
    ); err != nil {
        return nil, fmt.Errorf("o.QueryRow.Scan: %w", err)
    }
    return &order, nil
}
```

### Multiple rows

```go
rows, err := o.Query(ctx, query)
if err != nil {
    return nil, fmt.Errorf("o.Query: %w", err)
}
defer rows.Close()

orders := []Order{}
for rows.Next() {
    var d Order
    if err := rows.Scan(&d.ID, &d.Type, ...); err != nil {
        return &orders, fmt.Errorf("rows.Scan: %w", err)
    }
    orders = append(orders, d)
}
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("rows.Err: %w", err)
}
return &orders, nil
```

### Exec with rows-affected check

```go
res, err := o.Exec(ctx, fmt.Sprintf(`UPDATE orders SET %s WHERE id=%d`, toUpdate, orderId), args...)
if err != nil {
    return fmt.Errorf("problem updating order: %w", err)
}
if res.RowsAffected() == 0 {
    return common.ErrNoRowsAffected
}
```

### Transactions

```go
tx, err := o.Begin(ctx)
if err != nil {
    return fmt.Errorf("o.Begin: %w", err)
}
defer tx.Rollback(ctx)
// ... tx.QueryRow, tx.Exec ...
return tx.Commit(ctx)
```

### Dynamic query building

Conditional fields based on `null.*.Valid`. Uses `prepare*Query` helpers that build column lists, placeholders, and args slices. Not an ORM — just string building with `fmt.Sprintf` and positional `$N` params.

### Events from repo

```go
o.emitEvent(ctx, events.TypeCreateOrder, map[string]interface{}{"order_id": ID})
```

The `EventBuilder` comes from context (injected by middleware or event handler). Always emit after the DB operation succeeds.

## Database Conventions

- Legacy columns: PascalCase with double quotes — `"FirstName"`, `"AccountID"`, `"PaymentDate"`
- Newer columns: snake_case — `created_at`, `updated_at`, `deleted_at`, `card_details_id`
- New columns should use snake_case
- Nullable DB columns map to `null.String`, `null.Int`, `null.Float64`, `null.Time`, `null.Bool`, `null.JSON`

## Migrations

Files in `db/migrations/`. Format: `NN_description.up.sql` / `NN_description.down.sql` (sequential number prefix).

```sql
BEGIN;
ALTER TABLE payments_pelecard ADD COLUMN terminal VARCHAR(10);
COMMIT;
```

## Configuration

Global singleton via `envconfig`. No viper. No functional options.

```go
common.Config.Port          // read anywhere
common.LoadConfig()         // called once at startup
```

CLI flags via Cobra for subcommand-specific options (`--month`, `--dry-run`, `--max-workers`).

## Testing

**Libraries:** `testify/assert`, `testify/require`, `testify/mock`

**Mocks:** Auto-generated via mockery (`.mockery.yml`). Output in `internal/mocks/`. Regenerate with `mockery`.

```go
mockRepo := mocks.NewMockOrdersRepository(t)
mockRepo.EXPECT().ClearAllFlags(ctx).Return(nil)
```

**Style:** Scenario-based individual test functions, not table-driven:

```go
func TestFlagAndSkipOperations_SkippedWhenFlagsFalse(t *testing.T) { ... }
func TestChargeOperations_ConcurrentFallbackToEMV(t *testing.T) { ... }
```

**Integration tests:** Real PostgreSQL via `pgtestdb` (isolated migrated DBs per test). HTTP tests use `httptest` with helpers (`GET`, `POST`, `PATCH_ROOT`).

**Test context setup:** Use `eventstest.WithTestEventBuilder(t, ctx)` to inject a test event builder into context.

When adding tests, follow the existing pattern in the file. Don't convert existing tests to table-driven.

## Concurrency

- Inline worker pools with `sync.WaitGroup` and channels — no formal WorkerPool abstraction
- Panic recovery per goroutine via `defer func() { if r := recover() ... }()`
- `context.WithoutCancel(ctx)` for payment operations (prevent state corruption on cancellation)
- Thread-safe counters via `utils.CounterMap[T]` (generic, mutex-based)

## Build and Run

Uses [go-task](https://taskfile.dev) (`Taskfile.yml`). Run `task --list` to see all tasks.

```sh
task dev                                # init .env, ensure DB, run server (shared mode)
task dev:standalone                     # init .env, start docker infra, run server
task build                              # go build
task run                                # go run ./... server
task test                               # go test -v ./...
task test RACE=true                     # go test -race -v ./...
task test COVERAGE=true                 # go test with coverage report
task dev:migrate                        # ensure DB + run migrations
task db:shell                           # psql into shared dev DB
task db:drop                            # drop the service DB
mockery                                 # regenerate mocks (not a task, run directly)
```

Docker: `task docker:build` builds the image. Production entrypoint is `./orders server` (port 8185).

Build injects git SHA: `-ldflags "-X gitlab.bbdev.team/vh/pay/orders/common.GitSHA=${GIT_SHA}"`

## Tech Stack

| Component      | Library                                |
|----------------|----------------------------------------|
| HTTP framework | gin-gonic/gin                          |
| Database       | jackc/pgx/v4 (raw SQL, no ORM)        |
| Migrations     | golang-migrate/v4                      |
| Messaging      | nats-io/nats.go (JetStream)           |
| HTTP client    | go-resty/resty/v2                      |
| Auth           | coreos/go-oidc/v3 + Keycloak          |
| Mocking        | vektra/mockery                         |
| Testing        | stretchr/testify                       |
| Test DB        | peterldowns/pgtestdb                   |
| Sentry         | getsentry/sentry-go                    |
| Config         | kelseyhightower/envconfig              |
| CLI            | spf13/cobra                            |
| Nullable types | volatiletech/null/v9                   |

No gRPC. No ORM. No wire/fx/dig. No zap/logrus.
