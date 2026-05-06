package api

import (
	"net/http"

	"github.com/dada-tuda/console/backend/internal/auth"
	"github.com/dada-tuda/console/backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type loginRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password" binding:"required"`
}

// Login authenticates a user with username/email + password and returns a JWT token.
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Accept either username or email as the identifier
	identifier := req.Username
	if identifier == "" {
		identifier = req.Email
	}
	if identifier == "" {
		respondError(c, http.StatusBadRequest, "username or email is required")
		return
	}

	var user models.User
	err := h.pool.QueryRow(c.Request.Context(),
		"SELECT id, username, email, password_hash, display_name FROM users WHERE username = $1 OR email = $1",
		identifier,
	).Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.DisplayName)
	if err == pgx.ErrNoRows {
		respondError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "internal error")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		respondError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, user.Email, user.DisplayName, h.cfg.JWTSecret)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not generate token")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":           user.ID,
			"username":     user.Username,
			"email":        user.Email,
			"display_name": user.DisplayName,
		},
	})
}

// Me returns the currently authenticated user's info from JWT claims.
func (h *Handler) Me(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		respondUnauthorized(c)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":           claims.UserID,
			"username":     claims.Username,
			"email":        claims.Email,
			"display_name": claims.DisplayName,
		},
	})
}
