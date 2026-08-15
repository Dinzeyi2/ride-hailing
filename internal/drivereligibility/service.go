package drivereligibility

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Result is the authoritative, database-backed decision used by geo and rides.
type Result struct {
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons,omitempty"`
}

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Check verifies every production prerequisite before a driver can receive work.
func (s *Service) Check(ctx context.Context, userID uuid.UUID) (*Result, error) {
	const query = `
		SELECT
			u.is_active AND u.deleted_at IS NULL
			AND NULLIF(BTRIM(u.first_name), '') IS NOT NULL
			AND NULLIF(BTRIM(u.last_name), '') IS NOT NULL
			AND NULLIF(BTRIM(u.phone_number), '') IS NOT NULL AS profile_complete,
			COALESCE(d.approval_status = 'approved', false) AS approved,
			COALESCE(dvs.verification_status = 'approved', false) AS documents_approved,
			EXISTS (SELECT 1 FROM driver_background_checks bc WHERE bc.driver_id=d.id AND bc.status='passed' AND (bc.expires_at IS NULL OR bc.expires_at>NOW())) AS background_passed,
			EXISTS (
				SELECT 1 FROM vehicles v
				WHERE v.driver_id = u.id AND v.status = 'approved'
				  AND v.is_active = true
				  AND (v.insurance_expiry IS NULL OR v.insurance_expiry >= CURRENT_DATE)
				  AND (v.registration_expiry IS NULL OR v.registration_expiry >= CURRENT_DATE)
				  AND (v.inspection_expiry IS NULL OR v.inspection_expiry >= CURRENT_DATE)
			) AS active_vehicle
		FROM users u
		JOIN drivers d ON d.user_id = u.id
		LEFT JOIN driver_verification_status dvs ON dvs.driver_id = d.id
		WHERE u.id = $1 AND u.role = 'driver'`

	var profile, approved, documents, background, vehicle bool
	if err := s.db.QueryRow(ctx, query, userID).Scan(&profile, &approved, &documents, &background, &vehicle); err != nil {
		return nil, fmt.Errorf("check driver eligibility: %w", err)
	}

	reasons := make([]string, 0, 5)
	if !profile {
		reasons = append(reasons, "profile_incomplete")
	}
	if !approved {
		reasons = append(reasons, "onboarding_not_approved")
	}
	if !documents {
		reasons = append(reasons, "documents_not_approved")
	}
	if !background {
		reasons = append(reasons, "background_check_not_passed")
	}
	if !vehicle {
		reasons = append(reasons, "no_active_approved_vehicle")
	}
	return &Result{Eligible: len(reasons) == 0, Reasons: reasons}, nil
}

func (r *Result) Message() string {
	if r == nil || r.Eligible {
		return ""
	}
	return "driver is not eligible: " + strings.Join(r.Reasons, ", ")
}

func (s *Service) SetAvailability(ctx context.Context, userID uuid.UUID, status string) error {
	online := status != "offline"
	available := status == "available"
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE drivers SET is_online=$2,is_available=$3,updated_at=NOW() WHERE user_id=$1`, userID, online, available)
	if err != nil {
		return fmt.Errorf("persist driver availability: %w", err)
	}
	if online {
		_, err = tx.Exec(ctx, `INSERT INTO driver_online_sessions(driver_id) VALUES($1) ON CONFLICT(driver_id) WHERE ended_at IS NULL DO NOTHING`, userID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE driver_online_sessions SET ended_at=NOW() WHERE driver_id=$1 AND ended_at IS NULL`, userID)
	}
	if err != nil {
		return fmt.Errorf("persist driver session: %w", err)
	}
	return tx.Commit(ctx)
}
