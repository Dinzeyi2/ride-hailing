package payments

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/account"
	"github.com/stripe/stripe-go/v83/accountlink"
	"github.com/stripe/stripe-go/v83/transfer"
)

type ConnectAccountStatus struct {
	AccountID        string `json:"account_id"`
	DetailsSubmitted bool   `json:"details_submitted"`
	ChargesEnabled   bool   `json:"charges_enabled"`
	PayoutsEnabled   bool   `json:"payouts_enabled"`
	DisabledReason   string `json:"disabled_reason,omitempty"`
}

type PayoutTransfer struct {
	ID               uuid.UUID `json:"id"`
	DriverID         uuid.UUID `json:"driver_id"`
	Amount           float64   `json:"amount"`
	Currency         string    `json:"currency"`
	Status           string    `json:"status"`
	StripeTransferID *string   `json:"stripe_transfer_id,omitempty"`
	FailureReason    *string   `json:"failure_reason,omitempty"`
	AttemptCount     int       `json:"attempt_count"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// RequestPayout atomically reserves wallet funds and creates the durable payout
// before contacting Stripe. Provider failure is compensated in a second locked
// transaction, making retries and reconciliation safe after process crashes.
func (s *ConnectService) RequestPayout(ctx context.Context, driverID uuid.UUID, amount float64, currency string) (*PayoutTransfer, error) {
	if amount < 5 {
		return nil, fmt.Errorf("minimum withdrawal is 5.00")
	}
	connect, err := s.Status(ctx, driverID)
	if err != nil {
		return nil, err
	}
	if !connect.PayoutsEnabled {
		return nil, fmt.Errorf("Stripe payouts are not enabled for this driver")
	}
	payoutID := uuid.New()
	var walletID uuid.UUID
	var balance float64
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT id,balance FROM wallets WHERE user_id=$1 FOR UPDATE`, driverID).Scan(&walletID, &balance); err != nil {
		return nil, err
	}
	if balance < amount {
		return nil, fmt.Errorf("insufficient wallet balance")
	}
	_, err = tx.Exec(ctx, `UPDATE wallets SET balance=balance-$2,updated_at=NOW() WHERE id=$1`, walletID, amount)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO wallet_transactions(id,wallet_id,amount,type,description,reference_id,reference_type,balance_before,balance_after,created_at)
		VALUES($1,$2,$3,'debit','Stripe Connect payout',$4,'payout',$5,$6,NOW())`, uuid.New(), walletID, amount, payoutID, balance, balance-amount)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_payout_transfers(id,driver_id,stripe_account_id,amount,currency,status,wallet_id) VALUES($1,$2,$3,$4,$5,'processing',$6)`, payoutID, driverID, connect.AccountID, amount, currency, walletID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	stripe.Key = s.apiKey
	params := &stripe.TransferParams{Amount: stripe.Int64(int64(amount*100 + 0.5)), Currency: stripe.String(currency), Destination: stripe.String(connect.AccountID), Description: stripe.String("Fare driver payout")}
	params.SetIdempotencyKey(payoutID.String())
	params.AddMetadata("payout_id", payoutID.String())
	params.AddMetadata("driver_id", driverID.String())
	t, providerErr := transfer.New(params)
	if providerErr != nil {
		_ = s.failAndCompensate(ctx, payoutID, providerErr.Error())
		return nil, fmt.Errorf("create Stripe transfer: %w", providerErr)
	}
	_, err = s.db.Exec(ctx, `UPDATE driver_payout_transfers SET status='paid',provider_status='created',stripe_transfer_id=$2,reconciled_at=NOW(),updated_at=NOW() WHERE id=$1`, payoutID, t.ID)
	if err != nil {
		return nil, err
	}
	return s.GetPayout(ctx, driverID, payoutID)
}

