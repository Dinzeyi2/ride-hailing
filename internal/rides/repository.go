package rides

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/richxcame/ride-hailing/pkg/models"
)

// Repository handles database operations for rides
type Repository struct {
	db *pgxpool.Pool
}

type DriverStatistics struct {
	OffersReceived  int64   `json:"offers_received"`
	AcceptedRides   int64   `json:"accepted_rides"`
	CompletedRides  int64   `json:"completed_rides"`
	CancelledRides  int64   `json:"cancelled_rides"`
	AcceptanceRate  float64 `json:"acceptance_rate"`
	CompletionRate  float64 `json:"completion_rate"`
	AverageRating   float64 `json:"average_rating"`
	OnlineMinutes   int64   `json:"online_minutes"`
	WeeklyEarnings  float64 `json:"weekly_earnings"`
	WeeklyRides     int     `json:"weekly_rides"`
	CurrentKeepRate float64 `json:"current_keep_rate"`
}
type DispatchOfferAdmin struct {
	ID         uuid.UUID `json:"id"`
	RideID     uuid.UUID `json:"ride_id"`
	DriverID   uuid.UUID `json:"driver_id"`
	Status     string    `json:"status"`
	Score      float64   `json:"score"`
	DistanceKm float64   `json:"distance_km"`
	OfferedAt  time.Time `json:"offered_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// NewRepository creates a new rides repository
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// CreateRide creates a new ride request
func (r *Repository) CreateRide(ctx context.Context, ride *models.Ride) error {
	query := `
		INSERT INTO rides (
			id, rider_id, status, pickup_latitude, pickup_longitude, pickup_address,
			dropoff_latitude, dropoff_longitude, dropoff_address, estimated_distance,
			estimated_duration, estimated_fare, surge_multiplier, requested_at,
			ride_type_id, promo_code_id, discount_amount, scheduled_at, is_scheduled,
			scheduled_notification_sent,
			country_id, region_id, city_id, pickup_zone_id, dropoff_zone_id,
			currency_code, pricing_version_id, was_negotiated, negotiation_session_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
				$21, $22, $23, $24, $25, $26, $27, $28, $29)
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		ride.ID,
		ride.RiderID,
		ride.Status,
		ride.PickupLatitude,
		ride.PickupLongitude,
		ride.PickupAddress,
		ride.DropoffLatitude,
		ride.DropoffLongitude,
		ride.DropoffAddress,
		ride.EstimatedDistance,
		ride.EstimatedDuration,
		ride.EstimatedFare,
		ride.SurgeMultiplier,
		ride.RequestedAt,
		ride.RideTypeID,
		ride.PromoCodeID,
		ride.DiscountAmount,
		ride.ScheduledAt,
		ride.IsScheduled,
		ride.ScheduledNotificationSent,
		ride.CountryID,
		ride.RegionID,
		ride.CityID,
		ride.PickupZoneID,
		ride.DropoffZoneID,
		ride.CurrencyCode,
		ride.PricingVersionID,
		ride.WasNegotiated,
		ride.NegotiationSessionID,
	).Scan(&ride.CreatedAt, &ride.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create ride: %w", err)
	}

	return nil
}

// GetRideByID retrieves a ride by ID
func (r *Repository) GetRideByID(ctx context.Context, id uuid.UUID) (*models.Ride, error) {
	query := `
		SELECT id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
			   pickup_address, dropoff_latitude, dropoff_longitude, dropoff_address,
			   estimated_distance, estimated_duration, estimated_fare, actual_distance,
			   actual_duration, final_fare, surge_multiplier, requested_at, accepted_at,
			   started_at, completed_at, cancelled_at, cancellation_reason, rating,
			   feedback, created_at, updated_at, ride_type_id, promo_code_id,
			   discount_amount, scheduled_at, is_scheduled, scheduled_notification_sent,
			   country_id, region_id, city_id, pickup_zone_id, dropoff_zone_id,
			   currency_code, pricing_version_id, was_negotiated, negotiation_session_id
		FROM rides
		WHERE id = $1
	`

	ride := &models.Ride{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&ride.ID,
		&ride.RiderID,
		&ride.DriverID,
		&ride.Status,
		&ride.PickupLatitude,
		&ride.PickupLongitude,
		&ride.PickupAddress,
		&ride.DropoffLatitude,
		&ride.DropoffLongitude,
		&ride.DropoffAddress,
		&ride.EstimatedDistance,
		&ride.EstimatedDuration,
		&ride.EstimatedFare,
		&ride.ActualDistance,
		&ride.ActualDuration,
		&ride.FinalFare,
		&ride.SurgeMultiplier,
		&ride.RequestedAt,
		&ride.AcceptedAt,
		&ride.StartedAt,
		&ride.CompletedAt,
		&ride.CancelledAt,
		&ride.CancellationReason,
		&ride.Rating,
		&ride.Feedback,
		&ride.CreatedAt,
		&ride.UpdatedAt,
		&ride.RideTypeID,
		&ride.PromoCodeID,
		&ride.DiscountAmount,
		&ride.ScheduledAt,
		&ride.IsScheduled,
		&ride.ScheduledNotificationSent,
		&ride.CountryID,
		&ride.RegionID,
		&ride.CityID,
		&ride.PickupZoneID,
		&ride.DropoffZoneID,
		&ride.CurrencyCode,
		&ride.PricingVersionID,
		&ride.WasNegotiated,
		&ride.NegotiationSessionID,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get ride: %w", err)
	}

	return ride, nil
}

