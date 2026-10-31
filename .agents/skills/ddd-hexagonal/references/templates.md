# Go Skeletons — DDD & Hexagonal

Examples use a fictional `billing` bounded context. Replace names, keep the
shape: pure domain → ports → use case → adapter → delivery. Every file stays
under 300 lines and every function under 40.

## 1. Value object

```go
// domain/billing/money.go
package billing

import (
	"errors"
	"fmt"
)

var ErrInvalidAmount = errors.New("billing: amount must be positive")

// Amount is a value object: immutable, compared by value, always valid.
type Amount struct {
	cents    int64
	currency string
}

func NewAmount(cents int64, currency string) (Amount, error) {
	if cents <= 0 {
		return Amount{}, ErrInvalidAmount
	}
	if currency == "" {
		return Amount{}, errors.New("billing: currency is required")
	}
	return Amount{cents: cents, currency: currency}, nil
}

func (a Amount) Cents() int64      { return a.cents }
func (a Amount) Currency() string  { return a.currency }

func (a Amount) Add(other Amount) (Amount, error) {
	if a.currency != other.currency {
		return Amount{}, fmt.Errorf("billing: cannot add %s to %s", other.currency, a.currency)
	}
	return NewAmount(a.cents+other.cents, a.currency)
}
```

## 2. Aggregate root and domain event

```go
// domain/billing/invoice.go
package billing

import (
	"errors"
	"time"
)

var (
	ErrInvoiceEmpty   = errors.New("billing: invoice needs at least one line")
	ErrAlreadyIssued  = errors.New("billing: invoice already issued")
)

type InvoiceStatus string

const (
	InvoiceDraft  InvoiceStatus = "draft"
	InvoiceIssued InvoiceStatus = "issued"
)

// InvoiceIssued is a domain fact. It only carries domain data.
type InvoiceIssued struct {
	InvoiceID string
	At        time.Time
}

type InvoiceLine struct {
	Description string
	Amount      Amount
}

type Invoice struct {
	ID     string
	Lines  []InvoiceLine
	Status InvoiceStatus
	events []InvoiceIssued
}

// NewInvoice enforces the aggregate invariants at creation time.
func NewInvoice(id string, lines []InvoiceLine) (*Invoice, error) {
	if len(lines) == 0 {
		return nil, ErrInvoiceEmpty
	}
	return &Invoice{ID: id, Lines: lines, Status: InvoiceDraft}, nil
}

// Issue is the only way to reach the issued state.
func (i *Invoice) Issue(at time.Time) error {
	if i.Status == InvoiceIssued {
		return ErrAlreadyIssued
	}
	i.Status = InvoiceIssued
	i.events = append(i.events, InvoiceIssued{InvoiceID: i.ID, At: at})
	return nil
}

// PullEvents drains pending facts for the caller to dispatch.
func (i *Invoice) PullEvents() []InvoiceIssued {
	events := i.events
	i.events = nil
	return events
}
```

Keep `NewInvoice` short by delegating multi-step validation to private
helpers; never exceed 40 lines.

## 3. Outbound port

```go
// domain/billing/ports.go
package billing

import "context"

// InvoiceRepository is owned by the domain; infrastructure implements it.
type InvoiceRepository interface {
	Save(ctx context.Context, invoice *Invoice) error
	ByID(ctx context.Context, id string) (*Invoice, error)
}

// InvoiceNotifier is a second, optional capability (ISP).
type InvoiceNotifier interface {
	NotifyIssued(ctx context.Context, invoice *Invoice) error
}
```

For standard CRUD entities, compose the generic ports from
`domain/shared/store.go` instead of re-declaring methods:

```go
// domain/billing/ports.go
type InvoiceRepository = shared.UpdatableRepository[Invoice]
```

## 4. Application use case (inbound port implementation)