func (s *ConnectService) failAndCompensate(ctx context.Context, payoutID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var walletID uuid.UUID
	var amount, balance float64
	var status string
	var reversal *time.Time
	err = tx.QueryRow(ctx, `SELECT p.wallet_id,p.amount,p.status,p.reversal_at,w.balance FROM driver_payout_transfers p JOIN wallets w ON w.id=p.wallet_id WHERE p.id=$1 FOR UPDATE OF p,w`, payoutID).Scan(&walletID, &amount, &status, &reversal, &balance)
	if err != nil {
		return err
	}
	if reversal != nil {
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE wallets SET balance=balance+$2,updated_at=NOW() WHERE id=$1`, walletID, amount)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO wallet_transactions(id,wallet_id,amount,type,description,reference_id,reference_type,balance_before,balance_after,created_at) VALUES($1,$2,$3,'credit','Failed payout reversal',$4,'payout_reversal',$5,$6,NOW())`, uuid.New(), walletID, amount, payoutID, balance, balance+amount)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE driver_payout_transfers SET status='failed',failure_reason=$2,provider_status='failed',reversal_at=NOW(),updated_at=NOW() WHERE id=$1`, payoutID, reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *ConnectService) GetPayout(ctx context.Context, driverID, payoutID uuid.UUID) (*PayoutTransfer, error) {
	p := &PayoutTransfer{}
	err := s.db.QueryRow(ctx, `SELECT id,driver_id,amount,currency,status,stripe_transfer_id,failure_reason,attempt_count,created_at,updated_at FROM driver_payout_transfers WHERE id=$1 AND driver_id=$2`, payoutID, driverID).Scan(&p.ID, &p.DriverID, &p.Amount, &p.Currency, &p.Status, &p.StripeTransferID, &p.FailureReason, &p.AttemptCount, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
func (s *ConnectService) PayoutHistory(ctx context.Context, driverID uuid.UUID, limit int) ([]*PayoutTransfer, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `SELECT id,driver_id,amount,currency,status,stripe_transfer_id,failure_reason,attempt_count,created_at,updated_at FROM driver_payout_transfers WHERE driver_id=$1 ORDER BY created_at DESC LIMIT $2`, driverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*PayoutTransfer, 0)
	for rows.Next() {
		p := &PayoutTransfer{}
		if err := rows.Scan(&p.ID, &p.DriverID, &p.Amount, &p.Currency, &p.Status, &p.StripeTransferID, &p.FailureReason, &p.AttemptCount, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *ConnectService) AdminPayoutHistory(ctx context.Context, limit int) ([]*PayoutTransfer, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `SELECT id,driver_id,amount,currency,status,stripe_transfer_id,failure_reason,attempt_count,created_at,updated_at FROM driver_payout_transfers ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*PayoutTransfer{}
	for rows.Next() {
		p := &PayoutTransfer{}
		if err := rows.Scan(&p.ID, &p.DriverID, &p.Amount, &p.Currency, &p.Status, &p.StripeTransferID, &p.FailureReason, &p.AttemptCount, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *ConnectService) RetryPayout(ctx context.Context, actorID, payoutID uuid.UUID) (*PayoutTransfer, error) {
	var driverID uuid.UUID
	var amount float64
	var currency, status string
	err := s.db.QueryRow(ctx, `SELECT driver_id,amount,currency,status FROM driver_payout_transfers WHERE id=$1`, payoutID).Scan(&driverID, &amount, &currency, &status)
	if err != nil {
		return nil, err
	}
	if status != "failed" {
		return nil, fmt.Errorf("only failed payouts can be retried")
	}
	payout, err := s.RequestPayout(ctx, driverID, amount, currency)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.Exec(ctx, `INSERT INTO driver_operation_audit(actor_id,driver_id,operation,entity_type,entity_id,metadata) VALUES($1,$2,'retry_payout','payout',$3,jsonb_build_object('replacement_payout_id',$4))`, actorID, driverID, payoutID, payout.ID)
	return payout, nil
}

func (s *ConnectService) ReconcilePayouts(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT id,stripe_transfer_id FROM driver_payout_transfers WHERE stripe_transfer_id IS NOT NULL AND (reconciled_at IS NULL OR reconciled_at<NOW()-INTERVAL '1 hour') LIMIT 100`)
	if err != nil {
		return 0, err
	}
	type item struct {
		id       uuid.UUID
		stripeID string
	}
	items := []item{}
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.id, &i.stripeID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, i)
	}
	rows.Close()
	stripe.Key = s.apiKey
	count := 0
	for _, i := range items {
		t, err := transfer.Get(i.stripeID, nil)
		if err != nil {
			continue
		}
		status := "paid"
		providerStatus := "created"
		if t.Reversed {
			status = "failed"
			providerStatus = "reversed"
			_ = s.failAndCompensate(ctx, i.id, "Stripe transfer reversed")
		}
		_, _ = s.db.Exec(ctx, `UPDATE driver_payout_transfers SET status=$2,provider_status=$3,reconciled_at=NOW(),updated_at=NOW() WHERE id=$1`, i.id, status, providerStatus)
		count++
	}
	return count, nil
}

func (s *ConnectService) Transfer(ctx context.Context, driverID uuid.UUID, amount float64, currency string) (string, error) {
	status, err := s.Status(ctx, driverID)
	if err != nil {
		return "", err
	}
	if !status.PayoutsEnabled {
		return "", fmt.Errorf("Stripe payouts are not enabled for this driver")
	}
	payoutID := uuid.New()
	_, err = s.db.Exec(ctx, `INSERT INTO driver_payout_transfers(id,driver_id,stripe_account_id,amount,currency,status) VALUES($1,$2,$3,$4,$5,'processing')`, payoutID, driverID, status.AccountID, amount, currency)
	if err != nil {
		return "", err
	}
	stripe.Key = s.apiKey
	params := &stripe.TransferParams{Amount: stripe.Int64(int64(amount*100 + 0.5)), Currency: stripe.String(currency), Destination: stripe.String(status.AccountID), Description: stripe.String("Fare driver payout")}
	params.SetIdempotencyKey(payoutID.String())
	params.AddMetadata("payout_id", payoutID.String())
	params.AddMetadata("driver_id", driverID.String())
	t, transferErr := transfer.New(params)
	if transferErr != nil {
		_, _ = s.db.Exec(ctx, `UPDATE driver_payout_transfers SET status='failed',failure_reason=$2,updated_at=NOW() WHERE id=$1`, payoutID, transferErr.Error())
		return "", fmt.Errorf("create Stripe transfer: %w", transferErr)
	}
	_, err = s.db.Exec(ctx, `UPDATE driver_payout_transfers SET status='paid',stripe_transfer_id=$2,updated_at=NOW() WHERE id=$1`, payoutID, t.ID)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

type ConnectService struct {
	db         *pgxpool.Pool
	apiKey     string
	refreshURL string
	returnURL  string
}

func NewConnectService(db *pgxpool.Pool, apiKey, refreshURL, returnURL string) *ConnectService {
	return &ConnectService{db: db, apiKey: apiKey, refreshURL: refreshURL, returnURL: returnURL}
}

func (s *ConnectService) EnsureAccount(ctx context.Context, driverID uuid.UUID, country string) (*ConnectAccountStatus, error) {
	if s.apiKey == "" {
		return nil, fmt.Errorf("Stripe Connect is not configured")
	}
	stripe.Key = s.apiKey
	if existing, err := s.getStored(ctx, driverID); err == nil {
		return s.Refresh(ctx, driverID, existing.AccountID)
	}
	var email string
	if err := s.db.QueryRow(ctx, `SELECT email FROM users WHERE id=$1 AND role='driver' AND is_active=true`, driverID).Scan(&email); err != nil {
		return nil, fmt.Errorf("active driver account not found: %w", err)
	}
	params := &stripe.AccountParams{Type: stripe.String(string(stripe.AccountTypeExpress)), Email: stripe.String(email), Country: stripe.String(country)}
	params.AddMetadata("driver_id", driverID.String())
	acct, err := account.New(params)
	if err != nil {
		return nil, fmt.Errorf("create connected account: %w", err)
	}
	_, err = s.db.Exec(ctx, `INSERT INTO driver_stripe_accounts(driver_id,stripe_account_id,details_submitted,charges_enabled,payouts_enabled)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(driver_id) DO NOTHING`, driverID, acct.ID, acct.DetailsSubmitted, acct.ChargesEnabled, acct.PayoutsEnabled)
	if err != nil {
		return nil, fmt.Errorf("store connected account: %w", err)
	}
	return s.Refresh(ctx, driverID, acct.ID)
}

func (s *ConnectService) CreateOnboardingLink(ctx context.Context, driverID uuid.UUID) (string, error) {
	if s.apiKey == "" || s.refreshURL == "" || s.returnURL == "" {
		return "", fmt.Errorf("Stripe Connect onboarding URLs are not configured")
	}
	stored, err := s.getStored(ctx, driverID)
	if err != nil {
		return "", fmt.Errorf("create a Connect account first: %w", err)
	}
	stripe.Key = s.apiKey
	link, err := accountlink.New(&stripe.AccountLinkParams{Account: stripe.String(stored.AccountID), RefreshURL: stripe.String(s.refreshURL), ReturnURL: stripe.String(s.returnURL), Type: stripe.String(string(stripe.AccountLinkTypeAccountOnboarding))})
	if err != nil {
		return "", fmt.Errorf("create onboarding link: %w", err)
	}
	return link.URL, nil
}

func (s *ConnectService) Status(ctx context.Context, driverID uuid.UUID) (*ConnectAccountStatus, error) {
	stored, err := s.getStored(ctx, driverID)
	if err != nil {
		return nil, err
	}
	return s.Refresh(ctx, driverID, stored.AccountID)
}

func (s *ConnectService) RefreshByAccountID(ctx context.Context, accountID string) error {
	var driverID uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT driver_id FROM driver_stripe_accounts WHERE stripe_account_id=$1`, accountID).Scan(&driverID); err != nil {
		return err
	}
	_, err := s.Refresh(ctx, driverID, accountID)
	return err
}

func (s *ConnectService) MarkTransferEvent(ctx context.Context, transferID, eventType string) error {
	status := "paid"
	if eventType == "transfer.failed" || eventType == "transfer.reversed" {
		status = "failed"
	}
	if status == "failed" {
		var payoutID uuid.UUID
		if err := s.db.QueryRow(ctx, `SELECT id FROM driver_payout_transfers WHERE stripe_transfer_id=$1`, transferID).Scan(&payoutID); err != nil {
			return err
		}
		return s.failAndCompensate(ctx, payoutID, eventType)
	}
	_, err := s.db.Exec(ctx, `UPDATE driver_payout_transfers SET status=$2,provider_status=$3,reconciled_at=NOW(),updated_at=NOW() WHERE stripe_transfer_id=$1`, transferID, status, eventType)
	return err
}

func (s *ConnectService) Refresh(ctx context.Context, driverID uuid.UUID, accountID string) (*ConnectAccountStatus, error) {
	stripe.Key = s.apiKey
	acct, err := account.GetByID(accountID, nil)
	if err != nil {
		return nil, fmt.Errorf("refresh connected account: %w", err)
	}
	disabled := ""
	if acct.Requirements != nil {
		disabled = string(acct.Requirements.DisabledReason)
	}
	status := &ConnectAccountStatus{AccountID: acct.ID, DetailsSubmitted: acct.DetailsSubmitted, ChargesEnabled: acct.ChargesEnabled, PayoutsEnabled: acct.PayoutsEnabled, DisabledReason: disabled}
	_, err = s.db.Exec(ctx, `UPDATE driver_stripe_accounts SET details_submitted=$2,charges_enabled=$3,payouts_enabled=$4,disabled_reason=NULLIF($5,''),updated_at=NOW() WHERE driver_id=$1`, driverID, status.DetailsSubmitted, status.ChargesEnabled, status.PayoutsEnabled, status.DisabledReason)
	return status, err
}

func (s *ConnectService) getStored(ctx context.Context, driverID uuid.UUID) (*ConnectAccountStatus, error) {
	status := &ConnectAccountStatus{}
	err := s.db.QueryRow(ctx, `SELECT stripe_account_id,details_submitted,charges_enabled,payouts_enabled,COALESCE(disabled_reason,'') FROM driver_stripe_accounts WHERE driver_id=$1`, driverID).Scan(&status.AccountID, &status.DetailsSubmitted, &status.ChargesEnabled, &status.PayoutsEnabled, &status.DisabledReason)
	return status, err
}
