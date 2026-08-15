package documents

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/richxcame/ride-hailing/pkg/common"
	"github.com/richxcame/ride-hailing/pkg/logger"
	"github.com/richxcame/ride-hailing/pkg/storage"
	"go.uber.org/zap"
)

// Service handles document verification business logic
type Service struct {
	repo    RepositoryInterface
	storage storage.Storage
	config  ServiceConfig
}

// ServiceConfig holds service configuration
type ServiceConfig struct {
	MaxFileSizeMB    int
	AllowedMimeTypes []string
	OCREnabled       bool
	OCRProvider      string
}

// NewService creates a new documents service
func NewService(repo RepositoryInterface, storage storage.Storage, config ServiceConfig) *Service {
	if config.MaxFileSizeMB == 0 {
		config.MaxFileSizeMB = 10
	}
	if len(config.AllowedMimeTypes) == 0 {
		config.AllowedMimeTypes = []string{
			"image/jpeg", "image/png", "image/webp", "application/pdf",
		}
	}

	return &Service{
		repo:    repo,
		storage: storage,
		config:  config,
	}
}

// ========================================
// DOCUMENT TYPES
// ========================================

// GetDocumentTypes gets all available document types
func (s *Service) GetDocumentTypes(ctx context.Context) ([]*DocumentType, error) {
	return s.repo.GetDocumentTypes(ctx)
}

// GetRequiredDocumentTypes gets required document types
func (s *Service) GetRequiredDocumentTypes(ctx context.Context) ([]*DocumentType, error) {
	return s.repo.GetRequiredDocumentTypes(ctx)
}

// ========================================
// DOCUMENT UPLOAD
// ========================================

// UploadDocument uploads a new document for a driver
func (s *Service) UploadDocument(ctx context.Context, driverID uuid.UUID, req *UploadDocumentRequest, reader io.Reader, fileSize int64, fileName, contentType string) (*UploadDocumentResponse, error) {
	// Validate file size
	maxSize := int64(s.config.MaxFileSizeMB) * 1024 * 1024
	if fileSize > maxSize {
		return nil, common.NewBadRequestError(fmt.Sprintf("file size exceeds maximum of %d MB", s.config.MaxFileSizeMB), nil)
	}

	// Validate mime type
	if !storage.ValidateMimeType(contentType, s.config.AllowedMimeTypes) {
		return nil, common.NewBadRequestError("unsupported file type", nil)
	}

	// Get document type
	docType, err := s.repo.GetDocumentTypeByCode(ctx, req.DocumentTypeCode)
	if err != nil {
		return nil, common.NewBadRequestError("invalid document type", err)
	}

	// Check if there's an existing document of this type that needs to be superseded
	existing, _ := s.repo.GetLatestDocumentByType(ctx, driverID, docType.ID)
	version := 1
	var previousDocID *uuid.UUID
	if existing != nil && existing.Status != StatusRejected && existing.Status != StatusExpired {
		// Supersede the existing document
		if err := s.repo.SupersedeDocument(ctx, existing.ID); err != nil {
			logger.Warn("Failed to supersede existing document", zap.Error(err))
		}
		version = existing.Version + 1
		previousDocID = &existing.ID

		// Log history
		s.logHistory(ctx, existing.ID, "superseded", string(existing.Status), string(StatusSuperseded), nil, false, "New document uploaded")
	}

	// Generate storage key
	fileKey := storage.GenerateDocumentKey(driverID, req.DocumentTypeCode, fileName)

	// Upload to storage
	uploadResult, err := s.storage.Upload(ctx, fileKey, reader, fileSize, contentType)
	if err != nil {
		logger.Error("Failed to upload document to storage", zap.Error(err))
		return nil, common.NewInternalServerError("failed to upload document")
	}

	// Create document record
	doc := &DriverDocument{
		ID:                 uuid.New(),
		DriverID:           driverID,
		DocumentTypeID:     docType.ID,
		Status:             StatusPending,
		FileURL:            uploadResult.URL,
		FileKey:            uploadResult.Key,
		FileName:           fileName,
		FileSizeBytes:      &fileSize,
		FileMimeType:       &contentType,
		DocumentNumber:     nilIfEmpty(req.DocumentNumber),
		IssueDate:          req.IssueDate,
		ExpiryDate:         req.ExpiryDate,
		IssuingAuthority:   nilIfEmpty(req.IssuingAuthority),
		Version:            version,
		PreviousDocumentID: previousDocID,
		SubmittedAt:        time.Now(),
	}

	if err := s.repo.CreateDocument(ctx, doc); err != nil {
		// Cleanup uploaded file on failure
		_ = s.storage.Delete(ctx, fileKey)
		return nil, common.NewInternalServerError("failed to save document")
	}

	// Log history
	s.logHistory(ctx, doc.ID, "submitted", "", string(StatusPending), nil, false, nil)

	// Schedule OCR if enabled for this document type
	ocrScheduled := false
	if s.config.OCREnabled && docType.AutoOCREnabled {
		if err := s.scheduleOCR(ctx, doc.ID, 0); err != nil {
			logger.Warn("Failed to schedule OCR", zap.Error(err))
		} else {
			ocrScheduled = true
		}
	}

	return &UploadDocumentResponse{
		DocumentID:   doc.ID,
		Status:       doc.Status,
		FileURL:      doc.FileURL,
		Message:      "Document uploaded successfully",
		OCRScheduled: ocrScheduled,
	}, nil
}

