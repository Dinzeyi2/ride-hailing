package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/richxcame/ride-hailing/internal/documents"
	"github.com/richxcame/ride-hailing/internal/onboarding"
	"github.com/richxcame/ride-hailing/internal/pool"
	"github.com/richxcame/ride-hailing/internal/pricing"
	"github.com/richxcame/ride-hailing/internal/ridetypes"
	"github.com/richxcame/ride-hailing/internal/scheduling"
	"github.com/richxcame/ride-hailing/pkg/logger"
	"github.com/richxcame/ride-hailing/pkg/models"
	"github.com/richxcame/ride-hailing/pkg/storage"
	"go.uber.org/zap"
)

// ---- Pool MapsService stub ----

type stubMapsService struct{}

func (s *stubMapsService) GetRoute(_ context.Context, origin, destination pool.Location) (*pool.RouteInfo, error) {
	logger.Warn("stubMapsService.GetRoute called — wire a real MapsService",
		zap.Float64("origin_latitude", origin.Latitude), zap.Float64("origin_longitude", origin.Longitude))
	return &pool.RouteInfo{DistanceKm: 0, DurationMinutes: 0}, nil
}

func (s *stubMapsService) GetMultiStopRoute(_ context.Context, stops []pool.Location) (*pool.MultiStopRouteInfo, error) {
	logger.Warn("stubMapsService.GetMultiStopRoute called — wire a real MapsService",
		zap.Int("stops", len(stops)))
	return &pool.MultiStopRouteInfo{TotalDistanceKm: 0, TotalDurationMinutes: 0}, nil
}

// ---- Recording Storage stub ----

type stubStorage struct{}

func (s *stubStorage) Upload(_ context.Context, key string, _ io.Reader, _ int64, _ string) (*storage.UploadResult, error) {
	logger.Warn("stubStorage.Upload called — wire a real Storage provider", zap.String("key", key))
	return nil, fmt.Errorf("storage not configured")
}

func (s *stubStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	logger.Warn("stubStorage.Download called — wire a real Storage provider", zap.String("key", key))
	return nil, fmt.Errorf("storage not configured")
}

func (s *stubStorage) Delete(_ context.Context, key string) error {
	logger.Warn("stubStorage.Delete called — wire a real Storage provider", zap.String("key", key))
	return fmt.Errorf("storage not configured")
}

func (s *stubStorage) GetURL(key string) string { return "" }

func (s *stubStorage) GetPresignedUploadURL(_ context.Context, key, _ string, _ time.Duration) (*storage.PresignedURLResult, error) {
	return nil, fmt.Errorf("storage not configured")
}

func (s *stubStorage) GetPresignedDownloadURL(_ context.Context, key string, _ time.Duration) (*storage.PresignedURLResult, error) {
	return nil, fmt.Errorf("storage not configured")
}

func (s *stubStorage) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (s *stubStorage) Copy(_ context.Context, _, _ string) error {
	return fmt.Errorf("storage not configured")
}

type onboardingDocumentAdapter struct{ repo *documents.Repository }

func (a *onboardingDocumentAdapter) GetDriverDocuments(ctx context.Context, driverID uuid.UUID) ([]onboarding.DocumentInfo, error) {
	docs, err := a.repo.GetDriverDocuments(ctx, driverID)
	if err != nil {
		return nil, err
	}
	out := make([]onboarding.DocumentInfo, 0, len(docs))
	for _, d := range docs {
		out = append(out, onboarding.DocumentInfo{ID: d.ID, DocumentTypeID: d.DocumentTypeID, Status: string(d.Status), SubmittedAt: d.SubmittedAt, ReviewedAt: d.ReviewedAt, ExpiryDate: d.ExpiryDate, RejectionReason: d.RejectionReason})
	}
	return out, nil
}
func (a *onboardingDocumentAdapter) GetRequiredDocumentTypes(ctx context.Context) ([]onboarding.DocumentTypeInfo, error) {
	types, err := a.repo.GetRequiredDocumentTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]onboarding.DocumentTypeInfo, 0, len(types))
	for _, t := range types {
		out = append(out, onboarding.DocumentTypeInfo{ID: t.ID, Code: t.Code, Name: t.Name, IsRequired: t.IsRequired})
	}
	return out, nil
}
func (a *onboardingDocumentAdapter) GetDriverVerificationStatus(ctx context.Context, driverID uuid.UUID) (*onboarding.VerificationStatus, error) {
	status, err := a.repo.GetDriverVerificationStatus(ctx, driverID)
	if err != nil {
		return nil, err
	}
	return &onboarding.VerificationStatus{Status: string(status.VerificationStatus), CanDrive: status.VerificationStatus == documents.VerificationApproved, MissingDocuments: status.RequiredDocumentsCount - status.SubmittedDocumentsCount, PendingDocuments: status.SubmittedDocumentsCount - status.ApprovedDocumentsCount, ApprovedDocuments: status.ApprovedDocumentsCount}, nil
}

// ---- PaymentSplit PaymentService stub ----

type stubPaymentService struct{}

func (s *stubPaymentService) GetRideFare(_ context.Context, rideID uuid.UUID) (float64, string, error) {
	logger.Warn("stubPaymentService.GetRideFare called — wire a real PaymentService",
		zap.String("ride_id", rideID.String()))
	return 0, "USD", fmt.Errorf("payment service not configured")
}