// UpdateRideStatus updates ride status and related fields.
func (r *Repository) UpdateRideStatus(ctx context.Context, id uuid.UUID, status models.RideStatus, driverID *uuid.UUID) error {
	now := time.Now()
	var query string
	var args []interface{}

	switch status {
	case models.RideStatusAccepted:
		query = `UPDATE rides SET status = $1, driver_id = $2, accepted_at = $3, updated_at = $4 WHERE id = $5`
		args = []interface{}{status, driverID, now, now, id}
	case models.RideStatusInProgress:
		query = `UPDATE rides SET status = $1, started_at = $2, updated_at = $3 WHERE id = $4`
		args = []interface{}{status, now, now, id}
	case models.RideStatusCompleted:
		query = `UPDATE rides SET status = $1, completed_at = $2, updated_at = $3 WHERE id = $4`
		args = []interface{}{status, now, now, id}
	case models.RideStatusCancelled:
		query = `UPDATE rides SET status = $1, cancelled_at = $2, updated_at = $3 WHERE id = $4`
		args = []interface{}{status, now, now, id}
	default:
		query = `UPDATE rides SET status = $1, updated_at = $2 WHERE id = $3`
		args = []interface{}{status, now, id}
	}

	_, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update ride status: %w", err)
	}

	return nil
}