```go
// application/billing/issue_invoice.go
package billing

import (
	"context"
	"time"

	"bridge-gateway/domain/billing"
	"bridge-gateway/domain/errs"
)

// IssueInvoice is an inbound port implementation: one exported use case.
type IssueInvoice struct {
	invoices billing.InvoiceRepository
	notifier billing.InvoiceNotifier
	now      func() time.Time
}

func NewIssueInvoice(
	invoices billing.InvoiceRepository,
	notifier billing.InvoiceNotifier,
) *IssueInvoice {
	return &IssueInvoice{
		invoices: invoices,
		notifier: notifier,
		now:      time.Now,
	}
}

func (u *IssueInvoice) Execute(ctx context.Context, invoiceID string) error {
	invoice, err := u.invoices.ByID(ctx, invoiceID)
	if err != nil {
		return err
	}
	if err := invoice.Issue(u.now().UTC()); err != nil {
		return errs.Wrap(errs.CodeConflict, "invoice cannot be issued", err)
	}
	if err := u.invoices.Save(ctx, invoice); err != nil {
		return err
	}
	return u.notifier.NotifyIssued(ctx, invoice)
}
```

The use case imports only stdlib and domain. Concrete collaborators with
side effects are injected as ports; clocks and ID generators are injected as
functions so tests stay deterministic.

## 5. Consumer-side inbound port (delivery)

```go
// web/ports.go
type invoiceIssuer interface {
	Execute(ctx context.Context, invoiceID string) error
}
```

```go
// web/invoices.go
func (s *invoicesController) issueInvoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.issuer.Execute(r.Context(), id); err != nil {
		s.renderToast(w, "#invoice-error", err.Error())
		return
	}
	s.renderInvoice(w, r, id)
}
```

The handler parses transport input and renders; it holds no rule about when an
invoice may be issued.

## 6. Infrastructure adapter

```go
// infrastructure/invoice_store.go
package infrastructure

import (
	"context"
	"database/sql"

	"bridge-gateway/domain/billing"
)

// InvoiceStore adapts SQLite to billing.InvoiceRepository.
type InvoiceStore struct {
	db *sql.DB
}

func NewInvoiceStore(db *sql.DB) *InvoiceStore {
	return &InvoiceStore{db: db}
}

var _ billing.InvoiceRepository = (*InvoiceStore)(nil)

func (s *InvoiceStore) Save(ctx context.Context, invoice *billing.Invoice) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE invoices SET status = ? WHERE id = ?`,
		invoice.Status, invoice.ID,
	)
	return err
}

func (s *InvoiceStore) ByID(ctx context.Context, id string) (*billing.Invoice, error) {
	// Scan the row, then rebuild the aggregate through its constructor.
	return scanInvoice(s.db.QueryRowContext(ctx,
		`SELECT id, status FROM invoices WHERE id = ?`, id))
}
```

The adapter may use any library; it must not decide domain rules. Map every
storage shape into domain types before returning.

## 7. Error mapping (once, at delivery)

```go
// handlers/httpx/errors.go (illustrative)
func statusFor(err error) int {
	switch errs.CodeOf(err) {
	case errs.CodeValidation:
		return http.StatusBadRequest
	case errs.CodeNotFound:
		return http.StatusNotFound
	case errs.CodeConflict:
		return http.StatusConflict
	case errs.CodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
```

Use the existing mapping helper in `handlers/httpx` rather than adding a second
one. Never translate adapter errors in the domain: map them where the adapter
meets the port.

## 8. Test skeleton

```go
// application/billing/issue_invoice_test.go
package billing

import (
	"context"
	"testing"

	"bridge-gateway/domain/billing"
)

type fakeInvoices struct {
	saved *billing.Invoice
}

func (f *fakeInvoices) Save(_ context.Context, inv *billing.Invoice) error {
	f.saved = inv
	return nil
}

func (f *fakeInvoices) ByID(_ context.Context, _ string) (*billing.Invoice, error) {
	amount, _ := billing.NewAmount(100, "EUR")
	return billing.NewInvoice("inv-1", []billing.InvoiceLine{{Description: "x", Amount: amount}})
}
```

Test domain rules directly in `domain/...`, use cases against fakes,
adapters against the real SQLite file, and complete flows under `tests/`.