func (s *stubPaymentService) ProcessSplitPayment(_ context.Context, userID, rideID uuid.UUID, amount float64, method string) (uuid.UUID, error) {
	logger.Warn("stubPaymentService.ProcessSplitPayment called — wire a real PaymentService",
		zap.String("user_id", userID.String()), zap.Float64("amount", amount))
	return uuid.Nil, fmt.Errorf("payment service not configured")
}

// ---- PaymentSplit NotificationService stub ----

type stubSplitNotificationService struct{}

func (s *stubSplitNotificationService) SendSplitInvitation(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ float64) error {
	logger.Warn("stubSplitNotificationService.SendSplitInvitation called — wire a real NotificationService")
	return nil
}

func (s *stubSplitNotificationService) SendSplitReminder(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ float64) error {
	return nil
}

func (s *stubSplitNotificationService) SendSplitAccepted(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func (s *stubSplitNotificationService) SendSplitCompleted(_ context.Context, _ uuid.UUID) error {
	return nil
}

// ---- Subscriptions PaymentProcessor stub ----

type stubPaymentProcessor struct{}

func (s *stubPaymentProcessor) ChargeSubscription(_ context.Context, userID uuid.UUID, amount float64, currency, paymentMethod string) error {
	logger.Warn("stubPaymentProcessor.ChargeSubscription called — wire a real PaymentProcessor",
		zap.String("user_id", userID.String()), zap.Float64("amount", amount))
	return fmt.Errorf("payment processor not configured")
}

// ---- 2FA SMSSender stub ----

type stubSMSSender struct{}

func (s *stubSMSSender) SendOTP(to, otp string) (string, error) {
	logger.Warn("stubSMSSender.SendOTP called — wire a real SMSSender",
		zap.String("to", to[:3]+"***"))
	return "", fmt.Errorf("SMS sender not configured")
}

// ---- DemandForecast stubs (already nil-safe, but let's be explicit) ----
// demandforecast service nil-guards both weatherSvc and driverSvc, so nil is safe.
// No stubs needed.

type databaseDriverService struct{ db *pgxpool.Pool }

func (s *databaseDriverService) GetDriverByUserID(ctx context.Context, userID uuid.UUID) (*models.Driver, error) {
	d := &models.Driver{}
	err := s.db.QueryRow(ctx, `SELECT id,user_id,license_number,vehicle_model,vehicle_plate,vehicle_color,vehicle_year,is_available,is_online,COALESCE(approval_status,'pending'),approved_by,approved_at,rejection_reason,rejected_at,rating,total_rides,current_latitude,current_longitude,last_location_update,created_at,updated_at FROM drivers WHERE user_id=$1`, userID).Scan(&d.ID, &d.UserID, &d.LicenseNumber, &d.VehicleModel, &d.VehiclePlate, &d.VehicleColor, &d.VehicleYear, &d.IsAvailable, &d.IsOnline, &d.ApprovalStatus, &d.ApprovedBy, &d.ApprovedAt, &d.RejectionReason, &d.RejectedAt, &d.Rating, &d.TotalRides, &d.CurrentLatitude, &d.CurrentLongitude, &d.LastLocationUpdate, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

// ---- Scheduling PricingService adapter ----
// Adapts pricing.Service.GetEstimate to the scheduling.PricingService interface.

type schedulingPricingAdapter struct {
	svc *pricing.Service
}

func (a *schedulingPricingAdapter) EstimateFare(ctx context.Context, pickup, dropoff scheduling.Location, rideType string) (float64, error) {
	resp, err := a.svc.GetEstimate(ctx, pricing.EstimateRequest{
		PickupLatitude:   pickup.Latitude,
		PickupLongitude:  pickup.Longitude,
		DropoffLatitude:  dropoff.Latitude,
		DropoffLongitude: dropoff.Longitude,
	})
	if err != nil {
		return 0, err
	}
	return resp.EstimatedFare, nil
}

// ---- RideTypes Service Adapter (for pricing bulk estimates) ----

type rideTypesServiceAdapter struct {
	service interface {
		GetAvailableRideTypes(ctx context.Context, latitude, longitude float64) ([]*ridetypes.RideType, error)
	}
}

func (a *rideTypesServiceAdapter) GetAvailableRideTypes(ctx interface{}, latitude, longitude float64) ([]interface{}, error) {
	ctxTyped, ok := ctx.(context.Context)
	if !ok {
		return nil, fmt.Errorf("invalid context type")
	}

	rideTypes, err := a.service.GetAvailableRideTypes(ctxTyped, latitude, longitude)
	if err != nil {
		return nil, err
	}

	// Convert []*ridetypes.RideType to []interface{}
	result := make([]interface{}, len(rideTypes))
	for i, rt := range rideTypes {
		rtMap := map[string]interface{}{
			"id":       rt.ID.String(),
			"name":     rt.Name,
			"capacity": rt.Capacity,
		}
		if rt.Description != nil {
			rtMap["description"] = *rt.Description
		} else {
			rtMap["description"] = ""
		}
		if rt.Icon != nil {
			rtMap["icon_url"] = *rt.Icon
		}
		result[i] = rtMap
	}

	return result, nil
}