// AtomicAcceptRide atomically transitions a ride from "requested" to "accepted"
// in a single UPDATE with a WHERE status guard. Returns false if the ride was
// already accepted by another driver (prevents double-accept race condition).
func (r *Repository) AtomicAcceptRide(ctx context.Context, rideID, driverID uuid.UUID) (bool, error) {
	now := time.Now()
	query := `
		UPDATE rides
		SET status = $1, driver_id = $2, accepted_at = $3, updated_at = $3
		WHERE id = $4 AND status = $5
	`
	tag, err := r.db.Exec(ctx, query,
		models.RideStatusAccepted, driverID, now, rideID, models.RideStatusRequested,
	)
	if err != nil {
		return false, fmt.Errorf("failed to accept ride: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) CreateRideOffers(ctx context.Context, rideID uuid.UUID, candidates []*DriverCandidate, ttl time.Duration) error {
	if len(candidates) == 0 {
		return nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ride offers: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, candidate := range candidates {
		_, err = tx.Exec(ctx, `INSERT INTO ride_offers (ride_id, driver_id, score, distance_km, expires_at)
			VALUES ($1,$2,$3,$4,NOW()+$5::interval) ON CONFLICT (ride_id,driver_id) DO NOTHING`,
			rideID, candidate.DriverID, candidate.Score, candidate.DistanceKm, fmt.Sprintf("%f seconds", ttl.Seconds()))
		if err != nil {
			return fmt.Errorf("insert ride offer: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) QueueRideOfferNotification(ctx context.Context, rideID, driverID uuid.UUID, data map[string]interface{}) error {
	_, err := r.db.Exec(ctx, `INSERT INTO notifications(id,user_id,type,channel,title,body,data,status,scheduled_at,created_at,updated_at)
		VALUES($1,$2,'ride_offer','push','New ride request','A nearby rider is requesting a trip',$3,'pending',NOW(),NOW(),NOW())`,
		uuid.New(), driverID, data)
	return err
}

func (r *Repository) HasActiveOffer(ctx context.Context, rideID, driverID uuid.UUID) (bool, error) {
	var active bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ride_offers WHERE ride_id=$1 AND driver_id=$2 AND status='offered' AND expires_at>NOW())`, rideID, driverID).Scan(&active)
	return active, err
}

func (r *Repository) ResolveRideOffers(ctx context.Context, rideID, acceptedDriverID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `UPDATE ride_offers SET status=CASE WHEN driver_id=$2 THEN 'accepted' ELSE 'withdrawn' END,responded_at=NOW() WHERE ride_id=$1 AND status='offered' RETURNING driver_id,status`, rideID, acceptedDriverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	withdrawn := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		if status == "withdrawn" {
			withdrawn = append(withdrawn, id)
		}
	}
	return withdrawn, rows.Err()
}

func (r *Repository) GetOfferedRides(ctx context.Context, driverID uuid.UUID) ([]*models.Ride, error) {
	rows, err := r.db.Query(ctx, `SELECT r.id FROM ride_offers o JOIN rides r ON r.id=o.ride_id
		WHERE o.driver_id=$1 AND o.status='offered' AND o.expires_at>NOW() AND r.status='requested' ORDER BY o.score DESC,o.offered_at`, driverID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rides := make([]*models.Ride, 0, len(ids))
	for _, id := range ids {
		ride, err := r.GetRideByID(ctx, id)
		if err != nil {
			return nil, err
		}
		rides = append(rides, ride)
	}
	return rides, nil
}

func (r *Repository) GetActiveRideForDriver(ctx context.Context, driverID uuid.UUID) (*models.Ride, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT id FROM rides WHERE driver_id=$1 AND status IN ('accepted','in_progress') ORDER BY accepted_at DESC LIMIT 1`, driverID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetRideByID(ctx, id)
}

func (r *Repository) GetDriverStatistics(ctx context.Context, driverID uuid.UUID) (*DriverStatistics, error) {
	s := &DriverStatistics{}
	err := r.db.QueryRow(ctx, `SELECT
	(SELECT COUNT(*) FROM ride_offers WHERE driver_id=$1),
	(SELECT COUNT(*) FROM rides WHERE driver_id=$1 AND status IN ('accepted','in_progress','completed')),
	(SELECT COUNT(*) FROM rides WHERE driver_id=$1 AND status='completed'),
	(SELECT COUNT(*) FROM rides WHERE driver_id=$1 AND status='cancelled'),
	COALESCE((SELECT AVG(rating) FROM rides WHERE driver_id=$1 AND rating IS NOT NULL),0),
	COALESCE((SELECT SUM(EXTRACT(EPOCH FROM (COALESCE(ended_at,NOW())-started_at))/60)::bigint FROM driver_online_sessions WHERE driver_id=$1),0),
	COALESCE((SELECT SUM(net_amount) FROM driver_earnings WHERE driver_id=$1 AND created_at>=date_trunc('week',NOW() AT TIME ZONE 'UTC')),0),
	COALESCE((SELECT completed_rides FROM driver_weekly_rewards WHERE driver_id=$1 AND week_start=date_trunc('week',NOW() AT TIME ZONE 'UTC')::date),0),
	COALESCE((SELECT current_keep_rate FROM driver_weekly_rewards WHERE driver_id=$1 AND week_start=date_trunc('week',NOW() AT TIME ZONE 'UTC')::date),.80)`, driverID).Scan(&s.OffersReceived, &s.AcceptedRides, &s.CompletedRides, &s.CancelledRides, &s.AverageRating, &s.OnlineMinutes, &s.WeeklyEarnings, &s.WeeklyRides, &s.CurrentKeepRate)
	if err != nil {
		return nil, err
	}
	if s.OffersReceived > 0 {
		s.AcceptanceRate = float64(s.AcceptedRides) / float64(s.OffersReceived)
	}
	if s.AcceptedRides > 0 {
		s.CompletionRate = float64(s.CompletedRides) / float64(s.AcceptedRides)
	}
	return s, nil
}

func (r *Repository) ListDispatchOffers(ctx context.Context, limit int) ([]DispatchOfferAdmin, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := r.db.Query(ctx, `SELECT id,ride_id,driver_id,status,COALESCE(score,0),COALESCE(distance_km,0),offered_at,expires_at FROM ride_offers ORDER BY offered_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DispatchOfferAdmin{}
	for rows.Next() {
		var x DispatchOfferAdmin
		if err := rows.Scan(&x.ID, &x.RideID, &x.DriverID, &x.Status, &x.Score, &x.DistanceKm, &x.OfferedAt, &x.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *Repository) ExpireDispatchOffer(ctx context.Context, actorID, offerID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var driverID uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE ride_offers SET status='expired',responded_at=NOW() WHERE id=$1 AND status='offered' RETURNING driver_id`, offerID).Scan(&driverID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO driver_operation_audit(actor_id,driver_id,operation,entity_type,entity_id) VALUES($1,$2,'expire_offer','ride_offer',$3)`, actorID, driverID, offerID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateRideCompletion updates ride with actual data upon completion
func (r *Repository) UpdateRideCompletion(ctx context.Context, id uuid.UUID, actualDistance float64, actualDuration int, finalFare float64) error {
	query := `
		UPDATE rides
		SET actual_distance = $1, actual_duration = $2, final_fare = $3, updated_at = $4
		WHERE id = $5
	`

	_, err := r.db.Exec(ctx, query, actualDistance, actualDuration, finalFare, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update ride completion: %w", err)
	}

	return nil
}

// AtomicCompleteRide atomically sets completion data and status in a single UPDATE
// with a status+driver guard. Returns false if the ride was not in_progress or
// the driver doesn't match (prevents inconsistent state on partial failure).
func (r *Repository) AtomicCompleteRide(ctx context.Context, rideID, driverID uuid.UUID, actualDistance float64, actualDuration int, finalFare float64) (bool, error) {
	now := time.Now()
	query := `
		UPDATE rides
		SET status = $1, actual_distance = $2, actual_duration = $3,
		    final_fare = $4, completed_at = $5, updated_at = $5
		WHERE id = $6 AND status = $7 AND driver_id = $8
	`
	tag, err := r.db.Exec(ctx, query,
		models.RideStatusCompleted, actualDistance, actualDuration, finalFare,
		now, rideID, models.RideStatusInProgress, driverID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to complete ride: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// UpdateRideRating updates ride rating and feedback
func (r *Repository) UpdateRideRating(ctx context.Context, id uuid.UUID, rating int, feedback *string) error {
	query := `
		UPDATE rides
		SET rating = $1, feedback = $2, updated_at = $3
		WHERE id = $4
	`

	_, err := r.db.Exec(ctx, query, rating, feedback, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update ride rating: %w", err)
	}

	return nil
}

// GetRidesByRider retrieves rides for a specific rider
func (r *Repository) GetRidesByRider(ctx context.Context, riderID uuid.UUID, limit, offset int) ([]*models.Ride, error) {
	query := `
		SELECT id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
			   pickup_address, dropoff_latitude, dropoff_longitude, dropoff_address,
			   estimated_distance, estimated_duration, estimated_fare, actual_distance,
			   actual_duration, final_fare, surge_multiplier, requested_at, accepted_at,
			   started_at, completed_at, cancelled_at, cancellation_reason, rating,
			   feedback, created_at, updated_at, ride_type_id, promo_code_id,
			   discount_amount, scheduled_at, is_scheduled, scheduled_notification_sent,
			   country_id, region_id, city_id, pickup_zone_id, dropoff_zone_id,
			   currency_code, pricing_version_id, was_negotiated, negotiation_session_id
		FROM rides
		WHERE rider_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, riderID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get rides: %w", err)
	}
	defer rows.Close()

	rides := make([]*models.Ride, 0)
	for rows.Next() {
		ride := &models.Ride{}
		err := rows.Scan(
			&ride.ID,
			&ride.RiderID,
			&ride.DriverID,
			&ride.Status,
			&ride.PickupLatitude,
			&ride.PickupLongitude,
			&ride.PickupAddress,
			&ride.DropoffLatitude,
			&ride.DropoffLongitude,
			&ride.DropoffAddress,
			&ride.EstimatedDistance,
			&ride.EstimatedDuration,
			&ride.EstimatedFare,
			&ride.ActualDistance,
			&ride.ActualDuration,
			&ride.FinalFare,
			&ride.SurgeMultiplier,
			&ride.RequestedAt,
			&ride.AcceptedAt,
			&ride.StartedAt,
			&ride.CompletedAt,
			&ride.CancelledAt,
			&ride.CancellationReason,
			&ride.Rating,
			&ride.Feedback,
			&ride.CreatedAt,
			&ride.UpdatedAt,
			&ride.RideTypeID,
			&ride.PromoCodeID,
			&ride.DiscountAmount,
			&ride.ScheduledAt,
			&ride.IsScheduled,
			&ride.ScheduledNotificationSent,
			&ride.CountryID,
			&ride.RegionID,
			&ride.CityID,
			&ride.PickupZoneID,
			&ride.DropoffZoneID,
			&ride.CurrencyCode,
			&ride.PricingVersionID,
			&ride.WasNegotiated,
			&ride.NegotiationSessionID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ride: %w", err)
		}
		rides = append(rides, ride)
	}

	return rides, nil
}

// GetRidesByDriver retrieves rides for a specific driver
func (r *Repository) GetRidesByDriver(ctx context.Context, driverID uuid.UUID, limit, offset int) ([]*models.Ride, error) {
	query := `
		SELECT id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
			   pickup_address, dropoff_latitude, dropoff_longitude, dropoff_address,
			   estimated_distance, estimated_duration, estimated_fare, actual_distance,
			   actual_duration, final_fare, surge_multiplier, requested_at, accepted_at,
			   started_at, completed_at, cancelled_at, cancellation_reason, rating,
			   feedback, created_at, updated_at, ride_type_id, promo_code_id,
			   discount_amount, scheduled_at, is_scheduled, scheduled_notification_sent,
			   country_id, region_id, city_id, pickup_zone_id, dropoff_zone_id,
			   currency_code, pricing_version_id, was_negotiated, negotiation_session_id
		FROM rides
		WHERE driver_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, driverID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get rides: %w", err)
	}
	defer rows.Close()

	rides := make([]*models.Ride, 0)
	for rows.Next() {
		ride := &models.Ride{}
		err := rows.Scan(
			&ride.ID,
			&ride.RiderID,
			&ride.DriverID,
			&ride.Status,
			&ride.PickupLatitude,
			&ride.PickupLongitude,
			&ride.PickupAddress,
			&ride.DropoffLatitude,
			&ride.DropoffLongitude,
			&ride.DropoffAddress,
			&ride.EstimatedDistance,
			&ride.EstimatedDuration,
			&ride.EstimatedFare,
			&ride.ActualDistance,
			&ride.ActualDuration,
			&ride.FinalFare,
			&ride.SurgeMultiplier,
			&ride.RequestedAt,
			&ride.AcceptedAt,
			&ride.StartedAt,
			&ride.CompletedAt,
			&ride.CancelledAt,
			&ride.CancellationReason,
			&ride.Rating,
			&ride.Feedback,
			&ride.CreatedAt,
			&ride.UpdatedAt,
			&ride.RideTypeID,
			&ride.PromoCodeID,
			&ride.DiscountAmount,
			&ride.ScheduledAt,
			&ride.IsScheduled,
			&ride.ScheduledNotificationSent,
			&ride.CountryID,
			&ride.RegionID,
			&ride.CityID,
			&ride.PickupZoneID,
			&ride.DropoffZoneID,
			&ride.CurrencyCode,
			&ride.PricingVersionID,
			&ride.WasNegotiated,
			&ride.NegotiationSessionID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ride: %w", err)
		}
		rides = append(rides, ride)
	}

	return rides, nil
}

// GetRidesByRiderWithTotal retrieves rides for a specific rider with total count
func (r *Repository) GetRidesByRiderWithTotal(ctx context.Context, riderID uuid.UUID, limit, offset int) ([]*models.Ride, int64, error) {
	// Get total count
	var total int64
	countQuery := `SELECT COUNT(*) FROM rides WHERE rider_id = $1`
	err := r.db.QueryRow(ctx, countQuery, riderID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count rides: %w", err)
	}

	// Get paginated rides
	rides, err := r.GetRidesByRider(ctx, riderID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return rides, total, nil
}

// GetRidesByDriverWithTotal retrieves rides for a specific driver with total count
func (r *Repository) GetRidesByDriverWithTotal(ctx context.Context, driverID uuid.UUID, limit, offset int) ([]*models.Ride, int64, error) {
	// Get total count
	var total int64
	countQuery := `SELECT COUNT(*) FROM rides WHERE driver_id = $1`
	err := r.db.QueryRow(ctx, countQuery, driverID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count rides: %w", err)
	}

	// Get paginated rides
	rides, err := r.GetRidesByDriver(ctx, driverID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return rides, total, nil
}

// GetPendingRides retrieves all pending ride requests
func (r *Repository) GetPendingRides(ctx context.Context) ([]*models.Ride, error) {
	query := `
		SELECT id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
			   pickup_address, dropoff_latitude, dropoff_longitude, dropoff_address,
			   estimated_distance, estimated_duration, estimated_fare, actual_distance,
			   actual_duration, final_fare, surge_multiplier, requested_at, accepted_at,
			   started_at, completed_at, cancelled_at, cancellation_reason, rating,
			   feedback, created_at, updated_at, ride_type_id, promo_code_id,
			   discount_amount, scheduled_at, is_scheduled, scheduled_notification_sent,
			   country_id, region_id, city_id, pickup_zone_id, dropoff_zone_id,
			   currency_code, pricing_version_id, was_negotiated, negotiation_session_id
		FROM rides
		WHERE status = 'requested'
		ORDER BY requested_at ASC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending rides: %w", err)
	}
	defer rows.Close()

	rides := make([]*models.Ride, 0)
	for rows.Next() {
		ride := &models.Ride{}
		err := rows.Scan(
			&ride.ID,
			&ride.RiderID,
			&ride.DriverID,
			&ride.Status,
			&ride.PickupLatitude,
			&ride.PickupLongitude,
			&ride.PickupAddress,
			&ride.DropoffLatitude,
			&ride.DropoffLongitude,
			&ride.DropoffAddress,
			&ride.EstimatedDistance,
			&ride.EstimatedDuration,
			&ride.EstimatedFare,
			&ride.ActualDistance,
			&ride.ActualDuration,
			&ride.FinalFare,
			&ride.SurgeMultiplier,
			&ride.RequestedAt,
			&ride.AcceptedAt,
			&ride.StartedAt,
			&ride.CompletedAt,
			&ride.CancelledAt,
			&ride.CancellationReason,
			&ride.Rating,
			&ride.Feedback,
			&ride.CreatedAt,
			&ride.UpdatedAt,
			&ride.RideTypeID,
			&ride.PromoCodeID,
			&ride.DiscountAmount,
			&ride.ScheduledAt,
			&ride.IsScheduled,
			&ride.ScheduledNotificationSent,
			&ride.CountryID,
			&ride.RegionID,
			&ride.CityID,
			&ride.PickupZoneID,
			&ride.DropoffZoneID,
			&ride.CurrencyCode,
			&ride.PricingVersionID,
			&ride.WasNegotiated,
			&ride.NegotiationSessionID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ride: %w", err)
		}
		rides = append(rides, ride)
	}

	return rides, nil
}

// RideFilters represents filters for ride history queries
type RideFilters struct {
	Status    *string
	StartDate *time.Time
	EndDate   *time.Time
}

// GetRidesByRiderWithFilters retrieves filtered rides for a rider
func (r *Repository) GetRidesByRiderWithFilters(ctx context.Context, riderID uuid.UUID, filters *RideFilters, limit, offset int) ([]*models.Ride, int, error) {
	// Build dynamic query
	baseQuery := `
		SELECT id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
			   pickup_address, dropoff_latitude, dropoff_longitude, dropoff_address,
			   estimated_distance, estimated_duration, estimated_fare, actual_distance,
			   actual_duration, final_fare, surge_multiplier, requested_at, accepted_at,
			   started_at, completed_at, cancelled_at, cancellation_reason, rating,
			   feedback, created_at, updated_at, ride_type_id, promo_code_id,
			   discount_amount, scheduled_at, is_scheduled, scheduled_notification_sent,
			   country_id, region_id, city_id, pickup_zone_id, dropoff_zone_id,
			   currency_code, pricing_version_id, was_negotiated, negotiation_session_id
		FROM rides
		WHERE rider_id = $1
	`

	countQuery := `SELECT COUNT(*) FROM rides WHERE rider_id = $1`

	args := []interface{}{riderID}
	argCount := 2

	// Apply filters
	if filters.Status != nil {
		baseQuery += fmt.Sprintf(" AND status = $%d", argCount)
		countQuery += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, *filters.Status)
		argCount++
	}

	if filters.StartDate != nil {
		baseQuery += fmt.Sprintf(" AND created_at >= $%d", argCount)
		countQuery += fmt.Sprintf(" AND created_at >= $%d", argCount)
		args = append(args, *filters.StartDate)
		argCount++
	}

	if filters.EndDate != nil {
		baseQuery += fmt.Sprintf(" AND created_at <= $%d", argCount)
		countQuery += fmt.Sprintf(" AND created_at <= $%d", argCount)
		args = append(args, *filters.EndDate)
		argCount++
	}

	// Add ordering and pagination
	baseQuery += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argCount, argCount+1)
	args = append(args, limit, offset)

	// Get total count
	var total int
	err := r.db.QueryRow(ctx, countQuery, args[:len(args)-2]...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get rides
	rows, err := r.db.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get filtered rides: %w", err)
	}
	defer rows.Close()

	rides := make([]*models.Ride, 0)
	for rows.Next() {
		ride := &models.Ride{}
		err := rows.Scan(
			&ride.ID,
			&ride.RiderID,
			&ride.DriverID,
			&ride.Status,
			&ride.PickupLatitude,
			&ride.PickupLongitude,
			&ride.PickupAddress,
			&ride.DropoffLatitude,
			&ride.DropoffLongitude,
			&ride.DropoffAddress,
			&ride.EstimatedDistance,
			&ride.EstimatedDuration,
			&ride.EstimatedFare,
			&ride.ActualDistance,
			&ride.ActualDuration,
			&ride.FinalFare,
			&ride.SurgeMultiplier,
			&ride.RequestedAt,
			&ride.AcceptedAt,
			&ride.StartedAt,
			&ride.CompletedAt,
			&ride.CancelledAt,
			&ride.CancellationReason,
			&ride.Rating,
			&ride.Feedback,
			&ride.CreatedAt,
			&ride.UpdatedAt,
			&ride.RideTypeID,
			&ride.PromoCodeID,
			&ride.DiscountAmount,
			&ride.ScheduledAt,
			&ride.IsScheduled,
			&ride.ScheduledNotificationSent,
			&ride.CountryID,
			&ride.RegionID,
			&ride.CityID,
			&ride.PickupZoneID,
			&ride.DropoffZoneID,
			&ride.CurrencyCode,
			&ride.PricingVersionID,
			&ride.WasNegotiated,
			&ride.NegotiationSessionID,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan ride: %w", err)
		}
		rides = append(rides, ride)
	}

	return rides, total, nil
}

// GetRidesByDriverWithFilters retrieves filtered rides for a driver
func (r *Repository) GetRidesByDriverWithFilters(ctx context.Context, driverID uuid.UUID, filters *RideFilters, limit, offset int) ([]*models.Ride, int, error) {
	baseQuery := `
		SELECT id, rider_id, driver_id, status, pickup_latitude, pickup_longitude,
			   pickup_address, dropoff_latitude, dropoff_longitude, dropoff_address,
			   estimated_distance, estimated_duration, estimated_fare, actual_distance,
			   actual_duration, final_fare, surge_multiplier, requested_at, accepted_at,
			   started_at, completed_at, cancelled_at, cancellation_reason, rating,
			   feedback, created_at, updated_at, ride_type_id, promo_code_id,
			   discount_amount, scheduled_at, is_scheduled, scheduled_notification_sent,
			   country_id, region_id, city_id, pickup_zone_id, dropoff_zone_id,
			   currency_code, pricing_version_id, was_negotiated, negotiation_session_id
		FROM rides
		WHERE driver_id = $1
	`

	countQuery := `SELECT COUNT(*) FROM rides WHERE driver_id = $1`

	args := []interface{}{driverID}
	argCount := 2

	if filters.Status != nil {
		baseQuery += fmt.Sprintf(" AND status = $%d", argCount)
		countQuery += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, *filters.Status)
		argCount++
	}

	if filters.StartDate != nil {
		baseQuery += fmt.Sprintf(" AND created_at >= $%d", argCount)
		countQuery += fmt.Sprintf(" AND created_at >= $%d", argCount)
		args = append(args, *filters.StartDate)
		argCount++
	}

	if filters.EndDate != nil {
		baseQuery += fmt.Sprintf(" AND created_at <= $%d", argCount)
		countQuery += fmt.Sprintf(" AND created_at <= $%d", argCount)
		args = append(args, *filters.EndDate)
		argCount++
	}

	baseQuery += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argCount, argCount+1)
	args = append(args, limit, offset)

	var total int
	err := r.db.QueryRow(ctx, countQuery, args[:len(args)-2]...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	rows, err := r.db.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get filtered rides: %w", err)
	}
	defer rows.Close()

	rides := make([]*models.Ride, 0)
	for rows.Next() {
		ride := &models.Ride{}
		err := rows.Scan(
			&ride.ID, &ride.RiderID, &ride.DriverID, &ride.Status,
			&ride.PickupLatitude, &ride.PickupLongitude, &ride.PickupAddress,
			&ride.DropoffLatitude, &ride.DropoffLongitude, &ride.DropoffAddress,
			&ride.EstimatedDistance, &ride.EstimatedDuration, &ride.EstimatedFare,
			&ride.ActualDistance, &ride.ActualDuration, &ride.FinalFare,
			&ride.SurgeMultiplier, &ride.RequestedAt, &ride.AcceptedAt,
			&ride.StartedAt, &ride.CompletedAt, &ride.CancelledAt, &ride.CancellationReason,
			&ride.Rating, &ride.Feedback, &ride.CreatedAt, &ride.UpdatedAt,
			&ride.RideTypeID, &ride.PromoCodeID, &ride.DiscountAmount,
			&ride.ScheduledAt, &ride.IsScheduled, &ride.ScheduledNotificationSent,
			&ride.CountryID, &ride.RegionID, &ride.CityID,
			&ride.PickupZoneID, &ride.DropoffZoneID, &ride.CurrencyCode,
			&ride.PricingVersionID, &ride.WasNegotiated, &ride.NegotiationSessionID,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan ride: %w", err)
		}
		rides = append(rides, ride)
	}

	return rides, total, nil
}