// UploadDocumentBackSide uploads the back side of a document
func (s *Service) UploadDocumentBackSide(ctx context.Context, documentID uuid.UUID, reader io.Reader, fileSize int64, fileName, contentType string) error {
	doc, err := s.repo.GetDocument(ctx, documentID)
	if err != nil {
		return common.NewNotFoundError("document not found", err)
	}

	if !doc.DocumentType.RequiresFrontBack {
		return common.NewBadRequestError("this document type does not require a back side", nil)
	}

	// Validate file
	maxSize := int64(s.config.MaxFileSizeMB) * 1024 * 1024
	if fileSize > maxSize {
		return common.NewBadRequestError(fmt.Sprintf("file size exceeds maximum of %d MB", s.config.MaxFileSizeMB), nil)
	}

	if !storage.ValidateMimeType(contentType, s.config.AllowedMimeTypes) {
		return common.NewBadRequestError("unsupported file type", nil)
	}

	// Generate storage key for back side
	fileKey := storage.GenerateDocumentKey(doc.DriverID, doc.DocumentType.Code+"_back", fileName)

	// Upload to storage
	uploadResult, err := s.storage.Upload(ctx, fileKey, reader, fileSize, contentType)
	if err != nil {
		return common.NewInternalServerError("failed to upload document")
	}

	// Update document with back file
	if err := s.repo.UpdateDocumentBackFile(ctx, documentID, uploadResult.URL, uploadResult.Key); err != nil {
		_ = s.storage.Delete(ctx, fileKey)
		return common.NewInternalServerError("failed to update document")
	}

	return nil
}

// GetPresignedUploadURL generates a presigned URL for direct upload
func (s *Service) GetPresignedUploadURL(ctx context.Context, driverID uuid.UUID, req *PresignedUploadRequest) (*PresignedUploadResponse, error) {
	// Validate document type
	docType, err := s.repo.GetDocumentTypeByCode(ctx, req.DocumentTypeCode)
	if err != nil {
		return nil, common.NewBadRequestError("invalid document type", err)
	}

	// Validate content type
	if !storage.ValidateMimeType(req.ContentType, s.config.AllowedMimeTypes) {
		return nil, common.NewBadRequestError("unsupported file type", nil)
	}

	// Generate file key
	suffix := ""
	if !req.IsFrontSide && docType.RequiresFrontBack {
		suffix = "_back"
	}
	fileKey := storage.GenerateDocumentKey(driverID, req.DocumentTypeCode+suffix, req.FileName)

	// Get presigned URL
	presigned, err := s.storage.GetPresignedUploadURL(ctx, fileKey, req.ContentType, 15*time.Minute)
	if err != nil {
		return nil, common.NewInternalServerError("failed to generate upload URL")
	}

	return &PresignedUploadResponse{
		UploadURL:   presigned.URL,
		Method:      presigned.Method,
		Headers:     presigned.Headers,
		ExpiresAt:   presigned.ExpiresAt,
		FileKey:     fileKey,
		CallbackURL: fmt.Sprintf("/api/v1/documents/upload-complete"),
	}, nil
}

