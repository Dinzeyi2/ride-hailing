package payments

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/richxcame/ride-hailing/pkg/common"
	"github.com/richxcame/ride-hailing/pkg/jwtkeys"
	"github.com/richxcame/ride-hailing/pkg/logger"
	"github.com/richxcame/ride-hailing/pkg/middleware"
	"github.com/richxcame/ride-hailing/pkg/models"
	"github.com/richxcame/ride-hailing/pkg/pagination"
	"github.com/stripe/stripe-go/v83/webhook"
	"go.uber.org/zap"
)

type Handler struct {
	service       *Service
	adminRepo     AdminRepositoryInterface
	webhookSecret string
	connect       *ConnectService
}

func (h *Handler) SetConnectService(connect *ConnectService) { h.connect = connect }

func NewHandler(service *Service) *Handler {
	// Try to use the service repo as admin repo if it supports admin methods
	var adminRepo AdminRepositoryInterface
	if ar, ok := service.repo.(AdminRepositoryInterface); ok {
		adminRepo = ar
	}
	return &Handler{service: service, adminRepo: adminRepo}
}

// NewHandlerWithWebhookSecret creates a new handler with Stripe webhook secret for signature verification
func NewHandlerWithWebhookSecret(service *Service, webhookSecret string) *Handler {
	handler := NewHandler(service)
	handler.webhookSecret = webhookSecret
	return handler
}

// RegisterRoutes registers payment routes
func (h *Handler) RegisterRoutes(router *gin.Engine, jwtProvider jwtkeys.KeyProvider) {
	api := router.Group("/api/v1")

	// Protected routes
	protected := api.Group("")
	protected.Use(middleware.AuthMiddlewareWithProvider(jwtProvider))
	{
		// Wallet routes
		protected.GET("/wallet", h.GetWallet)
		protected.POST("/wallet/topup", h.TopUpWallet)
		protected.GET("/wallet/transactions", h.GetWalletTransactions)

		// Payment routes
		protected.POST("/payments/process", h.ProcessPayment)
		protected.POST("/payments/:id/refund", h.RefundPayment)
		protected.GET("/payments/:id", h.GetPayment)

		// Driver payout routes
		protected.GET("/driver/payouts/summary", h.GetPayoutSummary)
		protected.POST("/driver/payouts/withdraw", h.RequestWithdrawal)
		protected.GET("/driver/payouts/history", middleware.RequireRole(models.RoleDriver), h.GetPayoutHistory)
		protected.POST("/driver/connect/account", middleware.RequireRole(models.RoleDriver), h.CreateConnectAccount)
		protected.POST("/driver/connect/onboarding-link", middleware.RequireRole(models.RoleDriver), h.CreateConnectOnboardingLink)
		protected.GET("/driver/connect/status", middleware.RequireRole(models.RoleDriver), h.GetConnectStatus)
		protected.GET("/driver/rewards/progress", middleware.RequireRole(models.RoleDriver), h.GetDriverRewardProgress)
		protected.GET("/driver/rewards/notifications", middleware.RequireRole(models.RoleDriver), h.GetDriverRewardNotifications)
		protected.POST("/driver/rewards/notifications/:id/read", middleware.RequireRole(models.RoleDriver), h.MarkDriverRewardNotificationRead)
	}
	admin := api.Group("/admin")
	admin.Use(middleware.AuthMiddlewareWithProvider(jwtProvider), middleware.RequireRole(models.RoleAdmin))
	admin.POST("/payouts/reconcile", h.ReconcilePayouts)
	admin.GET("/payouts", h.AdminPayoutHistory)
	admin.POST("/payouts/:id/retry", h.AdminRetryPayout)

	// Webhook routes (no auth)
	api.POST("/webhooks/stripe", h.HandleStripeWebhook)
}

