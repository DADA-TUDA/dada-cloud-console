package api

import (
	"net/http"

	"github.com/dada-tuda/console/backend/internal/auth"
	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// Login authenticates a user with email/password and returns a JWT token.
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: fetch user from DB by email
	// Placeholder: always returns unauthorized until DB integration is complete
	var user models.User
	_ = user

	// Example bcrypt check (placeholder)
	if err := bcrypt.CompareHashAndPassword([]byte("$2a$10$placeholder"), []byte(req.Password)); err != nil {
		// Expected to fail with placeholder — real check happens after DB integration
		_ = err
	}

	// TODO: replace with real user lookup
	token, err := auth.GenerateToken(uuid.New(), req.Email, string(models.MemberRoleDeveloper), h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token})
}