// CompleteDirectUpload completes the document creation after direct upload
func (s *Service) CompleteDirectUpload(ctx context.Context, driverID uuid.UUID, req *UploadCompleteRequest) (*UploadDocumentResponse, error) {
	// Verify file exists in storage
	exists, err := s.storage.Exists(ctx, req.FileKey)
	if err != nil || !exists {
		return nil, common.NewBadRequestError("uploaded file not found", nil)
	}

	// Get document type
	docType, err := s.repo.GetDocumentTypeByCode(ctx, req.DocumentTypeCode)
	if err != nil {
		return nil, common.NewBadRequestError("invalid document type", err)
	}

	// If this is a back side upload
	if !req.IsFrontSide && docType.RequiresFrontBack {
		// Find the existing front document and update it
		existing, err := s.repo.GetLatestDocumentByType(ctx, driverID, docType.ID)
		if err != nil {
			return nil, common.NewBadRequestError("front side document not found", err)
		}

		if err := s.repo.UpdateDocumentBackFile(ctx, existing.ID, s.storage.GetURL(req.FileKey), req.FileKey); err != nil {
			return nil, common.NewInternalServerError("failed to update document")
		}

		return &UploadDocumentResponse{
			DocumentID: existing.ID,
			Status:     existing.Status,
			FileURL:    existing.FileURL,
			Message:    "Back side uploaded successfully",
		}, nil
	}

	// Handle front side / regular document
	existing, _ := s.repo.GetLatestDocumentByType(ctx, driverID, docType.ID)
	version := 1
	var previousDocID *uuid.UUID
	if existing != nil && existing.Status != StatusRejected && existing.Status != StatusExpired {
		if err := s.repo.SupersedeDocument(ctx, existing.ID); err != nil {
			logger.Warn("Failed to supersede existing document", zap.Error(err))
		}
		version = existing.Version + 1
		previousDocID = &existing.ID
	}

	doc := &DriverDocument{
		ID:                 uuid.New(),
		DriverID:           driverID,
		DocumentTypeID:     docType.ID,
		Status:             StatusPending,
		FileURL:            s.storage.GetURL(req.FileKey),
		FileKey:            req.FileKey,
		FileName:           req.FileKey,
		DocumentNumber:     nilIfEmpty(req.DocumentNumber),
		IssueDate:          req.IssueDate,
		ExpiryDate:         req.ExpiryDate,
		IssuingAuthority:   nilIfEmpty(req.IssuingAuthority),
		Version:            version,
		PreviousDocumentID: previousDocID,
		SubmittedAt:        time.Now(),
	}

	if err := s.repo.CreateDocument(ctx, doc); err != nil {
		return nil, common.NewInternalServerError("failed to save document")
	}

	s.logHistory(ctx, doc.ID, "submitted", "", string(StatusPending), nil, false, nil)

	// Schedule OCR
	ocrScheduled := false
	if s.config.OCREnabled && docType.AutoOCREnabled {
		if err := s.scheduleOCR(ctx, doc.ID, 0); err != nil {
			logger.Warn("Failed to schedule OCR", zap.Error(err))
		} else {
			ocrScheduled = true
		}
	}

	return &UploadDocumentResponse{
		DocumentID:   doc.ID,
		Status:       doc.Status,
		FileURL:      doc.FileURL,
		Message:      "Document uploaded successfully",
		OCRScheduled: ocrScheduled,
	}, nil
}

// ========================================
// DOCUMENT RETRIEVAL
// ========================================

// GetDocument gets a document by ID
func (s *Service) GetDocument(ctx context.Context, documentID uuid.UUID) (*DriverDocument, error) {
	return s.repo.GetDocument(ctx, documentID)
}

// GetDriverDocuments gets all documents for a driver
func (s *Service) GetDriverDocuments(ctx context.Context, driverID uuid.UUID) ([]*DriverDocument, error) {
	return s.repo.GetDriverDocuments(ctx, driverID)
}