func (h *Handler) CreateConnectAccount(c *gin.Context) {
	if h.connect == nil {
		common.ErrorResponse(c, http.StatusServiceUnavailable, "Stripe Connect is not configured")
		return
	}
	driverID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Country string `json:"country" binding:"required,len=2"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "country must be a two-letter code")
		return
	}
	status, err := h.connect.EnsureAccount(c.Request.Context(), driverID, req.Country)
	if err != nil {
		common.ErrorResponse(c, http.StatusBadGateway, err.Error())
		return
	}
	common.SuccessResponse(c, status)
}

func (h *Handler) CreateConnectOnboardingLink(c *gin.Context) {
	if h.connect == nil {
		common.ErrorResponse(c, http.StatusServiceUnavailable, "Stripe Connect is not configured")
		return
	}
	driverID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	url, err := h.connect.CreateOnboardingLink(c.Request.Context(), driverID)
	if err != nil {
		common.ErrorResponse(c, http.StatusBadGateway, err.Error())
		return
	}
	common.SuccessResponse(c, gin.H{"url": url})
}

func (h *Handler) GetConnectStatus(c *gin.Context) {
	if h.connect == nil {
		common.ErrorResponse(c, http.StatusServiceUnavailable, "Stripe Connect is not configured")
		return
	}
	driverID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	status, err := h.connect.Status(c.Request.Context(), driverID)
	if err != nil {
		common.ErrorResponse(c, http.StatusNotFound, "Stripe Connect account not found")
		return
	}
	common.SuccessResponse(c, status)
}

func (h *Handler) GetPayoutHistory(c *gin.Context) {
	if h.connect == nil {
		common.ErrorResponse(c, http.StatusServiceUnavailable, "Stripe Connect is not configured")
		return
	}
	driverID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.connect.PayoutHistory(c.Request.Context(), driverID, 50)
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get payout history")
		return
	}
	common.SuccessResponse(c, items)
}
func (h *Handler) ReconcilePayouts(c *gin.Context) {
	if h.connect == nil {
		common.ErrorResponse(c, http.StatusServiceUnavailable, "Stripe Connect is not configured")
		return
	}
	count, err := h.connect.ReconcilePayouts(c.Request.Context())
	if err != nil {
		common.ErrorResponse(c, http.StatusBadGateway, "payout reconciliation failed")
		return
	}
	common.SuccessResponse(c, gin.H{"reconciled": count})
}
func (h *Handler) AdminPayoutHistory(c *gin.Context) {
	items, err := h.connect.AdminPayoutHistory(c.Request.Context(), 100)
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to list payouts")
		return
	}
	common.SuccessResponse(c, items)
}
func (h *Handler) AdminRetryPayout(c *gin.Context) {
	actor, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid payout ID")
		return
	}
	payout, err := h.connect.RetryPayout(c.Request.Context(), actor, id)
	if err != nil {
		common.ErrorResponse(c, http.StatusConflict, err.Error())
		return
	}
	common.SuccessResponse(c, payout)
}

// ProcessPaymentRequest represents a payment request
type ProcessPaymentRequest struct {
	RideID        string  `json:"ride_id" binding:"required"`
	Amount        float64 `json:"amount" binding:"required,gt=0"` // Compatibility hint only; server replaces it with finalized pricing.
	PaymentMethod string  `json:"payment_method" binding:"required,oneof=wallet stripe"`
}

// GetDriverRewardProgress returns the current weekly Fare Keep Rate tracker.
func (h *Handler) GetDriverRewardProgress(c *gin.Context) {
	driverID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	progress, err := h.service.GetDriverRewardProgress(c.Request.Context(), driverID)
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get reward progress")
		return
	}
	common.SuccessResponse(c, progress)
}

func (h *Handler) GetDriverRewardNotifications(c *gin.Context) {
	driverID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	notifications, err := h.service.GetDriverRewardNotifications(c.Request.Context(), driverID, 20)
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get reward notifications")
		return
	}
	common.SuccessResponse(c, notifications)
}

func (h *Handler) MarkDriverRewardNotificationRead(c *gin.Context) {
	driverID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	notificationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid notification ID")
		return
	}
	if err := h.service.MarkDriverRewardNotificationRead(c.Request.Context(), driverID, notificationID); err != nil {
		common.ErrorResponse(c, http.StatusNotFound, "reward notification not found")
		return
	}
	common.SuccessResponse(c, gin.H{"is_read": true})
}

// TopUpWalletRequest represents a wallet top-up request
type TopUpWalletRequest struct {
	Amount              float64 `json:"amount" binding:"required,gt=0"`
	StripePaymentMethod string  `json:"stripe_payment_method"`
}

// RefundRequest represents a refund request
type RefundRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// ProcessPayment processes a payment for a ride
func (h *Handler) ProcessPayment(c *gin.Context) {
	userUUID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req ProcessPaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	rideID, err := uuid.Parse(req.RideID)
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid ride ID")
		return
	}

	// Fetch the driver ID from the ride
	driverIDPtr, err := h.service.GetRideDriverID(c.Request.Context(), rideID)
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "ride not found or has no driver assigned")
		return
	}
	if driverIDPtr == nil {
		common.ErrorResponse(c, http.StatusBadRequest, "ride has no driver assigned yet")
		return
	}

	payment, err := h.service.ProcessRidePayment(
		c.Request.Context(),
		rideID,
		userUUID,
		*driverIDPtr,
		req.Amount,
		req.PaymentMethod,
	)

	if err != nil {
		appErr, ok := err.(*common.AppError)
		if ok {
			common.ErrorResponse(c, appErr.Code, appErr.Message)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to process payment")
		return
	}

	common.SuccessResponseWithStatus(c, http.StatusOK, payment, "Payment processed successfully")
}

// TopUpWallet adds funds to user's wallet
func (h *Handler) TopUpWallet(c *gin.Context) {
	userUUID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req TopUpWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	transaction, err := h.service.TopUpWallet(
		c.Request.Context(),
		userUUID,
		req.Amount,
		req.StripePaymentMethod,
	)

	if err != nil {
		appErr, ok := err.(*common.AppError)
		if ok {
			common.ErrorResponse(c, appErr.Code, appErr.Message)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to top up wallet")
		return
	}

	common.SuccessResponseWithStatus(c, http.StatusOK, transaction, "Wallet top-up initiated")
}

// GetWallet retrieves user's wallet information
func (h *Handler) GetWallet(c *gin.Context) {
	userUUID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	wallet, err := h.service.GetWallet(c.Request.Context(), userUUID)
	if err != nil {
		appErr, ok := err.(*common.AppError)
		if ok {
			common.ErrorResponse(c, appErr.Code, appErr.Message)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get wallet")
		return
	}

	common.SuccessResponseWithStatus(c, http.StatusOK, wallet, "")
}

// GetWalletTransactions retrieves wallet transaction history
func (h *Handler) GetWalletTransactions(c *gin.Context) {
	userUUID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	params := pagination.ParseParams(c)

	transactions, total, err := h.service.GetWalletTransactions(
		c.Request.Context(),
		userUUID,
		params.Limit,
		params.Offset,
	)

	if err != nil {
		appErr, ok := err.(*common.AppError)
		if ok {
			common.ErrorResponse(c, appErr.Code, appErr.Message)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get transactions")
		return
	}

	meta := pagination.BuildMeta(params.Limit, params.Offset, total)
	common.SuccessResponseWithMeta(c, transactions, meta)
}

// GetPayment retrieves payment details
func (h *Handler) GetPayment(c *gin.Context) {
	userUUID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	paymentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid payment ID")
		return
	}

	payment, err := h.service.repo.GetPaymentByID(c.Request.Context(), paymentID)
	if err != nil {
		appErr, ok := err.(*common.AppError)
		if ok {
			common.ErrorResponse(c, appErr.Code, appErr.Message)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get payment")
		return
	}

	// Verify user owns this payment
	if payment.RiderID != userUUID && payment.DriverID != userUUID {
		common.ErrorResponse(c, http.StatusForbidden, "access denied")
		return
	}

	common.SuccessResponseWithStatus(c, http.StatusOK, payment, "")
}

// RefundPayment processes a refund
func (h *Handler) RefundPayment(c *gin.Context) {
	userUUID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	role, err := middleware.GetUserRole(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Only admins and riders can request refunds
	if role != models.RoleAdmin && role != models.RoleRider {
		common.ErrorResponse(c, http.StatusForbidden, "access denied")
		return
	}

	paymentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid payment ID")
		return
	}

	var req RefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	// Verify user owns this payment (if not admin)
	if role != models.RoleAdmin {
		payment, err := h.service.repo.GetPaymentByID(c.Request.Context(), paymentID)
		if err != nil {
			common.ErrorResponse(c, http.StatusNotFound, "payment not found")
			return
		}
		if payment.RiderID != userUUID {
			common.ErrorResponse(c, http.StatusForbidden, "access denied")
			return
		}
	}

	err = h.service.ProcessRefund(c.Request.Context(), paymentID, req.Reason)
	if err != nil {
		appErr, ok := err.(*common.AppError)
		if ok {
			common.ErrorResponse(c, appErr.Code, appErr.Message)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to process refund")
		return
	}

	common.SuccessResponseWithStatus(c, http.StatusOK, nil, "Refund processed successfully")
}

// HandleStripeWebhook handles Stripe webhook events with signature verification
func (h *Handler) HandleStripeWebhook(c *gin.Context) {
	// Read the raw body for signature verification
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Get().Error("Failed to read webhook body", zap.Error(err))
		common.ErrorResponse(c, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Verify webhook signature if secret is configured
	var eventType string
	var paymentIntentID string

	if h.webhookSecret != "" {
		sig := c.GetHeader("Stripe-Signature")
		if sig == "" {
			logger.Get().Warn("Missing Stripe-Signature header")
			common.ErrorResponse(c, http.StatusUnauthorized, "missing signature header")
			return
		}

		event, err := webhook.ConstructEvent(payload, sig, h.webhookSecret)
		if err != nil {
			logger.Get().Warn("Invalid webhook signature", zap.Error(err))
			common.ErrorResponse(c, http.StatusUnauthorized, "invalid webhook signature")
			return
		}

		eventType = string(event.Type)

		// Extract payment intent ID from the verified event
		if event.Data != nil && event.Data.Object != nil {
			if id, ok := event.Data.Object["id"].(string); ok {
				paymentIntentID = id
			}
		}

		logger.Get().Info("Verified Stripe webhook",
			zap.String("event_type", eventType),
			zap.String("event_id", event.ID),
		)
	} else {
		// Fallback for testing/development without signature verification
		// WARNING: This should not be used in production
		logger.Get().Warn("Webhook signature verification disabled - not recommended for production")

		var event struct {
			Type string                 `json:"type"`
			Data map[string]interface{} `json:"data"`
		}

		if err := json.Unmarshal(payload, &event); err != nil {
			common.ErrorResponse(c, http.StatusBadRequest, "invalid webhook payload")
			return
		}

		eventType = event.Type

		// Extract payment intent ID
		if obj, ok := event.Data["object"].(map[string]interface{}); ok {
			if id, ok := obj["id"].(string); ok {
				paymentIntentID = id
			}
		}
	}

	if eventType == "account.updated" && h.connect != nil {
		err = h.connect.RefreshByAccountID(c.Request.Context(), paymentIntentID)
	} else if (eventType == "transfer.created" || eventType == "transfer.failed" || eventType == "transfer.reversed") && h.connect != nil {
		err = h.connect.MarkTransferEvent(c.Request.Context(), paymentIntentID, eventType)
	} else {
		err = h.service.HandleStripeWebhook(c.Request.Context(), eventType, paymentIntentID)
	}
	if err != nil {
		logger.Get().Error("Failed to handle webhook event",
			zap.String("event_type", eventType),
			zap.Error(err),
		)
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to handle webhook")
		return
	}

	common.SuccessResponse(c, gin.H{"received": true})
}

// ========================================
// ADMIN ENDPOINTS
// ========================================

// AdminGetAllPayments returns all payments with filtering (admin only)
// GET /api/v1/admin/payments?status=completed&payment_method=wallet&rider_id=...&driver_id=...
func (h *Handler) AdminGetAllPayments(c *gin.Context) {
	params := pagination.ParseParams(c)

	filter := &AdminPaymentFilter{}
	if status := c.Query("status"); status != "" {
		filter.Status = status
	}
	if method := c.Query("payment_method"); method != "" {
		filter.PaymentMethod = method
	}
	if riderIDStr := c.Query("rider_id"); riderIDStr != "" {
		if id, err := uuid.Parse(riderIDStr); err == nil {
			filter.RiderID = &id
		}
	}
	if driverIDStr := c.Query("driver_id"); driverIDStr != "" {
		if id, err := uuid.Parse(driverIDStr); err == nil {
			filter.DriverID = &id
		}
	}

	if h.adminRepo == nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "admin operations not available")
		return
	}

	payments, total, err := h.adminRepo.GetAllPayments(c.Request.Context(), params.Limit, params.Offset, filter)
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get payments")
		return
	}

	meta := pagination.BuildMeta(params.Limit, params.Offset, total)
	common.SuccessResponseWithMeta(c, payments, meta)
}

// AdminGetPayment retrieves a single payment by ID (admin only)
// GET /api/v1/admin/payments/:id
func (h *Handler) AdminGetPayment(c *gin.Context) {
	paymentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid payment ID")
		return
	}

	payment, err := h.service.repo.GetPaymentByID(c.Request.Context(), paymentID)
	if err != nil {
		common.ErrorResponse(c, http.StatusNotFound, "payment not found")
		return
	}

	common.SuccessResponse(c, payment)
}

// AdminRefundPayment processes a refund for any payment (admin only)
// POST /api/v1/admin/payments/:id/refund
func (h *Handler) AdminRefundPayment(c *gin.Context) {
	paymentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid payment ID")
		return
	}

	var req RefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	err = h.service.ProcessRefund(c.Request.Context(), paymentID, req.Reason)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.AppErrorResponse(c, appErr)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to process refund")
		return
	}

	common.SuccessResponseWithStatus(c, http.StatusOK, nil, "Refund processed successfully")
}

// AdminGetPaymentStats returns platform-wide payment statistics (admin only)
// GET /api/v1/admin/payments/stats
func (h *Handler) AdminGetPaymentStats(c *gin.Context) {
	if h.adminRepo == nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "admin operations not available")
		return
	}

	stats, err := h.adminRepo.GetPaymentStats(c.Request.Context())
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get payment stats")
		return
	}

	common.SuccessResponse(c, stats)
}

// GetPayoutSummary returns the payout summary for the authenticated driver.
// GET /api/v1/driver/payouts/summary
func (h *Handler) GetPayoutSummary(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	summary, err := h.service.GetDriverPayoutSummary(c.Request.Context(), userID)
	if err != nil {
		appErr, ok := err.(*common.AppError)
		if ok {
			common.ErrorResponse(c, appErr.Code, appErr.Message)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get payout summary")
		return
	}

	common.SuccessResponse(c, summary)
}

// WithdrawRequest represents a withdrawal request body.
type WithdrawRequest struct {
	Amount float64 `json:"amount" binding:"required,gt=0"`
}

// RequestWithdrawal initiates a withdrawal from the driver's wallet.
// POST /api/v1/driver/payouts/withdraw
func (h *Handler) RequestWithdrawal(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req WithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	if h.connect == nil {
		common.ErrorResponse(c, http.StatusServiceUnavailable, "Stripe Connect is not configured")
		return
	}
	payout, err := h.connect.RequestPayout(c.Request.Context(), userID, req.Amount, "usd")
	if err != nil {
		common.ErrorResponse(c, http.StatusBadGateway, err.Error())
		return
	}
	common.SuccessResponseWithStatus(c, http.StatusOK, payout, "Withdrawal sent to Stripe Connect")
}

// RegisterAdminRoutes registers only admin payment routes on an existing router group.
func (h *Handler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	payments := rg.Group("/payments")
	{
		payments.GET("", h.AdminGetAllPayments)
		payments.GET("/stats", h.AdminGetPaymentStats)
		payments.GET("/:id", h.AdminGetPayment)
		payments.POST("/:id/refund", h.AdminRefundPayment)
	}
}