// GetUserProfile retrieves a user's profile information
func (r *Repository) GetUserProfile(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	query := `
		SELECT id, email, phone_number, first_name, last_name, role,
			   is_active, is_verified, profile_image, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	user := &models.User{}
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&user.ID,
		&user.Email,
		&user.PhoneNumber,
		&user.FirstName,
		&user.LastName,
		&user.Role,
		&user.IsActive,
		&user.IsVerified,
		&user.ProfileImage,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}

	return user, nil
}

// UpdateUserProfile updates a user's profile information
func (r *Repository) UpdateUserProfile(ctx context.Context, userID uuid.UUID, firstName, lastName, phoneNumber string) error {
	query := `
		UPDATE users
		SET first_name = $1, last_name = $2, phone_number = $3, updated_at = NOW()
		WHERE id = $4
	`

	result, err := r.db.Exec(ctx, query, firstName, lastName, phoneNumber, userID)
	if err != nil {
		return fmt.Errorf("failed to update user profile: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// DriverMatchStats holds aggregated stats used by the matching algorithm.
type DriverMatchStats struct {
	DriverID       uuid.UUID
	Rating         float64
	AcceptanceRate float64
	IdleMinutes    float64
}

// GetDriverMatchStats returns acceptance rate and idle time for a list of driver IDs.
func (r *Repository) GetDriverMatchStats(ctx context.Context, driverIDs []uuid.UUID) (map[uuid.UUID]*DriverMatchStats, error) {
	if len(driverIDs) == 0 {
		return make(map[uuid.UUID]*DriverMatchStats), nil
	}

	query := `
		SELECT
			u.id,
			COALESCE(u.rating, 4.0) AS rating,
			COALESCE(
				CAST(SUM(CASE WHEN r.status IN ('accepted','started','completed') THEN 1 ELSE 0 END) AS FLOAT) /
				NULLIF(COUNT(r.id), 0),
				0.8
			) AS acceptance_rate,
			COALESCE(
				EXTRACT(EPOCH FROM (NOW() - MAX(r.completed_at))) / 60.0,
				30.0
			) AS idle_minutes
		FROM users u
		LEFT JOIN rides r ON r.driver_id = u.id AND r.created_at > NOW() - INTERVAL '30 days'
		WHERE u.id = ANY($1)
		GROUP BY u.id, u.rating
	`

	rows, err := r.db.Query(ctx, query, driverIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get driver match stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[uuid.UUID]*DriverMatchStats, len(driverIDs))
	for rows.Next() {
		s := &DriverMatchStats{}
		if err := rows.Scan(&s.DriverID, &s.Rating, &s.AcceptanceRate, &s.IdleMinutes); err != nil {
			return nil, fmt.Errorf("failed to scan driver stats: %w", err)
		}
		stats[s.DriverID] = s
	}

	// Fill in defaults for drivers not in DB (new drivers)
	for _, id := range driverIDs {
		if _, ok := stats[id]; !ok {
			stats[id] = &DriverMatchStats{
				DriverID:       id,
				Rating:         4.0,
				AcceptanceRate: 0.8,
				IdleMinutes:    30.0,
			}
		}
	}

	return stats, nil
}

// GetPaymentByRideID retrieves payment information for a ride
func (r *Repository) GetPaymentByRideID(ctx context.Context, rideID uuid.UUID) (string, error) {
	query := `SELECT method FROM payments WHERE ride_id = $1 LIMIT 1`

	var method string
	err := r.db.QueryRow(ctx, query, rideID).Scan(&method)
	if err != nil {
		return "", fmt.Errorf("failed to get payment method: %w", err)
	}

	return method, nil
}