// GetDriverVerificationStatus gets the overall verification status for a driver
func (s *Service) GetDriverVerificationStatus(ctx context.Context, driverID uuid.UUID) (*VerificationStatusResponse, error) {
	// Get required document types
	requiredTypes, err := s.repo.GetRequiredDocumentTypes(ctx)
	if err != nil {
		return nil, common.NewInternalServerError("failed to get document types")
	}

	// Get driver's documents
	documents, err := s.repo.GetDriverDocuments(ctx, driverID)
	if err != nil {
		return nil, common.NewInternalServerError("failed to get documents")
	}

	// Get verification status
	verificationStatus, _ := s.repo.GetDriverVerificationStatus(ctx, driverID)

	// Map documents by type
	docByType := make(map[uuid.UUID]*DriverDocument)
	for _, doc := range documents {
		if existing, ok := docByType[doc.DocumentTypeID]; !ok || doc.SubmittedAt.After(existing.SubmittedAt) {
			docByType[doc.DocumentTypeID] = doc
		}
	}

	// Build requirements response
	var requirements []*DocumentRequirement
	var missingDocs []string
	var nextExpiry *time.Time
	approvedCount := 0
	canDrive := true

	for _, dt := range requiredTypes {
		req := &DocumentRequirement{
			DocumentType: dt,
		}

		if doc, ok := docByType[dt.ID]; ok {
			req.Document = doc
			switch doc.Status {
			case StatusApproved:
				req.Status = "approved"
				approvedCount++
				if dt.RequiresExpiry && doc.ExpiryDate != nil {
					if nextExpiry == nil || doc.ExpiryDate.Before(*nextExpiry) {
						nextExpiry = doc.ExpiryDate
					}
				}
			case StatusPending, StatusUnderReview:
				req.Status = string(doc.Status)
				canDrive = false
			case StatusRejected:
				req.Status = "rejected"
				canDrive = false
			case StatusExpired:
				req.Status = "expired"
				canDrive = false
			default:
				req.Status = "pending"
				canDrive = false
			}
		} else {
			req.Status = "not_submitted"
			missingDocs = append(missingDocs, dt.Name)
			canDrive = false
		}

		requirements = append(requirements, req)
	}

	// Determine overall status
	status := VerificationIncomplete
	message := "Please submit all required documents"

	if verificationStatus != nil {
		status = verificationStatus.VerificationStatus
		if status == VerificationSuspended {
			canDrive = false
			message = "Your account has been suspended"
			if verificationStatus.SuspensionReason != nil {
				message += ": " + *verificationStatus.SuspensionReason
			}
		}
	}

	if approvedCount == len(requiredTypes) {
		status = VerificationApproved
		message = "Your verification is complete"
		canDrive = true
	} else if approvedCount > 0 {
		status = VerificationPendingReview
		message = fmt.Sprintf("%d of %d documents approved", approvedCount, len(requiredTypes))
	}

	if len(missingDocs) > 0 {
		message = fmt.Sprintf("Missing documents: %d", len(missingDocs))
	}

	return &VerificationStatusResponse{
		Status:             status,
		RequiredDocuments:  requirements,
		SubmittedDocuments: documents,
		MissingDocuments:   missingDocs,
		NextExpiry:         nextExpiry,
		CanDrive:           canDrive,
		Message:            message,
	}, nil
}

// ========================================
// DOCUMENT REVIEW (ADMIN)
// ========================================

// ReviewDocument reviews a document (approve/reject)
func (s *Service) ReviewDocument(ctx context.Context, documentID uuid.UUID, reviewerID uuid.UUID, req *ReviewDocumentRequest) error {
	doc, err := s.repo.GetDocument(ctx, documentID)
	if err != nil {
		return common.NewNotFoundError("document not found", err)
	}

	if doc.Status != StatusPending && doc.Status != StatusUnderReview {
		return common.NewBadRequestError("document is not pending review", nil)
	}

	previousStatus := string(doc.Status)
	var newStatus DocumentStatus
	var rejectionReason *string

	switch req.Action {
	case "approve":
		newStatus = StatusApproved

		// Update document details if provided
		if req.DocumentNumber != nil || req.ExpiryDate != nil {
			var expiryDate *time.Time
			if req.ExpiryDate != nil {
				t, err := time.Parse("2006-01-02", *req.ExpiryDate)
				if err == nil {
					expiryDate = &t
				}
			}
			_ = s.repo.UpdateDocumentDetails(ctx, documentID, req.DocumentNumber, nil, expiryDate, nil)
		}

	case "reject":
		newStatus = StatusRejected
		if req.RejectionReason == "" {
			return common.NewBadRequestError("rejection reason is required", nil)
		}
		rejectionReason = &req.RejectionReason

	case "request_resubmit":
		newStatus = StatusRejected
		reason := "Document needs to be resubmitted"
		if req.RejectionReason != "" {
			reason = req.RejectionReason
		}
		rejectionReason = &reason

	default:
		return common.NewBadRequestError("invalid action", nil)
	}

	notes := nilIfEmpty(req.Notes)

	if err := s.repo.UpdateDocumentStatus(ctx, documentID, newStatus, &reviewerID, notes, rejectionReason); err != nil {
		return common.NewInternalServerError("failed to update document")
	}

	// Log history
	s.logHistory(ctx, documentID, req.Action, previousStatus, string(newStatus), &reviewerID, false, notes)

	logger.Info("Document reviewed",
		zap.String("document_id", documentID.String()),
		zap.String("action", req.Action),
		zap.String("reviewer_id", reviewerID.String()),
	)

	return nil
}

