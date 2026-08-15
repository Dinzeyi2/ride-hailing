package onboarding

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/richxcame/ride-hailing/pkg/common"
	"github.com/richxcame/ride-hailing/pkg/jwtkeys"
	"github.com/richxcame/ride-hailing/pkg/middleware"
	"github.com/richxcame/ride-hailing/pkg/models"
)

// Handler handles HTTP requests for onboarding
type Handler struct {
	service *Service
}

func (h *Handler) StartBackgroundCheck(c *gin.Context) {
	driverID, err := h.getDriverID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "not a registered driver")
		return
	}
	check, err := h.service.StartBackgroundCheck(c.Request.Context(), driverID)
	if err != nil {
		common.ErrorResponse(c, http.StatusBadGateway, err.Error())
		return
	}
	common.CreatedResponse(c, check)
}

func (h *Handler) BackgroundWebhook(c *gin.Context) {
	payload, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid body")
		return
	}
	processed, err := h.service.HandleBackgroundWebhook(c.Request.Context(), payload, c.GetHeader("X-Background-Signature"))
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, err.Error())
		return
	}
	common.SuccessResponse(c, gin.H{"processed": processed})
}

// NewHandler creates a new onboarding handler
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetProgress gets the current onboarding progress
// GET /api/v1/driver/onboarding/progress
func (h *Handler) GetProgress(c *gin.Context) {
	driverID, err := h.getDriverID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "not a registered driver")
		return
	}

	progress, err := h.service.GetOnboardingProgress(c.Request.Context(), driverID)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.AppErrorResponse(c, appErr)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get onboarding progress")
		return
	}

	common.SuccessResponse(c, progress)
}

// StartOnboarding starts the onboarding process
// POST /api/v1/driver/onboarding/start
func (h *Handler) StartOnboarding(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	progress, err := h.service.StartOnboarding(c.Request.Context(), userID)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.AppErrorResponse(c, appErr)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to start onboarding")
		return
	}

	common.SuccessResponse(c, progress)
}

// GetDocumentRequirements gets the document requirements
// GET /api/v1/driver/onboarding/documents
func (h *Handler) GetDocumentRequirements(c *gin.Context) {
	driverID, err := h.getDriverID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "not a registered driver")
		return
	}

	requirements, err := h.service.GetDocumentRequirements(c.Request.Context(), driverID)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.AppErrorResponse(c, appErr)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get document requirements")
		return
	}

	common.SuccessResponse(c, gin.H{
		"requirements": requirements,
	})
}

// GetOnboardingStats gets onboarding statistics (admin)
// GET /api/v1/admin/onboarding/stats
func (h *Handler) GetOnboardingStats(c *gin.Context) {
	stats, err := h.service.repo.GetOnboardingStats(c.Request.Context())
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to get onboarding stats")
		return
	}

	common.SuccessResponse(c, stats)
}
func (h *Handler) ApproveDriver(c *gin.Context) {
	actor, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	driverID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid driver ID")
		return
	}
	if err := h.service.ApproveDriver(c.Request.Context(), driverID, actor); err != nil {
		if app, ok := err.(*common.AppError); ok {
			common.AppErrorResponse(c, app)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to approve driver")
		return
	}
	common.SuccessResponse(c, gin.H{"approved": true})
}
func (h *Handler) RejectDriver(c *gin.Context) {
	actor, err := middleware.GetUserID(c)
	if err != nil {
		common.ErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	driverID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "invalid driver ID")
		return
	}
	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResponse(c, http.StatusBadRequest, "reason is required")
		return
	}
	if err := h.service.RejectDriver(c.Request.Context(), driverID, actor, req.Reason); err != nil {
		if app, ok := err.(*common.AppError); ok {
			common.AppErrorResponse(c, app)
			return
		}
		common.ErrorResponse(c, http.StatusInternalServerError, "failed to reject driver")
		return
	}
	common.SuccessResponse(c, gin.H{"rejected": true})
}

// getDriverID gets the driver ID from the authenticated user
func (h *Handler) getDriverID(c *gin.Context) (uuid.UUID, error) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return uuid.Nil, err
	}

	driver, err := h.service.repo.GetDriverByUserID(c.Request.Context(), userID)
	if err != nil {
		return uuid.Nil, err
	}

	return driver.ID, nil
}

// RegisterRoutes registers onboarding routes
func (h *Handler) RegisterRoutes(r *gin.Engine, jwtProvider jwtkeys.KeyProvider) {
	// Driver onboarding routes
	onboarding := r.Group("/api/v1/driver/onboarding")
	onboarding.Use(middleware.AuthMiddlewareWithProvider(jwtProvider))
	{
		onboarding.POST("/start", h.StartOnboarding)
		onboarding.GET("/progress", h.GetProgress)
		onboarding.GET("/documents", h.GetDocumentRequirements)
		onboarding.POST("/background-check", h.StartBackgroundCheck)
	}
	r.POST("/api/v1/webhooks/background-check", h.BackgroundWebhook)

	// Admin routes
	adminOnboarding := r.Group("/api/v1/admin/onboarding")
	adminOnboarding.Use(middleware.AuthMiddlewareWithProvider(jwtProvider))
	adminOnboarding.Use(middleware.RequireRole(models.RoleAdmin))
	{
		adminOnboarding.GET("/stats", h.GetOnboardingStats)
		adminOnboarding.POST("/drivers/:id/approve", h.ApproveDriver)
		adminOnboarding.POST("/drivers/:id/reject", h.RejectDriver)
	}
}

// RegisterRoutesOnGroup registers onboarding routes on an existing router group
func (h *Handler) RegisterRoutesOnGroup(rg *gin.RouterGroup) {
	onboarding := rg.Group("/driver/onboarding")
	{
		onboarding.POST("/start", h.StartOnboarding)
		onboarding.GET("/progress", h.GetProgress)
		onboarding.GET("/documents", h.GetDocumentRequirements)
	}
}