// GetPendingReviews gets documents pending review
func (s *Service) GetPendingReviews(ctx context.Context, limit, offset int) ([]*PendingReviewDocument, int, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.GetPendingReviews(ctx, limit, offset)
}

// GetExpiringDocuments gets documents expiring soon
func (s *Service) GetExpiringDocuments(ctx context.Context, daysAhead int) ([]*ExpiringDocument, error) {
	if daysAhead < 1 {
		daysAhead = 30
	}
	return s.repo.GetExpiringDocuments(ctx, daysAhead)
}

type expirationProcessor interface {
	ProcessDocumentExpirations(context.Context) (int64, error)
}

func (s *Service) ProcessDocumentExpirations(ctx context.Context) (int64, error) {
	repo, ok := s.repo.(expirationProcessor)
	if !ok {
		return 0, common.NewInternalServerError("document expiration processor is not configured")
	}
	return repo.ProcessDocumentExpirations(ctx)
}

// StartReview marks a document as under review
func (s *Service) StartReview(ctx context.Context, documentID uuid.UUID, reviewerID uuid.UUID) error {
	doc, err := s.repo.GetDocument(ctx, documentID)
	if err != nil {
		return common.NewNotFoundError("document not found", err)
	}

	if doc.Status != StatusPending {
		return common.NewBadRequestError("document is not pending", nil)
	}

	if err := s.repo.UpdateDocumentStatus(ctx, documentID, StatusUnderReview, &reviewerID, nil, nil); err != nil {
		return common.NewInternalServerError("failed to update document")
	}

	s.logHistory(ctx, documentID, "review_started", string(StatusPending), string(StatusUnderReview), &reviewerID, false, nil)

	return nil
}

// ========================================
// OCR
// ========================================

// scheduleOCR schedules an OCR job for a document
func (s *Service) scheduleOCR(ctx context.Context, documentID uuid.UUID, priority int) error {
	job := &OCRProcessingQueue{
		ID:         uuid.New(),
		DocumentID: documentID,
		Status:     "pending",
		Priority:   priority,
		MaxRetries: 3,
	}

	return s.repo.CreateOCRJob(ctx, job)
}

// ProcessOCRResult processes the result of OCR and updates the document
func (s *Service) ProcessOCRResult(ctx context.Context, documentID uuid.UUID, result *OCRResult) error {
	ocrData := map[string]interface{}{
		"document_number":   result.DocumentNumber,
		"full_name":         result.FullName,
		"date_of_birth":     result.DateOfBirth,
		"issue_date":        result.IssueDate,
		"expiry_date":       result.ExpiryDate,
		"issuing_authority": result.IssuingAuthority,
		"address":           result.Address,
		"vehicle_plate":     result.VehiclePlate,
		"vehicle_vin":       result.VehicleVIN,
		"raw_text":          result.RawText,
		"metadata":          result.Metadata,
	}

	if err := s.repo.UpdateDocumentOCRData(ctx, documentID, ocrData, result.Confidence); err != nil {
		return err
	}

	// Update document details from OCR
	docNum := nilIfEmpty(result.DocumentNumber)
	authority := nilIfEmpty(result.IssuingAuthority)
	if err := s.repo.UpdateDocumentDetails(ctx, documentID, docNum, result.IssueDate, result.ExpiryDate, authority); err != nil {
		logger.Warn("Failed to update document details from OCR", zap.Error(err))
	}

	s.logHistory(ctx, documentID, "ocr_processed", "", "", nil, true, nil)

	return nil
}

// ========================================
// HELPER METHODS
// ========================================

func (s *Service) logHistory(ctx context.Context, documentID uuid.UUID, action, prevStatus, newStatus string, performedBy *uuid.UUID, isSystem bool, notes interface{}) {
	var notesStr *string
	if notes != nil {
		if str, ok := notes.(string); ok {
			notesStr = &str
		} else if strPtr, ok := notes.(*string); ok {
			notesStr = strPtr
		}
	}

	var prevPtr, newPtr *string
	if prevStatus != "" {
		prevPtr = &prevStatus
	}
	if newStatus != "" {
		newPtr = &newStatus
	}

	history := &DocumentVerificationHistory{
		ID:             uuid.New(),
		DocumentID:     documentID,
		Action:         action,
		PreviousStatus: prevPtr,
		NewStatus:      newPtr,
		PerformedBy:    performedBy,
		IsSystemAction: isSystem,
		Notes:          notesStr,
	}

	if err := s.repo.CreateHistory(ctx, history); err != nil {
		logger.Warn("Failed to create history entry", zap.Error(err))
	}
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
